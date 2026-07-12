package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// porneec_entries is the durable home for every porneec.com scene. Mirrors
// freepornvideos_entries (per-entry ingest, natural 404 end-of-feed) but with a
// tokenless Bunny CDN mp4 stored on the doc (no rotating token): one doc per
// scene (_id = "pec:" + slug), flat store. StreamURL is resolved once at enrich
// from the clean-tube-player iframe and emitted directly by ResolveStream, so
// the stream path never re-fetches the detail page.

func porneecEntryDocID(slug string) string { return "pec:" + slug }

// UpsertPorneecEntry stores a scene's card fields. Enrichment-owned fields
// (date/description/stream_url/detail_scraped) are $setOnInsert only, so the
// ingest sweep re-walking /page/1/ on a cursor reset does NOT clobber a prior
// detail scrape - which would wipe `date` (the pec_recent sort key) and the
// stored stream mp4. The enrich sweep persists those via UpdatePorneecEnrichment.
// Card fields (video_id/slug/title/detail_url/poster/duration/studios/
// performers + their _norm) are $set so a full listing walk refreshes them.
func (c *Client) UpsertPorneecEntry(ctx context.Context, e models.PorneecEntry) error {
	if e.Slug == "" {
		return nil
	}
	filter := bson.M{"_id": porneecEntryDocID(e.Slug)}
	_, err := c.coll("porneec_entries").UpdateOne(ctx, filter, porneecUpsertUpdate(e), options.Update().SetUpsert(true))
	return err
}

func porneecUpsertUpdate(e models.PorneecEntry) bson.M {
	return bson.M{
		"$set": bson.M{
			"video_id":        e.VideoID,
			"slug":            e.Slug,
			"title":           e.Title,
			"detail_url":      e.DetailURL,
			"poster":          e.Poster,
			"duration":        e.Duration,
			"studios":         nonNil(e.Studios),
			"studios_norm":    normSlice(e.Studios),
			"performers":      nonNil(e.Performers),
			"performers_norm": normSlice(e.Performers),
			"website":         "porneec",
			"updated_at":      nowSec(),
		},
		"$setOnInsert": bson.M{
			"date":           e.Date,
			"description":    e.Description,
			"stream_url":     e.StreamURL,
			"detail_scraped": e.DetailScraped,
		},
	}
}

// UpdatePorneecEnrichment writes the detail-scraped fields (article:published_time
// date, og:description, the tokenless Bunny CDN stream mp4, detail_scraped) for an
// entry. Called by the enrich sweep after EnrichEntry succeeds (and on a
// permanently-gone page, where EnrichEntry sets DetailScraped=true and returns
// nil). date is the pec_recent sort key, so keeping it out of the ingest $set is
// what stops re-walks from disordering the feed; stream_url is the playable URL,
// so keeping it out of the ingest $set stops a re-walk from blanking playback.
func (c *Client) UpdatePorneecEnrichment(ctx context.Context, e models.PorneecEntry) error {
	if e.Slug == "" {
		return nil
	}
	_, err := c.coll("porneec_entries").UpdateOne(ctx, bson.M{"_id": porneecEntryDocID(e.Slug)}, porneecEnrichmentUpdate(e))
	return err
}

func porneecEnrichmentUpdate(e models.PorneecEntry) bson.M {
	return bson.M{"$set": bson.M{
		"date":           e.Date,
		"description":    e.Description,
		"stream_url":     e.StreamURL,
		"detail_scraped": e.DetailScraped,
		"updated_at":     nowSec(),
	}}
}

// GetPorneecRecent returns entries newest-first by date (the pec_recent feed).
func (c *Client) GetPorneecRecent(ctx context.Context, skip, limit int) ([]models.PorneecEntry, error) {
	return c.findPorneec(ctx, bson.M{}, skip, limit)
}

// GetPorneecByStudio returns entries whose studios_norm multikey includes
// studioNorm. An empty studioNorm falls back to Recent, matching the tag/
// performer paths - without this the $in: [""] query would match zero docs.
func (c *Client) GetPorneecByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PorneecEntry, error) {
	if studioNorm == "" {
		return c.findPorneec(ctx, bson.M{}, skip, limit)
	}
	return c.findPorneec(ctx, bson.M{"studios_norm": studioNorm}, skip, limit)
}

// GetPorneecByTag returns entries containing any of the normalized tags. An
// empty or all-empty tagsNorm (the "All" genre) falls back to Recent. porneec
// tags are left empty (obfuscated slugs), so this always falls back to Recent;
// kept for catalog parity with the other tube sources.
func (c *Client) GetPorneecByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PorneecEntry, error) {
	nonEmpty := filterNonEmpty(tagsNorm)
	if len(nonEmpty) == 0 {
		return c.findPorneec(ctx, bson.M{}, skip, limit)
	}
	return c.findPorneec(ctx, bson.M{"tags_norm": bson.M{"$in": nonEmpty}}, skip, limit)
}

// GetPorneecByPerformer returns entries whose performers_norm includes performerNorm.
func (c *Client) GetPorneecByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PorneecEntry, error) {
	if performerNorm == "" {
		return c.findPorneec(ctx, bson.M{}, skip, limit)
	}
	return c.findPorneec(ctx, bson.M{"performers_norm": performerNorm}, skip, limit)
}

// SearchPorneec returns entries whose title/performers/studios match query.
func (c *Client) SearchPorneec(ctx context.Context, query string, skip, limit int) ([]models.PorneecEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	esc := escapeRegex(q)
	return c.findPorneec(ctx, bson.M{"$or": []bson.M{
		{"title": bson.M{"$regex": esc, "$options": "i"}},
		{"performers": bson.M{"$regex": esc, "$options": "i"}},
		{"studios": bson.M{"$regex": esc, "$options": "i"}},
	}}, skip, limit)
}

// GetPorneecEntry returns one entry by slug, or nil.
func (c *Client) GetPorneecEntry(ctx context.Context, slug string) (*models.PorneecEntry, error) {
	if slug == "" {
		return nil, nil
	}
	res := c.coll("porneec_entries").FindOne(ctx, bson.M{"_id": porneecEntryDocID(slug)})
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
	e := mapPorneecEntry(doc)
	return &e, nil
}

// GetPorneecMissingDetail returns entries not yet detail-scraped, newest first.
func (c *Client) GetPorneecMissingDetail(ctx context.Context, limit int) ([]models.PorneecEntry, error) {
	return c.findPorneec(ctx, bson.M{"detail_scraped": bson.M{"$ne": true}}, 0, limit)
}

// PorneecEntriesCount is the total entry count for monitoring.
func (c *Client) PorneecEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("porneec_entries").CountDocuments(ctx, bson.M{})
}

// topValuesPorneec returns the most common non-empty display values desc by
// count. Used for studio/tag/performer discover options so the dropdown mirrors
// the store. studios/performers are multikey arrays; tags is empty for porneec.
func (c *Client) topValuesPorneec(ctx context.Context, arrayField string, limit int) ([]string, error) {
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
	cur, err := c.coll("porneec_entries").Aggregate(ctx, pipeline)
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

// GetPorneecTopStudios / TopTags / TopPerformers feed the manifest genre
// dropdowns for pec_studio / pec_tag / pec_performer. TopTags returns nil for
// porneec (tags are empty); kept for catalog parity.
func (c *Client) GetPorneecTopStudios(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesPorneec(ctx, "studios", limit)
}
func (c *Client) GetPorneecTopTags(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesPorneec(ctx, "tags", limit)
}
func (c *Client) GetPorneecTopPerformers(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesPorneec(ctx, "performers", limit)
}

// findPorneec runs a find with date:-1 sort (these sources have no rating/alpha
// browse).
func (c *Client) findPorneec(ctx context.Context, filter bson.M, skip, limit int) ([]models.PorneecEntry, error) {
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
	cur, err := c.coll("porneec_entries").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.PorneecEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapPorneecEntry(doc))
	}
	return out, cur.Err()
}

func mapPorneecEntry(doc bson.M) models.PorneecEntry {
	return models.PorneecEntry{
		VideoID:        strVal(doc, "video_id"),
		Slug:           strVal(doc, "slug"),
		Title:          strVal(doc, "title"),
		DetailURL:      strVal(doc, "detail_url"),
		Date:           strVal(doc, "date"),
		Poster:         strVal(doc, "poster"),
		Studios:        stringSlice(doc, "studios"),
		StudiosNorm:    stringSlice(doc, "studios_norm"),
		Performers:     stringSlice(doc, "performers"),
		PerformersNorm: stringSlice(doc, "performers_norm"),
		Description:    strVal(doc, "description"),
		Duration:       strVal(doc, "duration"),
		StreamURL:      strVal(doc, "stream_url"),
		DetailScraped:  boolVal(doc, "detail_scraped"),
		UpdatedAt:      int64Val(doc, "updated_at"),
	}
}
