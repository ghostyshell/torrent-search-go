# Code Structure

A module-by-module tour of every package in the repository.

## Top Level

```
torrent-search-go/
├── main.go                  Entry point — wires all packages together
├── go.mod / go.sum          Module definition (module: torrent-search-go)
├── Dockerfile               Root Dockerfile (alias for deployments/Dockerfile)
├── railpack.toml            Railpack build config (points at deployments/Dockerfile)
├── railway.json             Railway platform deploy config
├── static/
│   └── dashboard.html       Browser-side monitoring dashboard (served at /)
├── scripts/
│   ├── build-and-test.sh    Pre-push validation script
│   └── setup.sh             Developer environment bootstrap
├── deployments/
│   ├── Dockerfile           Multi-stage Docker build
│   └── docker-compose.yml   Local / compose deployment example
└── docs/                    Documentation (this directory)
```

## `main.go`

The application entry point performs the following in order:

1. `config.Load()` — reads all environment variables into a typed `Config` struct
2. `mongo.NewClient()` — connects to MongoDB and pings for readiness
3. `handlers.NewStorageProvider()` + `Initialize()` — wraps the DB client and runs migrations (creates indexes)
4. `scraper.NewService()` + `RegisterScraper()` × 7 — builds the scraper registry
5. Construct every handler (`HealthHandler`, `AuthHandler`, `TorrentHandler`, …)
6. `middleware.NewRouter()` — builds the global middleware chain
7. Route-registration helpers (`registerHealthRoutes`, `registerAuthRoutes`, …)
8. `jobScheduler.Start()` — begins background ticker loops
9. `http.Server.ListenAndServe()` in a goroutine
10. `signal.Notify` / `server.Shutdown` — graceful drain on SIGINT/SIGTERM

## `internal/config`

| File | Purpose |
|------|---------|
| `config.go` | `Config` struct, `Load()`, `Validate()`, helper functions |
| `environment.go` | `ValidateEnvironment()`, `FormatValidationErrors()` |

`Load()` reads every environment variable and returns a fully-typed `*Config`. No global state — the struct is passed by pointer to every handler/service that needs it.

Key sub-structs: `ServerConfig`, `DatabaseConfig` (→ `MongoConfig`), `GoogleConfig`, `APIKeysConfig`, `LoggingConfig`, `SecurityConfig`, `CacheConfig`, `RailwayConfig`.

## `internal/handlers`

Each handler file is a self-contained HTTP handler group with its own constructor and receiver methods.

| File | Handler | Responsibilities |
|------|---------|-----------------|
| `auth.go` | `AuthHandler` | Google OAuth flow, session create/destroy, exchange-code flow, Real-Debrid key management |
| `cache.go` | `CacheHandler` | Stream URL store/retrieve/refresh, cover image store/retrieve, magnet link cache, generic KV cache |
| `favorites.go` | `FavoritesHandler` | CRUD favorites, details, cached links |
| `health.go` | `HealthHandler` | `/health`, `/health/detailed`, `/health/ready`, `/health/live`, `/health/1337x` |
| `helpers.go` | — | Shared utilities: `writeJSON`, `writeError`, parameter extraction, pagination |
| `images.go` | `ImagesHandler` | Google Images search/suggestions, Pixhost upload and fallbacks, batch image processing |
| `jobfilelogger.go` | `JobFileLogger` | Structured per-job log file writer (JSON lines) |
| `monitoring.go` | `MonitoringHandler` | Dashboard data, logs, task stats, API usage, job triggers |
| `proxy.go` | `ProxyHandler` | Authenticated reverse proxy for Real-Debrid API calls |
| `storage.go` | `StorageProvider` | Thin wrapper over `pkg/storage.Database`; the single shared DB accessor |
| `torrents.go` | `TorrentHandler` | Search, browse, advanced-search, details — delegates to `scraper.Service` |
`StorageProvider` (in `storage.go`) is the central dependency injected into every handler. It exposes typed methods (`GetUser`, `SetStreamURL`, `GetFavorites`, …) over the underlying database driver.

## `internal/middleware`

| File | Type / Function | Purpose |
|------|----------------|---------|
| `router.go` | `Router` | ServeMux wrapper with per-route and global middleware; Express-style `:param` → Go `{param}` pattern translation |
| `auth.go` | `AuthMiddleware` | Session-cookie / Bearer token validation; `RequireAuth`, `OptionalAuth`, `GetUserRealDebridKey` |
| `cors.go` | — | CORS headers; configurable allowed origins from `CORSConfig` |
| `dashboard_auth.go` | `DashboardAuthMiddleware` | Password-based gate for the monitoring dashboard |
| `ip_allowlist.go` | `IPAllowlistMiddleware` | CIDR-aware IP gating for monitoring endpoints |
| `logger.go` | `Logger` | Structured log writer (file + optional console); `LogRequest`, `Error`, `Info`, `Debug` |
| `recovery.go` | — | Panic recovery middleware; converts panics to 500 responses |

### Middleware Chain Order

```
Recovery → CORS → Request Logger + API Tracker → (route-level middleware) → Handler
```

Route-level middleware is applied _inside_ the global chain, closest to the handler.

## `internal/models`

Plain Go structs with BSON and JSON tags used throughout the codebase.

| File | Types |
|------|-------|
| `torrent.go` | `Torrent`, `TorrentDetails`, `SearchOptions` |
| `user.go` | `User`, `UserSession`, `AuthExchangeCode` |
| `favorite.go` | `FavoriteEntry`, `CachedLink`, `FavoriteGroup` |
| `cache.go` | `StreamURL`, `CoverImage`, `KVCache`, `MagnetLink` |
| `torrent_test.go` | Unit tests for torrent model normalization |

`Torrent` is the central data transfer object: name, size, seeders, leechers, magnet link, source site, category, upload date, cover image URL.

## `internal/services/scraper`

| File | Scraper | Source |
|------|---------|--------|
| `service.go` | `Service` | Registry + fan-out orchestration |
| `hiddenbay.go` | `HiddenBayScraper` | Registered as both `"piratebay"` and `"hiddenbay"` |
| `x1337.go` | `X1337Scraper` | 1337x (via FlareSolverr if configured) |
| `yts.go` | `YTSScraper` | YTS JSON API |
| `nyaa.go` | `NyaaScraper` | NyaaSI RSS feed |
| `lime.go` | `LimeScraper` | LimeTorrents HTML |
| `torrentproject.go` | `TorrentProjectScraper` | TorrentProject HTML |

All scrapers implement the `Scraper` interface (`Search`). Some also implement `DetailsScraper` (`GetDetails`) and/or `BrowseScraper` (`Browse`).

`Service` holds a shared `*http.Client` (30 s timeout, connection pooling) passed to every scraper constructor to avoid per-scraper connection pools.

## `internal/services/realdebrid`

`Client` wraps the Real-Debrid REST API:
- `AddMagnet` — submit a magnet to RD's torrent queue
- `GetTorrentInfo` — poll for torrent status
- `SelectFiles` — trigger download of video files
- `UnrestrictLink` — convert RD download link to a streamable URL
- `RefreshStreamURL` — full round-trip: add magnet → select files → unrestrict → return stream URL

Errors are typed as `*APIError` with HTTP status code so the job runner can classify rate-limit vs auth failures.

## `internal/services/jobs`

`Runner` holds the storage provider and Real-Debrid client and exposes two jobs:

- `StorageCleanup(ctx)` — calls `storage.Cleanup` which removes expired documents
- `StreamURLRefresh(ctx)` — iterates all users with saved Real-Debrid keys, decrypts each key with `crypto.DecryptSecret`, refreshes each favorited torrent's stream URL, respects rate limits with a 30 s back-off, and writes results to the monitoring log

## `internal/crypto`

Provides AES-256-GCM encryption for secrets (e.g., user Real-Debrid API keys) stored in the database. The encryption key is derived from `REAL_DEBRID_ENCRYPTION_KEY` or falls back to `SESSION_SECRET` via SHA-256.

Ciphertext format: `v1:<base64 nonce>:<base64 tag>:<base64 ciphertext>`

Legacy plaintext values (no `v1:` prefix) are returned as-is by `DecryptSecret` for backward compatibility.

## `pkg/storage`

`Database` is the central interface implemented by `pkg/mongo`. Every storage operation that a handler needs flows through this interface, making the underlying driver swappable.

## `pkg/mongo`

MongoDB driver implementation of `pkg/storage.Database`. Key details:

- `NewClient` — connects with 15 s timeout, pings primary, returns error if unreachable
- `Migrate` — creates all necessary indexes on startup (unique, TTL, compound)
- Connection pool: max 10 connections
- `HealthCheck` — ping with 5 s timeout; returns structured `HealthStatus`
- Collections used: `users`, `user_sessions`, `auth_exchange_codes`, `favorite_entries`, `torrent_details`, `stream_urls`, `images`, `cached_links`, `cache`

## `pkg/turso`

Legacy package retained for its shared types (`HealthStatus`, `Stats`, `DBTableStats`) referenced by `pkg/mongo`. The Turso/libSQL client code is present but not used in the active deployment — MongoDB (`pkg/mongo`) is the active storage backend.

## `scripts/`

| Script | Purpose |
|--------|---------|
| `build-and-test.sh` | Pre-push: checks Go version, runs `go mod tidy`, `go build`, `go test ./...`, `go vet`, `gofmt` |
| `setup.sh` | Developer onboarding: `go mod download`, installs `golangci-lint`, `mockgen`, `errcheck`, `govulncheck`, sets up pre-commit hooks |

## `deployments/`

| File | Purpose |
|------|---------|
| `Dockerfile` | Multi-stage build: `golang:1.25-alpine` builder → `alpine:3.19` runtime with non-root user, dumb-init, health check |
| `docker-compose.yml` | Local compose setup with optional FlareSolverr sidecar |

## `static/`

`dashboard.html` is a self-contained browser monitoring dashboard served at `/`. It polls the `/api/monitoring/*` endpoints and renders job status, API usage charts, and log viewers. No build step — plain HTML + inline JS.
