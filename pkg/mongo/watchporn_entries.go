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

// watchporn_entries is the durable home for every watchporn.to scene. Mirrors
// yesporn_entries (multi-key Studios, rotating-token mp4 streams resolved live):
// one doc per scene (_id = "wpt:" + videoID), flat store. Stream qualities (one
// mp4 per video_url/video_alt_url[N]) are resolved live from the detail page by
// the scraper's stream resolver; the /get_file v-acctoken rotates per request so
// it is never stored. Populated by a Mac-side launchd cron (watchporn.to is
// TLS-blocked from prod); the deployed backend reads Mongo + Redis only.

func watchpornEntryDocID(id string) string { return "wpt:" + id }

// UpsertWatchpornEntry stores a scene's card fields. Enrichment-owned fields
// (date/duration/poster/studios/studios_norm/tags/tags_norm/performers/
// performers_norm/description/detail_scraped) are $setOnInsert only, so the ingest
// sweep re-walking /latest-updates/1/ on a cursor reset does NOT clobber a prior
// detail scrape - which would wipe `date` (the wpt_recent sort key) and the
// category/model/tag lists, and would replace the enriched HH:MM:SS duration with
// the card's MM:SS placeholder. The enrich sweep persists those via
// UpdateWatchpornEnrichment. Card fields (video_id/slug/title/detail_url) are $set so
// a full listing walk refreshes them; poster is $setOnInsert so a re-walk's smaller
// card image does not overwrite the og:image set at enrich.
func (c *Client) UpsertWatchpornEntry(ctx context.Context, e models.WatchpornEntry) error {
	if e.VideoID == "" {
		return nil
	}
	filter := bson.M{"_id": watchpornEntryDocID(e.VideoID)}
	_, err := c.coll("watchporn_entries").UpdateOne(ctx, filter, watchpornUpsertUpdate(e), options.Update().SetUpsert(true))
	return err
}

// UpsertWatchpornEntries batches card-field upserts via BulkWrite (500 per batch,
// unordered) so the Mac->prod-WAN ingest sweep finishes in minutes, not the hours a
// sequential per-entry UpdateOne would take - the same pattern pornrips uses
// (BackfillPornripsSceneGroup). Mirrors UpsertWatchpornEntry's $set/$setOnInsert
// contract per doc. Errors are logged and skipped (SetOrdered(false) continues past
// one bad doc), matching the per-entry path's err-ignored tolerance. Returns the
// count of docs written (upserted+matched). Empty/empty-VideoID input is a no-op.
func (c *Client) UpsertWatchpornEntries(ctx context.Context, entries []models.WatchpornEntry) (int, error) {
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
			SetFilter(bson.M{"_id": watchpornEntryDocID(e.VideoID)}).
			SetUpdate(watchpornUpsertUpdate(e)).
			SetUpsert(true))
	}
	if len(wms) == 0 {
		return 0, nil
	}
	written := 0
	for i := 0; i < len(wms); i += batchSize {
		j := i + batchSize
		if j > len(wms) {
			j = len(wms)
		}
		res, werr := c.coll("watchporn_entries").BulkWrite(ctx, wms[i:j], options.BulkWrite().SetOrdered(false))
		if werr != nil {
			log.Printf("watchporn bulk upsert: %v", werr)
		}
		if res != nil {
			written += int(res.UpsertedCount + res.MatchedCount)
		}
	}
	return written, nil
}

func watchpornUpsertUpdate(e models.WatchpornEntry) bson.M {
	return bson.M{
		"$set": bson.M{
			"video_id":   e.VideoID,
			"slug":       e.Slug,
			"title":      e.Title,
			"detail_url": e.DetailURL,
			"has_4k":     e.Has4K,
			"website":    "watchporn",
			"updated_at": nowSec(),
		},
		"$setOnInsert": bson.M{
			"date":            e.Date,
			"duration":        e.Duration,
			"poster":          e.Poster,
			"excerpt":         e.Excerpt,
			"studios":         nonNil(e.Studios),
			"studios_norm":    normSlice(e.Studios),
			"tags":            nonNil(e.Tags),
			"tags_norm":       normSlice(e.Tags),
			"performers":      nonNil(e.Performers),
			"performers_norm": normSlice(e.Performers),
			"description":     e.Description,
			"detail_scraped":  e.DetailScraped,
		},
	}
}

// UpdateWatchpornEnrichment writes the detail-scraped fields (og: release_date,
// duration seconds -> HH:MM:SS, og:image poster, /categories/ links -> Studios,
// /models/ links -> Performers, /tags/ links -> Tags, description, detail_scraped)
// for an entry. Called by the enrich sweep after EnrichEntry succeeds (and on a
// permanently-gone page, where EnrichEntry sets DetailScraped=true and returns
// nil). date is the wpt_recent sort key, so keeping it out of the ingest $set is
// what stops re-walks from disordering the feed.
func (c *Client) UpdateWatchpornEnrichment(ctx context.Context, e models.WatchpornEntry) error {
	if e.VideoID == "" {
		return nil
	}
	_, err := c.coll("watchporn_entries").UpdateOne(ctx, bson.M{"_id": watchpornEntryDocID(e.VideoID)}, watchpornEnrichmentUpdate(e))
	return err
}

func watchpornEnrichmentUpdate(e models.WatchpornEntry) bson.M {
	return bson.M{"$set": bson.M{
		"date":            e.Date,
		"poster":          e.Poster,
		"duration":        e.Duration,
		"studios":         nonNil(e.Studios),
		"studios_norm":    normSlice(e.Studios),
		"tags":            nonNil(e.Tags),
		"tags_norm":       normSlice(e.Tags),
		"performers":      nonNil(e.Performers),
		"performers_norm": normSlice(e.Performers),
		"description":     e.Description,
		"detail_scraped":  e.DetailScraped,
		"updated_at":      nowSec(),
	}}
}

// GetWatchpornRecent returns entries newest-first by date (the wpt_recent feed).
func (c *Client) GetWatchpornRecent(ctx context.Context, skip, limit int) ([]models.WatchpornEntry, error) {
	return c.findWatchporn(ctx, bson.M{}, skip, limit)
}

// GetWatchpornByStudio returns entries whose studios_norm multikey includes
// studioNorm. An empty studioNorm falls back to Recent, matching the tag/performer
// paths - without this the $in: [""] query would match zero docs.
func (c *Client) GetWatchpornByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.WatchpornEntry, error) {
	if studioNorm == "" {
		return c.findWatchporn(ctx, bson.M{}, skip, limit)
	}
	return c.findWatchporn(ctx, bson.M{"studios_norm": studioNorm}, skip, limit)
}

// GetWatchpornByTag returns entries containing any of the normalized tags. An
// empty or all-empty tagsNorm (the "All" genre) falls back to Recent.
func (c *Client) GetWatchpornByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.WatchpornEntry, error) {
	nonEmpty := filterNonEmpty(tagsNorm)
	if len(nonEmpty) == 0 {
		return c.findWatchporn(ctx, bson.M{}, skip, limit)
	}
	return c.findWatchporn(ctx, bson.M{"tags_norm": bson.M{"$in": nonEmpty}}, skip, limit)
}

// GetWatchpornByPerformer returns entries whose performers_norm includes performerNorm.
func (c *Client) GetWatchpornByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.WatchpornEntry, error) {
	if performerNorm == "" {
		return c.findWatchporn(ctx, bson.M{}, skip, limit)
	}
	return c.findWatchporn(ctx, bson.M{"performers_norm": performerNorm}, skip, limit)
}

// SearchWatchporn returns entries whose title/performers/studios match query.
func (c *Client) SearchWatchporn(ctx context.Context, query string, skip, limit int) ([]models.WatchpornEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	esc := escapeRegex(q)
	return c.findWatchporn(ctx, bson.M{"$or": []bson.M{
		{"title": bson.M{"$regex": esc, "$options": "i"}},
		{"performers": bson.M{"$regex": esc, "$options": "i"}},
		{"studios": bson.M{"$regex": esc, "$options": "i"}},
	}}, skip, limit)
}

// GetWatchpornEntry returns one entry by videoID, or nil.
func (c *Client) GetWatchpornEntry(ctx context.Context, videoID string) (*models.WatchpornEntry, error) {
	if videoID == "" {
		return nil, nil
	}
	res := c.coll("watchporn_entries").FindOne(ctx, bson.M{"_id": watchpornEntryDocID(videoID)})
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
	e := mapWatchpornEntry(doc)
	return &e, nil
}

// GetWatchpornMissingDetail returns entries not yet detail-scraped, newest first.
func (c *Client) GetWatchpornMissingDetail(ctx context.Context, limit int) ([]models.WatchpornEntry, error) {
	return c.findWatchporn(ctx, bson.M{"detail_scraped": bson.M{"$ne": true}}, 0, limit)
}

// WatchpornEntriesCount is the total entry count for monitoring.
func (c *Client) WatchpornEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("watchporn_entries").CountDocuments(ctx, bson.M{})
}

// topValuesWatchporn returns the most common non-empty display values desc by
// count. Used for studio/tag/performer discover options so the dropdown mirrors
// the store. studios/tags/performers are all multikey arrays.
func (c *Client) topValuesWatchporn(ctx context.Context, arrayField string, limit int) ([]string, error) {
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
	cur, err := c.coll("watchporn_entries").Aggregate(ctx, pipeline)
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

// GetWatchpornTopStudios / TopTags / TopPerformers feed the manifest genre
// dropdowns for wpt_studio / wpt_tag / wpt_performer.
func (c *Client) GetWatchpornTopStudios(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesWatchporn(ctx, "studios", limit)
}
func (c *Client) GetWatchpornTopTags(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesWatchporn(ctx, "tags", limit)
}
func (c *Client) GetWatchpornTopPerformers(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesWatchporn(ctx, "performers", limit)
}

// findWatchporn runs a find with date:-1 sort (these sources have no rating/alpha
// browse).
func (c *Client) findWatchporn(ctx context.Context, filter bson.M, skip, limit int) ([]models.WatchpornEntry, error) {
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
	cur, err := c.coll("watchporn_entries").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.WatchpornEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapWatchpornEntry(doc))
	}
	return out, cur.Err()
}

func mapWatchpornEntry(doc bson.M) models.WatchpornEntry {
	return models.WatchpornEntry{
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