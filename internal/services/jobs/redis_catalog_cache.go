package jobs

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/models"
)

const (
	catalogCacheCategory4K  = "507"
	catalogCacheCategoryFHD = "505"
	catalogScraper          = "piratebay"
)

var (
	catalogReUnderscore = regexp.MustCompile(`_+`)
)

type catalogCategory struct {
	Marker   string
	Label    string
	Category string
}

type catalogSort struct {
	Suffix string
	Sort   string
}

var catalogCategories = []catalogCategory{
	{Marker: "", Label: "4K", Category: catalogCacheCategory4K},
	{Marker: "fhd", Label: "1080p", Category: catalogCacheCategoryFHD},
}

var catalogSorts = []catalogSort{
	{Suffix: "top", Sort: "7"},
	{Suffix: "recent", Sort: "3"},
}

// CatalogTorrent mirrors the normalized camelCase shape produced by the Node
// addon's normalizeHbTorrent so the Stremio addon can consume Redis hits directly.
type CatalogTorrent struct {
	Title      string `json:"title"`
	Size       string `json:"size"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	InfoHash   string `json:"infoHash"`
	MagnetLink string `json:"magnetLink"`
	TorrentURL string `json:"torrentUrl"`
	DetailURL  string `json:"detailUrl"`
	CoverImage string `json:"coverImage"`
	Website    string `json:"website"`
	Indexer    string `json:"indexer"`
	Quality    string `json:"quality"`
}

// studioSafeId replicates adultSections.js studioSafeId().
func studioSafeId(name string) string {
	lower := strings.ToLower(name)
	lower = nonAlnum.ReplaceAllString(lower, "_")
	lower = catalogReUnderscore.ReplaceAllString(lower, "_")
	lower = strings.Trim(lower, "_")
	if len(lower) > 40 {
		lower = lower[:40]
	}
	return strings.Trim(lower, "_")
}

func normalizeCatalogTorrent(t models.Torrent) CatalogTorrent {
	cover := ""
	if t.CoverImage != nil {
		cover = t.CoverImage.URL
	}
	return CatalogTorrent{
		Title:      t.Name,
		Size:       t.Size,
		Seeders:    t.Seeders,
		Leechers:   t.Leechers,
		InfoHash:   extractMagnetHash(t.MagnetLink),
		MagnetLink: t.MagnetLink,
		TorrentURL: t.TorrentURL,
		// HiddenBay detail page == torrent URL. Carry it so warmed entries
		// match the Node addon's normalizeHbTorrent shape (and so the stremio
		// reader can key metadata / stream lookups off the same field).
		DetailURL:  t.TorrentURL,
		CoverImage: cover,
		Website:    "hiddenbay",
		Indexer:    "hiddenbay",
		Quality:    "",
	}
}

func dedupeAndSortCatalogTorrents(torrents []CatalogTorrent, sortCode string) []CatalogTorrent {
	seen := make(map[string]struct{})
	out := make([]CatalogTorrent, 0, len(torrents))
	for _, t := range torrents {
		key := t.InfoHash
		if key == "" {
			key = t.Title
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	if sortCode == "7" {
		sort.Slice(out, func(i, j int) bool { return out[i].Seeders > out[j].Seeders })
	}
	return out
}

func buildCatalogRedisKey(baseURL, catalogID string) string {
	// Must match stremio buildCatalogListKey for hiddenbay-only installs (|hb).
	//   cat:v1:{backendUrl}|{catalogId}|Porn|||0|hb
	return fmt.Sprintf("cat:v1:%s|%s|Porn|||0|hb", baseURL, catalogID)
}

func catalogTTL() time.Duration {
	// Jittered 25-35 minutes to match the Node job interval.
	n, err := rand.Int(rand.Reader, big.NewInt(600))
	if err != nil {
		return 30 * time.Minute
	}
	return 25*time.Minute + time.Duration(n.Int64())*time.Second
}

// ResolveCatalogBaseURL returns the first segment of the cat:v1: Redis keys.
// The Stremio reader MUST resolve this identically to the warmer or it will
// never hit warmed keys, so this is the single source of truth for both.
func ResolveCatalogBaseURL(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	// Prefer ADDON_CACHE_BASE_URL - must match the Stremio addon's BACKEND_URL
	// (used as the first segment of cat:v1: Redis keys).
	if baseURL := os.Getenv("ADDON_CACHE_BASE_URL"); baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	// Node parity: BASE_URL is the canonical source when present.
	if baseURL := os.Getenv("BASE_URL"); baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	if cfg.FrontendURL != "" {
		return strings.TrimRight(cfg.FrontendURL, "/")
	}
	if cfg.Railway.PublicDomain != "" {
		return "https://" + strings.TrimRight(cfg.Railway.PublicDomain, "/")
	}
	return ""
}

// runRedisCatalogCache implements the RedisCatalogCache job.
// It mirrors the Node redisCatalogCacheService.js behavior: browse/search
// adult categories on TheHiddenBay, normalize the results, dedupe/sort, and
// cache each catalog under cat:v1:{baseURL}|{catalogId}|Porn|||0.
func (r *Runner) runRedisCatalogCache(ctx context.Context) (map[string]interface{}, error) {
	if r.cfg == nil || !r.cfg.Redis.Enabled {
		return map[string]interface{}{"skipped": true, "reason": "redis not enabled"}, nil
	}
	if r.redis == nil {
		return map[string]interface{}{"success": false, "skipped": true, "reason": "redis client not initialized"}, nil
	}

	baseURL := ResolveCatalogBaseURL(r.cfg)
	if baseURL == "" {
		return map[string]interface{}{"skipped": true, "reason": "base url not configured"}, nil
	}
	if r.scrapers == nil {
		return map[string]interface{}{"success": false, "error": "scraper service not configured"}, fmt.Errorf("scraper service not configured")
	}

	results := map[string]interface{}{
		"catalogsCached": 0,
		"torrentsCached": 0,
		"skipped":        0,
		"errors":         0,
		"duration":       0.0,
	}

	start := time.Now()
	log.Printf("[RedisCatalog] Starting catalog cache job")

	for ci, cat := range catalogCategories {
		qSuffix := ""
		if cat.Marker != "" {
			qSuffix = "_" + cat.Marker
		}
		log.Printf("[RedisCatalog] === %s (category %s) ===", cat.Label, cat.Category)

		// Browse (XXX) catalogs.
		for _, sv := range catalogSorts {
			catalogID := fmt.Sprintf("xxx%s_%s", qSuffix, sv.Suffix)
			if err := r.cacheCatalog(ctx, baseURL, catalogID, cat.Category, sv.Sort, results, func(ctx context.Context, catID, sort string) ([]models.Torrent, error) {
				return r.scrapers.Browse(ctx, catalogScraper, cat.Category, 1, sort, models.SearchOptions{})
			}); err != nil {
				results["errors"] = results["errors"].(int) + 1
				log.Printf("[RedisCatalog] browse %s: %v", catalogID, err)
			}
			if err := sleepJitter(ctx); err != nil {
				return results, err
			}
		}

		// Trans search catalogs.
		for _, sv := range catalogSorts {
			catalogID := fmt.Sprintf("xxx_trans%s_%s", qSuffix, sv.Suffix)
			if err := r.cacheCatalog(ctx, baseURL, catalogID, cat.Category, sv.Sort, results, func(ctx context.Context, catID, sort string) ([]models.Torrent, error) {
				return r.scrapers.Search(ctx, catalogScraper, "trans", 1, models.SearchOptions{Sort: sort, Category: cat.Category})
			}); err != nil {
				results["errors"] = results["errors"].(int) + 1
				log.Printf("[RedisCatalog] trans %s: %v", catalogID, err)
			}
			if err := sleepJitter(ctx); err != nil {
				return results, err
			}
		}

		// Studio search catalogs.
		for _, studio := range StudioSearchTerms() {
			slug := studioSafeId(studio)
			for _, sv := range catalogSorts {
				catalogID := fmt.Sprintf("xxx_studio_%s%s_%s", slug, qSuffix, sv.Suffix)
				if err := r.cacheCatalog(ctx, baseURL, catalogID, cat.Category, sv.Sort, results, func(ctx context.Context, catID, sort string) ([]models.Torrent, error) {
					return r.scrapers.Search(ctx, catalogScraper, studio, 1, models.SearchOptions{Sort: sort, Category: cat.Category})
				}); err != nil {
					results["errors"] = results["errors"].(int) + 1
				}
				if err := sleepJitter(ctx); err != nil {
					return results, err
				}
			}
		}

		if ci < len(catalogCategories)-1 {
			if err := sleepCtx(ctx, 3*time.Second); err != nil {
				return results, err
			}
		}
	}

	duration := time.Since(start).Seconds()
	results["duration"] = duration
	log.Printf("[RedisCatalog] Job done - %d catalogs cached, %d torrents cached, %d errors, %.1fs",
		results["catalogsCached"], results["torrentsCached"], results["errors"], duration)

	if sukebeiResults, err := r.runSukebeiCatalogCache(ctx); err != nil {
		log.Printf("[SukebeiCatalog] Job error: %v", err)
	} else {
		for k, v := range sukebeiResults {
			results["sukebei_"+k] = v
		}
	}

	return results, nil
}

type catalogFetcher func(ctx context.Context, catalogID, sort string) ([]models.Torrent, error)

func (r *Runner) cacheCatalog(ctx context.Context, baseURL, catalogID, category, sort string, results map[string]interface{}, fetch catalogFetcher) error {
	raw, err := fetch(ctx, catalogID, sort)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		results["skipped"] = results["skipped"].(int) + 1
		return nil
	}
	normalized := make([]CatalogTorrent, 0, len(raw))
	for _, t := range raw {
		normalized = append(normalized, normalizeCatalogTorrent(t))
	}
	torrents := dedupeAndSortCatalogTorrents(normalized, sort)
	if len(torrents) == 0 {
		results["skipped"] = results["skipped"].(int) + 1
		return nil
	}

	key := buildCatalogRedisKey(baseURL, catalogID)
	if err := r.redis.SetCatalogJSON(ctx, key, torrents, catalogTTL()); err != nil {
		return err
	}

	results["catalogsCached"] = results["catalogsCached"].(int) + 1
	results["torrentsCached"] = results["torrentsCached"].(int) + len(torrents)
	log.Printf("[RedisCatalog] Cached %d torrents for %s", len(torrents), catalogID)
	return nil
}

func sleepJitter(ctx context.Context) error {
	// 1-2 second jitter between scrape requests to respect rate limits.
	n, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		return sleepCtx(ctx, 1500*time.Millisecond)
	}
	return sleepCtx(ctx, time.Second+time.Duration(n.Int64())*time.Millisecond)
}
