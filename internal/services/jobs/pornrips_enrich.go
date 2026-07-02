package jobs

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// splitCSV splits a comma-separated env value into trimmed, non-empty tokens. Used to
// build a pool of TPDB/StashDB clients from TPDB_API_KEY="k1,k2,k3" so N keys give N
// independent throttle locks (N parallel calls) instead of one shared single-key client.
func splitCSV(s string) []string {
	out := make([]string, 0, 4)
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

const (
	pornripsEnrichPerTick     = 50
	pornripsEnrichConcurrency = 4
)

// PornripsEnrich scans pornrips_entries that have not yet been enriched and fills
// their studio/tags/genres/performers/poster from TPDB (and StashDB as fallback)
// so the pr_studio/pr_tag catalogs can query Mongo-side. Studio comes from the
// TPDB scene's site.name (carried on NormalizedMeta.Studio); tags/performers/
// poster come from whichever source resolves. UpdatePornripsEnrichment marks
// both enriched flags true (hit or miss) so the sweep does not re-query an entry
// it has already tried; a later manual reset re-enriches when TPDB/Stash adds the
// scene. TPDB's 250ms minGap throttle serializes TPDB calls internally, so the
// worker pool mostly overlaps StashDB round-trips with TPDB waits.
func (r *Runner) PornripsEnrich(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo not configured"}, nil
	}

	perTick := pornripsEnrichPerTick
	if v, err := strconv.Atoi(os.Getenv("PORNRIPS_ENRICH_PER_TICK")); err == nil && v > 0 {
		perTick = v
	}
	concurrency := pornripsEnrichConcurrency
	if v, err := strconv.Atoi(os.Getenv("PORNRIPS_ENRICH_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	// Torrent backfill runs on every enrich tick regardless of TPDB/Stash keys
	// (it only needs the scrapers + Mongo): resolve .torrent → infoHash for entries
	// the sweep hasn't resolved yet, so the catalog payload carries h:<infoHash>
	// and stream opens skip the live Cloudflare-blocked detail-page fetch.
	// ponytail: shares this tick's perTick/concurrency budget with TPDB/Stash
	// enrich rather than a separate scheduled job (saves ~14 config/monitoring
	// touchpoints); both are catch-up sweeps, so the shared budget is fine. Split
	// a dedicated budget off if torrent resolution starves TPDB enrichment.
	bf := r.pornripsTorrentBackfill(ctx, perTick, concurrency)

	tpdbKeys := splitCSV(r.cfg.Metadata.TPDBAPIKey)
	stashKeys := splitCSV(r.cfg.Metadata.StashDBAPIKey)
	useTPDB := len(tpdbKeys) > 0
	useStash := len(stashKeys) > 0
	if !useTPDB && !useStash {
		out := map[string]interface{}{"success": true, "skipped": true, "reason": "no TPDB_API_KEY or STASHDB_API_KEY"}
		for k, v := range bf {
			out[k] = v
		}
		return out, nil
	}

	entries, err := r.storage.GetPornripsEntriesMissingEnrichment(ctx, perTick)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		out := map[string]interface{}{"success": true, "scanned": 0}
		for k, v := range bf {
			out[k] = v
		}
		return out, nil
	}

	// Build a pool of TPDB/StashDB clients, one per comma-separated key, so N keys give
	// N independent 250ms throttle locks (N parallel calls). Single key = pool of 1 =
	// identical to the prior single-client behavior.
	tpdbPool := make([]*metadata.TPDBClient, len(tpdbKeys))
	for i, k := range tpdbKeys {
		tpdbPool[i] = metadata.NewTPDBClient(r.cfg.Metadata.TPDBAPIURL, k)
	}
	stashPool := make([]*metadata.StashDBClient, len(stashKeys))
	for i, k := range stashKeys {
		stashPool[i] = metadata.NewStashDBClient(r.cfg.Metadata.StashDBAPIURL, k)
	}

	// Redis shared_meta cache (same store the live meta handler reads). Nil when
	// Redis is not configured; the durable Mongo write still happens and the live
	// handler rehydrates Redis from Mongo via the MetaEnricher rehydrate path.
	var redisStore *addonRedisStore
	if r.redis != nil {
		redisStore = newAddonRedisStore(r.redis)
	}

	var (
		mu        sync.Mutex
		enriched  int
		attempted int
	)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(entry models.PornripsEntry, idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			// Shard entries round-robin across the client pool so each key's throttle
			// lock runs in parallel. nil when that source is disabled (enrichOne checks
			// the use flags, not the pointer).
			var tpdb *metadata.TPDBClient
			if useTPDB {
				tpdb = tpdbPool[idx%len(tpdbPool)]
			}
			var stash *metadata.StashDBClient
			if useStash {
				stash = stashPool[idx%len(stashPool)]
			}
			ok := r.enrichOnePornripsEntry(ctx, entry, tpdb, stash, redisStore, useTPDB, useStash)
			mu.Lock()
			attempted++
			if ok {
				enriched++
			}
			mu.Unlock()
		}(e, i)
	}
	wg.Wait()

	out := map[string]interface{}{
		"success":   true,
		"scanned":   len(entries),
		"attempted": attempted,
		"enriched":  enriched,
	}
	for k, v := range bf {
		out[k] = v
	}
	return out, nil
}

// pornripsTorrentBackfill resolves the .torrent infoHash for pornrips_entries that
// lack one, writing it back via SetPornripsTorrent so the next catalog open emits
// h:<infoHash> and stream opens skip the live detail-page fetch. Returns counts
// keyed torrentScanned/torrentAttempted/torrentResolved for the enrich job's
// result map. No-op when the scraper service or Mongo store is absent.
func (r *Runner) pornripsTorrentBackfill(ctx context.Context, perTick, concurrency int) map[string]interface{} {
	out := map[string]interface{}{"torrentScanned": 0, "torrentAttempted": 0, "torrentResolved": 0}
	if r.scrapers == nil {
		return out
	}
	entries, err := r.storage.GetPornripsEntriesMissingTorrent(ctx, perTick)
	if err != nil || len(entries) == 0 {
		return out
	}
	out["torrentScanned"] = len(entries)

	var mu sync.Mutex
	attempted, resolved := 0, 0
	sem := make(chan struct{}, concurrency)
	// PORNRIPS_BACKFILL_DIRECT skips the detail-page fetch (often Cloudflare-challenged
	// and the slow half of the 2-fetch path) and goes straight to the
	// /torrents/{name}.torrent fallback. ~2x faster bulk backfill; the deployed job leaves
	// it unset so it keeps the detail-page-first path for resilience on its small batches.
	direct := os.Getenv("PORNRIPS_BACKFILL_DIRECT") != ""
	var wg sync.WaitGroup
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(entry models.PornripsEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			mu.Lock()
			attempted++
			mu.Unlock()
			postURL := entry.DetailURL
			if direct {
				postURL = ""
			}
			data, err := r.scrapers.FetchTorrentData(ctx, "pornrips", postURL, entry.Title)
			if err != nil || len(data) == 0 {
				return
			}
			hash := scraper.InfoHashFromTorrent(data)
			if hash == "" {
				return
			}
			_ = r.storage.SetPornripsTorrent(ctx, entry.Slug, hash, scraper.PornripsTorrentURL(entry.Title))
			mu.Lock()
			resolved++
			mu.Unlock()
		}(e)
	}
	wg.Wait()
	out["torrentAttempted"] = attempted
	out["torrentResolved"] = resolved
	return out
}

// enrichOnePornripsEntry resolves one entry against TPDB then StashDB and writes
// the merged studio/tags/genres/performers/poster. Returns true if it wrote an
// enrichment update (a hit on either source), false on a clean miss (still marks
// the entry enriched-attempted via UpdatePornripsEnrichment so it is not re-scanned).
func (r *Runner) enrichOnePornripsEntry(ctx context.Context, e models.PornripsEntry, tpdb *metadata.TPDBClient, stash *metadata.StashDBClient, store *addonRedisStore, useTPDB, useStash bool) bool {
	// Studio is owned by ingest (the WP post_tag, set via UpsertPornripsEntry) and
	// is not touched here, so the enrich sweep can't clobber a fresher ingest value
	// with a stale in-memory one. TPDB site.name can diverge from the curated
	// pr_studio list, so it is not used for studio. Enrichment fills tags/
	// performers/poster from TPDB/Stash scene data, and durably writes the resolved
	// scene metadata to shared_meta (keyed e.MetaID = "pr:<slug>", the same key
	// buildMetas reads) so pr_recent shows the TPDB/Stash scene name + cover instead
	// of the raw release filename.
	poster := ""
	tags := make([]string, 0, 8)
	performers := make([]string, 0, 4)
	seenTag := make(map[string]struct{})
	seenPerf := make(map[string]struct{})
	hit := false
	var tpdbMeta, stashMeta *metadata.NormalizedMeta
	// transient signals: if either source rate-limits or errors, skip the write so
	// the entry stays enriched_tpdb/stash==false and is retried next tick instead of
	// being permanently marked enriched on a transient failure.
	tpdbTransient, stashTransient := false, false

	addTags := func(ts []string) {
		for _, t := range ts {
			if t == "" {
				continue
			}
			if _, ok := seenTag[t]; ok {
				continue
			}
			seenTag[t] = struct{}{}
			tags = append(tags, t)
		}
	}
	addPerformers := func(ps []string) {
		for _, p := range ps {
			if p == "" {
				continue
			}
			if _, ok := seenPerf[p]; ok {
				continue
			}
			seenPerf[p] = struct{}{}
			performers = append(performers, p)
		}
	}

	if useTPDB {
		// SearchMetadata returns (nil, ErrTPDBRateLimited) on a 429 and (nil, nil)
		// on a clean miss; any non-nil error means "retry later", not "no scene".
		if m, err := tpdb.SearchMetadata(ctx, e.Title); err != nil {
			tpdbTransient = true
		} else if m != nil {
			hit = true
			poster = m.Poster
			tpdbMeta = m
			addTags(m.Tags)
			addTags(m.Genres) // merged into tags so genre-style data is pr_tag-queryable
			addPerformers(m.Cast)
		}
	}
	// StashDB carries no studio on NormalizedMeta, but contributes tags/performers/poster
	// and is the only source when TPDB has no key.
	if useStash {
		if m, err := stash.SearchMetadata(ctx, e.Title, e.DetailURL); err != nil {
			stashTransient = true
		} else if m != nil {
			hit = true
			if poster == "" {
				poster = m.Poster
			}
			stashMeta = m
			addTags(m.Tags)
			addTags(m.Genres)
			addPerformers(m.Cast)
		}
	}

	// A transient failure on either source means we can't truthfully mark both
	// enriched flags done (UpdatePornripsEnrichment sets both). Leave the entry
	// unenriched so the next tick retries both sources. Re-calling the source that
	// succeeded is cheap relative to losing the rate-limited source's data forever.
	if tpdbTransient || stashTransient {
		return false
	}

	// Fall back to the WP featured image so the entry always has a poster.
	if poster == "" {
		poster = e.WpPoster
	}

	// Resolved scene title (TPDB first, Stash fallback - mirrors MergeShared used
	// for catalog display) denormalized onto the entry so SearchPornrips matches it
	// without a shared_meta join. Empty on a clean miss.
	resolvedTitle := ""
	if tpdbMeta != nil {
		resolvedTitle = tpdbMeta.Title
	}
	if resolvedTitle == "" && stashMeta != nil {
		resolvedTitle = stashMeta.Title
	}

	_ = r.storage.UpdatePornripsEnrichment(ctx, e.Slug, poster, resolvedTitle, tags, nil, performers)

	// Persist the resolved scene metadata to shared_meta (the store buildMetas reads
	// for the catalog name/cover) for each source that resolved. Only on a hit: a
	// clean miss leaves no shared_meta, so the on-demand live resolver (now enabled
	// for PornRips) re-probes on view. The sweep itself marks the entry attempted
	// (UpdatePornripsEnrichment above flips both enriched flags) and won't retry
	// without a manual reset. Keyed by e.MetaID ("pr:<slug>" = StableMetaID), the
	// same key the live meta handler and MetaEnricher use, so the dedup there skips
	// entries the sweep already filled.
	metaID := e.MetaID
	if metaID == "" {
		metaID = StableMetaID("pornrips", e.DetailURL, "")
	}
	if tpdbMeta != nil {
		r.writePornripsSharedMeta(ctx, store, "tpdb", metaID, tpdbMeta)
	}
	if stashMeta != nil {
		r.writePornripsSharedMeta(ctx, store, "stashdb", metaID, stashMeta)
	}

	// ponytail: light pace so a full-tick burst of TPDB calls does not pin the
	// shared API key against the 429 ceiling; the throttle already serializes
	// TPDB starts, this just spreads StashDB overlap. Drop if throughput matters.
	select {
	case <-ctx.Done():
	case <-time.After(50 * time.Millisecond):
	}
	return hit
}

// writePornripsSharedMeta durably persists a TPDB/Stash-resolved scene to the
// shared_meta cache (Redis + Mongo), keyed by the entry's metaID, mirroring the
// MetaEnricher's runTPDB/runStash writes. Redis is skipped when store is nil
// (Redis not configured); the Mongo row is still written and the live meta handler
// rehydrates Redis from it.
func (r *Runner) writePornripsSharedMeta(ctx context.Context, store *addonRedisStore, source, metaID string, m *metadata.NormalizedMeta) {
	if metaID == "" || m == nil {
		return
	}
	shared := normalizedToShared(m)
	if store != nil {
		_ = store.SetSharedMeta(ctx, source, metaID, shared)
	}
	_ = r.storage.SetSharedMeta(ctx, source, metaID, sharedToPayload(&shared))
}
