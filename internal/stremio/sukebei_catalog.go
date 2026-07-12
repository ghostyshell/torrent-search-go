package stremio

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/jobs"
)

const (
	sukebeiOnDemandPages       = 3
	sukebeiOnDemandMaxEntries  = 60
	sukebeiOnDemandConcurrency = 3
	sukebeiOnDemandTimeout     = 35 * time.Second
)

var sukebeiCatalogSorts = map[string]string{
	"sukebei_top":    "7",
	"sukebei_recent": "3",
}

func sukebeiManifestCatalogs(disabled map[string]struct{}, homeGenre []map[string]interface{}, enabledSorts []string) []map[string]interface{} {
	allowedSorts := map[string]struct{}{"top": {}, "recent": {}}
	if len(enabledSorts) > 0 {
		allowedSorts = make(map[string]struct{}, len(enabledSorts))
		for _, s := range enabledSorts {
			allowedSorts[s] = struct{}{}
		}
	}
	defs := []struct {
		id   string
		name string
		sort string
	}{
		{id: "sukebei_top", name: "Sukebei · Top", sort: "top"},
		{id: "sukebei_recent", name: "Sukebei · Recent", sort: "recent"},
	}
	out := make([]map[string]interface{}, 0, len(defs))
	opt := map[string]interface{}{"isRequired": false}
	for _, c := range defs {
		if _, off := disabled["sukebei"]; off {
			continue
		}
		if _, ok := allowedSorts[c.sort]; !ok {
			continue
		}
		out = append(out, map[string]interface{}{
			"type": "Porn",
			"id":   c.id,
			"name": c.name,
			"extra": []map[string]interface{}{
				mergeExtra(map[string]interface{}{"name": "search"}, opt),
				mergeExtra(map[string]interface{}{"name": "skip"}, opt),
			},
		})
	}
	if len(out) > 0 && len(homeGenre) > 0 {
		for i := range out {
			extras := out[i]["extra"].([]map[string]interface{})
			out[i]["extra"] = append(extras, homeGenre...)
		}
	}
	return out
}

func (h *Handler) serveSukebeiCatalog(ctx context.Context, cfg Config, catalogID, searchQ string, skip, maxResults int) (CatalogResponse, error) {
	if _, ok := sukebeiCatalogSorts[catalogID]; !ok {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if maxResults <= 0 {
		maxResults = cfg.MaxResults
	}
	if maxResults <= 0 {
		maxResults = 20
	}

	var entries []jobs.SukebeiCatalogEntry
	var err error
	if strings.TrimSpace(searchQ) != "" {
		entries, err = h.loadSukebeiSearchEntries(ctx, cfg, catalogID, searchQ)
	} else {
		entries, err = h.loadSukebeiCatalogEntries(ctx, cfg, catalogID)
	}
	if err != nil || len(entries) == 0 {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	if skip >= len(entries) {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	end := skip + maxResults
	if end > len(entries) {
		end = len(entries)
	}
	page := entries[skip:end]

	store := newRedisStore(h.Redis)
	metas := make([]MetaPreview, 0, len(page))
	for _, entry := range page {
		rec := sukebeiTorrentToRecord(entry.Torrent)
		id := EncodeItemID(rec)
		rec.ID = id
		if store != nil {
			_ = store.setTorrent(ctx, id, rec)
			if entry.Meta.Poster != "" {
				metaID := StableMetaID("sukebei", rec.DetailURL, rec.InfoHash)
				if metaID != "" {
					_ = store.setSharedMeta(ctx, "stashdb", metaID, jobs.SharedMeta{
						Title:       entry.Meta.Name,
						Poster:      entry.Meta.Poster,
						Background:  entry.Meta.Background,
						Description: entry.Meta.Description,
						Year:        entry.Meta.ReleaseInfo,
						Source:      "stashdb",
					})
				}
			}
		}
		m := entry.Meta
		m.ID = id
		metas = append(metas, MetaPreview{
			ID:          m.ID,
			Type:        m.Type,
			Name:        m.Name,
			Poster:      m.Poster,
			Background:  m.Background,
			Description: m.Description,
			ReleaseInfo: m.ReleaseInfo,
			PosterShape: m.PosterShape,
		})
	}
	return CatalogResponse{Metas: metas}, nil
}

func (h *Handler) loadSukebeiCatalogEntries(ctx context.Context, cfg Config, catalogID string) ([]jobs.SukebeiCatalogEntry, error) {
	baseURL := h.catalogBaseURL(cfg)
	key := prefixSukebeiCatalog + baseURL + "|" + catalogID + "|Porn|||0"

	store := newRedisStore(h.Redis)
	if store != nil {
		if entries, err := store.getSukebeiCatalogEntries(ctx, key); err == nil && len(entries) > 0 {
			return entries, nil
		}
	}

	if h.CatalogStore != nil {
		if blob, ok, err := h.CatalogStore.GetSukebeiCatalog(ctx, catalogID); err == nil && ok {
			var entries []jobs.SukebeiCatalogEntry
			if json.Unmarshal(blob, &entries) == nil && len(entries) > 0 {
				if store != nil {
					_ = store.setSukebeiCatalogEntries(ctx, key, entries)
				}
				return entries, nil
			}
		}
	}

	stashKey, _ := resolveStashdbCredentials(cfg, h.Env)
	if stashKey == "" || h.Scrapers == nil {
		return nil, nil
	}

	sortCode := sukebeiCatalogSorts[catalogID]
	entries, err := h.fetchSukebeiCatalogOnDemand(ctx, cfg, sortCode)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	if store != nil {
		_ = store.setSukebeiCatalogEntries(ctx, key, entries)
	}
	return entries, nil
}

func (h *Handler) loadSukebeiSearchEntries(ctx context.Context, cfg Config, catalogID, searchQ string) ([]jobs.SukebeiCatalogEntry, error) {
	searchQ = strings.TrimSpace(searchQ)
	if searchQ == "" {
		return nil, nil
	}

	baseURL := h.catalogBaseURL(cfg)
	key := prefixSukebeiCatalog + baseURL + "|" + catalogID + "|search|" + searchQ

	store := newRedisStore(h.Redis)
	if store != nil {
		if entries, err := store.getSukebeiCatalogEntries(ctx, key); err == nil && len(entries) > 0 {
			return entries, nil
		}
	}

	stashKey, _ := resolveStashdbCredentials(cfg, h.Env)
	if stashKey == "" || h.Scrapers == nil {
		return nil, nil
	}

	rctx, cancel := context.WithTimeout(ctx, sukebeiOnDemandTimeout)
	defer cancel()

	terms := h.resolveSearchTerms(rctx, cfg, searchQ)
	raw, err := h.searchSukebeiTerms(rctx, terms, 1, models.SearchOptions{Sort: "7"})
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	raw = dedupeSukebeiTorrents(raw)
	sort.Slice(raw, func(i, j int) bool { return raw[i].Seeders > raw[j].Seeders })

	entries := h.resolveSukebeiTorrents(rctx, cfg, raw, sukebeiOnDemandMaxEntries)
	if store != nil && len(entries) > 0 {
		_ = store.setSukebeiCatalogEntries(ctx, key, entries)
	}
	return entries, nil
}

func dedupeSukebeiTorrents(raw []models.Torrent) []models.Torrent {
	seen := make(map[string]struct{}, len(raw))
	out := make([]models.Torrent, 0, len(raw))
	for _, t := range raw {
		hash := ExtractInfoHash(t.MagnetLink)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, t)
	}
	return out
}

func (h *Handler) fetchSukebeiCatalogOnDemand(ctx context.Context, cfg Config, sortCode string) ([]jobs.SukebeiCatalogEntry, error) {
	rctx, cancel := context.WithTimeout(ctx, sukebeiOnDemandTimeout)
	defer cancel()

	raw, err := h.fetchSukebeiTorrents(rctx, sortCode)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	return h.resolveSukebeiTorrents(rctx, cfg, raw, sukebeiOnDemandMaxEntries), nil
}

func (h *Handler) resolveSukebeiTorrents(ctx context.Context, cfg Config, raw []models.Torrent, maxEntries int) []jobs.SukebeiCatalogEntry {
	if maxEntries <= 0 {
		maxEntries = sukebeiOnDemandMaxEntries
	}

	store := newRedisStore(h.Redis)

	type job struct {
		torrent jobs.CatalogTorrent
		metaID  string
	}
	jobsList := make([]job, 0, len(raw))
	for _, t := range raw {
		ct := normalizeSukebeiTorrent(t)
		if ct.InfoHash == "" {
			continue
		}
		metaID := StableMetaID("sukebei", ct.DetailURL, ct.InfoHash)
		if metaID == "" {
			continue
		}
		jobsList = append(jobsList, job{torrent: ct, metaID: metaID})
	}

	entries := make([]jobs.SukebeiCatalogEntry, 0, maxEntries)
	var mu sync.Mutex
	sem := make(chan struct{}, sukebeiOnDemandConcurrency)
	var wg sync.WaitGroup

	for _, item := range jobsList {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			stashMeta := h.loadStashMeta(ctx, cfg, store, item.metaID, item.torrent.Title, item.torrent.DetailURL)
			if stashMeta == nil {
				return
			}

			name := strings.TrimSpace(stashMeta.Title)
			if name == "" {
				name = item.torrent.Title
			}
			id := EncodeItemID(sukebeiTorrentToRecord(item.torrent))
			preview := jobs.StremioMetaPreview{
				ID:          id,
				Type:        "Porn",
				Name:        name,
				Poster:      stashMeta.Poster,
				Background:  stashBackground(stashMeta),
				Description: stashMeta.Description,
				ReleaseInfo: stashMeta.Year,
				PosterShape: "landscape",
			}

			mu.Lock()
			defer mu.Unlock()
			if len(entries) >= maxEntries {
				return
			}
			entries = append(entries, jobs.SukebeiCatalogEntry{Meta: preview, Torrent: item.torrent})
		}()
	}
	wg.Wait()
	return entries
}

func (h *Handler) fetchSukebeiTorrents(ctx context.Context, sortCode string) ([]models.Torrent, error) {
	seen := make(map[string]struct{})
	out := make([]models.Torrent, 0, sukebeiOnDemandMaxEntries*2)
	for page := 1; page <= sukebeiOnDemandPages; page++ {
		batch, err := h.Scrapers.Browse(ctx, "sukebei", "0_0", page, sortCode, models.SearchOptions{})
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		for _, t := range batch {
			hash := ExtractInfoHash(t.MagnetLink)
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
	}
	if sortCode == "7" {
		sort.Slice(out, func(i, j int) bool { return out[i].Seeders > out[j].Seeders })
	}
	if len(out) > sukebeiOnDemandMaxEntries*2 {
		out = out[:sukebeiOnDemandMaxEntries*2]
	}
	return out, nil
}

func normalizeSukebeiTorrent(t models.Torrent) jobs.CatalogTorrent {
	detail := strings.TrimSpace(t.UploadedBy)
	if detail == "" {
		detail = strings.TrimSpace(t.TorrentURL)
	}
	return jobs.CatalogTorrent{
		Title:      t.Name,
		Size:       t.Size,
		Seeders:    t.Seeders,
		Leechers:   t.Leechers,
		InfoHash:   ExtractInfoHash(t.MagnetLink),
		MagnetLink: t.MagnetLink,
		TorrentURL: t.TorrentURL,
		DetailURL:  detail,
		Website:    "sukebei",
		Indexer:    "sukebei",
	}
}

func sukebeiTorrentToRecord(t jobs.CatalogTorrent) TorrentRecord {
	website := t.Website
	if website == "" {
		website = "sukebei"
	}
	indexer := t.Indexer
	if indexer == "" {
		indexer = website
	}
	return TorrentRecord{
		Title:      t.Title,
		Size:       t.Size,
		Seeders:    t.Seeders,
		Leechers:   t.Leechers,
		InfoHash:   t.InfoHash,
		MagnetLink: t.MagnetLink,
		TorrentURL: t.TorrentURL,
		DetailURL:  t.DetailURL,
		Website:    website,
		Indexer:    indexer,
		CoverImage: t.CoverImage,
	}
}
