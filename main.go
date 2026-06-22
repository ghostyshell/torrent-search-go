package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/handlers"
	"torrent-search-go/internal/middleware"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/internal/services/redis"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/internal/services/streams"
	"torrent-search-go/internal/services/streams/providers"
	"torrent-search-go/internal/stremio"
	"torrent-search-go/internal/stremio/magnetio"
	"torrent-search-go/pkg/mongo"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize logger
	logger := middleware.NewLogger(cfg)

	// Route package-level log.Printf (used by background jobs) to both stderr and
	// the dashboard-visible all.log so job progress shows up in monitoring endpoints.
	if cfg.Logging.EnableFile {
		if err := os.MkdirAll(cfg.Logging.LogDir, 0755); err == nil {
			allLogPath := filepath.Join(cfg.Logging.LogDir, "all.log")
			log.SetOutput(io.MultiWriter(os.Stderr, &lumberjack.Logger{
				Filename:   allLogPath,
				MaxSize:    10,
				MaxBackups: 5,
				MaxAge:     7,
				Compress:   true,
			}))
		}
	}

	// Initialize Redis once for both Stremio and AtishMKV
	var redisClient *redis.Client
	if cfg.Redis.Enabled {
		redisClient = redis.NewClient(cfg.Redis)
		if err := redisClient.Connect(context.Background()); err != nil {
			log.Printf("[redis] connection failed: %v", err)
			redisClient = nil
		}
	}

	// Initialize database client (MongoDB - same store as Node backend)
	dbClient, err := mongo.NewClient(cfg.Database.Mongo.URI, cfg.Database.Mongo.DBName, int64(cfg.Cache.StreamUrlTTLSeconds))
	if err != nil {
		log.Fatalf("Failed to initialize database client: %v", err)
	}
	defer dbClient.Close()

	// Initialize storage provider
	storageProvider, err := handlers.NewStorageProvider(dbClient)
	if err != nil {
		log.Fatalf("Failed to initialize storage provider: %v", err)
	}
	if err := storageProvider.Initialize(context.Background()); err != nil {
		log.Fatalf("Failed to run database migrations: %v", err)
	}

	// Initialize scraper service and register all scrapers
	scraperService := scraper.NewService()
	// "piratebay" and "hiddenbay" both scrape thehiddenbay.com.
	hiddenBay := scraper.NewHiddenBayScraper(scraperService.GetHTTPClient(), os.Getenv("HIDDENBAY_URL"))
	scraperService.RegisterScraper("piratebay", hiddenBay)
	scraperService.RegisterScraper("hiddenbay", hiddenBay)
	scraperService.RegisterScraper("1337x", scraper.NewX1337Cache(scraper.NewX1337Scraper(scraperService.GetHTTPClient(), cfg.Scraper.FlareSolverrURL)))
	scraperService.RegisterScraper("yts", scraper.NewYTSScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("nyaasi", scraper.NewNyaaScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("limetorrent", scraper.NewLimeScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("torrentproject", scraper.NewTorrentProjectScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("pornrips", scraper.NewPornripsScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("sukebei", scraper.NewSukebeiScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("knaben_adult", scraper.NewKnabenAdultScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("bitsearch", scraper.NewBitsearchScraper(scraperService.GetHTTPClient()))
	scraperService.RegisterScraper("xxxclub", scraper.NewXxxClubScraper(scraperService.GetHTTPClient()))

	// Initialize Magnetio stream service
	streamHTTP := streams.NewHTTPClient(scraperService.GetHTTPClient(), cfg.Scraper.FlareSolverrURL)
	var streamOpts []streams.Option
	if cfg.Cache.StreamsCacheTTLSeconds > 0 {
		streamOpts = append(streamOpts, streams.WithCache(redisClient, time.Duration(cfg.Cache.StreamsCacheTTLSeconds)*time.Second))
	}
	streamService := streams.NewService(scraperService.GetHTTPClient(), streamOpts...)
	streamService.Register(providers.NewKnabenProvider(scraperService.GetHTTPClient()))
	streamService.Register(providers.NewBitsearchProvider(streamHTTP))
	streamService.Register(providers.NewNyaaProvider(scraperService.GetHTTPClient()))
	streamService.Register(providers.NewSubsPleaseProvider(scraperService.GetHTTPClient()))
	streamService.Register(providers.NewAnimeToshoProvider(scraperService.GetHTTPClient()))
	streamService.Register(providers.NewNekobtProvider(scraperService.GetHTTPClient()))
	streamService.MarkCore("knaben")

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(cfg, scraperService, dbClient)
	streamsHandler := handlers.NewStreamsHandler(streamService)
	var atishmkvProvider *providers.AtishmkvProvider
	if atishmkvProvider, err = providers.NewAtishmkvProvider(streamHTTP, redisClient); err == nil && atishmkvProvider != nil {
		streamService.Register(atishmkvProvider)
		streamService.MarkCore("atishmkv")
	}
	atishmkvHandler := handlers.NewAtishmkvHandler(atishmkvProvider)
	authHandler := handlers.NewAuthHandler(storageProvider, cfg)
	torrentHandler := handlers.NewTorrentHandler(storageProvider, scraperService)
	cacheHandler := handlers.NewCacheHandler(storageProvider, cfg)
	favoritesHandler := handlers.NewFavoritesHandler(storageProvider)
	imagesHandler := handlers.NewImagesHandler(storageProvider, cfg)
	proxyHandler := handlers.NewProxyHandler(cfg)
	// Initialize DDoS guard - loads blocklist from MongoDB and starts background sync.
	ddosGuard := middleware.NewDDoSGuard(dbClient, middleware.DefaultDDoSConfig(), cfg.APIKeys.AddonAPIToken)
	ddosGuard.Start(context.Background())

	monitoringHandler := handlers.NewMonitoringHandler(storageProvider, cfg)
	jobRunner := jobs.NewRunner(storageProvider, cfg, scraperService)
	jobRunner.SetAtishmkvProvider(atishmkvProvider)
	jobScheduler := handlers.NewJobScheduler(storageProvider, cfg, monitoringHandler, jobRunner)
	monitoringHandler.SetJobScheduler(jobScheduler)
	monitoringHandler.SetDDoSGuard(ddosGuard)

	// Initialize rate limiter (production-only by default)
	rateLimiter := middleware.NewRateLimiter(cfg.Security.RateLimiting, cfg.APIKeys.AddonAPIToken)
	rateLimiter.Cleanup(5 * time.Minute)

	// Create router with middleware
	router := middleware.NewRouter(
		middleware.WithRequestID(),
		middleware.WithSecurityHeaders(),
		middleware.WithLogger(logger),
		middleware.WithAPITracker(monitoringHandler.RecordAPIRequest),
		middleware.WithCORS(cfg.CORS),
		middleware.WithRecovery(),
		middleware.WithTrustProxy(cfg.Security.TrustProxy),
		middleware.WithRateLimiter(rateLimiter),
		middleware.WithDDoSGuard(ddosGuard),
	)

	// Initialize auth service for authentication operations
	authService := handlers.NewAuthService(storageProvider, cfg)

	// Initialize IP allowlist middleware for monitoring endpoints
	ipAllowlistMiddleware := middleware.NewIPAllowlistMiddleware(cfg.Security.MonitoringIPAllowlist)
	dashboardAuthMiddleware := middleware.NewDashboardAuthMiddleware(cfg)

	// Initialize auth middleware for protected routes
	authMiddleware := middleware.NewAuthMiddleware(authService, cfg.APIKeys.AddonAPIToken)

	// Register routes
	registerHealthRoutes(router, healthHandler)
	registerAuthRoutes(router, authHandler, authMiddleware, rateLimiter)
	registerTorrentRoutes(router, torrentHandler)
	registerStreamsRoutes(router, streamsHandler)
	registerAtishmkvRoutes(router, atishmkvHandler)
	registerCacheRoutes(router, cacheHandler, authMiddleware)
	registerFavoritesRoutes(router, favoritesHandler, authMiddleware)
	registerImagesRoutes(router, imagesHandler)
	registerProxyRoutes(router, imagesHandler, proxyHandler, authMiddleware)
	registerAddonRoutes(router, jobRunner, authMiddleware)
	registerStremioRoutes(router, scraperService, jobRunner, cfg, storageProvider, redisClient)
	registerMagnetioRoutes(router, cfg)
	registerMonitoringRoutes(router, monitoringHandler, ipAllowlistMiddleware, dashboardAuthMiddleware, ddosGuard)
	// Legacy per-scraper torrent routes must be registered last to avoid shadowing other /api/* paths.
	registerLegacyTorrentRoutes(router, torrentHandler, scraperService.GetAvailableScrapers())
	registerStaticRoutes(router)

	// Start background jobs (after routes; scheduler wired to monitoring)
	jobScheduler.Start()

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("Starting server", "address", addr, "environment", cfg.Environment)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Start background jobs
	// (started after route registration - see above)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop background jobs
	done := make(chan struct{})
	jobScheduler.Stop(done)
	close(done)

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("Server stopped gracefully")
}

// registerHealthRoutes registers health check endpoints
func registerHealthRoutes(router *middleware.Router, handler *handlers.HealthHandler) {
	router.Get("/health", handler.Health)
	router.Get("/health/detailed", handler.DetailedHealth)
	router.Get("/health/ready", handler.Ready)
	router.Get("/health/live", handler.Live)
	router.Get("/health/1337x", handler.Health1337x)
}

// registerAuthRoutes registers authentication endpoints
func registerAuthRoutes(router *middleware.Router, handler *handlers.AuthHandler, authMiddleware *middleware.AuthMiddleware, rateLimiter *middleware.RateLimiter) {
	authLimiter := rateLimiter.AuthLimiter()

	router.Get("/api/auth/google", handler.GoogleLogin, authLimiter)
	router.Get("/api/auth/google/callback", handler.GoogleCallback, authLimiter)
	router.Post("/api/auth/exchange", handler.ExchangeAuthCode, authLimiter)
	router.Post("/api/auth/logout", handler.Logout, authMiddleware.RequireAuth, authLimiter)
	router.Get("/api/auth/user", handler.GetUser, authMiddleware.RequireAuth)
	router.Get("/api/auth/realdebrid/api-key", handler.GetRealDebridKey, authMiddleware.RequireAuth)
	router.Post("/api/auth/realdebrid/api-key", handler.SetRealDebridKey, authMiddleware.RequireAuth)
	router.Delete("/api/auth/realdebrid/api-key", handler.DeleteRealDebridKey, authMiddleware.RequireAuth)
	router.Post("/api/auth/validate", handler.ValidateSession, authLimiter)
	router.Get("/api/auth/sessions", handler.GetSessions, authMiddleware.RequireAuth)
}

// registerTorrentRoutes registers torrent search endpoints
func registerTorrentRoutes(router *middleware.Router, handler *handlers.TorrentHandler) {
	router.Get("/api/torrents/websites", handler.GetWebsites)
	router.Get("/api/torrents/", handler.GetWebsites)
	router.Get("/api/torrents/search/:website/:query/:page?", handler.Search)
	router.Get("/api/torrents/browse/:category/:page?", handler.Browse)
	router.Post("/api/torrents/advanced-search", handler.AdvancedSearch)
	router.Get("/api/torrents/details/:website/:torrentUrl", handler.DetailsPrefetch)
	router.Get("/api/torrents/torrent-details/:website/:torrentUrl", handler.Details)
	// Backward compatibility: returns a bare array (matches the JS searchTorrents handler).
	router.Get("/api/torrents/:website/:query/:page?", handler.SearchLegacy)
}

// registerStreamsRoutes registers Magnetio-compatible stream resolution endpoints.
func registerStreamsRoutes(router *middleware.Router, handler *handlers.StreamsHandler) {
	router.Get("/providers", handler.ListProviders)
	router.Get("/streams/:type/:id", handler.GetStreams)
}

// registerAtishmkvRoutes registers AtishMKV catalog/refresh/status endpoints.
func registerAtishmkvRoutes(router *middleware.Router, handler *handlers.AtishmkvHandler) {
	router.Get("/atishmkv/status", handler.Status)
	router.Post("/atishmkv/sync", handler.Sync)
	router.Post("/atishmkv/refresh", handler.Refresh)
}

// registerLegacyTorrentRoutes registers per-scraper legacy routes LAST to avoid conflicts.
// A true catch-all ("/api/:website/:query/:page?") is not allowed by Go 1.22+ ServeMux
// because it conflicts with other "/api/..." routes such as "/api/torrents/".
func registerLegacyTorrentRoutes(router *middleware.Router, handler *handlers.TorrentHandler, scraperNames []string) {
	router.Get("/api/torrent-details/:website/:torrentUrl", handler.Details)
	for _, name := range scraperNames {
		// Per-scraper legacy routes return a bare array; website is fixed by the route.
		router.Get(fmt.Sprintf("/api/%s/:query/:page?", name), handler.SearchLegacyFor(name))
	}
}

// registerCacheRoutes registers cache/storage endpoints. Protected by session auth,
// but also accept the shared ADDON_API_TOKEN service token so the Stremio addon can
// hit these routes without a user session.
func registerCacheRoutes(router *middleware.Router, handler *handlers.CacheHandler, authMiddleware *middleware.AuthMiddleware) {
	router.Get("/api/cache/stats", handler.GetStats, authMiddleware.RequireAuth)
	router.Get("/api/storage/stats", handler.GetStats, authMiddleware.RequireAuth)

	router.Post("/api/cache/cover-image", handler.StoreCoverImage, authMiddleware.RequireAuth)
	router.Delete("/api/cache/cover-image", handler.DeleteCoverImage, authMiddleware.RequireAuth)
	router.Put("/api/cache/cover-image/favorite/:favoriteId", handler.UpdateFavoriteEntryCoverImage, authMiddleware.RequireAuth)
	router.Put("/api/cache/cover-image/cached-link/:cachedLinkId", handler.UpdateCachedLinkCoverImage, authMiddleware.RequireAuth)
	router.Put("/api/cache/cover-image/torrent-details/:favoriteId/:source", handler.UpdateTorrentDetailsCoverImage, authMiddleware.RequireAuth)
	router.Get("/api/cache/cover-image/:torrentKey", handler.GetCoverImage, authMiddleware.RequireAuth)
	router.Post("/api/cache/cover-image/torrent", handler.GetCoverImageForTorrent, authMiddleware.RequireAuth)
	router.Post("/api/cache/stream-url", handler.StoreStreamURL, authMiddleware.RequireAuth)
	router.Get("/api/cache/stream-url/:magnetHash", handler.GetStreamURL, authMiddleware.RequireAuth)
	router.Post("/api/cache/magnet", handler.StoreMagnetLink, authMiddleware.RequireAuth)
	router.Get("/api/cache/magnet", handler.GetMagnetLink, authMiddleware.RequireAuth)
	router.Post("/api/cache/set", handler.SetCacheValue, authMiddleware.RequireAuth)
	router.Get("/api/cache/get/:key", handler.GetCacheValue, authMiddleware.RequireAuth)
	router.Delete("/api/cache/delete/:key", handler.DeleteCacheValue, authMiddleware.RequireAuth)

	router.Post("/api/storage/stream-url", handler.StoreStreamURL, authMiddleware.RequireAuth)
	router.Get("/api/storage/stream-url/:magnetHash", handler.GetStreamURL, authMiddleware.RequireAuth)
	router.Post("/api/storage/cover-image", handler.StoreCoverImage, authMiddleware.RequireAuth)
	router.Delete("/api/storage/cover-image", handler.DeleteCoverImage, authMiddleware.RequireAuth)
	router.Put("/api/storage/cover-image/favorite/:favoriteId", handler.UpdateFavoriteEntryCoverImage, authMiddleware.RequireAuth)
	router.Put("/api/storage/cover-image/cached-link/:cachedLinkId", handler.UpdateCachedLinkCoverImage, authMiddleware.RequireAuth)
	router.Put("/api/storage/cover-image/torrent-details/:favoriteId/:source", handler.UpdateTorrentDetailsCoverImage, authMiddleware.RequireAuth)
	router.Get("/api/storage/cover-image/:torrentKey", handler.GetCoverImage, authMiddleware.RequireAuth)
	router.Post("/api/storage/cover-image/torrent", handler.GetCoverImageForTorrent, authMiddleware.RequireAuth)
	router.Put("/api/storage/favorites/:favoriteId/magnet", handler.UpdateFavoriteEntryMagnetLink, authMiddleware.RequireAuth)
	router.Post("/api/storage/magnet", handler.StoreMagnetLink, authMiddleware.RequireAuth)
	router.Get("/api/storage/magnet", handler.GetMagnetLink, authMiddleware.RequireAuth)
	router.Post("/api/storage/set", handler.SetCacheValue, authMiddleware.RequireAuth)
	router.Get("/api/storage/get/:key", handler.GetCacheValue, authMiddleware.RequireAuth)
	router.Delete("/api/storage/delete/:key", handler.DeleteCacheValue, authMiddleware.RequireAuth)

	router.Post("/api/cache/stream-url/refresh", handler.RefreshStreamURL, authMiddleware.RequireAuth)
	router.Post("/api/storage/stream-url/refresh", handler.RefreshStreamURL, authMiddleware.RequireAuth)
}

// registerAddonRoutes registers Stremio-addon integration endpoints.
func registerAddonRoutes(router *middleware.Router, runner *jobs.Runner, authMiddleware *middleware.AuthMiddleware) {
	router.Post("/api/addon/meta-enqueue", func(w http.ResponseWriter, r *http.Request) {
		if runner == nil {
			http.Error(w, `{"success":false,"error":"runner unavailable"}`, http.StatusInternalServerError)
			return
		}
		var body struct {
			Items []jobs.MetaEnqueueItem `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"success":false,"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		added := runner.EnqueueMetaLookups(body.Items)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"added":   added,
		})
	}, authMiddleware.RequireAuth)
}

// registerStremioRoutes registers Stremio protocol handlers (manifest/catalog/meta).
func registerStremioRoutes(router *middleware.Router, scrapers *scraper.Service, runner *jobs.Runner, cfg *config.Config, storage *handlers.StorageProvider, redisClient *redis.Client) {
	baseURL := strings.TrimSuffix(os.Getenv("STREMIO_EDGE_URL"), "/")
	if baseURL == "" {
		baseURL = strings.TrimSuffix(os.Getenv("BASE_URL"), "/")
	}

	// The Go Stremio handler serves xxx_* catalogs from piratebay/HiddenBay only.
	// Multi-source aggregation (ADULT_SOURCE=all) and the torrentgalaxy/magnetdl/
	// limetorrents browse sources are not ported, so warn the operator if the Node
	// addon was running with a different ADULT_SOURCE before switching STREMIO_ON_GO=1.
	if src := strings.ToLower(strings.TrimSpace(os.Getenv("ADULT_SOURCE"))); src != "" && src != "piratebay" && src != "hiddenbay" {
		log.Printf("[stremio] WARNING: ADULT_SOURCE=%q - Go serves xxx_* catalogs from piratebay/HiddenBay only; multi-source and torrentgalaxy/magnetdl/limetorrents browse are not ported. Those catalogs will be piratebay-only.", src)
	}

	studios, cover := newStremioProviders(storage)
	h := &stremio.Handler{
		Scrapers:     scrapers,
		Redis:        redisClient,
		Env:          cfg,
		BaseURL:      baseURL,
		CatalogStore: storage,
		Reference:    metadata.NewReferenceClient(),
		Studios:   studios,
		Cover:     cover,
		External: &stremio.ExternalProxy{
			HentaiURL: strings.TrimSuffix(os.Getenv("HENTAI_URL"), "/"),
		},
	}
	if runner != nil {
		h.MetaEnqueuer = func(ctx context.Context, items []jobs.MetaEnqueueItem) {
			runner.EnqueueMetaLookups(items)
		}
	}

	router.Get("/stremio/:config/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTPManifest(w, r, r.PathValue("config"))
	})
	router.Get("/stremio/:config/catalog/:type/:catalogFile", func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTPCatalog(w, r, r.PathValue("config"), r.PathValue("type"), stripJSONSuffix(r.PathValue("catalogFile")), "")
	})
	router.Get("/stremio/:config/catalog/:type/:catalogFile/:extraFile", func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTPCatalog(w, r, r.PathValue("config"), r.PathValue("type"), stripJSONSuffix(r.PathValue("catalogFile")), stripJSONSuffix(r.PathValue("extraFile")))
	})
	router.Get("/stremio/:config/meta/:type/:metaFile", func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTPMeta(w, r, r.PathValue("config"), r.PathValue("type"), stripJSONSuffix(r.PathValue("metaFile")))
	})
	// Stream resolution is intentionally NOT served by Go (per-user debrid keys
	// stay on the Node edge). Register the route anyway so it returns a clean
	// JSON 404 instead of falling through to the static handler's HTML 200, which
	// a Stremio client cannot parse.
	router.Get("/stremio/:config/stream/:type/:streamFile", h.ServeStremioStream)
	router.Options("/stremio/:config/manifest.json", stremioOptions)
	router.Options("/stremio/:config/catalog/:type/:catalogFile", stremioOptions)
	router.Options("/stremio/:config/catalog/:type/:catalogFile/:extraFile", stremioOptions)
	router.Options("/stremio/:config/meta/:type/:metaFile", stremioOptions)
	router.Options("/stremio/:config/stream/:type/:streamFile", stremioOptions)
}

// registerMagnetioRoutes registers Magnetio Stremio protocol handlers.
// The Node Magnetio addon can become a thin edge by proxying manifest/catalog/meta
// here; stream resolution stays on the Node edge because it uses per-user debrid keys.
func registerMagnetioRoutes(router *middleware.Router, cfg *config.Config) {
	baseURL := strings.TrimSuffix(os.Getenv("MAGNETIO_EDGE_URL"), "/")
	if baseURL == "" {
		baseURL = strings.TrimSuffix(os.Getenv("BASE_URL"), "/")
	}

	h := magnetio.NewHandler(baseURL)

	router.Get("/magnetio/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		h.ServeDummyManifest(w, r)
	})
	router.Options("/magnetio/manifest.json", stremioOptions)

	router.Get("/magnetio/:config/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		h.ServeManifest(w, r, r.PathValue("config"))
	})
	router.Get("/magnetio/:config/catalog/:type/:catalogFile", func(w http.ResponseWriter, r *http.Request) {
		h.ServeCatalog(w, r, r.PathValue("config"), r.PathValue("type"), stripJSONSuffix(r.PathValue("catalogFile")), "")
	})
	router.Get("/magnetio/:config/catalog/:type/:catalogFile/:extraFile", func(w http.ResponseWriter, r *http.Request) {
		h.ServeCatalog(w, r, r.PathValue("config"), r.PathValue("type"), stripJSONSuffix(r.PathValue("catalogFile")), stripJSONSuffix(r.PathValue("extraFile")))
	})
	router.Get("/magnetio/:config/meta/:type/:metaFile", func(w http.ResponseWriter, r *http.Request) {
		h.ServeMeta(w, r, r.PathValue("config"), r.PathValue("type"), stripJSONSuffix(r.PathValue("metaFile")))
	})
	router.Get("/magnetio/:config/stream/:type/:streamFile", h.ServeStream)
	router.Options("/magnetio/:config/manifest.json", stremioOptions)
	router.Options("/magnetio/:config/catalog/:type/:catalogFile", stremioOptions)
	router.Options("/magnetio/:config/catalog/:type/:catalogFile/:extraFile", stremioOptions)
	router.Options("/magnetio/:config/meta/:type/:metaFile", stremioOptions)
	router.Options("/magnetio/:config/stream/:type/:streamFile", stremioOptions)
}


// stripJSONSuffix removes the Stremio ".json" resource suffix from a path segment.
func stripJSONSuffix(segment string) string {
	return strings.TrimSuffix(segment, ".json")
}

func stremioOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

// registerFavoritesRoutes registers favorites endpoints
func registerFavoritesRoutes(router *middleware.Router, handler *handlers.FavoritesHandler, authMiddleware *middleware.AuthMiddleware) {
	router.Post("/api/cache/favorites", handler.AddFavorite, authMiddleware.RequireAuth)
	router.Get("/api/cache/favorites", handler.GetFavorites, authMiddleware.RequireAuth)
	router.Delete("/api/cache/favorites", handler.RemoveFavorite, authMiddleware.RequireAuth)

	router.Post("/api/storage/favorites", handler.AddFavorite, authMiddleware.RequireAuth)
	router.Get("/api/storage/favorites", handler.GetFavorites, authMiddleware.RequireAuth)
	router.Delete("/api/storage/favorites", handler.RemoveFavorite, authMiddleware.RequireAuth)

	router.Get("/api/favorites/:favoriteId/details", handler.GetFavoriteDetails, authMiddleware.RequireAuth)
	router.Post("/api/favorites/:favoriteId/details", handler.StoreFavoriteDetails, authMiddleware.RequireAuth)
	router.Post("/api/favorites/check", handler.CheckFavorite, authMiddleware.RequireAuth)
	router.Post("/api/favorites/entry", handler.StoreFavoriteEntry, authMiddleware.RequireAuth)

	router.Get("/api/storage/stored-links", handler.GetCachedLinks, authMiddleware.RequireAuth)
	router.Post("/api/storage/stored-links", handler.AddCachedLink, authMiddleware.RequireAuth)
	router.Put("/api/storage/stored-links/:id", handler.UpdateCachedLink, authMiddleware.RequireAuth)
	router.Delete("/api/storage/stored-links/:id", handler.RemoveCachedLink, authMiddleware.RequireAuth)

	router.Get("/api/cache/cached-links", handler.GetCachedLinks, authMiddleware.RequireAuth)
	router.Post("/api/cache/cached-links", handler.AddCachedLink, authMiddleware.RequireAuth)
	router.Put("/api/cache/cached-links/:id", handler.UpdateCachedLink, authMiddleware.RequireAuth)
	router.Delete("/api/cache/cached-links/:id", handler.RemoveCachedLink, authMiddleware.RequireAuth)
}

// registerImagesRoutes registers image endpoints
func registerImagesRoutes(router *middleware.Router, handler *handlers.ImagesHandler) {
	router.Get("/api/images/google-images/search", handler.GoogleSearch)
	router.Get("/api/images/google-images/suggestions", handler.GoogleSuggestions)
	router.Get("/api/images/search", handler.GoogleSearch)
	router.Get("/api/images/suggestions", handler.GoogleSuggestions)
	router.Post("/api/images/pixhost/upload", handler.PixhostUpload)
	router.Get("/api/images/pixhost/fallbacks", handler.GetPixhostFallbacks)
	router.Post("/api/images/batch-process", handler.BatchProcess)

	// Backward compatibility routes
	router.Get("/api/google-images/search", handler.GoogleSearch)
	router.Get("/api/google-images/suggestions", handler.GoogleSuggestions)
	router.Post("/api/pixhost/upload", handler.PixhostUpload)
}

// registerProxyRoutes registers proxy endpoints
func registerProxyRoutes(router *middleware.Router, imagesHandler *handlers.ImagesHandler, proxyHandler *handlers.ProxyHandler, authMiddleware *middleware.AuthMiddleware) {
	router.Get("/api/proxy/search", imagesHandler.GoogleSearch)
	router.Get("/api/proxy/suggestions", imagesHandler.GoogleSuggestions)
	router.All("/api/proxy/real-debrid/*", proxyHandler.RealDebridProxy,
		authMiddleware.RequireAuth,
		authMiddleware.GetUserRealDebridKey,
	)
}

// registerStaticRoutes registers static file routes
func registerStaticRoutes(router *middleware.Router) {
	// Serve dashboard HTML at root
	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/dashboard.html")
	})
}

// registerMonitoringRoutes registers monitoring endpoints (IP-restricted)
func registerMonitoringRoutes(router *middleware.Router, handler *handlers.MonitoringHandler, ipAllowlistMiddleware *middleware.IPAllowlistMiddleware, dashboardAuthMiddleware *middleware.DashboardAuthMiddleware, _ *middleware.DDoSGuard) {
	monitoring := func(h http.HandlerFunc) http.HandlerFunc {
		wrapped := http.HandlerFunc(h)
		withAuth := dashboardAuthMiddleware.RequireDashboardAuth(wrapped)
		withIP := ipAllowlistMiddleware.RequireIPAllowlist(withAuth)
		return func(w http.ResponseWriter, r *http.Request) {
			withIP.ServeHTTP(w, r)
		}
	}

	router.Get("/api/monitoring/dashboard", monitoring(handler.GetDashboardData))
	router.Get("/api/monitoring/logs", monitoring(handler.GetLogs))
	router.Get("/api/monitoring/tasks", monitoring(handler.GetBackgroundTaskStats))
	router.Get("/api/monitoring/api-usage", monitoring(handler.GetApiUsageStats))
	router.Get("/api/monitoring/stream-url-refresh-logs", monitoring(handler.GetStreamUrlRefreshLogs))
	router.Post("/api/monitoring/stream-url-refresh-trigger", monitoring(handler.TriggerStreamUrlRefresh))
	router.Get("/api/monitoring/description-image-cache-logs", monitoring(handler.GetDescriptionImageCacheLogs))
	router.Post("/api/monitoring/description-image-cache-trigger", monitoring(handler.TriggerDescriptionImageCache))
	router.Post("/api/monitoring/description-image-cache-force-refresh", monitoring(handler.TriggerDescriptionImageCacheForceRefresh))
	router.Get("/api/monitoring/search-results-cache-logs", monitoring(handler.GetSearchResultsCacheLogs))
	router.Post("/api/monitoring/search-results-cache-trigger", monitoring(handler.TriggerSearchResultsCache))
	router.Get("/api/monitoring/redis-catalog-cache-logs", monitoring(handler.GetRedisCatalogCacheLogs))
	router.Post("/api/monitoring/redis-catalog-cache-trigger", monitoring(handler.TriggerRedisCatalogCache))
	router.Get("/api/monitoring/search-query-cache-logs", monitoring(handler.GetSearchQueryCacheLogs))
	router.Post("/api/monitoring/search-query-cache-trigger", monitoring(handler.TriggerSearchQueryCache))
	router.Get("/api/monitoring/category-warmer-logs", monitoring(handler.GetCategoryWarmerLogs))
	router.Post("/api/monitoring/category-warmer-trigger", monitoring(handler.TriggerCategoryWarmer))
	router.Post("/api/monitoring/cover-storage-maintenance-trigger", monitoring(handler.TriggerCoverStorageMaintenance))
	router.Post("/api/monitoring/atishmkv-catalog-sync-trigger", monitoring(handler.TriggerAtishmkvCatalogSync))
	router.Post("/api/monitoring/atishmkv-direct-link-refresh-trigger", monitoring(handler.TriggerAtishmkvDirectLinkRefresh))
	router.Get("/api/monitoring/job-logs/list", monitoring(handler.ListJobLogs))
	router.Get("/api/monitoring/job-logs/search", monitoring(handler.SearchJobLogs))
	router.Get("/api/monitoring/job-logs/file", monitoring(handler.ServeJobLogFile))
	router.Post("/api/monitoring/job-logs/maintenance", monitoring(handler.TriggerJobLogMaintenance))
	router.Get("/api/monitoring/debug-favorites", monitoring(handler.DebugFavorites))
	router.Get("/api/monitoring/debug-stream-refresh", monitoring(handler.DebugStreamRefresh))
	router.Get("/api/debug/favorite-entry/:favoriteEntryId", monitoring(handler.DebugFavoriteEntry))

	// IP traffic & block management
	router.Get("/api/monitoring/ip-traffic", monitoring(handler.GetIPTraffic))
	router.Get("/api/monitoring/ip-block", monitoring(handler.GetBlockedIPs))
	router.Post("/api/monitoring/ip-block", monitoring(handler.BlockIP))
	router.Delete("/api/monitoring/ip-block/:ip", monitoring(handler.UnblockIP))
}
