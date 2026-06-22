package stremio

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/metadata"
)

type pornripsParams struct {
	Website  string
	Category string
	Query    string
}

var pornripsQualityRE = regexp.MustCompile(`(?i)\b(?:480p|540p|720p|1080p|1440p|2160p|4k|uhd|hevc|x265|x264|h\.?265|h\.?264|prt)\b`)

// normalizePornripsSearchQuery turns dotted release filenames into word queries
// that pornrips.to's WordPress search can match.
func normalizePornripsSearchQuery(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return q
	}
	if strings.Count(q, ".") >= 3 {
		q = strings.ReplaceAll(q, ".", " ")
		q = regexp.MustCompile(`\s+`).ReplaceAllString(q, " ")
	}
	return strings.TrimSpace(q)
}

func getPornripsParams(catalogID, genre, searchQ string) *pornripsParams {
	if !strings.HasPrefix(catalogID, "pr_") {
		return nil
	}
	g := strings.TrimSpace(genre)
	if g == "All" {
		g = ""
	}
	s := strings.TrimSpace(searchQ)
	query := ""
	switch catalogID {
	case "pr_search":
		query = s
	default:
		parts := make([]string, 0, 2)
		if g != "" {
			parts = append(parts, g)
		}
		if s != "" {
			parts = append(parts, s)
		}
		query = strings.Join(parts, " ")
	}
	return &pornripsParams{Website: "pornrips", Category: "all", Query: query}
}

func dedupePornripsCatalog(torrents []catalogTorrent) []catalogTorrent {
	keyOf := func(t catalogTorrent) string {
		title := strings.ToLower(t.Title)
		title = pornripsQualityRE.ReplaceAllString(title, "")
		title = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(title, " ")
		return strings.TrimSpace(title)
	}
	rank := func(t catalogTorrent) int {
		title := t.Title
		switch {
		case regexp.MustCompile(`(?i)\b(?:2160p|4k|uhd)\b`).MatchString(title):
			return 3
		case regexp.MustCompile(`(?i)\b(?:1080p|1440p)\b`).MatchString(title):
			return 2
		default:
			return 1
		}
	}

	seen := make(map[string]catalogTorrent)
	order := make([]string, 0, len(torrents))
	for _, t := range torrents {
		k := keyOf(t)
		if k == "" {
			k = t.DetailURL
		}
		if k == "" {
			k = t.Title
		}
		if k == "" {
			continue
		}
		if existing, ok := seen[k]; ok {
			if rank(t) > rank(existing) {
				seen[k] = t
			}
			continue
		}
		seen[k] = t
		order = append(order, k)
	}
	out := make([]catalogTorrent, 0, len(order))
	for _, k := range order {
		out = append(out, seen[k])
	}
	return out
}

func (h *Handler) servePornripsCatalog(ctx context.Context, cfg Config, catalogID, contentType, genre, searchQ string, skip int) (CatalogResponse, error) {
	params := getPornripsParams(catalogID, genre, searchQ)
	if params == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if catalogID == "pr_search" && strings.TrimSpace(searchQ) == "" {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	store := newRedisStore(h.Redis)
	cacheKey := buildPornripsCatalogKey(catalogID, contentType, searchQ, genre, skip)
	if store != nil {
		if cached, err := store.getTorrentList(ctx, prefixPornripsCatalog+cacheKey); err == nil && len(cached) > 0 {
			torrents := dedupePornripsCatalog(cached)
			max := cfg.MaxResults
			if max <= 0 {
				max = 20
			}
			if len(torrents) > max {
				torrents = torrents[:max]
			}
			metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "")
			return CatalogResponse{Metas: metas}, err
		}
	}

	var torrents []catalogTorrent
	if params.Query != "" {
		torrents = h.fetchPornripsQuery(ctx, cfg, catalogID, genre, searchQ, skip)
	} else {
		torrents = h.fetchPornripsBrowse(ctx, skip)
	}
	torrents = dedupePornripsCatalog(torrents)
	max := cfg.MaxResults
	if max <= 0 {
		max = 20
	}
	if len(torrents) > max {
		torrents = torrents[:max]
	}
	if store != nil && len(torrents) > 0 {
		_ = store.setTorrentList(ctx, prefixPornripsCatalog+cacheKey, torrents, ttlCatalogList)
	}

	metas, err := h.buildMetas(ctx, cfg, torrents, contentType, "")
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	return CatalogResponse{Metas: metas}, nil
}

func (h *Handler) fetchPornripsQuery(ctx context.Context, cfg Config, catalogID, genre, searchQ string, skip int) []catalogTorrent {
	if torrents := h.fetchPornripsReferenceCatalog(ctx, catalogID, genre, searchQ, skip); len(torrents) > 0 {
		return torrents
	}
	max := cfg.MaxResults
	if max <= 0 {
		max = 20
	}
	if torrents := h.fetchPornripsBrowseSearch(ctx, searchQ, skip, max); len(torrents) > 0 {
		return torrents
	}
	return h.fetchPornripsScrapeSearch(ctx, catalogID, genre, searchQ, skip)
}

func (h *Handler) fetchPornripsScrapeSearch(ctx context.Context, catalogID, genre, searchQ string, skip int) []catalogTorrent {
	if h.Scrapers == nil {
		return nil
	}
	params := getPornripsParams(catalogID, genre, searchQ)
	if params == nil || params.Query == "" {
		return nil
	}
	page := skip/30 + 1
	if page < 1 {
		page = 1
	}
	query := normalizePornripsSearchQuery(params.Query)
	raw, err := h.Scrapers.Search(ctx, "pornrips", query, page, models.SearchOptions{})
	if err != nil || len(raw) == 0 {
		return nil
	}
	out := make([]catalogTorrent, 0, len(raw))
	for _, t := range raw {
		out = append(out, normalizeModelTorrent(t))
	}
	return out
}

func (h *Handler) fetchPornripsReferenceCatalog(ctx context.Context, catalogID, genre, searchQ string, skip int) []catalogTorrent {
	if h.Reference == nil || !h.Reference.Enabled() {
		return nil
	}
	refCat := "studio"
	value := genre
	switch catalogID {
	case "pr_tag":
		refCat = "tag"
	case "pr_search":
		refCat = "search"
		value = searchQ
	}
	items, err := h.Reference.FetchPornripsCatalog(ctx, refCat, value, skip)
	if err != nil || len(items) == 0 {
		return nil
	}
	out := recentItemsToCatalog(items)
	h.warmReferenceMetaBackground(items)
	return out
}

func (h *Handler) fetchPornripsBrowse(ctx context.Context, skip int) []catalogTorrent {
	if h.Scrapers != nil {
		page := skip/30 + 1
		raw, err := h.Scrapers.Browse(ctx, "pornrips", "all", page, "", models.SearchOptions{})
		if err == nil && len(raw) > 0 {
			out := make([]catalogTorrent, 0, len(raw))
			for _, t := range raw {
				out = append(out, normalizeModelTorrent(t))
			}
			return out
		}
	}
	return h.fetchPornripsReferenceRecent(ctx, skip)
}

func (h *Handler) fetchPornripsReferenceRecent(ctx context.Context, skip int) []catalogTorrent {
	if h.Reference == nil || !h.Reference.Enabled() {
		return nil
	}
	items, err := h.Reference.FetchRecent(ctx, skip)
	if err != nil || len(items) == 0 {
		return nil
	}
	out := recentItemsToCatalog(items)
	h.warmReferenceMetaBackground(items)
	return out
}

func recentItemsToCatalog(items []metadata.ReferenceRecentItem) []catalogTorrent {
	out := make([]catalogTorrent, 0, len(items))
	for _, item := range items {
		if item.Slug == "" {
			continue
		}
		title := item.Slug
		cover := ""
		if item.Meta != nil {
			if item.Meta.Name != "" {
				title = item.Meta.Name
			}
			cover = item.Meta.Poster
		}
		out = append(out, catalogTorrent{
			Title:      title,
			DetailURL:  "https://pornrips.to/" + item.Slug + "/",
			CoverImage: cover,
			Website:    "pornrips",
			Indexer:    "pornrips",
		})
	}
	return out
}

// warmReferenceMetaBackground caches per-slug metadata from embedded WP catalog
// responses in the background, so subsequent meta requests skip the WP API call.
func (h *Handler) warmReferenceMetaBackground(items []metadata.ReferenceRecentItem) {
	if h.Redis == nil {
		return
	}
	go func(its []metadata.ReferenceRecentItem) {
		store := newRedisStore(h.Redis)
		if store == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, it := range its {
			if it.Slug == "" || it.Meta == nil {
				continue
			}
			if _, found := store.getReferenceMeta(ctx, it.Slug); found {
				continue
			}
			_ = store.setReferenceMeta(ctx, it.Slug,
				referenceMetaCacheEntry{Found: true, Meta: it.Meta}, ttlReferenceMeta)
		}
	}(items)
}

func strMetaVal(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.Itoa(int(s))
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.Itoa(int(s))
	case json.Number:
		return s.String()
	}
	return ""
}

func buildPornripsCatalogKey(catalogID, contentType, searchQ, genre string, skip int) string {
	return catalogID + "|" + contentType + "|" + searchQ + "|" + genre + "|" + itoa(skip)
}
