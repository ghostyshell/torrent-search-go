package jobs

import (
	"context"
	"os"
	"strconv"
	"time"

	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/pkg/models"
)

const (
	pornripsIngestCursorKey    = "pornrips_ingest_cursor"
	pornripsIngestPagesPerTick = 10 // 240 entries/tick at 24/page
	pornripsIngestPageDelay    = 800 * time.Millisecond
)

// PornripsIngest walks the PornRips WordPress feed forward and upserts every
// post into the durable pornrips_entries Mongo collection. Re-walking re-upserts
// listing fields ($set) but leaves enrichment intact ($setOnInsert in UpsertPornripsEntry),
// so an already-TPDB-enriched entry is not clobbered. A resumable cursor (skip) is
// stored in the cache KV collection; when the walk hits the empty tail (HTTP 400,
// the WP end-of-feed signal) the cursor resets to 0 so the next tick re-walks from
// the top and picks up newly published posts.
func (r *Runner) PornripsIngest(ctx context.Context) (map[string]interface{}, error) {
	if r.storage == nil {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "mongo not configured"}, nil
	}
	ref := metadata.NewReferenceClient()

	// PORNRIPS_INGEST_RECENT_PAGES>0 = "recent-only" mode: ignore the deep cursor,
	// walk the top N pages each tick to pick up newly published posts, and do not touch
	// the shared cursor (no deep re-walk). Default 0 = the original full-archive walk
	// that resumes from the cursor and advances it. The bulk-fill one-shot leaves it
	// unset; the post-fill deployed job sets 1-10.
	recentPages := 0
	if v, err := strconv.Atoi(os.Getenv("PORNRIPS_INGEST_RECENT_PAGES")); err == nil && v > 0 {
		recentPages = v
	}
	pagesCap := pornripsIngestPagesPerTick
	skip := r.loadPornripsIngestCursor(ctx)
	if recentPages > 0 {
		skip = 0
		pagesCap = recentPages
	}
	upserted := 0
	pagesWalked := 0
	hitEmpty := false

	for pagesWalked < pagesCap {
		if err := ctx.Err(); err != nil {
			return r.ingestResults(upserted, pagesWalked, hitEmpty), err
		}
		items, err := ref.FetchRecent(ctx, skip)
		if err != nil {
			return r.ingestResults(upserted, pagesWalked, hitEmpty), err
		}
		if len(items) == 0 {
			// End of feed. Only the full-archive walk resets the cursor so the next tick
			// re-walks from the top; recent-only mode never touches it.
			if recentPages == 0 {
				hitEmpty = true
				r.storePornripsIngestCursor(ctx, 0)
			}
			return r.ingestResults(upserted, pagesWalked, hitEmpty), nil
		}
		for _, item := range items {
			if item.Slug == "" {
				continue
			}
			entry := pornripsEntryFromItem(item)
			if err := r.storage.UpsertPornripsEntry(ctx, entry); err == nil {
				upserted++
			}
		}
		skip += len(items)
		pagesWalked++
		if err := sleepCtx(ctx, pornripsIngestPageDelay); err != nil {
			return r.ingestResults(upserted, pagesWalked, hitEmpty), err
		}
	}

	// Advance the cursor for the next tick only in full-archive mode; recent-only mode
	// leaves the shared cursor alone.
	if recentPages == 0 {
		r.storePornripsIngestCursor(ctx, skip)
	}
	return r.ingestResults(upserted, pagesWalked, hitEmpty), nil
}

func pornripsEntryFromItem(item metadata.ReferenceRecentItem) models.PornripsEntry {
	e := models.PornripsEntry{
		Slug:      item.Slug,
		DetailURL: "https://pornrips.to/" + item.Slug + "/",
		MetaID:    "pr:" + item.Slug,
	}
	if item.Meta != nil {
		e.Title = item.Meta.Name
		e.WpPoster = item.Meta.Poster
		// Studio comes from the WP post_tag (the site name), set at ingest so
		// pr_studio works for every entry without waiting for TPDB enrichment.
		e.Studio = item.Meta.Studio
		e.StudioNorm = models.NormToken(item.Meta.Studio)
	}
	// SceneGroup groups every resolution variant of one scene (720p/1080p/4K rips
	// of the same WP post title) so the catalog emits one jstrg: entry with one
	// stream per variant. Empty title -> "pr:"+slug so each such doc is its own
	// group and never collapses with another.
	e.SceneGroup = models.PornripsSceneGroup(e.Title)
	if e.SceneGroup == "" {
		e.SceneGroup = "pr:" + e.Slug
	}
	e.Date = item.Date // full WP post date for the date -1 sort index
	return e
}

func (r *Runner) ingestResults(upserted, pages int, hitEmpty bool) map[string]interface{} {
	return map[string]interface{}{
		"success":    true,
		"upserted":   upserted,
		"pages":      pages,
		"hitEmpty":  hitEmpty,
	}
}

func (r *Runner) loadPornripsIngestCursor(ctx context.Context) int {
	v, ok, err := r.storage.KVGet(ctx, pornripsIngestCursorKey)
	if err != nil || !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (r *Runner) storePornripsIngestCursor(ctx context.Context, skip int) {
	_ = r.storage.KVSet(ctx, pornripsIngestCursorKey, strconv.Itoa(skip), nil) // nil TTL = durable cursor
}