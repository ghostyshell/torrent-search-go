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
	yespornIngestCursorKey    = "yesporn_ingest_cursor"
	yespornIngestPagesPerTick = 10
	yespornIngestPageDelay    = 800 * time.Millisecond
	yespornEnrichPerTick      = 50
	yespornEnrichConcurrency  = 4
	yespornGenreTopN          = 50
)

// YespornSync is the background job that discovers new YesPorn entries and
// populates Mongo with their source-scraped metadata. One tick runs three
// sweeps - ingest (recent keep-fresh: pages 1..cap via YespornIngestRecent, so
// prod re-walks page 1 for new posts each tick instead of advancing into the
// archive), enrich (detail-page og: meta + channels + player-config
// categories/models), and genre precompute (top-N studios/tags/performers -> KV
// blob for the manifest) - and merges their result maps. No TPDB or StashDB: all
// metadata is scraped directly from yesporn.vip.
func (r *Runner) YespornSync(ctx context.Context) (map[string]interface{}, error) {
	ingest, err := r.YespornIngestRecent(ctx)
	enrich, enrichErr := r.YespornEnrich(ctx)
	genres := r.yespornGenrePrecompute(ctx)
	out := mergeSyncResults(ingest, enrich)
	for k, v := range genres {
		out["genres_"+k] = v
	}
	if err != nil {
		return out, err
	}
	return out, enrichErr
}

// YespornIngest walks the YesPorn latest-updates feed forward and upserts every
// card into yesporn_entries. Resumable page cursor in KV. End-of-feed is dedup-based,
// NOT an empty page: yesporn.vip returns a 20-card page for any page number
// (including out-of-range ones - no 404, no empty), so the walk never sees an empty
// tail. The feed surfaces a fixed ~989-video window (out-of-range pages just rotate
// that window), so once the store has all 989, every page's cards are already present.
// Thus when a tick upserts 0 NEW entries (every card already in the store), the
// archive is fully covered, so the cursor resets to 0 and hitEmpty=true (the one-time
// Mac bulk fill uses this to terminate; the prod sync tick uses YespornIngestRecent,
// not this forward walk). A transient all-matched tick before full coverage is
// statistically unreachable: a 2000-card batch sampling the ~989-window touches most
// of it (coupon-collector), so if even one video is still missing the batch almost
// certainly contains it and n > 0; 0-new only when all 989 are stored.
func (r *Runner) YespornIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.yesporn == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	pagesCap := yespornIngestPagesPerTick
	if v, err := strconv.Atoi(os.Getenv("YESPORN_INGEST_PAGES_PER_TICK")); err == nil && v > 0 {
		pagesCap = v
	}
	delay := yespornIngestPageDelay
	if v, err := time.ParseDuration(os.Getenv("YESPORN_INGEST_PAGE_DELAY")); err == nil && v > 0 {
		delay = v
	}
	page := r.loadYespornIngestCursor(ctx)
	upserted := 0
	pagesWalked := 0
	hitEmpty := false

	for pagesWalked < pagesCap {
		if err := ctx.Err(); err != nil {
			return yespornIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		// /latest-updates/{N}/ is 1-indexed (page 0 404s). Offset by +1 so a fresh
		// cursor (0) fetches page 1, mirroring freepornvideos_ingest.go.
		entries, err := r.yesporn.IngestPage(ctx, page+1)
		if err != nil {
			return yespornIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		if len(entries) == 0 {
			hitEmpty = true
			r.storeYespornIngestCursor(ctx, 0)
			return yespornIngestResults(upserted, pagesWalked, hitEmpty), nil
		}
		// Batch the page's upserts into one BulkWrite (500/batch, unordered). The Mac
		// one-time bulk fill is WAN-bound (Mac->prod Mongo); the prod sync tick uses
		// YespornIngestRecent (not this forward walk), but both share the batched call
		// as a no-op-cost superset (one round-trip for ~35 entries). Mirrors watchporn.
		// n here is NEW inserts only (UpsertYespornEntries returns UpsertedCount); 0
		// new means the archive is fully covered -> end-of-feed (see YespornIngest doc).
		pageBatch := make([]models.YespornEntry, 0, len(entries))
		for _, e := range entries {
			if e.VideoID != "" {
				pageBatch = append(pageBatch, e)
			}
		}
		if n, err := r.storage.UpsertYespornEntries(ctx, pageBatch); err == nil {
			upserted += n
			if n == 0 && pagesWalked > 0 {
				// 0 new inserts this page: every card was already in the store, so the
				// forward walk has covered the archive. yesporn.vip never returns an
				// empty/404 page for out-of-range numbers, so this dedup signal is the
				// only end-of-feed. Reset the cursor to 0 (-> recent-only next time).
				hitEmpty = true
				r.storeYespornIngestCursor(ctx, 0)
				return yespornIngestResults(upserted, pagesWalked, hitEmpty), nil
			}
		}
		page++
		pagesWalked++
		if err := sleepCtx(ctx, delay); err != nil {
			return yespornIngestResults(upserted, pagesWalked, hitEmpty), err
		}
	}
	r.storeYespornIngestCursor(ctx, page)
	return yespornIngestResults(upserted, pagesWalked, hitEmpty), nil
}

// YespornIngestRecent is the keep-fresh ingest: pins the cursor to 0 so each call
// walks pages 1..pagesCap (the newest posts). prod's YespornSync owns routine
// recent-only keep-fresh (cursor resets to 0 on hitEmpty post-fill); this is the
// manual Mac equivalent for cmd/yesporningest's default mode.
func (r *Runner) YespornIngestRecent(ctx context.Context) (map[string]interface{}, error) {
	r.storeYespornIngestCursor(ctx, 0)
	return r.YespornIngest(ctx)
}

func yespornIngestResults(upserted, pages int, hitEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"upserted": upserted,
		"pages":    pages,
		"hitEmpty": hitEmpty,
	}
}

func (r *Runner) loadYespornIngestCursor(ctx context.Context) int {
	v, ok, err := r.storage.KVGet(ctx, yespornIngestCursorKey)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Runner) storeYespornIngestCursor(ctx context.Context, page int) {
	_ = r.storage.KVSet(ctx, yespornIngestCursorKey, strconv.Itoa(page), nil)
}

// YespornEnrich scans yesporn_entries that have not yet been detail-scraped and
// fills date/duration/poster/studios/tags/performers/description from the detail
// page (og: meta + channel links + JS player config). EnrichEntry sets
// detail_scraped on success and on a permanently-gone page (HTTP 410/404) so
// deleted posts do not livelock the newest-first sweep; a transient failure
// (5xx/CF/timeout) leaves detail_scraped false so the entry is retried next tick.
func (r *Runner) YespornEnrich(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.yesporn == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	perTick := yespornEnrichPerTick
	if v, err := strconv.Atoi(os.Getenv("YESPORN_ENRICH_PER_TICK")); err == nil && v > 0 {
		perTick = v
	}
	concurrency := yespornEnrichConcurrency
	if v, err := strconv.Atoi(os.Getenv("YESPORN_ENRICH_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	missing, err := r.storage.GetYespornMissingDetail(ctx, perTick)
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
		go func(e models.YespornEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := r.yesporn.EnrichEntry(ctx, &e); err != nil {
				if err := r.storage.UpsertYespornEntry(ctx, e); err == nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
				return
			}
			// Success or gone: EnrichEntry filled the detail fields (date/duration,
			// poster, studios, tags, performers, description, detail_scraped). Persist
			// via the dedicated enrichment writer so the ingest sweep's $setOnInsert
			// contract keeps date (the ypv_recent sort key) and the rest safe from a
			// later listing re-walk.
			if err := r.storage.UpdateYespornEnrichment(ctx, e); err == nil {
				mu.Lock()
				enriched++
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	return map[string]interface{}{"success": true, "scanned": len(missing), "enriched": enriched, "failed": failed}, nil
}

// yespornGenrePrecompute builds the top-N studio/tag/performer option lists and
// writes them to Redis (cache.PrefixYespornGenres+"opts") for the manifest path.
// No-op when Redis is not configured.
func (r *Runner) yespornGenrePrecompute(ctx context.Context) map[string]interface{} {
	if r.storage == nil || r.redis == nil {
		return map[string]interface{}{"success": true, "skipped": true}
	}
	studios, err := r.storage.GetYespornTopStudios(ctx, yespornGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	tags, err := r.storage.GetYespornTopTags(ctx, yespornGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	performers, err := r.storage.GetYespornTopPerformers(ctx, yespornGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	blob, _ := json.Marshal(struct {
		Studios    []string `json:"studios"`
		Tags       []string `json:"tags"`
		Performers []string `json:"performers"`
	}{Studios: studios, Tags: tags, Performers: performers})
	if err := r.redis.Set(ctx, cache.PrefixYespornGenres+"opts", string(blob), 7*24*time.Hour); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	return map[string]interface{}{"success": true, "studios": len(studios), "tags": len(tags), "performers": len(performers)}
}