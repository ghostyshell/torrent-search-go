package jobs

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"time"

	"torrent-search-go/internal/services/hentai"
	"torrent-search-go/pkg/models"
)

const (
	hentaiIngestCursorKey    = "hentai_ingest_cursor"
	hentaiIngestPagesPerTick = 10
	hentaiIngestPageDelay    = 600 * time.Millisecond
)

// hentaiIngestCursor is the resumable HentaiMama cursor: the next listing
// page to walk (0 = not started / reset). Stored as JSON in the KV collection.
type hentaiIngestCursor struct {
	Hmm int `json:"hmm"`
}

// HentaiIngest walks HentaiMama (paged listing + per-series fetch) and upserts
// every series into the durable hentai_entries Mongo collection. Re-walking
// re-upserts listing fields ($set). HentaiTV was removed (its r2 CDN 403s from
// the backend and it was never part of the configured source scope).
//
// HENTAI_INGEST_RECENT_PAGES>0 = recent-only mode (post-fill deployed job):
// ignore the deep cursor, walk the top N HentaiMama listing pages each tick,
// and do not touch the shared cursor. Default 0 = full-archive walk that
// resumes from the cursor and advances it (the bulk-fill one-shot).
func (r *Runner) HentaiIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil || r.hentai == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo or hentai service not configured"}, nil
	}

	recentPages := 0
	if v, err := strconv.Atoi(os.Getenv("HENTAI_INGEST_RECENT_PAGES")); err == nil && v > 0 {
		recentPages = v
	}
	cur := r.loadHentaiIngestCursor(ctx)
	if recentPages > 0 {
		cur = hentaiIngestCursor{}
	}

	upserted, pagesWalked := 0, 0

	// HentaiMama: paged listing walk with a resumable cursor.
	hmmUp, hmmPages, hmmEmpty, hmmNext := r.ingestMama(ctx, cur.Hmm, recentPages)
	upserted += hmmUp
	pagesWalked += hmmPages
	if recentPages == 0 {
		cur.Hmm = hmmNext
		r.storeHentaiIngestCursor(ctx, cur)
	}
	return r.hentaiIngestResults(upserted, pagesWalked, hmmEmpty), nil
}

// ingestMama walks HentaiMama listing pages starting at startPage, fetching and
// upserting each series. Returns upserts, pages walked, whether the empty tail
// was reached, and the next page to resume from.
func (r *Runner) ingestMama(ctx context.Context, startPage, recentPages int) (upserted, pages int, empty bool, nextPage int) {
	pagesCap := hentaiIngestPagesPerTick
	page := startPage
	if recentPages > 0 {
		pagesCap = recentPages
		page = 0
	}
	for walked := 0; walked < pagesCap; walked++ {
		if err := ctx.Err(); err != nil {
			return
		}
		items, err := r.hentai.ListSeries(ctx, "hentaimama", page+1)
		if err != nil {
			return
		}
		if len(items) == 0 {
			if recentPages == 0 {
				empty = true
				nextPage = 0
			}
			return
		}
		for _, item := range items {
			if item.Slug == "" {
				continue
			}
			if err := r.hentaiIngestOne(ctx, "hmm", "hentaimama", item.Slug); err == nil {
				upserted++
			}
			if err := sleepCtx(ctx, hentaiIngestPageDelay/2); err != nil {
				return
			}
		}
		page++
		pages++
		if err := sleepCtx(ctx, hentaiIngestPageDelay); err != nil {
			return
		}
	}
	nextPage = page
	return
}

// hentaiIngestOne fetches one HentaiMama series' full detail and upserts it.
func (r *Runner) hentaiIngestOne(ctx context.Context, prefix, source, slug string) error {
	d, err := r.hentai.FetchSeries(ctx, source, slug)
	if err != nil || d == nil {
		return err
	}
	return r.hentaiUpsertDetail(ctx, prefix, d)
}

// hentaiUpsertDetail maps a scraped SeriesDetail to a HentaiEntry and upserts it.
func (r *Runner) hentaiUpsertDetail(ctx context.Context, prefix string, d *hentai.SeriesDetail) error {
	eps := make([]models.HentaiEpisode, 0, len(d.Episodes))
	for _, ep := range d.Episodes {
		eps = append(eps, models.HentaiEpisode{
			Number:    ep.Number,
			Title:     ep.Title,
			Slug:      ep.Slug,
			SourceURL: ep.SourceURL,
			Thumbnail: ep.Thumbnail,
			Released:  ep.Released,
		})
	}
	return r.storage.UpsertHentaiEntry(ctx, models.HentaiEntry{
		ID:          hentai.ID(prefix, d.Slug),
		Prefix:      prefix,
		Slug:        d.Slug,
		Source:      d.Source,
		Title:       d.Title,
		Poster:      d.Poster,
		Background:  d.Background,
		Excerpt:     d.Excerpt,
		ReleaseYear: d.ReleaseYear,
		Studio:      d.Studio,
		Genres:      d.Genres,
		Rating:      d.Rating,
		RatingSrc:   d.RatingSrc,
		DetailURL:   d.DetailURL,
		Episodes:    eps,
	})
}

func (r *Runner) hentaiIngestResults(upserted, pages int, hmmEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"upserted": upserted,
		"pages":    pages,
		"hmmEmpty": hmmEmpty,
		"hitEmpty": hmmEmpty,
	}
}

func (r *Runner) loadHentaiIngestCursor(ctx context.Context) hentaiIngestCursor {
	v, ok, err := r.storage.KVGet(ctx, hentaiIngestCursorKey)
	if err != nil || !ok {
		return hentaiIngestCursor{}
	}
	var c hentaiIngestCursor
	if json.Unmarshal([]byte(v), &c) != nil {
		return hentaiIngestCursor{}
	}
	return c
}

func (r *Runner) storeHentaiIngestCursor(ctx context.Context, c hentaiIngestCursor) {
	if data, err := json.Marshal(c); err == nil {
		_ = r.storage.KVSet(ctx, hentaiIngestCursorKey, string(data), nil)
	}
}