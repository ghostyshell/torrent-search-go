package mongo

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"torrent-search-go/pkg/models"
)

// pornrips_entries is the durable home for every PornRips WordPress post. It is
// the queryable source for all PornRips Stremio catalogs (recent/search/studio/
// tag), so repeated catalog opens are served from Mongo instead of re-hitting the
// PornRips WP API or HTML scraper. Documents are keyed _id = "pr:" + slug (the same
// metaID the shared_meta cache uses for PornRips), so a cover resolved for a
// favorite/catalog item under "pr:<slug>" links back here. No expires_at field and
// no TTL index: the collection is never cleaned, matching shared_meta/sukebei_catalog.

func pornripsEntryDocID(slug string) string { return "pr:" + slug }

// UpsertPornripsEntry stores a post's listing fields. Enrichment fields
// (studio/tags/genres/performers/poster/enriched_*) are $setOnInsert only, so a
// re-walk that re-upserts an already-enriched entry does not clobber its TPDB/
// StashDB data.
func (c *Client) UpsertPornripsEntry(ctx context.Context, e models.PornripsEntry) error {
	if e.Slug == "" {
		return nil
	}
	if e.MetaID == "" {
		e.MetaID = "pr:" + e.Slug
	}
	if e.DetailURL == "" {
		e.DetailURL = "https://pornrips.to/" + e.Slug + "/"
	}
	filter := bson.M{"_id": pornripsEntryDocID(e.Slug)}
	update := bson.M{
		"$set": bson.M{
			"slug":       e.Slug,
			"title":      e.Title,
			"detail_url": e.DetailURL,
			"date":       e.Date,
			"excerpt":    e.Excerpt,
			"wp_poster":  e.WpPoster,
			"meta_id":    e.MetaID,
			"website":    "pornrips",
			// studio/studio_norm are $set (not $setOnInsert) so a re-walk backfills
			// the WP post_tag on entries ingested before this field existed and
			// tracks a WP tag change. Enrich never overwrites studio: the WP
			// post_tag is the authoritative studio source and matches the curated
			// pr_studio option list via NormToken.
			"studio":      e.Studio,
			"studio_norm": e.StudioNorm,
			// info_hash/torrent_url are $set (not $setOnInsert) so a re-walk refreshes
			// the hash if a WP post's .torrent changed; the backfill sweep is the
			// usual writer, ingest passes empty here.
			"info_hash":   e.InfoHash,
			"torrent_url": e.TorrentURL,
			"updated_at":  nowSec(),
		},
		"$setOnInsert": bson.M{
			"tags":           nonNil(e.Tags),
			"tags_norm":      nonNil(e.TagsNorm),
			"genres":         nonNil(e.Genres),
			"performers":     nonNil(e.Performers),
			"poster":         e.Poster,
			"resolved_title": e.ResolvedTitle,
			"enriched_tpdb":  e.EnrichedTpdb,
			"enriched_stash": e.EnrichedStash,
			"enriched_at":    e.EnrichedAt,
		},
	}
	_, err := c.coll("pornrips_entries").UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// UpdatePornripsEnrichment writes the TPDB/StashDB-resolved enrichment fields for
// an entry and marks both sources attempted (hit or miss), so the sweep does not
// re-query TPDB/Stash for entries it has already tried. A later full re-enrich
// (manual reset) handles scenes added to TPDB/Stash after the first attempt.
// Studio is NOT written here: ingest owns studio (the WP post_tag) and sets it
// via UpsertPornripsEntry, so the enrich sweep never clobbers a fresher ingest
// value with a stale in-memory one. resolvedTitle is the merged TPDB/Stash scene
// title denormalized so SearchPornrips matches it without a shared_meta join.
func (c *Client) UpdatePornripsEnrichment(ctx context.Context, slug, poster, resolvedTitle string, tags, genres, performers []string) error {
	if slug == "" {
		return nil
	}
	tagsNorm := make([]string, 0, len(tags))
	for _, t := range tags {
		if n := models.NormToken(t); n != "" {
			tagsNorm = append(tagsNorm, n)
		}
	}
	_, err := c.coll("pornrips_entries").UpdateOne(ctx, bson.M{"_id": pornripsEntryDocID(slug)}, bson.M{
		"$set": bson.M{
			"tags":           nonNil(tags),
			"tags_norm":      tagsNorm,
			"genres":         nonNil(genres),
			"performers":     nonNil(performers),
			"poster":         poster,
			"resolved_title": resolvedTitle,
			"enriched_tpdb":  true,
			"enriched_stash": true,
			"enriched_at":    nowSec(),
			"updated_at":     nowSec(),
		},
	})
	return err
}

// GetPornripsRecent returns newest-first streamable entries (the pr_recent / "All"
// feed): findPornrips drops entries without a resolved info_hash (not yet
// backfilled). See streamablePornripsFilter.
func (c *Client) GetPornripsRecent(ctx context.Context, skip, limit int) ([]models.PornripsEntry, error) {
	return c.findPornrips(ctx, bson.M{}, skip, limit)
}

// GetPornripsByStudio returns streamable entries whose TPDB studio normalizes to
// studioNorm; findPornrips drops entries without a resolved info_hash. See
// streamablePornripsFilter.
func (c *Client) GetPornripsByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PornripsEntry, error) {
	if studioNorm == "" {
		return nil, nil
	}
	return c.findPornrips(ctx, bson.M{"studio_norm": studioNorm}, skip, limit)
}

// GetPornripsByTag returns streamable entries whose normalized tags contain any of
// tags (tags_norm $in); findPornrips drops entries without a resolved info_hash.
// For a pr_tag category whose content lives under compound TPDB tokens, the caller
// passes the alias-resolved token set (see prTagTokens); for a plain category it
// passes a one-element slice (the bare NormToken), preserving the original
// exact-match behaviour. See streamablePornripsFilter.
func (c *Client) GetPornripsByTag(ctx context.Context, tags []string, skip, limit int) ([]models.PornripsEntry, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	return c.findPornrips(ctx, bson.M{"tags_norm": bson.M{"$in": tags}}, skip, limit)
}

// SearchPornrips returns entries matching query against the original WP title,
// the TPDB/Stash-resolved scene title (resolved_title), or the enriched performers
// array - all case-insensitive substring. The collection is bounded to every
// PornRips post, so a regex scan is adequate and avoids the index-management
// overhead of a Mongo text index. performers is matched as a regex against the
// array (any element), so a performer name surfaces every release that names them
// without the live pornrips.to WP search the Mongo store replaced.
func (c *Client) SearchPornrips(ctx context.Context, query string, skip, limit int) ([]models.PornripsEntry, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	return c.findPornrips(ctx, pornripsSearchFilter(q), skip, limit)
}

// pornripsSearchFilter builds the $or filter matching query (case-insensitive
// substring) against title, resolved_title, and performers. Regex metacharacters
// in the user query are escaped. Pure (no *Client) so it unit-tests without Mongo.
func pornripsSearchFilter(query string) bson.M {
	escaped := escapeRegex(query)
	return bson.M{"$or": bson.A{
		bson.M{"title": bson.M{"$regex": escaped, "$options": "i"}},
		bson.M{"resolved_title": bson.M{"$regex": escaped, "$options": "i"}},
		bson.M{"performers": bson.M{"$regex": escaped, "$options": "i"}},
	}}
}

// escapeRegex escapes regex metacharacters in s for a safe Mongo $regex.
func escapeRegex(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	for _, ch := range []string{".", "*", "+", "?", "(", ")", "[", "]", "{", "}", "^", "$", "|"} {
		escaped = strings.ReplaceAll(escaped, ch, "\\"+ch)
	}
	return escaped
}

// GetPornripsEntryBySlug returns one entry by slug (the metaID stem), or nil when
// absent. Backs the Mongo-only meta path: render a PornRips item's poster/name from
// the durable store without a live WP/TPDB/Stash probe.
func (c *Client) GetPornripsEntryBySlug(ctx context.Context, slug string) (*models.PornripsEntry, error) {
	if slug == "" {
		return nil, nil
	}
	res := c.coll("pornrips_entries").FindOne(ctx, bson.M{"_id": pornripsEntryDocID(slug)})
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
	e := mapPornripsEntry(doc)
	return &e, nil
}

// GetPornripsEntriesByPerformer returns entries whose TPDB/Stash-enriched performers
// array contains performer (exact). Backs the tpdb_cat stream path's Mongo-only match
// of a TPDB scene to PornRips releases, using the enrichment the background sweep
// populated instead of a live pornrips.to search. performers is a canonical TPDB name,
// so an exact element match (backed by the multikey index) is precise.
func (c *Client) GetPornripsEntriesByPerformer(ctx context.Context, performer string, limit int) ([]models.PornripsEntry, error) {
	if strings.TrimSpace(performer) == "" {
		return nil, nil
	}
	return c.findPornrips(ctx, bson.M{"performers": performer}, 0, limit)
}

// GetPornripsEntriesByPerformers returns up to limit newest entries whose
// performers array contains ANY of the given names AND that already have a
// resolved info_hash. Filtering info_hash in Mongo (not in Go) guarantees the
// limit-bounded fetch only returns playable entries, so the catalog-time
// filter (PerformersWithTorrent) and the stream-time resolver (tpdbStreams)
// agree on what resolves. Backed by the {performers:1, info_hash:1} compound
// index; empty/nil/missing info_hash excluded via $nin (mirrors the inverted
// "missing torrent" filter in GetPornripsEntriesMissingTorrent).
func (c *Client) GetPornripsEntriesByPerformers(ctx context.Context, performers []string, limit int) ([]models.PornripsEntry, error) {
	if len(performers) == 0 {
		return nil, nil
	}
	ctx, cancel := opTimeoutCtx(ctx)
	defer cancel()
	return c.findPornrips(ctx, bson.M{
		"performers": bson.M{"$in": performers},
		"info_hash":  bson.M{"$nin": []interface{}{"", nil}},
	}, 0, limit)
}

// PerformersWithTorrent returns the subset of `performers` that have at least
// one resolved (non-empty info_hash) pornrips_entries doc. One distinct query
// over the {performers:1, info_hash:1} index; the distinct result is
// intersected with the input so only requested names are reported. Used by the
// tpdb_new/tpdb_search catalog filter to emit only scenes that will resolve to
// a stream against the pornrips source.
func (c *Client) PerformersWithTorrent(ctx context.Context, performers []string) (map[string]bool, error) {
	if len(performers) == 0 {
		return nil, nil
	}
	vals, err := c.coll("pornrips_entries").Distinct(ctx, "performers", bson.M{
		"performers": bson.M{"$in": performers},
		"info_hash":  bson.M{"$nin": []interface{}{"", nil}},
	})
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(performers))
	for _, p := range performers {
		want[p] = true
	}
	out := make(map[string]bool, len(performers))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok || !want[s] {
			continue
		}
		out[s] = true
	}
	return out, nil
}

// GetPornripsEntriesMissingEnrichment returns entries the sweep has not yet tried
// to enrich (neither source attempted), newest-first. Newest-first is intentional:
// the deployed PornripsSync job must enrich newly-ingested posts promptly (the
// "full schbang" for new entries), not sit them behind the old-archive backlog that
// the local cmd/pringest one-off is clearing. Backed by the {enriched_tpdb:1,
// enriched_stash:1, date:-1} index so the sort is index-covered (no in-memory SORT
// over the false/false partition).
func (c *Client) GetPornripsEntriesMissingEnrichment(ctx context.Context, limit int) ([]models.PornripsEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	cur, err := c.coll("pornrips_entries").Find(ctx, bson.M{
		"enriched_tpdb":  false,
		"enriched_stash": false,
	}, options.Find().SetSort(bson.D{{Key: "date", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodePornripsEntries(ctx, cur)
}

// GetPornripsEntriesMissingTorrent returns entries the backfill sweep has not yet
// resolved a .torrent infoHash for (info_hash empty or absent), newest-first. The
// sweep resolves these via FetchTorrentData + InfoHashFromTorrent and writes the
// hash back through SetPornripsTorrent.
func (c *Client) GetPornripsEntriesMissingTorrent(ctx context.Context, limit int) ([]models.PornripsEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetLimit(int64(limit))
	cur, err := c.coll("pornrips_entries").Find(ctx, bson.M{
		"info_hash": bson.M{"$in": []interface{}{"", nil}},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodePornripsEntries(ctx, cur)
}

// SetPornripsTorrent writes the resolved infoHash and .torrent URL for an entry so
// later stream opens skip the live Cloudflare-blocked detail-page fetch. Used by the
// backfill sweep and the lazy write-back in jstrmStreams.
func (c *Client) SetPornripsTorrent(ctx context.Context, slug, infoHash, torrentURL string) error {
	if slug == "" {
		return nil
	}
	_, err := c.coll("pornrips_entries").UpdateOne(ctx, bson.M{"_id": pornripsEntryDocID(slug)}, bson.M{
		"$set": bson.M{
			"info_hash":   infoHash,
			"torrent_url": torrentURL,
			"updated_at":  nowSec(),
		},
	})
	return err
}

// SetPornripsResolvedTitle writes the resolved scene title onto an entry and only
// that, leaving the enriched_* flags untouched so it is safe to run on entries the
// enrich sweep has already marked done. Used by cmd/backfill-pr-resolved-title to
// denormalize the title for entries enriched before this field existed, without
// re-running the slow TPDB/Stash sweep.
func (c *Client) SetPornripsResolvedTitle(ctx context.Context, slug, title string) error {
	if slug == "" || title == "" {
		return nil
	}
	_, err := c.coll("pornrips_entries").UpdateOne(ctx, bson.M{"_id": pornripsEntryDocID(slug)}, bson.M{
		"$set": bson.M{
			"resolved_title": title,
			"updated_at":     nowSec(),
		},
	})
	return err
}

// BackfillPornripsResolvedTitle populates resolved_title on entries that lack one,
// from their shared_meta row (TPDB title first, Stash fallback - the same merge the
// catalog displays). Entries already carrying a resolved_title are skipped, so
// re-runs are idempotent and cheap. Returns considered (scanned) and updated.
// ponytail: N+1 shared_meta lookups (one GetSharedMetaPair per entry) - the
// collection is bounded to every PornRips post and this is a one-off, so a Go
// cursor is simpler than a $lookup+$merge aggregation; switch to the aggregation
// if the collection grows large enough that the round-trip cost matters.
func (c *Client) BackfillPornripsResolvedTitle(ctx context.Context) (considered, updated int, err error) {
	cur, ferr := c.coll("pornrips_entries").Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "date", Value: -1}}))
	if ferr != nil {
		return 0, 0, ferr
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		considered++
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		e := mapPornripsEntry(doc)
		if e.ResolvedTitle != "" {
			continue
		}
		metaID := e.MetaID
		if metaID == "" {
			metaID = pornripsEntryDocID(e.Slug) // "pr:<slug>" = StableMetaID for pornrips
		}
		tpdb, stash, _ := c.GetSharedMetaPair(ctx, metaID)
		title := ""
		if tpdb != nil {
			title = tpdb.Title
		}
		if title == "" && stash != nil {
			title = stash.Title
		}
		if title == "" {
			continue
		}
		if uerr := c.SetPornripsResolvedTitle(ctx, e.Slug, title); uerr != nil {
			continue
		}
		updated++
	}
	return considered, updated, cur.Err()
}

// PornripsEntriesCount is the total entry count for monitoring.
func (c *Client) PornripsEntriesCount(ctx context.Context) (int64, error) {
	return c.coll("pornrips_entries").CountDocuments(ctx, bson.M{})
}

func (c *Client) findPornrips(ctx context.Context, filter bson.M, skip, limit int) ([]models.PornripsEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetSkip(int64(skip)).
		SetLimit(int64(limit))
	cur, err := c.coll("pornrips_entries").Find(ctx, streamablePornripsFilter(filter), opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	return decodePornripsEntries(ctx, cur)
}

// streamablePornripsFilter returns a copy of filter with an info_hash requirement
// added so catalog/search reads (pr_recent / pr_studio / pr_tag / SearchPornrips /
// performer lookups) only surface entries a stream can be opened for. The
// torrent-backfill sweep writes info_hash+torrent_url together via SetPornripsTorrent,
// so an empty/absent info_hash means the entry is not yet backfilled (the newest
// posts - ~0.04% of the collection); hiding them keeps the catalog to playable
// items. The sweep reads (GetPornripsEntriesMissing*) and GetPornripsEntryBySlug
// use their own Find and bypass this, so backfill and meta rendering still work for
// unswept entries. Copy (not mutate) so callers may reuse their filter map; a
// caller that already sets info_hash (GetPornripsEntriesByPerformers) is idempotent.
func streamablePornripsFilter(filter bson.M) bson.M {
	f := make(bson.M, len(filter)+1)
	for k, v := range filter {
		f[k] = v
	}
	f["info_hash"] = bson.M{"$nin": []interface{}{"", nil}}
	return f
}

func decodePornripsEntries(ctx context.Context, cur *mongo.Cursor) ([]models.PornripsEntry, error) {
	var out []models.PornripsEntry
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		out = append(out, mapPornripsEntry(doc))
	}
	return out, cur.Err()
}

func mapPornripsEntry(doc bson.M) models.PornripsEntry {
	return models.PornripsEntry{
		Slug:          strVal(doc, "slug"),
		Title:         strVal(doc, "title"),
		DetailURL:     strVal(doc, "detail_url"),
		Date:          strVal(doc, "date"),
		Excerpt:       strVal(doc, "excerpt"),
		WpPoster:      strVal(doc, "wp_poster"),
		Poster:        strVal(doc, "poster"),
		Studio:        strVal(doc, "studio"),
		StudioNorm:    strVal(doc, "studio_norm"),
		Tags:          stringSlice(doc, "tags"),
		TagsNorm:      stringSlice(doc, "tags_norm"),
		Genres:        stringSlice(doc, "genres"),
		Performers:    stringSlice(doc, "performers"),
		ResolvedTitle: strVal(doc, "resolved_title"),
		MetaID:        strVal(doc, "meta_id"),
		EnrichedTpdb:  boolVal(doc, "enriched_tpdb"),
		EnrichedStash: boolVal(doc, "enriched_stash"),
		EnrichedAt:    int64Val(doc, "enriched_at"),
		UpdatedAt:     int64Val(doc, "updated_at"),
		InfoHash:      strVal(doc, "info_hash"),
		TorrentURL:    strVal(doc, "torrent_url"),
	}
}

// nonNil returns a non-nil slice for bson storage so missing fields decode as
// []string{} rather than null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// boolVal decodes a bson bool field.
func boolVal(doc bson.M, key string) bool {
	if v, ok := doc[key].(bool); ok {
		return v
	}
	return false
}
