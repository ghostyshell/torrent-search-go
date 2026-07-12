package jobs

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/crypto"
	"torrent-search-go/internal/handlers"
	"torrent-search-go/internal/services/hentai"
	"torrent-search-go/internal/services/realdebrid"
	"torrent-search-go/internal/services/redis"
	"torrent-search-go/internal/services/scraper"
	objectstorage "torrent-search-go/internal/services/storage"
	"torrent-search-go/internal/services/streams/providers"
	"torrent-search-go/pkg/models"
)

const (
	refreshSpacing    = 2 * time.Second
	rateLimitJobPause = 30 * time.Second
)

// Runner executes background maintenance tasks.
type Runner struct {
	storage        *handlers.StorageProvider
	cfg            *config.Config
	rd             *realdebrid.Client
	scrapers       *scraper.Service
	pornrips       *scraper.PornripsScraper
	hentai         *hentai.HentaiService
	perverzija     *scraper.PerverzijaScraper
	freepornvideos *scraper.FreepornvideosScraper
	yesporn        *scraper.YespornScraper
	watchporn      *scraper.WatchpornScraper
	porneec        *scraper.PorneecScraper
	httpClient     *http.Client
	redis          *redis.Client
	objectStorage  *objectstorage.ObjectStorage
	metaQueue      *metaEnrichQueue
	atishmkv       *providers.AtishmkvProvider
}

// NewRunner creates a job runner.
func NewRunner(storage *handlers.StorageProvider, cfg *config.Config, scrapers *scraper.Service) *Runner {
	r := &Runner{
		storage:  storage,
		cfg:      cfg,
		rd:       realdebrid.NewClient(),
		scrapers: scrapers,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		metaQueue: newMetaEnrichQueue(),
	}

	if scrapers != nil {
		if pr, ok := scrapers.GetScraper("pornrips"); ok {
			if pornrips, ok := pr.(*scraper.PornripsScraper); ok {
				r.pornrips = pornrips
			}
		}
		// Tube sources are NOT registered in the torrent Scraper service (they are
		// not torrent sources and would pollute SearchAll). Construct them here with
		// the shared 30s HTTP client so the sync job (ingest/enrich) and the stremio
		// stream resolver can use them; held as typed Runner fields, mirroring hentai.
		hc := scrapers.GetHTTPClient()
		r.perverzija = scraper.NewPerverzijaScraper(hc)
		r.freepornvideos = scraper.NewFreepornvideosScraper(hc)
		r.yesporn = scraper.NewYespornScraper(hc)
		r.watchporn = scraper.NewWatchpornScraper(hc)
		r.porneec = scraper.NewPorneecScraper(hc)
	}

	if cfg != nil && cfg.Redis.Enabled {
		r.redis = redis.NewClient(cfg.Redis)
		if err := r.redis.Connect(context.Background()); err != nil {
			log.Printf("[Runner] Redis connection failed: %v", err)
			r.redis = nil
		}
	}

	// Hentai self-scrape service (HentaiMama). Nil HTTP client -> hentai's 20s
	// default. Ratings come from the source, no external client.
	r.hentai = hentai.NewService(nil)

	if cfg != nil && cfg.S3.Enabled {
		osClient, err := objectstorage.NewObjectStorage(cfg.S3)
		if err != nil {
			log.Printf("[Runner] S3 object storage init failed: %v", err)
		} else if osClient != nil {
			r.objectStorage = osClient
			if storage != nil {
				storage.SetObjectStorage(osClient)
			}
		}
	}

	return r
}

// StorageCleanup removes expired sessions and cache rows.
func (r *Runner) StorageCleanup(ctx context.Context) error {
	return r.storage.Cleanup(ctx)
}

// SetAtishmkvProvider wires the AtishMKV provider for background jobs.
func (r *Runner) SetAtishmkvProvider(p *providers.AtishmkvProvider) {
	r.atishmkv = p
}

// HentaiResolver exposes the hentai stream resolver for the stremio Handler
// (Phase C). Returns nil if the hentai service is not configured.
func (r *Runner) HentaiResolver() hentai.EpisodeStreamResolver {
	if r == nil {
		return nil
	}
	return r.hentai
}

// PerverzijaScraper exposes the Perverzija scraper for the stremio stream
// resolver (the concrete scraper satisfies stremio.PerverzijaStreamResolver).
// Returns nil if no scraper service was wired (cold install).
func (r *Runner) PerverzijaScraper() *scraper.PerverzijaScraper {
	if r == nil {
		return nil
	}
	return r.perverzija
}

// FreepornvideosScraper exposes the FreePornVideos scraper for the stremio
// stream resolver (the concrete scraper satisfies stremio.FreepornvideosStreamResolver).
// Returns nil if no scraper service was wired (cold install).
func (r *Runner) FreepornvideosScraper() *scraper.FreepornvideosScraper {
	if r == nil {
		return nil
	}
	return r.freepornvideos
}

// YespornScraper exposes the YesPorn scraper for the stremio stream resolver (the
// concrete scraper satisfies stremio.YespornStreamResolver). Returns nil if no
// scraper service was wired (cold install).
func (r *Runner) YespornScraper() *scraper.YespornScraper {
	if r == nil {
		return nil
	}
	return r.yesporn
}

// WatchpornScraper exposes the WatchPorn scraper for the stremio stream resolver
// (the concrete scraper satisfies stremio.WatchpornStreamResolver). Returns nil if
// no scraper service was wired (cold install).
func (r *Runner) WatchpornScraper() *scraper.WatchpornScraper {
	if r == nil {
		return nil
	}
	return r.watchporn
}

// PorneecScraper exposes the Porneec scraper for the stremio stream resolver
// (the concrete scraper satisfies stremio.PorneecStreamResolver). Returns nil if
// no scraper service was wired (cold install).
func (r *Runner) PorneecScraper() *scraper.PorneecScraper {
	if r == nil {
		return nil
	}
	return r.porneec
}

// AtishmkvCatalogSync runs the daily AtishMKV catalog sync.
func (r *Runner) AtishmkvCatalogSync(ctx context.Context) (map[string]interface{}, error) {
	if r.atishmkv == nil {
		return nil, errors.New("AtishMKV provider not configured")
	}
	return r.atishmkv.SyncCatalog(ctx)
}

// AtishmkvDirectLinkRefresh refreshes cached direct video links for the catalog.
func (r *Runner) AtishmkvDirectLinkRefresh(ctx context.Context) (map[string]interface{}, error) {
	if r.atishmkv == nil {
		return nil, errors.New("AtishMKV provider not configured")
	}
	return r.atishmkv.RefreshDirectLinks(ctx)
}

// RedisCatalogCache pre-populates Stremio addon catalog keys in Redis.
func (r *Runner) RedisCatalogCache(ctx context.Context) (map[string]interface{}, error) {
	return r.runRedisCatalogCache(ctx)
}

// CoverStorageMaintenance refreshes S3 presigned URLs and removes expired temp covers.
func (r *Runner) CoverStorageMaintenance(ctx context.Context) (map[string]interface{}, error) {
	return runCoverStorageMaintenance(ctx, r)
}

// StreamURLRefresh refreshes cached stream URLs for all favorites with RD keys.
func (r *Runner) StreamURLRefresh(ctx context.Context) (map[string]interface{}, error) {
	groups, err := r.storage.GetFavoritesForStreamRefresh(ctx)
	if err != nil {
		return nil, err
	}

	results := map[string]interface{}{
		"totalFavorites": 0,
		"usersProcessed": 0,
		"refreshed":      0,
		"skipped":        0,
		"failed":         0,
		"errorCounts":    map[string]int{},
	}
	errorCounts := results["errorCounts"].(map[string]int)

	for _, group := range groups {
		results["totalFavorites"] = results["totalFavorites"].(int) + len(group.Favorites)
		if group.UserID == "" {
			results["skipped"] = results["skipped"].(int) + len(group.Favorites)
			continue
		}

		encKey, err := r.storage.GetRealDebridKey(ctx, group.UserID)
		if err != nil || encKey == "" {
			results["skipped"] = results["skipped"].(int) + len(group.Favorites)
			continue
		}
		apiKey, err := crypto.DecryptSecret(encKey)
		if err != nil || apiKey == "" {
			results["skipped"] = results["skipped"].(int) + len(group.Favorites)
			continue
		}

		results["usersProcessed"] = results["usersProcessed"].(int) + 1
		for _, fav := range group.Favorites {
			stream, err := r.rd.RefreshStreamURL(ctx, apiKey, fav.MagnetLink)
			if err != nil {
				results["failed"] = results["failed"].(int) + 1
				kind := classifyRefreshError(err)
				errorCounts[kind]++
				log.Printf("[Stream Refresh] failed %s: %v", fav.TorrentName, err)
				if kind == "rd_rate_limit" {
					if pauseErr := sleepCtx(ctx, rateLimitJobPause); pauseErr != nil {
						return results, pauseErr
					}
				}
				continue
			}
			hash := extractMagnetHash(fav.MagnetLink)
			if err := r.storage.SetStreamURL(ctx, models.StreamURLInput{
				MagnetHash:            hash,
				MagnetLink:            fav.MagnetLink,
				StreamURL:             stream.StreamURL,
				Filename:              stream.Filename,
				Filesize:              stream.Filesize,
				SupportsRangeRequests: stream.SupportsRangeRequests,
				TorrentName:           fav.TorrentName,
			}); err != nil {
				results["failed"] = results["failed"].(int) + 1
				errorCounts["cache_write_error"]++
				continue
			}
			results["refreshed"] = results["refreshed"].(int) + 1
			if err := sleepCtx(ctx, refreshSpacing); err != nil {
				return results, err
			}
		}
	}

	return results, nil
}

func classifyRefreshError(err error) string {
	if err == nil {
		return "other"
	}
	var apiErr *realdebrid.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401:
			return "rd_auth_error"
		case 403:
			return "rd_forbidden"
		case 429:
			return "rd_rate_limit"
		default:
			if apiErr.StatusCode >= 500 {
				return "rd_server_error"
			}
		}
	}
	lower := stringsToLower(err.Error())
	switch {
	case stringsContains(lower, "401"):
		return "rd_auth_error"
	case stringsContains(lower, "403"):
		return "rd_forbidden"
	case stringsContains(lower, "429"):
		return "rd_rate_limit"
	case stringsContains(lower, "5") && stringsContains(lower, "real-debrid api error"):
		return "rd_server_error"
	case stringsContains(lower, "no video files"):
		return "no_video_files"
	case stringsContains(lower, "no download links"):
		return "no_download_links"
	case stringsContains(lower, "magnet error"), stringsContains(lower, "invalid magnet"):
		return "magnet_error"
	case stringsContains(lower, "failed to add magnet"):
		return "add_magnet_failed"
	case stringsContains(lower, "failed to unrestrict"):
		return "unrestrict_failed"
	case stringsContains(lower, "timeout"), stringsContains(lower, "deadline"):
		return "timeout"
	default:
		return "other"
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func stringsContains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func extractMagnetHash(magnet string) string {
	const prefix = "urn:btih:"
	lower := stringsToLower(magnet)
	idx := indexOf(lower, prefix)
	if idx < 0 {
		return magnet
	}
	hash := magnet[idx+len(prefix):]
	if end := indexOfByte(hash, '&'); end >= 0 {
		hash = hash[:end]
	}
	return stringsToLower(hash)
}

func stringsToLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func indexOfByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
