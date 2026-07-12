package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/metadata"
)

const (
	sukebeiCatalogPages       = 3
	sukebeiCatalogMaxEntries  = 60
	sukebeiResolveConcurrency = 3
	prefixSukebeiCatalog      = "cat:sb:v1:"
)

func buildSukebeiCatalogRedisKey(baseURL, catalogID string) string {
	return fmt.Sprintf("%s%s|%s|Porn|||0", prefixSukebeiCatalog, baseURL, catalogID)
}

func sukebeiCatalogTTL() time.Duration {
	return catalogTTL()
}

// runSukebeiCatalogCache scrapes Sukebei top/recent lists, resolves metadata via
// StashDB, and caches only matched rows to Redis + Mongo.
func (r *Runner) runSukebeiCatalogCache(ctx context.Context) (map[string]interface{}, error) {
	if r.cfg == nil || !r.cfg.Redis.Enabled || r.redis == nil {
		return map[string]interface{}{"skipped": true, "reason": "redis not enabled"}, nil
	}
	stashKey := r.cfg.Metadata.StashDBAPIKey
	if stashKey == "" {
		return map[string]interface{}{"skipped": true, "reason": "STASHDB_API_KEY not set"}, nil
	}
	if r.scrapers == nil {
		return map[string]interface{}{"success": false, "error": "scraper service not configured"}, fmt.Errorf("scraper service not configured")
	}

	baseURL := ResolveCatalogBaseURL(r.cfg)
	if baseURL == "" {
		return map[string]interface{}{"skipped": true, "reason": "base url not configured"}, nil
	}

	store := newAddonRedisStore(r.redis)
	stash := metadata.NewStashDBClient(r.cfg.Metadata.StashDBAPIURL, stashKey)

	results := map[string]interface{}{
		"catalogsCached": 0,
		"entriesCached":  0,
		"resolved":       0,
		"skipped":        0,
		"errors":         0,
	}

	log.Printf("[SukebeiCatalog] Starting cache job")

	for _, cat := range sukebeiCatalogIDs {
		raw, err := r.fetchSukebeiTorrents(ctx, cat.Sort)
		if err != nil {
			results["errors"] = results["errors"].(int) + 1
			log.Printf("[SukebeiCatalog] scrape %s: %v", cat.ID, err)
			continue
		}
		if len(raw) == 0 {
			results["skipped"] = results["skipped"].(int) + 1
			continue
		}

		entries, resolved := r.resolveSukebeiEntries(ctx, store, stash, raw)
		if len(entries) == 0 {
			results["skipped"] = results["skipped"].(int) + 1
			continue
		}

		key := buildSukebeiCatalogRedisKey(baseURL, cat.ID)
		if err := r.redis.SetCatalogJSON(ctx, key, entries, sukebeiCatalogTTL()); err != nil {
			results["errors"] = results["errors"].(int) + 1
			log.Printf("[SukebeiCatalog] redis %s: %v", cat.ID, err)
			continue
		}

		if blob, err := json.Marshal(entries); err == nil && r.storage != nil {
			_ = r.storage.SetSukebeiCatalog(ctx, cat.ID, blob)
		}

		for _, entry := range entries {
			t := entry.Torrent
			id := encodeItemID(t.InfoHash, t.Title, t.TorrentURL, "sukebei", t.DetailURL)
			_ = store.SetTorrent(ctx, id, TorrentRecord{
				ID:         id,
				Title:      t.Title,
				InfoHash:   t.InfoHash,
				MagnetLink: t.MagnetLink,
				TorrentURL: t.TorrentURL,
				DetailURL:  t.DetailURL,
				Website:    "sukebei",
				Seeders:    t.Seeders,
			})
		}

		results["catalogsCached"] = results["catalogsCached"].(int) + 1
		results["entriesCached"] = results["entriesCached"].(int) + len(entries)
		results["resolved"] = results["resolved"].(int) + resolved
		log.Printf("[SukebeiCatalog] Cached %d resolved entries for %s", len(entries), cat.ID)

		if err := sleepJitter(ctx); err != nil {
			return results, err
		}
	}

	return results, nil
}

func (r *Runner) fetchSukebeiTorrents(ctx context.Context, sortCode string) ([]models.Torrent, error) {
	seen := make(map[string]struct{})
	out := make([]models.Torrent, 0, sukebeiCatalogMaxEntries*2)
	for page := 1; page <= sukebeiCatalogPages; page++ {
		batch, err := r.scrapers.Browse(ctx, "sukebei", "0_0", page, sortCode, models.SearchOptions{})
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		for _, t := range batch {
			hash := extractMagnetHash(t.MagnetLink)
			if hash == "" {
				continue
			}
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			out = append(out, t)
		}
		if len(batch) == 0 {
			break
		}
		if err := sleepCtx(ctx, 800*time.Millisecond); err != nil {
			return out, err
		}
	}
	if sortCode == "7" {
		sort.Slice(out, func(i, j int) bool { return out[i].Seeders > out[j].Seeders })
	}
	if len(out) > sukebeiCatalogMaxEntries*2 {
		out = out[:sukebeiCatalogMaxEntries*2]
	}
	return out, nil
}

func (r *Runner) resolveSukebeiEntries(
	ctx context.Context,
	store *addonRedisStore,
	stash *metadata.StashDBClient,
	raw []models.Torrent,
) ([]SukebeiCatalogEntry, int) {
	type job struct {
		torrent CatalogTorrent
		metaID  string
	}
	jobs := make([]job, 0, len(raw))
	for _, t := range raw {
		ct := normalizeSukebeiTorrent(t)
		if ct.InfoHash == "" {
			continue
		}
		metaID := StableMetaID("sukebei", ct.DetailURL, ct.InfoHash)
		if metaID == "" {
			continue
		}
		jobs = append(jobs, job{torrent: ct, metaID: metaID})
	}

	entries := make([]SukebeiCatalogEntry, 0, sukebeiCatalogMaxEntries)
	var resolved atomic.Int32
	var mu sync.Mutex
	sem := make(chan struct{}, sukebeiResolveConcurrency)
	var wg sync.WaitGroup

	for _, item := range jobs {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			stashMeta, ok := r.loadStashMeta(ctx, store, item.metaID)
			if !ok {
				meta, err := stash.SearchMetadata(ctx, item.torrent.Title, item.torrent.DetailURL)
				if err != nil || meta == nil {
					return
				}
				shared := normalizedToShared(meta)
				_ = store.SetSharedMeta(ctx, "stashdb", item.metaID, shared)
				if r.storage != nil {
					_ = r.storage.SetSharedMeta(ctx, "stashdb", item.metaID, sharedToPayload(&shared))
				}
				stashMeta = &shared
			}

			name := strings.TrimSpace(stashMeta.Title)
			if name == "" {
				name = item.torrent.Title
			}
			id := encodeItemID(item.torrent.InfoHash, item.torrent.Title, item.torrent.TorrentURL, "sukebei", item.torrent.DetailURL)
			preview := StremioMetaPreview{
				ID:          id,
				Type:        "Porn",
				Name:        name,
				Poster:      stashMeta.Poster,
				Background:  pickBackgroundShared(stashMeta),
				Description: stashMeta.Description,
				ReleaseInfo: stashMeta.Year,
				PosterShape: "landscape",
			}

			mu.Lock()
			defer mu.Unlock()
			if len(entries) >= sukebeiCatalogMaxEntries {
				return
			}
			entries = append(entries, SukebeiCatalogEntry{Meta: preview, Torrent: item.torrent})
			resolved.Add(1)
		}()
	}
	wg.Wait()
	return entries, int(resolved.Load())
}

func (r *Runner) loadStashMeta(ctx context.Context, store *addonRedisStore, metaID string) (*SharedMeta, bool) {
	if shared, err := store.GetSharedMeta(ctx, "stashdb", metaID); err == nil && shared != nil {
		return shared, true
	}
	if r.storage == nil {
		return nil, false
	}
	_, stash, err := r.storage.GetSharedMetaPair(ctx, metaID)
	if err != nil || stash == nil {
		return nil, false
	}
	shared := payloadToShared(stash)
	if shared == nil {
		return nil, false
	}
	_ = store.SetSharedMeta(ctx, "stashdb", metaID, *shared)
	return shared, true
}

func normalizeSukebeiTorrent(t models.Torrent) CatalogTorrent {
	detail := strings.TrimSpace(t.UploadedBy)
	if detail == "" {
		detail = strings.TrimSpace(t.TorrentURL)
	}
	return CatalogTorrent{
		Title:      t.Name,
		Size:       t.Size,
		Seeders:    t.Seeders,
		Leechers:   t.Leechers,
		InfoHash:   extractMagnetHash(t.MagnetLink),
		MagnetLink: t.MagnetLink,
		TorrentURL: t.TorrentURL,
		DetailURL:  detail,
		Website:    "sukebei",
		Indexer:    "sukebei",
	}
}

func pickBackgroundShared(m *SharedMeta) string {
	if m == nil {
		return ""
	}
	if m.Background != "" {
		return m.Background
	}
	return m.Poster
}
