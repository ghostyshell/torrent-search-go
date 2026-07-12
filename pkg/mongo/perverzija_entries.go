package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// perverzija_entries is the durable home for every tube.perverzija.com scene.
// It backs the pvz_* Stremio catalogs (recent/studio/tag/performer/search) and
// the meta/stream paths, so repeated opens are served from Mongo instead of
// re-scraping the source. Documents are keyed _id = "pvz:" + slug; the Stremio
// item id "pvz:{slug}" maps straight back here. No expires_at / no TTL: the
// collection is durable, like pornrips_entries / hentai_entries. One doc per
// scene (no quality grouping); stream qualities are resolved live from the
// xtremestream master m3u8 by the scraper's stream resolver.

func perverzijaEntryDocID(slug string) string { return "pvz:" + slug }

// normSlice returns NormToken keys for s, dropping empties.
func normSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if n := models.NormToken(v); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// filterNonEmpty returns in with empty strings dropped. Used by the ByTag store
// methods so an all-empty tagsNorm (the "All" genre, sent as []string{""}) falls
// back to Recent instead of querying $in: [""] (which matches zero docs).
func filterNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// UpsertPerverzijaEntry stores a scene's listing fields. Enrichment-owned
// fields (performers/poster/description/stream_hash/detail_scraped) are
// $setOnInsert only, so the ingest sweep re-walking from page 1 on a cursor
// reset does NOT clobber a prior detail scrape with zero values (the bug that
// wiped stream_hash -> "no streams" and performers -> no Cast links on every
// re-walked entry). The enrich sweep persists those via UpdatePerverzijaEnrichment.
// Listing fields (slug/title/detail_url/date/excerpt/wp_poster/studios/tags) are
// $set so a full WP REST walk wholesale-refreshes them; date is listing-owned
// here (WP REST date_gmt is authoritative, unlike freepornvideos where date is
// enrich-owned from JSON-LD).
func (c *Client) UpsertPerverzijaEntry(ctx context.Context, e models.PerverzijaEntry) error {
	if e.Slug == "" {
		return nil
	}
	filter := bson.M{"_id": perverzijaEntryDocID(e.Slug)}
	_, err := c.coll("perverzija_entries").UpdateOne(ctx, filter, perverzijaUpsertUpdate(e), options.Update().SetUpsert(true))
	return err
}

// perverzijaUpsertUpdate builds the upsert doc: listing fields in $set, enrichment-
// owned fields in $setOnInsert. Extracted so the field-placement contract is
// unit-testable without a live Mongo (see TestPerverzijaUpsertKeepsEnrichmentInSetOnInsert).
func perverzijaUpsertUpdate(e models.PerverzijaEntry) bson.M {
	return bson.M{
		"$set": bson.M{
			"slug":         e.Slug,
			"title":        e.Title,
			"detail_url":   e.DetailURL,
			"date":         e.Date,
			"excerpt":      e.Excerpt,
			"wp_poster":    e.WpPoster,
			"studios":      nonNil(e.Studios),
			"studios_norm": normSlice(e.Studios),
			"tags":         nonNil(e.Tags),
			"tags_norm":    normSlice(e.Tags),
			"duration":     e.Duration,
			"website":      "perverzija",
			"updated_at":   nowSec(),
		},
		"$setOnInsert": bson.M{
			"performers":      nonNil(e.Performers),
			"performers_norm": normSlice(e.Performers),
			"poster":          e.Poster,
			"description":     e.Description,
			"stream_hash":     e.StreamHash,
			"detail_scraped":  e.DetailScraped,
		},
	}
}

// UpdatePerverzijaEnrichment writes the detail-scraped fields (performers, full
// poster, description, xtremestream stream hash, detail_scraped) for an entry.
// Called by the enrich sweep after EnrichEntry succeeds (and on a permanently-
// gone page, where EnrichEntry sets DetailScraped=true and returns nil so the
// sweep stops retrying the deleted post). The ingest sweep never calls this, so
// a listing re-walk cannot wipe enrichment it did not perform.
func (c *Client) UpdatePerverzijaEnrichment(ctx context.Context, e models.PerverzijaEntry) error {
	if e.Slug == "" {
		return nil
	}
	_, err := c.coll("perverzija_entries").UpdateOne(ctx, bson.M{"_id": perverzijaEntryDocID(e.Slug)}, perverzijaEnrichmentUpdate(e))
	return err
}

func perverzijaEnrichmentUpdate(e models.PerverzijaEntry) bson.M {
	return bson.M{"$set": bson.M{
		"performers":      nonNil(e.Performers),
		"performers_norm": normSlice(e.Performers),
		"poster":          e.Poster,
		"description":     e.Description,
		"stream_hash":     e.StreamHash,
		"detail_scraped":  e.DetailScraped,
		"updated_at":      nowSec(),
	}}
}

// GetPerverzijaRecent returns entries newest-first by date (the pvz_recent feed).
func (c *Client) GetPerverzijaRecent(ctx context.Context, skip, limit int) ([]models.PerverzijaEntry, error) {
	return c.findPerverzija(ctx, bson.M{}, "recent", skip, limit)
}

// GetPerverzijaByStudio returns entries whose studios list includes studioNorm.
func (c *Client) GetPerverzijaByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PerverzijaEntry, error) {
	if studioNorm == "" {
		return c.findPerverzija(ctx, bson.M{}, "recent", skip, limit)
	}
	return c.findPerverzija(ctx, bson.M{"studios_norm": studioNorm}, "recent", skip, limit)
}

// GetPerverzijaByTag returns entries containing any of the normalized tags. An
// empty or all-empty tagsNorm (the "All" genre, which the catalog sends as
// []string{NormToken("")}) falls back to Recent, matching the studio/performer
// paths - without this the $in: [""] query would match zero docs (normSlice
// drops empties, so no doc has "" in tags_norm).
func (c *Client) GetPerverzijaByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PerverzijaEntry, error) {
	nonEmpty := filterNonEmpty(tagsNorm)
	if len(nonEmpty) == 0 {
		return c.findPerverzija(ctx, bson.M{}, "recent", skip, limit)
	}
	return c.findPerverzija(ctx, bson.M{"tags_norm": bson.M{"$in": nonEmpty}}, "recent", skip, limit)
}

// GetPerverzijaByPerformer returns entries whose performers include performerNorm.
func (c *Client) GetPerverzijaByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PerverzijaEntry, error) {
	if performerNorm == "" {
		return c.findPerverzija(ctx, bson.M{}, "recent", skip, limit)
	}
	return c.findPerverzija(ctx, bson.M{"performers_norm": performerNorm}, "recent", skip, limit)
}

// SearchPerverzija returns entries whose title/performers/studios match query
// (case-insensitive substring). The collection is bounded to scraped scenes.
func (c *Client) SearchPerverzija(ctx context.Context, query string, skip, limit int) ([]models.PerverzijaEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	esc := escapeRegex(q)
	return c.findPerverzija(ctx, bson.M{"$or": []bson.M{
		{"title": bson.M{"$regex": esc, "$options": "i"}},
		{"performers": bson.M{"$regex": esc, "$options": "i"}},
		{"studios": bson.M{"$regex": esc, "$options": "i"}},
	}}, "recent", skip, limit)
}

// GetPerverzijaEntry returns one entry by slug, or nil.
func (c *Client) GetPerverzijaEntry(ctx context.Context, slug string) (*models.PerverzijaEntry, error) {
	if slug == "" {
		return nil, nil
	}
	res := c.coll("perverzija_entries").FindOne(ctx, bson.M{"_id": perverzijaEntryDocID(slug)})
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
	e := mapPerverzijaEntry(doc)
	return &e, nil
}

// GetPerverzijaMissingDetail returns entries not yet detail-scraped, newest
// first, backing the enrich sweep. detail_scraped:false + date:-1 index.
func (c *Client) GetPerverzijaMissingDetail(ctx context.Context, limit int) ([]models.PerverzijaEntry, error) {
	return c.findPerverzija(ctx, bson.M{"detail_scraped": bson.M{"$ne": true}}, "recent", 0, limit)
}

// PerverzijaEntriesCount is the total entry count for monitoring.
func (c *Client) PerverzijaEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("perverzija_entries").CountDocuments(ctx, bson.M{})
}

// topValues returns the most common non-empty display values for field (or the
// multikey arrayField) desc by count, for the manifest genre options. Used for
// studio/tag/performer discover options so the dropdown mirrors the store.
func (c *Client) topValues(ctx context.Context, field string, arrayField string, limit int) ([]string, error) {
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
	cur, err := c.coll("perverzija_entries").Aggregate(ctx, pipeline)
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

// GetPerverzijaTopStudios / TopTags / TopPerformers feed the manifest genre
// dropdowns for pvz_studio / pvz_tag / pvz_performer. Studios/tags/performers
// are all multikey arrays.
func (c *Client) GetPerverzijaTopStudios(ctx context.Context, limit int) ([]string, error) {
	return c.topValues(ctx, "studios", "studios", limit)
}
func (c *Client) GetPerverzijaTopTags(ctx context.Context, limit int) ([]string, error) {
	return c.topValues(ctx, "tags", "tags", limit)
}
func (c *Client) GetPerverzijaTopPerformers(ctx context.Context, limit int) ([]string, error) {
	return c.topValues(ctx, "performers", "performers", limit)
}

// findPerverzija runs a find with the given filter and sort. sort is "recent"
// (date -1) only - these sources have no rating/alpha browse.
func (c *Client) findPerverzija(ctx context.Context, filter bson.M, sort string, skip, limit int) ([]models.PerverzijaEntry, error) {
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
	cur, err := c.coll("perverzija_entries").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.PerverzijaEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapPerverzijaEntry(doc))
	}
	return out, cur.Err()
}

func mapPerverzijaEntry(doc bson.M) models.PerverzijaEntry {
	return models.PerverzijaEntry{
		Slug:           strVal(doc, "slug"),
		Title:          strVal(doc, "title"),
		DetailURL:      strVal(doc, "detail_url"),
		Date:           strVal(doc, "date"),
		Excerpt:        strVal(doc, "excerpt"),
		WpPoster:       strVal(doc, "wp_poster"),
		Poster:         strVal(doc, "poster"),
		Studios:        stringSlice(doc, "studios"),
		StudiosNorm:    stringSlice(doc, "studios_norm"),
		Tags:           stringSlice(doc, "tags"),
		TagsNorm:       stringSlice(doc, "tags_norm"),
		Performers:     stringSlice(doc, "performers"),
		PerformersNorm: stringSlice(doc, "performers_norm"),
		Description:    strVal(doc, "description"),
		Duration:       strVal(doc, "duration"),
		StreamHash:     strVal(doc, "stream_hash"),
		DetailScraped:  boolVal(doc, "detail_scraped"),
		UpdatedAt:      int64Val(doc, "updated_at"),
	}
}
