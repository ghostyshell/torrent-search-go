package jobs

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"torrent-search-go/internal/cache"
	"torrent-search-go/pkg/models"
)

const (
	perverzijaIngestCursorKey    = "perverzija_ingest_cursor"
	perverzijaIngestPagesPerTick = 10
	perverzijaIngestPageDelay    = 800 * time.Millisecond
	perverzijaEnrichPerTick      = 50
	perverzijaEnrichConcurrency  = 4
	perverzijaGenreTopN          = 50
)

// PerverzijaSync is the sole background job that discovers new Perverzija
// entries and populates Mongo with their source-scraped metadata. One tick runs
// three sweeps in order - ingest (WP REST feed -> UpsertPerverzijaEntry), enrich
// (detail-page scrape -> performers/description/poster/stream hash), and genre
// precompute (top-N studios/tags/performers -> KV blob for the manifest) - and
// merges their result maps (namespaced ingest_*/enrich_*/genres_*). No TPDB or
// StashDB: all metadata is scraped directly from tube.perverzija.com.
func (r *Runner) PerverzijaSync(ctx context.Context) (map[string]interface{}, error) {
	ingest, err := r.PerverzijaIngest(ctx)
	enrich, enrichErr := r.PerverzijaEnrich(ctx)
	genres := r.perverzijaGenrePrecompute(ctx)
	out := mergeSyncResults(ingest, enrich)
	for k, v := range genres {
		out["genres_"+k] = v
	}
	if err != nil {
		return out, err
	}
	return out, enrichErr
}

// PerverzijaIngest walks the Perverzija WordPress REST feed forward and upserts
// every post into perverzija_entries. A resumable cursor (page number) lives in
// the cache KV collection; when the walk hits the empty tail (WP end-of-feed) the
// cursor resets to 0 so the next tick re-walks from the top and picks up newly
// published posts. Re-walking re-upserts listing fields ($set) but leaves the
// enrich-only fields intact, so an already-detail-scraped entry is not clobbered.
func (r *Runner) PerverzijaIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.perverzija == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	pagesCap := perverzijaIngestPagesPerTick
	if v, err := strconv.Atoi(os.Getenv("PERVERZIJA_INGEST_PAGES_PER_TICK")); err == nil && v > 0 {
		pagesCap = v
	}
	delay := perverzijaIngestPageDelay
	if v, err := time.ParseDuration(os.Getenv("PERVERZIJA_INGEST_PAGE_DELAY")); err == nil && v > 0 {
		delay = v
	}
	page := r.loadPerverzijaIngestCursor(ctx)
	upserted := 0
	pagesWalked := 0
	hitEmpty := false

	for pagesWalked < pagesCap {
		if err := ctx.Err(); err != nil {
			return perverzijaIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		// WP REST /wp/v2/posts is 1-indexed: page=0 returns HTTP 400
		// (rest_post_invalid_page_number), which IngestPage maps to an empty
		// (end-of-feed) result. Offset by +1 so a fresh cursor (0) fetches page 1,
		// mirroring hentai_ingest.go's ListSeries(page+1). Without this the very
		// first tick on a cold cursor would treat page 0's 400 as end-of-feed and
		// the collection would never populate.
		entries, err := r.perverzija.IngestPage(ctx, page+1)
		if err != nil {
			return perverzijaIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		if len(entries) == 0 {
			hitEmpty = true
			r.storePerverzijaIngestCursor(ctx, 0)
			return perverzijaIngestResults(upserted, pagesWalked, hitEmpty), nil
		}
		for _, e := range entries {
			if e.Slug == "" {
				continue
			}
			if err := r.storage.UpsertPerverzijaEntry(ctx, e); err == nil {
				upserted++
			}
		}
		page++
		pagesWalked++
		if err := sleepCtx(ctx, delay); err != nil {
			return perverzijaIngestResults(upserted, pagesWalked, hitEmpty), err
		}
	}
	r.storePerverzijaIngestCursor(ctx, page)
	return perverzijaIngestResults(upserted, pagesWalked, hitEmpty), nil
}

func perverzijaIngestResults(upserted, pages int, hitEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"upserted": upserted,
		"pages":    pages,
		"hitEmpty": hitEmpty,
	}
}

func (r *Runner) loadPerverzijaIngestCursor(ctx context.Context) int {
	v, ok, err := r.storage.KVGet(ctx, perverzijaIngestCursorKey)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Runner) storePerverzijaIngestCursor(ctx context.Context, page int) {
	_ = r.storage.KVSet(ctx, perverzijaIngestCursorKey, strconv.Itoa(page), nil)
}

// PerverzijaEnrich scans perverzija_entries that have not yet been detail-scraped
// and fills performers/description/full poster/stream hash from the source detail
// page. EnrichEntry sets detail_scraped on success and on a permanently-gone page
// (HTTP 410/404) so deleted posts do not livelock the newest-first sweep; a
// transient failure (5xx/CF/timeout) leaves detail_scraped false so the entry is
// retried next tick (the pornrips enrich philosophy: retry transient, never
// permanently mark on a blip).
func (r *Runner) PerverzijaEnrich(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.perverzija == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	perTick := perverzijaEnrichPerTick
	if v, err := strconv.Atoi(os.Getenv("PERVERZIJA_ENRICH_PER_TICK")); err == nil && v > 0 {
		perTick = v
	}
	concurrency := perverzijaEnrichConcurrency
	if v, err := strconv.Atoi(os.Getenv("PERVERZIJA_ENRICH_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	missing, err := r.storage.GetPerverzijaMissingDetail(ctx, perTick)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, err
	}
	if len(missing) == 0 {
		return map[string]interface{}{"success": true, "scanned": 0, "enriched": 0}, nil
	}

	var mu sync.Mutex
	enriched := 0
	failed := 0
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, e := range missing {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		go func(e models.PerverzijaEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := r.perverzija.EnrichEntry(ctx, &e); err != nil {
				if err := r.storage.UpsertPerverzijaEntry(ctx, e); err == nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
				return
			}
			// Success or gone: EnrichEntry filled the detail fields (and sets
			// detail_scraped=true on a 410/404). Persist via the dedicated enrichment
			// writer so the ingest sweep's $setOnInsert contract keeps them safe
			// from a later listing re-walk.
			if err := r.storage.UpdatePerverzijaEnrichment(ctx, e); err == nil {
				mu.Lock()
				enriched++
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	return map[string]interface{}{"success": true, "scanned": len(missing), "enriched": enriched, "failed": failed}, nil
}

// perverzijaGenrePrecompute builds the top-N studio/tag/performer option lists
// from the store distinct-aggregations and writes them to Redis as one blob
// (cache.PrefixPerverzijaGenres+"opts") so the manifest path reads a cheap KV
// blob instead of running an aggregation on every manifest fetch. No-op when
// Redis is not configured (the manifest falls back to empty options and hides
// the Studio/Tag/Performer catalogs).
func (r *Runner) perverzijaGenrePrecompute(ctx context.Context) map[string]interface{} {
	if r.storage == nil || r.redis == nil {
		return map[string]interface{}{"success": true, "skipped": true}
	}
	studios, err := r.storage.GetPerverzijaTopStudios(ctx, perverzijaGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	tags, err := r.storage.GetPerverzijaTopTags(ctx, perverzijaGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	performers, err := r.storage.GetPerverzijaTopPerformers(ctx, perverzijaGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	blob, _ := json.Marshal(struct {
		Studios    []string `json:"studios"`
		Tags       []string `json:"tags"`
		Performers []string `json:"performers"`
	}{Studios: studios, Tags: tags, Performers: performers})
	if err := r.redis.Set(ctx, cache.PrefixPerverzijaGenres+"opts", string(blob), 7*24*time.Hour); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	return map[string]interface{}{"success": true, "studios": len(studios), "tags": len(tags), "performers": len(performers)}
}
