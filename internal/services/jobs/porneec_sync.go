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
	porneecIngestCursorKey    = "porneec_ingest_cursor"
	porneecIngestPagesPerTick = 10
	porneecIngestPageDelay    = 800 * time.Millisecond
	porneecEnrichPerTick      = 50
	porneecEnrichConcurrency  = 4
	porneecGenreTopN          = 50
)

// PorneecSync is the background job that discovers new Porneec entries and
// populates Mongo with their source-scraped metadata. One tick runs three
// sweeps - ingest (HTML listing /page/{N}/ -> UpsertPorneecEntry), enrich
// (detail-page article:published_time + og:description + the clean-tube-player
// iframe base64 q -> tokenless Bunny CDN mp4, stored on the doc), and genre
// precompute (top-N studios/performers -> KV blob for the manifest) - and
// merges their result maps. No TPDB or StashDB: all metadata is scraped
// directly from porneec.com. porneec.com is reachable from prod egress, so this
// runs in the deployed container (no Mac cron).
func (r *Runner) PorneecSync(ctx context.Context) (map[string]interface{}, error) {
	ingest, err := r.PorneecIngest(ctx)
	enrich, enrichErr := r.PorneecEnrich(ctx)
	genres := r.porneecGenrePrecompute(ctx)
	out := mergeSyncResults(ingest, enrich)
	for k, v := range genres {
		out["genres_"+k] = v
	}
	if err != nil {
		return out, err
	}
	return out, enrichErr
}

// PorneecIngest walks the porneec.com listing forward (/page/{N}/, 1-indexed)
// and upserts every card into porneec_entries. Resumable page cursor in KV.
// porneec /page/{N}/ 404s past the archive end (like freepornvideos, unlike
// yesporn which never 404s), so the empty tail is the natural end-of-feed: it
// resets the cursor to 0 so the next tick re-walks from page 1 for new posts.
// The initial walk advances page 1..cap each tick until the 404 tail, then
// becomes recent-only.
func (r *Runner) PorneecIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.porneec == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	pagesCap := porneecIngestPagesPerTick
	if v, err := strconv.Atoi(os.Getenv("PORNEEC_INGEST_PAGES_PER_TICK")); err == nil && v > 0 {
		pagesCap = v
	}
	delay := porneecIngestPageDelay
	if v, err := time.ParseDuration(os.Getenv("PORNEEC_INGEST_PAGE_DELAY")); err == nil && v > 0 {
		delay = v
	}
	page := r.loadPorneecIngestCursor(ctx)
	upserted := 0
	pagesWalked := 0
	hitEmpty := false

	for pagesWalked < pagesCap {
		if err := ctx.Err(); err != nil {
			return porneecIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		// /page/{N}/ is 1-indexed (page 0 404s). Offset by +1 so a fresh cursor (0)
		// fetches page 1, mirroring freepornvideos_ingest.go.
		entries, err := r.porneec.IngestPage(ctx, page+1)
		if err != nil {
			return porneecIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		if len(entries) == 0 {
			hitEmpty = true
			r.storePorneecIngestCursor(ctx, 0)
			return porneecIngestResults(upserted, pagesWalked, hitEmpty), nil
		}
		for _, e := range entries {
			if e.Slug == "" {
				continue
			}
			if err := r.storage.UpsertPorneecEntry(ctx, e); err == nil {
				upserted++
			}
		}
		page++
		pagesWalked++
		if err := sleepCtx(ctx, delay); err != nil {
			return porneecIngestResults(upserted, pagesWalked, hitEmpty), err
		}
	}
	r.storePorneecIngestCursor(ctx, page)
	return porneecIngestResults(upserted, pagesWalked, hitEmpty), nil
}

func porneecIngestResults(upserted, pages int, hitEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"upserted": upserted,
		"pages":    pages,
		"hitEmpty": hitEmpty,
	}
}

func (r *Runner) loadPorneecIngestCursor(ctx context.Context) int {
	v, ok, err := r.storage.KVGet(ctx, porneecIngestCursorKey)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Runner) storePorneecIngestCursor(ctx context.Context, page int) {
	_ = r.storage.KVSet(ctx, porneecIngestCursorKey, strconv.Itoa(page), nil)
}

// PorneecEnrich scans porneec_entries that have not yet been detail-scraped and
// fills date (article:published_time, the pec_recent sort key), description
// (og:description), and the tokenless Bunny CDN mp4 (decoded from the
// clean-tube-player iframe base64 q param) from the detail page. EnrichEntry
// sets detail_scraped on success and on a permanently-gone page (HTTP 410/404)
// so deleted posts do not livelock the newest-first sweep; a transient failure
// (5xx/timeout) leaves detail_scraped false so the entry is retried next tick.
func (r *Runner) PorneecEnrich(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.porneec == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	perTick := porneecEnrichPerTick
	if v, err := strconv.Atoi(os.Getenv("PORNEEC_ENRICH_PER_TICK")); err == nil && v > 0 {
		perTick = v
	}
	concurrency := porneecEnrichConcurrency
	if v, err := strconv.Atoi(os.Getenv("PORNEEC_ENRICH_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	missing, err := r.storage.GetPorneecMissingDetail(ctx, perTick)
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
		go func(e models.PorneecEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := r.porneec.EnrichEntry(ctx, &e); err != nil {
				if err := r.storage.UpsertPorneecEntry(ctx, e); err == nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
				return
			}
			// Success or gone: EnrichEntry filled the detail fields (date,
			// description, stream_url, detail_scraped). Persist via the dedicated
			// enrichment writer so the ingest sweep's $setOnInsert contract keeps
			// date (the pec_recent sort key) and stream_url safe from a later
			// listing re-walk.
			if err := r.storage.UpdatePorneecEnrichment(ctx, e); err == nil {
				mu.Lock()
				enriched++
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	return map[string]interface{}{"success": true, "scanned": len(missing), "enriched": enriched, "failed": failed}, nil
}

// porneecGenrePrecompute builds the top-N studio/performer option lists and
// writes them to Redis (cache.PrefixPorneecGenres+"opts") for the manifest
// path. Tags are empty for porneec (obfuscated slugs), so TopTags returns nil
// and the tag dropdown is empty. No-op when Redis is not configured.
func (r *Runner) porneecGenrePrecompute(ctx context.Context) map[string]interface{} {
	if r.storage == nil || r.redis == nil {
		return map[string]interface{}{"success": true, "skipped": true}
	}
	studios, err := r.storage.GetPorneecTopStudios(ctx, porneecGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	tags, err := r.storage.GetPorneecTopTags(ctx, porneecGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	performers, err := r.storage.GetPorneecTopPerformers(ctx, porneecGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	blob, _ := json.Marshal(struct {
		Studios    []string `json:"studios"`
		Tags       []string `json:"tags"`
		Performers []string `json:"performers"`
	}{Studios: studios, Tags: tags, Performers: performers})
	if err := r.redis.Set(ctx, cache.PrefixPorneecGenres+"opts", string(blob), 7*24*time.Hour); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	return map[string]interface{}{"success": true, "studios": len(studios), "tags": len(tags), "performers": len(performers)}
}
