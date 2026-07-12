package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// freepornvideos_entries is the durable home for every freepornvideos.xxx scene.
// Mirrors perverzija_entries: one doc per scene (_id = "fpv:" + videoID), flat
// store (no quality grouping), pvz/fpv catalog + meta + stream paths read from
// here. Stream qualities (one mp4 per <source> quality) are resolved live from
// the detail page by the scraper's stream resolver; the token rotates per
// request so it is never stored.

func freepornvideosEntryDocID(id string) string { return "fpv:" + id }

// UpsertFreepornvideosEntry stores a scene's card fields. Enrichment-owned
// fields (date/duration/categories/network/description/detail_scraped) are
// $setOnInsert only, so the ingest sweep re-walking /latest-updates/1/ on a
// cursor reset does NOT clobber a prior detail scrape with zero values - which
// would wipe `date` (the fpv_recent sort key) and sink re-walked entries to the
// bottom of the feed, plus break streams/categories. The enrich sweep persists
// those via UpdateFreepornvideosEnrichment. Card fields (video_id/slug/title/
// detail_url/poster/studio/performers/rating/views/has_4k) are $set so a full
// listing walk refreshes them; performers come from the card (thumb_model), not
// the detail page.
func (c *Client) UpsertFreepornvideosEntry(ctx context.Context, e models.FreepornvideosEntry) error {
	if e.VideoID == "" {
		return nil
	}
	filter := bson.M{"_id": freepornvideosEntryDocID(e.VideoID)}
	_, err := c.coll("freepornvideos_entries").UpdateOne(ctx, filter, freepornvideosUpsertUpdate(e), options.Update().SetUpsert(true))
	return err
}

// freepornvideosUpsertUpdate builds the upsert doc: card fields in $set, detail-
// owned fields (date/duration/categories/network/description/detail_scraped) in
// $setOnInsert. Extracted so the field-placement contract is unit-testable
// without a live Mongo (see TestFreepornvideosUpsertKeepsEnrichmentInSetOnInsert).
func freepornvideosUpsertUpdate(e models.FreepornvideosEntry) bson.M {
	if e.StudioNorm == "" {
		e.StudioNorm = models.NormToken(e.Studio)
	}
	return bson.M{
		"$set": bson.M{
			"video_id":        e.VideoID,
			"slug":            e.Slug,
			"title":           e.Title,
			"detail_url":      e.DetailURL,
			"excerpt":         e.Excerpt,
			"poster":          e.Poster,
			"studio":          e.Studio,
			"studio_norm":     e.StudioNorm,
			"performers":      nonNil(e.Performers),
			"performers_norm": normSlice(e.Performers),
			"rating":          e.Rating,
			"views":           e.Views,
			"has_4k":          e.Has4K,
			"website":         "freepornvideos",
			"updated_at":      nowSec(),
		},
		"$setOnInsert": bson.M{
			"date":            e.Date,
			"duration":        e.Duration,
			"categories":      nonNil(e.Categories),
			"categories_norm": normSlice(e.Categories),
			"network":         e.Network,
			"description":     e.Description,
			"detail_scraped":  e.DetailScraped,
		},
	}
}

// UpdateFreepornvideosEnrichment writes the detail-scraped fields (JSON-LD
// uploadDate, ISO8601 duration, categories, network, description, detail_scraped)
// for an entry. Called by the enrich sweep after EnrichEntry succeeds (and on a
// permanently-gone page, where EnrichEntry sets DetailScraped=true and returns
// nil). The ingest sweep never calls this, so a listing re-walk cannot wipe
// enrichment it did not perform. date is the fpv_recent sort key, so keeping it
// out of the ingest $set is what stops re-walks from disordering the feed.
func (c *Client) UpdateFreepornvideosEnrichment(ctx context.Context, e models.FreepornvideosEntry) error {
	if e.VideoID == "" {
		return nil
	}
	_, err := c.coll("freepornvideos_entries").UpdateOne(ctx, bson.M{"_id": freepornvideosEntryDocID(e.VideoID)}, freepornvideosEnrichmentUpdate(e))
	return err
}

func freepornvideosEnrichmentUpdate(e models.FreepornvideosEntry) bson.M {
	return bson.M{"$set": bson.M{
		"date":            e.Date,
		"duration":        e.Duration,
		"categories":      nonNil(e.Categories),
		"categories_norm": normSlice(e.Categories),
		"network":         e.Network,
		"description":     e.Description,
		"detail_scraped":  e.DetailScraped,
		"updated_at":      nowSec(),
	}}
}

// GetFreepornvideosRecent returns entries newest-first by date (the fpv_recent feed).
func (c *Client) GetFreepornvideosRecent(ctx context.Context, skip, limit int) ([]models.FreepornvideosEntry, error) {
	return c.findFreepornvideos(ctx, bson.M{}, skip, limit)
}

// GetFreepornvideosByStudio returns entries whose studio normalizes to studioNorm.
func (c *Client) GetFreepornvideosByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	if studioNorm == "" {
		return c.findFreepornvideos(ctx, bson.M{}, skip, limit)
	}
	return c.findFreepornvideos(ctx, bson.M{"studio_norm": studioNorm}, skip, limit)
}

// GetFreepornvideosByTag returns entries containing any of the normalized
// categories. An empty or all-empty tagsNorm (the "All" genre, which the catalog
// sends as []string{NormToken("")}) falls back to Recent, matching the
// studio/performer paths - without this the $in: [""] query would match zero
// docs (no doc has "" in categories_norm).
func (c *Client) GetFreepornvideosByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	nonEmpty := filterNonEmpty(tagsNorm)
	if len(nonEmpty) == 0 {
		return c.findFreepornvideos(ctx, bson.M{}, skip, limit)
	}
	return c.findFreepornvideos(ctx, bson.M{"categories_norm": bson.M{"$in": nonEmpty}}, skip, limit)
}

// GetFreepornvideosByPerformer returns entries whose performers include performerNorm.
func (c *Client) GetFreepornvideosByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	if performerNorm == "" {
		return c.findFreepornvideos(ctx, bson.M{}, skip, limit)
	}
	return c.findFreepornvideos(ctx, bson.M{"performers_norm": performerNorm}, skip, limit)
}

// SearchFreepornvideos returns entries whose title/performers/studio match query.
func (c *Client) SearchFreepornvideos(ctx context.Context, query string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	esc := escapeRegex(q)
	return c.findFreepornvideos(ctx, bson.M{"$or": []bson.M{
		{"title": bson.M{"$regex": esc, "$options": "i"}},
		{"performers": bson.M{"$regex": esc, "$options": "i"}},
		{"studio": bson.M{"$regex": esc, "$options": "i"}},
	}}, skip, limit)
}

// GetFreepornvideosEntry returns one entry by videoID, or nil.
func (c *Client) GetFreepornvideosEntry(ctx context.Context, videoID string) (*models.FreepornvideosEntry, error) {
	if videoID == "" {
		return nil, nil
	}
	res := c.coll("freepornvideos_entries").FindOne(ctx, bson.M{"_id": freepornvideosEntryDocID(videoID)})
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
	e := mapFreepornvideosEntry(doc)
	return &e, nil
}

// GetFreepornvideosMissingDetail returns entries not yet detail-scraped, newest first.
func (c *Client) GetFreepornvideosMissingDetail(ctx context.Context, limit int) ([]models.FreepornvideosEntry, error) {
	return c.findFreepornvideos(ctx, bson.M{"detail_scraped": bson.M{"$ne": true}}, 0, limit)
}

// FreepornvideosEntriesCount is the total entry count for monitoring.
func (c *Client) FreepornvideosEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("freepornvideos_entries").CountDocuments(ctx, bson.M{})
}

// topValuesFPV returns the most common non-empty display values desc by count.
// Used for studio/category/performer discover options so the dropdown mirrors
// the store. categories/performers are multikey arrays.
func (c *Client) topValuesFPV(ctx context.Context, field string, arrayField string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 60
	}
	matchField := field
	if arrayField != "" {
		matchField = arrayField
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{matchField: bson.M{"$nin": []interface{}{"", nil}}}}},
	}
	if arrayField != "" {
		pipeline = append(pipeline,
			bson.D{{Key: "$unwind", Value: bson.D{
				{Key: "path", Value: "$" + arrayField},
				{Key: "preserveNullAndEmptyArrays", Value: false},
			}}},
			bson.D{{Key: "$match", Value: bson.M{arrayField: bson.M{"$ne": ""}}}},
		)
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$" + matchField},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "n", Value: -1}}}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	)
	cur, err := c.coll("freepornvideos_entries").Aggregate(ctx, pipeline)
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

// GetFreepornvideosTopStudios / TopTags / TopPerformers feed the manifest genre
// dropdowns for fpv_studio / fpv_tag / fpv_performer.
func (c *Client) GetFreepornvideosTopStudios(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesFPV(ctx, "studio", "", limit)
}
func (c *Client) GetFreepornvideosTopTags(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesFPV(ctx, "categories", "categories", limit)
}
func (c *Client) GetFreepornvideosTopPerformers(ctx context.Context, limit int) ([]string, error) {
	return c.topValuesFPV(ctx, "performers", "performers", limit)
}

// findFreepornvideos runs a find with date:-1 sort (these sources have no
// rating/alpha browse).
func (c *Client) findFreepornvideos(ctx context.Context, filter bson.M, skip, limit int) ([]models.FreepornvideosEntry, error) {
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
	cur, err := c.coll("freepornvideos_entries").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.FreepornvideosEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapFreepornvideosEntry(doc))
	}
	return out, cur.Err()
}

func mapFreepornvideosEntry(doc bson.M) models.FreepornvideosEntry {
	return models.FreepornvideosEntry{
		VideoID:        strVal(doc, "video_id"),
		Slug:           strVal(doc, "slug"),
		Title:          strVal(doc, "title"),
		DetailURL:      strVal(doc, "detail_url"),
		Date:           strVal(doc, "date"),
		Excerpt:        strVal(doc, "excerpt"),
		Poster:         strVal(doc, "poster"),
		Studio:         strVal(doc, "studio"),
		StudioNorm:     strVal(doc, "studio_norm"),
		Network:        strVal(doc, "network"),
		Categories:     stringSlice(doc, "categories"),
		CategoriesNorm: stringSlice(doc, "categories_norm"),
		Performers:     stringSlice(doc, "performers"),
		PerformersNorm: stringSlice(doc, "performers_norm"),
		Description:    strVal(doc, "description"),
		Duration:       strVal(doc, "duration"),
		Rating:         strVal(doc, "rating"),
		Views:          strVal(doc, "views"),
		Has4K:          boolVal(doc, "has_4k"),
		DetailScraped:  boolVal(doc, "detail_scraped"),
		UpdatedAt:      int64Val(doc, "updated_at"),
	}
}
