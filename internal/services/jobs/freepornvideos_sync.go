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
	freepornvideosIngestCursorKey    = "freepornvideos_ingest_cursor"
	freepornvideosIngestPagesPerTick = 10
	freepornvideosIngestPageDelay    = 800 * time.Millisecond
	freepornvideosEnrichPerTick      = 50
	freepornvideosEnrichConcurrency  = 4
	freepornvideosGenreTopN          = 50
)

// FreepornvideosSync is the sole background job that discovers new FreePornVideos
// entries and populates Mongo with their source-scraped metadata. One tick runs
// three sweeps - ingest (latest-updates feed -> UpsertFreepornvideosEntry), enrich
// (detail-page JSON-LD + categories/network/duration), and genre precompute
// (top-N studios/tags/performers -> KV blob for the manifest) - and merges their
// result maps. No TPDB or StashDB: all metadata is scraped directly from
// freepornvideos.xxx.
func (r *Runner) FreepornvideosSync(ctx context.Context) (map[string]interface{}, error) {
	ingest, err := r.FreepornvideosIngest(ctx)
	enrich, enrichErr := r.FreepornvideosEnrich(ctx)
	genres := r.freepornvideosGenrePrecompute(ctx)
	out := mergeSyncResults(ingest, enrich)
	for k, v := range genres {
		out["genres_"+k] = v
	}
	if err != nil {
		return out, err
	}
	return out, enrichErr
}

// FreepornvideosIngest walks the FreePornVideos latest-updates feed forward and
// upserts every card into freepornvideos_entries. Resumable page cursor in KV;
// empty tail resets to 0 so the next tick re-walks from the top for new posts.
func (r *Runner) FreepornvideosIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.freepornvideos == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	pagesCap := freepornvideosIngestPagesPerTick
	if v, err := strconv.Atoi(os.Getenv("FREEPORNVIDEOS_INGEST_PAGES_PER_TICK")); err == nil && v > 0 {
		pagesCap = v
	}
	delay := freepornvideosIngestPageDelay
	if v, err := time.ParseDuration(os.Getenv("FREEPORNVIDEOS_INGEST_PAGE_DELAY")); err == nil && v > 0 {
		delay = v
	}
	page := r.loadFreepornvideosIngestCursor(ctx)
	upserted := 0
	pagesWalked := 0
	hitEmpty := false

	for pagesWalked < pagesCap {
		if err := ctx.Err(); err != nil {
			return freepornvideosIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		// /latest-updates/{N}/ is 1-indexed (page 0 404s). Offset by +1 so a fresh
		// cursor (0) fetches page 1, mirroring hentai_ingest.go's ListSeries(page+1).
		// Without this the first tick on a cold cursor would treat page 0's empty
		// response as end-of-feed and the collection would never populate.
		entries, err := r.freepornvideos.IngestPage(ctx, page+1)
		if err != nil {
			return freepornvideosIngestResults(upserted, pagesWalked, hitEmpty), err
		}
		if len(entries) == 0 {
			hitEmpty = true
			r.storeFreepornvideosIngestCursor(ctx, 0)
			return freepornvideosIngestResults(upserted, pagesWalked, hitEmpty), nil
		}
		for _, e := range entries {
			if e.VideoID == "" {
				continue
			}
			if err := r.storage.UpsertFreepornvideosEntry(ctx, e); err == nil {
				upserted++
			}
		}
		page++
		pagesWalked++
		if err := sleepCtx(ctx, delay); err != nil {
			return freepornvideosIngestResults(upserted, pagesWalked, hitEmpty), err
		}
	}
	r.storeFreepornvideosIngestCursor(ctx, page)
	return freepornvideosIngestResults(upserted, pagesWalked, hitEmpty), nil
}

func freepornvideosIngestResults(upserted, pages int, hitEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"upserted": upserted,
		"pages":    pages,
		"hitEmpty": hitEmpty,
	}
}

func (r *Runner) loadFreepornvideosIngestCursor(ctx context.Context) int {
	v, ok, err := r.storage.KVGet(ctx, freepornvideosIngestCursorKey)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Runner) storeFreepornvideosIngestCursor(ctx context.Context, page int) {
	_ = r.storage.KVSet(ctx, freepornvideosIngestCursorKey, strconv.Itoa(page), nil)
}

// FreepornvideosEnrich scans freepornvideos_entries that have not yet been
// detail-scraped and fills categories/network/description/duration/date from the
// detail-page JSON-LD + HTML. EnrichEntry sets detail_scraped on success and on a
// permanently-gone page (HTTP 410/404) so deleted posts do not livelock the
// newest-first sweep; a transient failure (5xx/CF/timeout) leaves detail_scraped
// false so the entry is retried next tick.
func (r *Runner) FreepornvideosEnrich(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.freepornvideos == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or scraper not configured"}, nil
	}
	perTick := freepornvideosEnrichPerTick
	if v, err := strconv.Atoi(os.Getenv("FREEPORNVIDEOS_ENRICH_PER_TICK")); err == nil && v > 0 {
		perTick = v
	}
	concurrency := freepornvideosEnrichConcurrency
	if v, err := strconv.Atoi(os.Getenv("FREEPORNVIDEOS_ENRICH_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	missing, err := r.storage.GetFreepornvideosMissingDetail(ctx, perTick)
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
		go func(e models.FreepornvideosEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := r.freepornvideos.EnrichEntry(ctx, &e); err != nil {
				if err := r.storage.UpsertFreepornvideosEntry(ctx, e); err == nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
				return
			}
			// Success or gone: EnrichEntry filled the detail fields (JSON-LD
			// uploadDate/duration, categories, network, description, detail_scraped).
			// Persist via the dedicated enrichment writer so the ingest sweep's
			// $setOnInsert contract keeps date (the fpv_recent sort key) and the
			// rest safe from a later listing re-walk.
			if err := r.storage.UpdateFreepornvideosEnrichment(ctx, e); err == nil {
				mu.Lock()
				enriched++
				mu.Unlock()
			}
		}(e)
	}
	wg.Wait()
	return map[string]interface{}{"success": true, "scanned": len(missing), "enriched": enriched, "failed": failed}, nil
}

// freepornvideosGenrePrecompute builds the top-N studio/tag/performer option
// lists and writes them to Redis (cache.PrefixFreepornvideosGenres+"opts") for
// the manifest path. No-op when Redis is not configured.
func (r *Runner) freepornvideosGenrePrecompute(ctx context.Context) map[string]interface{} {
	if r.storage == nil || r.redis == nil {
		return map[string]interface{}{"success": true, "skipped": true}
	}
	studios, err := r.storage.GetFreepornvideosTopStudios(ctx, freepornvideosGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	tags, err := r.storage.GetFreepornvideosTopTags(ctx, freepornvideosGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	performers, err := r.storage.GetFreepornvideosTopPerformers(ctx, freepornvideosGenreTopN)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	blob, _ := json.Marshal(struct {
		Studios    []string `json:"studios"`
		Tags       []string `json:"tags"`
		Performers []string `json:"performers"`
	}{Studios: studios, Tags: tags, Performers: performers})
	if err := r.redis.Set(ctx, cache.PrefixFreepornvideosGenres+"opts", string(blob), 7*24*time.Hour); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	return map[string]interface{}{"success": true, "studios": len(studios), "tags": len(tags), "performers": len(performers)}
}
