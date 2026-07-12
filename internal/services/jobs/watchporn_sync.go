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
	watchpornIngestCursorKey    = "watchporn_ingest_cursor"
	watchpornIngestPagesPerTick = 10
	watchpornIngestPageDelay    = 800 * time.Millisecond
	watchpornEnrichPerTick      = 50
	watchpornEnrichConcurrency  = 4
	watchpornGenreTopN          = 50
)

// WatchpornIngest walks the WatchPorn latest-updates feed forward and upserts every
// card into watchporn_entries. Resumable page cursor in KV; empty tail resets to 0
// so the next tick re-walks from the top for new posts. Called by the Mac-side
// launchd cron tool (cmd/watchporningest) - there is no deployed prod sync tick for
// this source because watchporn.to is TLS-blocked from prod egress.
func (r *Runner) WatchpornIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.watchporn == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	pagesCap := watchpornIngestPagesPerTick
	if v, err := strconv.Atoi(os.Getenv("WATCHPORN_INGEST_PAGES_PER_TICK")); err == nil && v > 0 {
		pagesCap = v
	}
	delay := watchpornIngestPageDelay
	if v, err := time.ParseDuration(os.Getenv("WATCHPORN_INGEST_PAGE_DELAY")); err == nil && v > 0 {
		delay = v
	}
	page := r.loadWatchpornIngestCursor(ctx)
	upserted := 0
	pagesWalked := 0
	hitEmpty := false

	for pagesWalked < pagesCap {
		if err := ctx.Err(); err != nil {
			return watchpornIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		// /latest-updates/{N}/ is 1-indexed (page 0 404s). Offset by +1 so a fresh
		// cursor (0) fetches page 1, mirroring the yesporn/freepornvideos ingest.
		entries, err := r.watchporn.IngestPage(ctx, page+1)
		if err != nil {
			return watchpornIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		if len(entries) == 0 {
			hitEmpty = true
			r.storeWatchpornIngestCursor(ctx, 0)
			return watchpornIngestResults(upserted, pagesWalked, hitEmpty), nil
		}
		// Batch the page's upserts into one BulkWrite (500/batch, unordered) instead
		// of one WAN round-trip per entry: the Mac->prod Mongo upsert is the only
		// WAN-bound ingest (the other tube sources ingest from prod at LAN speed), so
		// this is the one place per-entry UpdateOne would bottleneck a full-archive
		// sweep at ~280ms/entry. Mirrors pornrips' BackfillPornripsSceneGroup.
		pageBatch := make([]models.WatchpornEntry, 0, len(entries))
		for _, e := range entries {
			if e.VideoID != "" {
				pageBatch = append(pageBatch, e)
			}
		}
		if n, err := r.storage.UpsertWatchpornEntries(ctx, pageBatch); err == nil {
			upserted += n
		}
		page++
		pagesWalked++
		if err := sleepCtx(ctx, delay); err != nil {
			return watchpornIngestResults(upserted, pagesWalked, hitEmpty), err
		}
	}
	r.storeWatchpornIngestCursor(ctx, page)
	return watchpornIngestResults(upserted, pagesWalked, hitEmpty), nil
}

func watchpornIngestResults(upserted, pages int, hitEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"upserted": upserted,
		"pages":    pages,
		"hitEmpty": hitEmpty,
	}
}

// WatchpornIngestRecent is the keep-fresh ingest: it pins the cursor to 0 so each
// call walks pages 1..pagesCap (the newest posts) and stops, never advancing across
// the full archive. New posts appear at page 1, so a sparse Mac cron (every few
// hours) picks them up each fire instead of waiting for the cursor to roll the
// whole ~3300-page archive and reset (which at a 4h cadence would lag weeks). The
// cursor value stored between calls is irrelevant - always reset to 0 first. Use
// WatchpornIngest (cursor-advancing, looped until hitEmpty) for the one-time full
// archive fill, and this for the periodic cron.
func (r *Runner) WatchpornIngestRecent(ctx context.Context) (map[string]interface{}, error) {
	r.storeWatchpornIngestCursor(ctx, 0)
	return r.WatchpornIngest(ctx)
}

func (r *Runner) loadWatchpornIngestCursor(ctx context.Context) int {
	v, ok, err := r.storage.KVGet(ctx, watchpornIngestCursorKey)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Runner) storeWatchpornIngestCursor(ctx context.Context, page int) {
	_ = r.storage.KVSet(ctx, watchpornIngestCursorKey, strconv.Itoa(page), nil)
}

// WatchpornEnrich scans watchporn_entries that have not yet been detail-scraped and
// fills date/duration/poster/studios/tags/performers/description from the detail
// page (og: meta + /categories/ /models/ /tags/ links). EnrichEntry sets
// detail_scraped on success and on a permanently-gone page (HTTP 410/404) so
// deleted posts do not livelock the newest-first sweep; a transient failure
// (5xx/CF/timeout) leaves detail_scraped false so the entry is retried next tick.
func (r *Runner) WatchpornEnrich(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.watchporn == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	perTick := watchpornEnrichPerTick
	if v, err := strconv.Atoi(os.Getenv("WATCHPORN_ENRICH_PER_TICK")); err == nil && v > 0 {
		perTick = v
	}
	concurrency := watchpornEnrichConcurrency
	if v, err := strconv.Atoi(os.Getenv("WATCHPORN_ENRICH_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	missing, err := r.storage.GetWatchpornMissingDetail(ctx, perTick)
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
		go func(e models.WatchpornEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := r.watchporn.EnrichEntry(ctx, &e); err != nil {
				if err := r.storage.UpsertWatchpornEntry(ctx, e); err == nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
				return
			}
			// Success or gone: EnrichEntry filled the detail fields. Persist via the
			// dedicated enrichment writer so the ingest sweep's $setOnInsert contract
			// keeps date (the wpt_recent sort key) and the rest safe from a later
			// listing re-walk.
			if err := r.storage.UpdateWatchpornEnrichment(ctx, e); err == nil {
				mu.Lock()
				enriched++
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	return map[string]interface{}{"success": true, "scanned": len(missing), "enriched": enriched, "failed": failed}, nil
}

// WatchpornGenrePrecompute builds the top-N studio/tag/performer option lists and
// writes them to Redis (cache.PrefixWatchpornGenres+"opts") for the manifest path.
// Called by the Mac-cron tool after ingest+enrich (there is no deployed sync job to
// do this). No-op when Redis is not configured.
func (r *Runner) WatchpornGenrePrecompute(ctx context.Context) map[string]interface{} {
	if r.storage == nil || r.redis == nil {
		return map[string]interface{}{"success": true, "skipped": true}
	}
	studios, err := r.storage.GetWatchpornTopStudios(ctx, watchpornGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	tags, err := r.storage.GetWatchpornTopTags(ctx, watchpornGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	performers, err := r.storage.GetWatchpornTopPerformers(ctx, watchpornGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	blob, _ := json.Marshal(struct {
		Studios    []string `json:"studios"`
		Tags       []string `json:"tags"`
		Performers []string `json:"performers"`
	}{Studios: studios, Tags: tags, Performers: performers})
	if err := r.redis.Set(ctx, cache.PrefixWatchpornGenres+"opts", string(blob), 7*24*time.Hour); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	return map[string]interface{}{"success": true, "studios": len(studios), "tags": len(tags), "performers": len(performers)}
}