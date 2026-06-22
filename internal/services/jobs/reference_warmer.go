package jobs

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/internal/services/scraper"
)

const (
	refPages        = 4
	refPageSize     = 24
	refPageDelay    = time.Second
	refReadBatch    = 200
	refMagnetWarmMax = 48
	refMagnetConcurrency = 4
)

// ReferenceWarmer seeds and repairs PornRips metadata from the pornrips.to WordPress API.
func (r *Runner) ReferenceWarmer(ctx context.Context) (map[string]interface{}, error) {
	if r.redis == nil {
		return nil, fmt.Errorf("redis not configured")
	}
	ref := metadata.NewReferenceClient()

	store := newAddonRedisStore(r.redis)
	recent, err := warmReferenceRecent(ctx, store, ref, r.pornrips)
	if err != nil {
		return nil, err
	}
	sweep, err := sweepIncompleteReference(ctx, store, ref)
	if err != nil {
		return nil, err
	}
	stash, err := warmReferenceStashdb(ctx, store, r.cfg.Metadata.StashDBAPIKey, r.cfg.Metadata.StashDBAPIURL)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":          true,
		"recentScanned":    recent.scanned,
		"recentSeeded":     recent.seeded,
		"recentCompleted":  recent.completed,
		"magnetsWarmed":    recent.magnets,
		"sweepExamined":    sweep.examined,
		"sweepFixed":       sweep.fixed,
		"stashScanned":     stash.scanned,
		"stashMatched":     stash.matched,
		"stashSkipped":     stash.skipped,
	}, nil
}

type refRecentStats struct {
	scanned, seeded, completed, magnets int
}

func warmReferenceRecent(ctx context.Context, store *addonRedisStore, ref *metadata.ReferenceClient, pr *scraper.PornripsScraper) (refRecentStats, error) {
	var stats refRecentStats
	slugs := make([]string, 0, refPages*refPageSize)
	for p := 0; p < refPages; p++ {
		items, err := ref.FetchRecent(ctx, p*refPageSize)
		if err != nil {
			return stats, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			stats.scanned++
			slugs = append(slugs, item.Slug)
			switch r := upsertReferenceSlug(ctx, store, ref, item.Slug, item.Meta); r {
			case "seeded":
				stats.seeded++
			case "completed":
				stats.completed++
			}
		}
		if p < refPages-1 {
			if err := sleepCtx(ctx, refPageDelay); err != nil {
				return stats, err
			}
		}
	}
	if pr != nil {
		stats.magnets = prewarmPornripsMagnets(ctx, store, pr, slugs)
	}
	return stats, nil
}

func upsertReferenceSlug(ctx context.Context, store *addonRedisStore, ref *metadata.ReferenceClient, slug string, meta *metadata.ReferenceMeta) string {
	key := "pr:" + slug
	existing, _ := store.GetSharedMeta(ctx, "tpdb", key)
	shared := meta.ToSharedMeta()
	if existing == nil {
		_ = store.SetSharedMeta(ctx, "tpdb", key, referenceToShared(shared))
		_ = store.SetReferenceMeta(ctx, slug, meta, ttlReferenceMeta)
		return "seeded"
	}
	merged, changed := mergeReferenceFill(*existing, shared)
	if changed {
		_ = store.SetSharedMeta(ctx, "tpdb", key, merged)
		return "completed"
	}
	return "ok"
}

func referenceToShared(s metadata.SharedMetaFromReference) SharedMeta {
	return SharedMeta{
		Title:       s.Title,
		Poster:      s.Poster,
		Background:  s.Background,
		Description: s.Description,
		Year:        s.Year,
		Cast:        s.Cast,
		Genres:      s.Genres,
		Source:      s.Source,
	}
}

func mergeReferenceFill(existing SharedMeta, ref metadata.SharedMetaFromReference) (SharedMeta, bool) {
	merged := existing
	changed := false
	fill := func(field *string, val string) {
		if *field == "" && val != "" {
			*field = val
			changed = true
		}
	}
	fill(&merged.Title, ref.Title)
	fill(&merged.Poster, ref.Poster)
	fill(&merged.Background, ref.Background)
	fill(&merged.Description, ref.Description)
	fill(&merged.Year, ref.Year)
	if len(merged.Cast) == 0 && len(ref.Cast) > 0 {
		merged.Cast = ref.Cast
		changed = true
	}
	if len(merged.Genres) == 0 && len(ref.Genres) > 0 {
		merged.Genres = ref.Genres
		changed = true
	}
	return merged, changed
}

func isIncompleteShared(m *SharedMeta) bool {
	return m == nil || m.Poster == "" || m.Title == ""
}

func sweepIncompleteReference(ctx context.Context, store *addonRedisStore, ref *metadata.ReferenceClient) (struct{ examined, fixed int }, error) {
	out := struct{ examined, fixed int }{}
	scanLimit := envIntDefault("REF_WARMER_SCAN_LIMIT", 2000)
	maxFix := envIntDefault("REF_WARMER_MAX_FIX", 150)
	concurrency := envIntDefault("REF_WARMER_CONCURRENCY", 6)

	subkeys, err := store.ScanTPDBSharedKeys(ctx, "pr:", scanLimit)
	if err != nil || len(subkeys) == 0 {
		return out, err
	}
	out.examined = len(subkeys)

	incomplete := make([]string, 0)
	for i := 0; i < len(subkeys) && len(incomplete) < maxFix; i += refReadBatch {
		end := i + refReadBatch
		if end > len(subkeys) {
			end = len(subkeys)
		}
		batch := subkeys[i:end]
		entries, err := store.GetManyTPDBShared(ctx, batch)
		if err != nil {
			continue
		}
		for j, e := range entries {
			if isIncompleteShared(e) {
				incomplete = append(incomplete, batch[j])
			}
			if len(incomplete) >= maxFix {
				break
			}
		}
	}
	if len(incomplete) == 0 {
		return out, nil
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for _, subkey := range incomplete {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			slug := key[len("pr:"):]
			meta, err := ref.GetPornripsMeta(ctx, slug)
			if err != nil || meta == nil {
				return
			}
			existing, _ := store.GetSharedMeta(ctx, "tpdb", key)
			if existing == nil {
				return
			}
			merged, changed := mergeReferenceFill(*existing, meta.ToSharedMeta())
			if changed {
				_ = store.SetSharedMeta(ctx, "tpdb", key, merged)
				mu.Lock()
				out.fixed++
				mu.Unlock()
			}
		}(subkey)
	}
	wg.Wait()
	return out, nil
}

func warmReferenceStashdb(ctx context.Context, store *addonRedisStore, stashKey, stashURL string) (struct{ scanned, matched, skipped int }, error) {
	out := struct{ scanned, matched, skipped int }{}
	if stashKey == "" {
		return out, nil
	}

	scanLimit := envIntDefault("REF_WARMER_SCAN_LIMIT", 2000)
	maxPerRun := envIntDefault("REF_WARMER_STASHDB_MAX", 150)
	batchSize := envIntDefault("REF_WARMER_STASHDB_BATCH", 16)
	batchDelay := envMsDefault("REF_WARMER_STASHDB_DELAY_MS", 300*time.Millisecond)

	subkeys, err := store.ScanTPDBSharedKeys(ctx, "pr:", scanLimit)
	if err != nil || len(subkeys) == 0 {
		return out, err
	}

	type workItem struct {
		key  string
		term string
	}
	todo := make([]workItem, 0, maxPerRun)
	for i := 0; i < len(subkeys) && len(todo) < maxPerRun; i += refReadBatch {
		end := i + refReadBatch
		if end > len(subkeys) {
			end = len(subkeys)
		}
		batch := subkeys[i:end]
		entries, err := store.GetManyTPDBShared(ctx, batch)
		if err != nil {
			continue
		}
		hasStash, _ := store.ExistsManyShared(ctx, "stashdb", batch)
		for j, e := range entries {
			if e == nil || e.Title == "" {
				continue
			}
			if j < len(hasStash) && hasStash[j] {
				out.skipped++
				continue
			}
			todo = append(todo, workItem{key: batch[j], term: e.Title})
			if len(todo) >= maxPerRun {
				break
			}
		}
	}
	if len(todo) == 0 {
		return out, nil
	}

	stash := metadata.NewStashDBClient(stashURL, stashKey)
	for i := 0; i < len(todo); i += batchSize {
		end := i + batchSize
		if end > len(todo) {
			end = len(todo)
		}
		batch := todo[i:end]
		for _, item := range batch {
			out.scanned++
			meta, err := stash.SearchMetadata(ctx, item.term, "")
			if err != nil {
				continue
			}
			if meta != nil {
				_ = store.SetSharedMeta(ctx, "stashdb", item.key, normalizedToShared(meta))
				out.matched++
			}
		}
		if end < len(todo) {
			_ = sleepCtx(ctx, batchDelay)
		}
	}
	return out, nil
}

func envMsDefault(name string, def time.Duration) time.Duration {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return def
}

func prewarmPornripsMagnets(ctx context.Context, store *addonRedisStore, pr *scraper.PornripsScraper, slugs []string) int {
	limit := refMagnetWarmMax
	if len(slugs) < limit {
		limit = len(slugs)
	}
	todo := make([]string, 0, limit)
	for _, slug := range slugs[:limit] {
		has, _ := store.HasPornripsMagnet(ctx, slug)
		if !has {
			todo = append(todo, slug)
		}
	}
	if len(todo) == 0 {
		return 0
	}
	warmed := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, refMagnetConcurrency)
	for _, slug := range todo {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			url := pr.ResolveDownloadURL(ctx, "https://pornrips.to/"+s+"/")
			ttl := ttlPornripsMagnet
			if url == "" {
				ttl = 10 * time.Minute
			}
			_ = store.SetPornripsMagnet(ctx, s, url, ttl)
			if url != "" {
				mu.Lock()
				warmed++
				mu.Unlock()
			}
		}(slug)
	}
	wg.Wait()
	return warmed
}

func envIntDefault(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}
