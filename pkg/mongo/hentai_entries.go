package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// hentai_entries is the durable home for every HentaiMama series. It backs all
// hentai Stremio catalogs (new/top/all/studios/years/search) and the meta path,
// so repeated catalog/meta opens are served from Mongo instead of re-scraping
// the source WordPress site. Documents are keyed _id = "he:" + id (id is the
// bare "hmm-…" the manifest idPrefixes declare), so a Stremio item maps straight
// back here. No expires_at / no TTL: the collection is durable, like
// pornrips_entries.
//
// HentaiTV (htv-) was removed from the source scope; any leftover htv docs are
// hidden by filtering every read to prefix "hmm" rather than re-ingested.

func hentaiEntryDocID(id string) string { return "he:" + id }

// hmmOnlyFilter is the base filter for every read: only HentaiMama docs. htv
// docs (leftover from the dropped HentaiTV source) are excluded from all
// catalogs/meta/studios/genres/years reads.
func hmmOnlyFilter() bson.M { return bson.M{"prefix": "hmm"} }

// UpsertHentaiEntry stores a series' listing fields (filled by HentaiIngest
// from the source series/browse page). Every field is re-$set on each re-walk,
// so a full browse walk wholesale-refreshes a doc; there are no enrich-owned
// fields (MAL/Jikan enrichment was dropped - the source rating is shown
// directly), so no $setOnInsert split is needed.
func (c *Client) UpsertHentaiEntry(ctx context.Context, e models.HentaiEntry) error {
	if e.ID == "" {
		return nil
	}
	genresNorm := make([]string, 0, len(e.Genres))
	for _, g := range e.Genres {
		if n := models.NormToken(g); n != "" {
			genresNorm = append(genresNorm, n)
		}
	}
	e.GenresNorm = genresNorm
	if e.StudioNorm == "" {
		e.StudioNorm = models.NormToken(e.Studio)
	}
	episodes := make(bson.A, 0, len(e.Episodes))
	for _, ep := range e.Episodes {
		episodes = append(episodes, bson.M{
			"number":     ep.Number,
			"title":      ep.Title,
			"slug":       ep.Slug,
			"source_url": ep.SourceURL,
			"thumbnail":  ep.Thumbnail,
			"released":   ep.Released,
		})
	}
	filter := bson.M{"_id": hentaiEntryDocID(e.ID)}
	update := bson.M{
		"$set": bson.M{
			"id":           e.ID,
			"prefix":       e.Prefix,
			"slug":         e.Slug,
			"source":       e.Source,
			"title":        e.Title,
			"poster":       e.Poster,
			"background":   e.Background,
			"excerpt":      e.Excerpt,
			"release_year": e.ReleaseYear,
			"studio":       e.Studio,
			"studio_norm":  e.StudioNorm,
			"genres":       nonNil(e.Genres),
			"genres_norm":  nonNil(e.GenresNorm),
			"rating":       e.Rating,
			"rating_src":   e.RatingSrc,
			"detail_url":   e.DetailURL,
			"episodes":     episodes,
			"website":      "hentai",
			"updated_at":   nowSec(),
		},
	}
	_, err := c.coll("hentai_entries").UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// GetHentaiRecent returns entries newest-first by updated_at (the hentai_new
// "New Releases" feed).
func (c *Client) GetHentaiRecent(ctx context.Context, skip, limit int) ([]models.HentaiEntry, error) {
	return c.findHentai(ctx, bson.M{}, "recent", skip, limit)
}

// GetHentaiTop returns entries highest-rated first (hentai_top). An optional
// genre (normalized) filters via the genres_norm multikey index.
func (c *Client) GetHentaiTop(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error) {
	f := bson.M{}
	if genreNorm != "" {
		f["genres_norm"] = genreNorm
	}
	return c.findHentai(ctx, f, "top", skip, limit)
}

// GetHentaiAll returns entries alphabetical-by-title (hentai_all), optional
// genre filter.
func (c *Client) GetHentaiAll(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error) {
	f := bson.M{}
	if genreNorm != "" {
		f["genres_norm"] = genreNorm
	}
	return c.findHentai(ctx, f, "alpha", skip, limit)
}

// GetHentaiByStudio returns entries whose studio normalizes to studioNorm.
func (c *Client) GetHentaiByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.HentaiEntry, error) {
	if studioNorm == "" {
		return c.findHentai(ctx, bson.M{}, "recent", skip, limit)
	}
	return c.findHentai(ctx, bson.M{"studio_norm": studioNorm}, "recent", skip, limit)
}

// GetHentaiByYear returns entries released in year (hentai_years).
func (c *Client) GetHentaiByYear(ctx context.Context, year string, skip, limit int) ([]models.HentaiEntry, error) {
	if year == "" {
		return c.findHentai(ctx, bson.M{}, "recent", skip, limit)
	}
	return c.findHentai(ctx, bson.M{"release_year": year}, "recent", skip, limit)
}

// SearchHentai returns entries whose title matches query (case-insensitive
// substring). The collection is bounded to every scraped series, so a regex
// scan is adequate.
func (c *Client) SearchHentai(ctx context.Context, query string, skip, limit int) ([]models.HentaiEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	escaped := escapeRegex(q)
	return c.findHentai(ctx, bson.M{"title": bson.M{"$regex": escaped, "$options": "i"}}, "recent", skip, limit)
}

// GetHentaiEntry returns one HentaiMama entry by its bare id (hmm-…), or nil.
func (c *Client) GetHentaiEntry(ctx context.Context, id string) (*models.HentaiEntry, error) {
	if id == "" {
		return nil, nil
	}
	f := hmmOnlyFilter()
	f["_id"] = hentaiEntryDocID(id)
	res := c.coll("hentai_entries").FindOne(ctx, f)
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
	e := mapHentaiEntry(doc)
	return &e, nil
}

// HentaiEntriesCount is the total entry count for monitoring.
func (c *Client) HentaiEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("hentai_entries").CountDocuments(ctx, bson.M{})
}

// GetHentaiTopStudios returns the most common studios (display form) desc by
// count, so hentai_studios options mirror studios actually present in the store.
func (c *Client) GetHentaiTopStudios(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 60
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"prefix": "hmm", "studio": bson.M{"$nin": []interface{}{"", nil}}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$studio"},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "n", Value: -1}}}},
		{{Key: "$limit", Value: int64(limit)}},
	}
	cur, err := c.coll("hentai_entries").Aggregate(ctx, pipeline)
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

// GetHentaiTopGenres returns the most common genres (display form) desc by
// count, for hentai_top/hentai_all genre options.
func (c *Client) GetHentaiTopGenres(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 120
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"prefix": "hmm"}}},
		{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$genres"},
			{Key: "preserveNullAndEmptyArrays", Value: false},
		}}},
		{{Key: "$match", Value: bson.M{"genres": bson.M{"$ne": ""}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$genres"},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "n", Value: -1}}}},
		{{Key: "$limit", Value: int64(limit)}},
	}
	cur, err := c.coll("hentai_entries").Aggregate(ctx, pipeline)
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

// GetHentaiYears returns distinct release_year values desc, for hentai_years.
func (c *Client) GetHentaiYears(ctx context.Context) ([]string, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"prefix": "hmm", "release_year": bson.M{"$nin": []interface{}{"", nil}}}}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$release_year"}}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: -1}}}},
	}
	cur, err := c.coll("hentai_entries").Aggregate(ctx, pipeline)
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

// findHentai runs a find with the given filter and sort mode. sort is one of
// "recent" (updated_at -1), "top" (rating -1), "alpha" (title 1). The filter is
// always narrowed to HentaiMama docs (prefix "hmm").
func (c *Client) findHentai(ctx context.Context, filter bson.M, sort string, skip, limit int) ([]models.HentaiEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	if filter == nil {
		filter = hmmOnlyFilter()
	} else {
		filter["prefix"] = "hmm"
	}
	var sortDoc bson.D
	switch sort {
	case "top":
		sortDoc = bson.D{{Key: "rating", Value: -1}}
	case "alpha":
		sortDoc = bson.D{{Key: "title", Value: 1}}
	default:
		sortDoc = bson.D{{Key: "updated_at", Value: -1}}
	}
	opts := options.Find().
		SetSort(sortDoc).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))
	cur, err := c.coll("hentai_entries").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodeHentaiEntries(ctx, cur)
}

func decodeHentaiEntries(ctx context.Context, cur *mongo.Cursor) ([]models.HentaiEntry, error) {
	var out []models.HentaiEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapHentaiEntry(doc))
	}
	return out, cur.Err()
}

func mapHentaiEntry(doc bson.M) models.HentaiEntry {
	e := models.HentaiEntry{
		ID:          strVal(doc, "id"),
		Prefix:      strVal(doc, "prefix"),
		Slug:        strVal(doc, "slug"),
		Source:      strVal(doc, "source"),
		Title:       strVal(doc, "title"),
		Poster:      strVal(doc, "poster"),
		Background:  strVal(doc, "background"),
		Excerpt:     strVal(doc, "excerpt"),
		ReleaseYear: strVal(doc, "release_year"),
		Studio:      strVal(doc, "studio"),
		StudioNorm:  strVal(doc, "studio_norm"),
		Genres:      stringSlice(doc, "genres"),
		GenresNorm:  stringSlice(doc, "genres_norm"),
		Rating:      floatVal(doc, "rating"),
		RatingSrc:   strVal(doc, "rating_src"),
		DetailURL:   strVal(doc, "detail_url"),
		UpdatedAt:   int64Val(doc, "updated_at"),
	}
	if arr, ok := doc["episodes"].(bson.A); ok {
		e.Episodes = make([]models.HentaiEpisode, 0, len(arr))
		for _, v := range arr {
			m, ok := v.(bson.M)
			if !ok {
				continue
			}
			e.Episodes = append(e.Episodes, models.HentaiEpisode{
				Number:    int(int64Val(m, "number")),
				Title:     strVal(m, "title"),
				Slug:      strVal(m, "slug"),
				SourceURL: strVal(m, "source_url"),
				Thumbnail: strVal(m, "thumbnail"),
				Released:  strVal(m, "released"),
			})
		}
	}
	return e
}