package mongo

import (
	"context"
	"log"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// yesporn_entries is the durable home for every yesporn.vip scene. Mirrors
// perverzija_entries (multi-key Studios) + freepornvideos_entries (rotating-token
// mp4 streams resolved live): one doc per scene (_id = "ypv:" + videoID), flat
// store. Stream qualities (one mp4 per video_url/video_alt_url[N]) are resolved
// live from the detail page by the scraper's stream resolver; the /get_file
// token rotates per request so it is never stored.

func yespornEntryDocID(id string) string { return "ypv:" + id }

// UpsertYespornEntry stores a scene's card fields. Enrichment-owned fields
// (date/duration/poster/studios/studios_norm/tags/tags_norm/performers/
// performers_norm/description/detail_scraped) are $setOnInsert only, so the ingest
// sweep re-walking /latest-updates/1/ on a cursor reset does NOT clobber a prior
// detail scrape - which would wipe `date` (the ypv_recent sort key) and the channel/
// category/model lists, and would replace the enriched HH:MM:SS duration with the
// card's MM:SS placeholder. The enrich sweep persists those via
// UpdateYespornEnrichment. Card fields (video_id/slug/title/detail_url) are $set so
// a full listing walk refreshes them; poster is $setOnInsert so a re-walk's smaller
// card image does not overwrite the og:image set at enrich.
func (c *Client) UpsertYespornEntry(ctx context.Context, e models.YespornEntry) error {
	if e.VideoID == "" {
		return nil
	}
	filter := bson.M{"_id": yespornEntryDocID(e.VideoID)}
	_, err := c.coll("yesporn_entries").UpdateOne(ctx, filter, yespornUpsertUpdate(e), options.Update().SetUpsert(true))
	return err
}

// UpsertYespornEntries batches card-field upserts via BulkWrite (500 per batch,
// unordered) so the Mac->prod-WAN one-time fill finishes in minutes, not the hours a
// sequential per-entry UpdateOne would take - the same pattern pornrips/watchporn
// use. Mirrors UpsertYespornEntry's $set/$setOnInsert contract per doc. Errors are
// logged and skipped (SetOrdered(false) continues past one bad doc), matching the
// per-entry path's err-ignored tolerance. Empty/empty-VideoID input is a no-op.
//
// Returns NEW inserts only (UpsertedCount), NOT total docs touched. yesporn.vip's
// /latest-updates/{N}/ returns a 20-card page for ANY page number, including
// out-of-range ones (no 404, no empty page), so the forward ingest walk never sees
// an empty page to use as end-of-feed. The ingest loop instead treats "0 new
// inserts in a tick" (every card already in the store) as the archive-fully-covered
// signal and resets the cursor - that requires the caller to see UpsertedCount,
// not the matched count (which is non-zero forever once the archive is covered).
// Both the Mac one-time bulk fill (WAN-bound) and the prod sync tick (LAN) call
// this; UpsertWatchpornEntries differs (returns total touched) because watchporn.to
// DOES 404 on out-of-range pages, so its walk ends naturally and it does not need a
// dedup-based end signal.
func (c *Client) UpsertYespornEntries(ctx context.Context, entries []models.YespornEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	const batchSize = 500
	wms := make([]mongo.WriteModel, 0, len(entries))
	for _, e := range entries {
		if e.VideoID == "" {
			continue
		}
		wms = append(wms, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": yespornEntryDocID(e.VideoID)}).
			SetUpdate(yespornUpsertUpdate(e)).
			SetUpsert(true))
	}
	if len(wms) == 0 {
		return 0, nil
	}
	inserts := 0
	var totalFailErr error // first error from a batch that returned res==nil (total failure)
	for i := 0; i < len(wms); i += batchSize {
		j := i + batchSize
		if j > len(wms) {
			j = len(wms)
		}
		res, werr := c.coll("yesporn_entries").BulkWrite(ctx, wms[i:j], options.BulkWrite().SetOrdered(false))
		if werr != nil {
			log.Printf("yesporn bulk upsert: %v", werr)
			if res == nil && totalFailErr == nil {
				totalFailErr = werr
			}
		}
		if res != nil {
			inserts += int(res.UpsertedCount)
		}
	}
	// If every batch failed with res==nil (total Mongo outage / connection loss), return
	// the error so the ingest loop does NOT read 0-new as a false end-of-feed (a mid-bulk
	// blip would otherwise silently terminate the one-time fill with partial coverage).
	// A partial failure (some batches ok) returns the counted inserts + nil.
	if inserts == 0 && totalFailErr != nil {
		return 0, totalFailErr
	}
	return inserts, nil
}

func yespornUpsertUpdate(e models.YespornEntry) bson.M {
	return bson.M{
		"$set": bson.M{
			"video_id":   e.VideoID,
			"slug":       e.Slug,
			"title":      e.Title,
			"detail_url": e.DetailURL,
			"has_4k":     e.Has4K,
			"website":    "yesporn",
			"updated_at": nowSec(),
		},
		"$setOnInsert": bson.M{
			"date":            e.Date,
			"duration":        e.Duration,
			"poster":          e.Poster,
			"excerpt":          e.Excerpt,
			"studios":          nonNil(e.Studios),
			"studios_norm":     normSlice(e.Studios),
			"tags":             nonNil(e.Tags),
			"tags_norm":        normSlice(e.Tags),
			"performers":       nonNil(e.Performers),
			"performers_norm":  normSlice(e.Performers),
			"description":      e.Description,
			"detail_scraped":   e.DetailScraped,
		},
	}
}

// UpdateYespornEnrichment writes the detail-scraped fields (og: release_date,
// duration seconds -> HH:MM:SS, og:image poster, channel links -> Studios, JS
// config categories -> Tags, models -> Performers, description, detail_scraped)
// for an entry. Called by the enrich sweep after EnrichEntry succeeds (and on a
// permanently-gone page, where EnrichEntry sets DetailScraped=true and returns
// nil). date is the ypv_recent sort key, so keeping it out of the ingest $set is
// what stops re-walks from disordering the feed.
func (c *Client) UpdateYespornEnrichment(ctx context.Context, e models.YespornEntry) error {
	if e.VideoID == "" {
		return nil
	}
	_, err := c.coll("yesporn_entries").UpdateOne(ctx, bson.M{"_id": yespornEntryDocID(e.VideoID)}, yespornEnrichmentUpdate(e))
	return err
}

func yespornEnrichmentUpdate(e models.YespornEntry) bson.M {
	return bson.M{"$set": bson.M{
		"date":           e.Date,
		"poster":         e.Poster,
		"duration":       e.Duration,
		"studios":         nonNil(e.Studios),
		"studios_norm":   normSlice(e.Studios),
		"tags":            nonNil(e.Tags),
		"tags_norm":       normSlice(e.Tags),
		"performers":      nonNil(e.Performers),
		"performers_norm": normSlice(e.Performers),
		"description":     e.Description,
		"detail_scraped":  e.DetailScraped,
		"updated_at":      nowSec(),
	}}
}

// GetYespornRecent returns entries newest-first by date (the ypv_recent feed).
func (c *Client) GetYespornRecent(ctx context.Context, skip, limit int) ([]models.YespornEntry, error) {
	return c.findYesporn(ctx, bson.M{}, skip, limit)
}

// GetYespornByStudio returns entries whose studios_norm multikey includes
// studioNorm. An empty studioNorm falls back to Recent, matching the tag/performer
// paths - without this the $in: [""] query would match zero docs.
func (c *Client) GetYespornByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.YespornEntry, error) {
	if studioNorm == "" {
		return c.findYesporn(ctx, bson.M{}, skip, limit)
	}
	return c.findYesporn(ctx, bson.M{"studios_norm": studioNorm}, skip, limit)
}

// GetYespornByTag returns entries containing any of the normalized tags. An
// empty or all-empty tagsNorm (the "All" genre) falls back to Recent.
func (c *Client) GetYespornByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.YespornEntry, error) {
	nonEmpty := filterNonEmpty(tagsNorm)
	if len(nonEmpty) == 0 {
		return c.findYesporn(ctx, bson.M{}, skip, limit)
	}
	return c.findYesporn(ctx, bson.M{"tags_norm": bson.M{"$in": nonEmpty}}, skip, limit)
}

// GetYespornByPerformer returns entries whose performers_norm includes performerNorm.
func (c *Client) GetYespornByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.YespornEntry, error) {
	if performerNorm == "" {
		return c.findYesporn(ctx, bson.M{}, skip, limit)
	}
	return c.findYesporn(ctx, bson.M{"performers_norm": performerNorm}, skip, limit)
}

// SearchYesporn returns entries whose title/performers/studios match query.
func (c *Client) SearchYesporn(ctx context.Context, query string, skip, limit int) ([]models.YespornEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	esc := escapeRegex(q)
	return c.findYesporn(ctx, bson.M{"$or": []bson.M{
		{"title": bson.M{"$regex": esc, "$options": "i"}},
		{"performers": bson.M{"$regex": esc, "$options": "i"}},
		{"studios": bson.M{"$regex": esc, "$options": "i"}},
	}}, skip, limit)
}

// GetYespornEntry returns one entry by videoID, or nil.
func (c *Client) GetYespornEntry(ctx context.Context, videoID string) (*models.YespornEntry, error) {
	if videoID == "" {
		return nil, nil
	}
	res := c.coll("yesporn_entries").FindOne(ctx, bson.M{"_id": yespornEntryDocID(videoID)})
	if err := res.Err(); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	var doc bson.M
	if err := res.Decode(&doc); err != nil {
		return nil, err
	}
	e := mapYespornEntry(doc)
	return &e, nil
}

// GetYespornMissingDetail returns entries not yet detail-scraped, newest first.
func (c *Client) GetYespornMissingDetail(ctx context.Context, limit int) ([]models.YespornEntry, error) {
	return c.findYesporn(ctx, bson.M{"detail_scraped": bson.M{"$ne": true}}, 0, limit)
}

// YespornEntriesCount is the total entry count for monitoring.
func (c *Client) YespornEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("yesporn_entries").CountDocuments(ctx, bson.M{})
}

// topValuesYesporn returns the most common non-empty display values desc by
// count. Used for studio/tag/performer discover options so the dropdown mirrors
// the store. studios/tags/performers are all multikey arrays.
func (c *Client) topValuesYesporn(ctx context.Context, arrayField string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 60
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{arrayField: bson.M{"$nin": []interface{}{"", nil}}}}},
		bson.D{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$" + arrayField},
			{Key: "preserveNullAndEmptyArrays", Value: false},
		}}},
		bson.D{{Key: "$match", Value: bson.M{arrayField: bson.M{"$ne": ""}}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$" + arrayField},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "n", Value: -1}}}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	}
	cur, err := c.coll("yesporn_entries").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var doc struct {
			ID string `bson:"_id"`
		}
		if err := cur.Decode(&doc); err == nil && doc.ID != "" {
			out = append(out, doc.ID)
		}
	}
	return out, cur.Err()
}

// GetYespornTopStudios / TopTags / TopPerformers feed the manifest genre
// dropdowns for ypv_studio / ypv_tag / ypv_performer.
func (c *Client) GetYespornTopStudios(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesYesporn(ctx, "studios", limit)
}
func (c *Client) GetYespornTopTags(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesYesporn(ctx, "tags", limit)
}
func (c *Client) GetYespornTopPerformers(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesYesporn(ctx, "performers", limit)
}

// findYesporn runs a find with date:-1 sort (these sources have no rating/alpha
// browse).
func (c *Client) findYesporn(ctx context.Context, filter bson.M, skip, limit int) ([]models.YespornEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	if filter == nil {
		filter = bson.M{}
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))
	cur, err := c.coll("yesporn_entries").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.YespornEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapYespornEntry(doc))
	}
	return out, cur.Err()
}

func mapYespornEntry(doc bson.M) models.YespornEntry {
	return models.YespornEntry{
		VideoID:        strVal(doc, "video_id"),
		Slug:           strVal(doc, "slug"),
		Title:          strVal(doc, "title"),
		DetailURL:      strVal(doc, "detail_url"),
		Date:           strVal(doc, "date"),
		Excerpt:        strVal(doc, "excerpt"),
		Poster:         strVal(doc, "poster"),
		Studios:        stringSlice(doc, "studios"),
		StudiosNorm:    stringSlice(doc, "studios_norm"),
		Tags:           stringSlice(doc, "tags"),
		TagsNorm:       stringSlice(doc, "tags_norm"),
		Performers:     stringSlice(doc, "performers"),
		PerformersNorm: stringSlice(doc, "performers_norm"),
		Description:    strVal(doc, "description"),
		Duration:       strVal(doc, "duration"),
		Has4K:          boolVal(doc, "has_4k"),
		DetailScraped:  boolVal(doc, "detail_scraped"),
		UpdatedAt:      int64Val(doc, "updated_at"),
	}
}