package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// futureDateCutoff returns tomorrow's "YYYY-MM-DD" string in UTC. Catalogs filter
// `date < cutoff` so future-dated (pre-release) TPDB/StashDB scenes stop
// dominating the top of the date:-1 sort and recently-released scenes surface.
// `$lt: tomorrow` (not `$lte: today`) so a same-day date with any time suffix
// still counts as today; date is stored as the source's raw YYYY-MM-DD string
// (a UTC calendar date) so the lexicographic compare matches calendar order.
// UTC is explicit (not bare time.Now()) to match every other date-producing
// line in pkg/mongo, so a non-UTC host (local bulk-fill, dev) does not drift
// the cutoff boundary by up to 24h.
func futureDateCutoff() string {
	return dateCutoffFrom(time.Now().UTC())
}

// dateCutoffFrom is the pure, clock-injected core of futureDateCutoff so the
// boundary is unit-testable. tomorrow = t + 1 day.
func dateCutoffFrom(t time.Time) string {
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// enriched_scenes is the durable home for every TPDB/StashDB scene matched to a
// resolvable torrent from the configured sources. It is the queryable source
// for the tpdb_new / tpdb_cat / stashdb_cat catalogs and the porndb: meta/stream
// path, so repeated opens are served from Mongo instead of re-hitting the live
// TPDB/StashDB APIs. Documents are keyed _id = the scene's Stremio metaID
// ("porndb:<num>" for TPDB, a stash-scene id for StashDB), so a meta/stream open
// under that ID links straight back here. No expires_at / TTL: the collection is
// never cleaned, matching pornrips_entries / shared_meta.

// UpsertEnrichedScene stores a scene. Metadata fields (title/poster/cast/tags/
// date) are $setOnInsert only so a re-walk that re-upserts an already-stored
// scene does not clobber its data - the scene metadata is set once at discovery
// (or on-demand first touch) and never refreshed, since TPDB/StashDB scene data
// is immutable. matched_sources/attempted_sources are $addToSet $each so multiple
// sweep rounds and on-demand upserts union into the existing doc. Per-source
// torrents are $set by dotted key (torrents.<src>) so a round that refreshes one
// source's best torrent preserves the other sources' refs.
func (c *Client) UpsertEnrichedScene(ctx context.Context, s models.EnrichedScene) error {
	if s.ID == "" {
		return nil
	}
	set := bson.M{
		"source":     s.Source,
		"updated_at": nowSec(),
	}
	for src, ref := range s.Torrents {
		set["torrents."+src] = torrentRefDoc(ref)
	}
	update := bson.M{
		"$set": set,
		"$setOnInsert": bson.M{
			"title":       s.Title,
			"poster":      s.Poster,
			"background":  s.Background,
			"description": s.Description,
			"cast":        nonNil(s.Cast),
			"tags":        nonNil(s.Tags),
			"tags_norm":   nonNil(s.TagsNorm),
			"date":        s.Date,
			"studio":      s.Studio,
		},
	}
	addToSet := bson.M{}
	if len(s.MatchedSources) > 0 {
		addToSet["matched_sources"] = bson.M{"$each": s.MatchedSources}
	}
	if len(s.AttemptedSources) > 0 {
		addToSet["attempted_sources"] = bson.M{"$each": s.AttemptedSources}
	}
	if len(addToSet) > 0 {
		update["$addToSet"] = addToSet
	}
	ctx, cancel := opTimeoutCtx(ctx)
	defer cancel()
	_, err := c.coll("enriched_scenes").UpdateOne(ctx, bson.M{"_id": s.ID}, update, options.Update().SetUpsert(true))
	return err
}

// GetEnrichedScenesByMatchedSources returns newest-first scenes for a metadata
// source ("tpdb"/"stashdb") whose matched_sources intersect the configured
// torrent sources. This is the store-backed catalog browse (tpdb_new) and the
// category catalog (tpdb_cat/stashdb_cat): the {source, [tags_norm,]
// matched_sources $in, date:-1} query is the source-gate fix - a scene only
// surfaces when one of the user's configured torrent sources resolved it. tags
// is an optional tags_norm $in filter (a category genre expanded to its bare +
// compound tokens; nil/empty = no tag filter, the browse path). Backed by the
// {source:1, matched_sources:1, date:-1} index; the tag filter additionally
// uses the {tags_norm:1} multikey index.
func (c *Client) GetEnrichedScenesByMatchedSources(ctx context.Context, source string, tags []string, sources []string, skip, limit int) ([]models.EnrichedScene, error) {
	if len(sources) == 0 {
		// No matched_sources gate = no catalog. Current callers pre-gate, but
		// defend against a future caller serving the whole store unfiltered.
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	filter := bson.M{}
	if source != "" {
		filter["source"] = source
	}
	if len(tags) > 0 {
		filter["tags_norm"] = bson.M{"$in": tags}
	}
	filter["matched_sources"] = bson.M{"$in": sources}
	// Drop future-dated (pre-release) scenes so the date:-1 sort surfaces
	// recently-released scenes at the top instead of TPDB's future-dated
	// entries. tpdb_search (GetEnrichedScenesByMatchedSourcesAndIDs) does NOT
	// apply this - a search returns all matched scenes for the query regardless
	// of release date.
	filter["date"] = bson.M{"$lt": futureDateCutoff()}
	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))
	ctx, cancel := opTimeoutCtx(ctx)
	defer cancel()
	cur, err := c.coll("enriched_scenes").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodeEnrichedScenes(ctx, cur)
}

// GetEnrichedScenesByMatchedSourcesAndIDs returns the store scenes among `ids`
// whose matched_sources intersect `sources`, newest-first. Used by the live
// tpdb_search filter: after a synchronous on-demand enrich, cross-ref the
// search items against the store so only scenes with a resolved torrent source
// in the user's configured set surface. The _id $in is a point lookup on the PK
// alongside the {source:1, matched_sources:1, date:-1} index the browse path uses.
func (c *Client) GetEnrichedScenesByMatchedSourcesAndIDs(ctx context.Context, source string, ids, sources []string, limit int) ([]models.EnrichedScene, error) {
	if len(sources) == 0 || len(ids) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	filter := bson.M{
		"_id":              bson.M{"$in": ids},
		"matched_sources":  bson.M{"$in": sources},
	}
	if source != "" {
		filter["source"] = source
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetLimit(int64(limit))
	ctx, cancel := opTimeoutCtx(ctx)
	defer cancel()
	cur, err := c.coll("enriched_scenes").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodeEnrichedScenes(ctx, cur)
}

// GetEnrichedSceneByID returns one scene by its Stremio metaID (_id), or nil when
// absent. Backs the Mongo-only meta/stream path: render a scene's poster/name/
// streams from the durable store without a live TPDB/Stash probe.
func (c *Client) GetEnrichedSceneByID(ctx context.Context, id string) (*models.EnrichedScene, error) {
	if id == "" {
		return nil, nil
	}
	res := c.coll("enriched_scenes").FindOne(ctx, bson.M{"_id": id})
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
	s := mapEnrichedScene(doc)
	return &s, nil
}

// GetEnrichedScenesMissingSourceMatch returns newest-first scenes the sweep has
// not yet tried to match against `source` (source absent from attempted_sources),
// so a clean miss is not re-scraped every tick but a transient failure (which
// stays out of attempted_sources) retries. Newest-first prioritizes freshly
// discovered scenes over the backlog the local bulk-fill clears.
// ponytail: $ne on a multikey array is index-unfriendly; the collection is
// bounded to discovered scenes and date:-1 sort + limit bound the scan. Switch to
// a per-source "next_attempt" cursor field if the collection grows large enough
// that the scan cost matters.
func (c *Client) GetEnrichedScenesMissingSourceMatch(ctx context.Context, source string, limit int) ([]models.EnrichedScene, error) {
	if source == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	ctx, cancel := opTimeoutCtx(ctx)
	defer cancel()
	cur, err := c.coll("enriched_scenes").Find(ctx, bson.M{
		"attempted_sources": bson.M{"$ne": source},
	}, options.Find().SetSort(bson.D{{Key: "date", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodeEnrichedScenes(ctx, cur)
}

// EnrichedScenesCount is the total scene count for monitoring.
func (c *Client) EnrichedScenesCount(ctx context.Context) (int64, error) {
	return c.coll("enriched_scenes").CountDocuments(ctx, bson.M{})
}

func decodeEnrichedScenes(ctx context.Context, cur *mongo.Cursor) ([]models.EnrichedScene, error) {
	var out []models.EnrichedScene
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapEnrichedScene(doc))
	}
	return out, cur.Err()
}

func mapEnrichedScene(doc bson.M) models.EnrichedScene {
	return models.EnrichedScene{
		ID:               strVal(doc, "_id"),
		Source:           strVal(doc, "source"),
		Title:            strVal(doc, "title"),
		Poster:           strVal(doc, "poster"),
		Background:       strVal(doc, "background"),
		Description:      strVal(doc, "description"),
		Cast:             stringSlice(doc, "cast"),
		Tags:             stringSlice(doc, "tags"),
		TagsNorm:         stringSlice(doc, "tags_norm"),
		Date:             strVal(doc, "date"),
		Studio:           strVal(doc, "studio"),
		MatchedSources:   stringSlice(doc, "matched_sources"),
		AttemptedSources: stringSlice(doc, "attempted_sources"),
		Torrents:         mapTorrents(doc),
		UpdatedAt:        int64Val(doc, "updated_at"),
	}
}

func mapTorrents(doc bson.M) map[string]models.TorrentRef {
	raw, ok := doc["torrents"].(bson.M)
	if !ok {
		return nil
	}
	out := make(map[string]models.TorrentRef, len(raw))
	for src, v := range raw {
		m, ok := v.(bson.M)
		if !ok {
			continue
		}
		out[src] = models.TorrentRef{
			InfoHash:   strVal(m, "info_hash"),
			TorrentURL: strVal(m, "torrent_url"),
			Title:      strVal(m, "title"),
			Seeders:    int(int64Val(m, "seeders")),
			Quality:    strVal(m, "quality"),
		}
	}
	return out
}

func torrentRefDoc(r models.TorrentRef) bson.M {
	return bson.M{
		"info_hash":   r.InfoHash,
		"torrent_url":  r.TorrentURL,
		"title":        r.Title,
		"seeders":      r.Seeders,
		"quality":      r.Quality,
	}
}