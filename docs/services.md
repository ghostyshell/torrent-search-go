# Services

This document describes the major external integrations and internal service components.

## Torrent Scrapers

### Overview

`internal/services/scraper.Service` is the central scraper registry. At startup, `main.go` registers eight scrapers — each targeting a different public torrent indexer. Every scraper gets the shared `*http.Client` (30 s timeout, 100 max idle connections) to avoid redundant connection pools.

```
scraperService.RegisterScraper("piratebay",      hiddenBay)
scraperService.RegisterScraper("hiddenbay",      hiddenBay)
scraperService.RegisterScraper("1337x",          x1337Scraper)
scraperService.RegisterScraper("yts",            ytsScraper)
scraperService.RegisterScraper("nyaasi",         nyaaScraper)
scraperService.RegisterScraper("limetorrent",    limeScraper)
scraperService.RegisterScraper("torrentproject", torrentProjectScraper)
scraperService.RegisterScraper("pornrips",       pornripsScraper)
```

The `pornrips` scraper targets the PornRips.to release blog and fetches detail pages to extract magnet or `.torrent` links because the listing page does not expose them.

### Search Execution

**Single-site search** (`GET /api/torrents/search/:website/:query/:page?`):

```
handler → scraperService.Search(website, query, page, options)
             → scraper.Search(ctx, query, page, options)
             → HTTP GET to indexer → HTML/JSON parse → []Torrent
```

**Multi-site search** (`POST /api/torrents/advanced-search`):

```
handler → scraperService.SearchAll(query, page, options)
             → goroutine per registered scraper (fan-out)
             → results collected on buffered channel
             → merge, filter by MinSeeders / MaxResults, return
```

Each goroutine runs within the request context — if the client disconnects, scrapers abort via context cancellation.

### Scraper Interfaces

```go
// All scrapers implement:
type Scraper interface {
    Search(ctx, query string, page int, options SearchOptions) ([]Torrent, error)
}

// Some additionally implement:
type DetailsScraper interface {
    Scraper
    GetDetails(ctx, torrentURL string) (*TorrentDetails, error)
}

type BrowseScraper interface {
    Scraper
    Browse(ctx, category string, page int, sort string, options SearchOptions) ([]Torrent, error)
}
```

### FlareSolverr Integration

1337x uses Cloudflare protection. The 1337x scraper accepts a `FLARE_SOLVERR_URL` environment variable and routes requests through a FlareSolverr proxy instance when configured. Without it, 1337x searches will fail with a 403.

## Real-Debrid Integration

Real-Debrid is a premium link resolver that turns magnet links into direct, high-speed HTTP streams.

### Stream URL Resolution Flow

```
magnet link
    │
    ▼
POST /torrents/addMagnet          → RD torrent ID
    │
    ▼
GET  /torrents/info/{id}           → poll until "downloaded" status
    │
    ▼
POST /torrents/selectFiles         → trigger file selection (video files only)
    │
    ▼
POST /unrestrict/link              → convert RD link to streamable URL
    │
    ▼
stream URL  (stored in MongoDB cache)
```

### Proxy

`/api/proxy/real-debrid/*` is an authenticated reverse proxy. The user's Real-Debrid API key is decrypted server-side and injected as a header — the key is never exposed to the frontend. `RequireAuth` + `GetUserRealDebridKey` middleware gates this endpoint.

### Error Classification

`realdebrid.APIError` carries the HTTP status code from the RD API. The job runner classifies errors into:

| Code | Category |
|------|---------|
| 401 | `rd_auth_error` |
| 403 | `rd_forbidden` |
| 429 | `rd_rate_limit` (triggers 30 s back-off) |
| 5xx | `rd_server_error` |
| — | `no_video_files`, `magnet_error`, `timeout`, `other` |

## Image Processing

### Google Images Search

`GET /api/images/google-images/search?q=...` calls the Google Custom Search API using the server-side service account credentials. Results are cover image URLs for a given title.

`GET /api/images/google-images/suggestions` returns search-as-you-type suggestions.

### Pixhost Upload

`POST /api/images/pixhost/upload` accepts an image URL, fetches the image server-side, and uploads it to Pixhost. The resulting Pixhost URL is stored in MongoDB as the canonical cover image.

`GET /api/images/pixhost/fallbacks?url=...` returns alternative CDN URLs for a given Pixhost image (used when the primary URL is temporarily unavailable).

### Batch Processing

`POST /api/images/batch-process` accepts an array of torrent keys and processes cover image lookup + upload for each in a single request. Used by the background image-cache job.

### Image Extractors

`internal/services/images` contains a dispatcher and per-host extractors that resolve image-viewer pages to direct image URLs. Supported hosts include:

- `trafficimage.club`
- `imgtraffic.com`
- `imgbb.com`
- `postimg.cc`
- `imgur.com`
- `fastpic.org` / `fastpic.ru`
- `xxxwebdlxxx.org`

The description/image cache job passes each scraped cover-image candidate through the extractor before storing it, so the database keeps a direct image URL rather than a viewer page URL.

## S3-Compatible Cover Storage

When `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, and `S3_SECRET_ACCESS_KEY` are configured, the `ObjectStorage` service uploads cover images to an S3-compatible bucket and serves them via presigned URLs. The cover storage maintenance job refreshes presigned URLs before they expire and deletes temporary covers older than `S3_TEMP_EXPIRE_DAYS`.

## Background Jobs

Background jobs are managed by `handlers.JobScheduler`, which holds a `jobs.Runner` and a `MonitoringHandler`. Each job:

1. Is triggered on a `time.Ticker`
2. Runs in its own goroutine
3. Writes structured JSON-lines to a rotating log file in `LOG_DIR/`
4. Reports status to the monitoring dashboard via `MonitoringHandler`
5. Can also be triggered manually via `POST /api/monitoring/<job>-trigger`

### Storage Cleanup

- **Interval**: 1 hour
- **Action**: `mongo.Client.CleanupExpired` — bulk-deletes expired sessions, used exchange codes, and expired KV cache rows using TTL index queries.

### Stream URL Refresh

- **Interval**: 24 hours (configurable via `STREAM_URL_TTL_SECONDS`)
- **Action**: For every user with a stored (encrypted) Real-Debrid key, iterates their favorited torrents and re-resolves the stream URL. Paces requests with a 2 s inter-request delay; backs off 30 s on rate-limit errors.
- **Result shape**: `{ totalFavorites, usersProcessed, refreshed, skipped, failed, errorCounts }`

### Description / Image Cache

- **Interval**: 6 hours
- **Action**: Fetches and stores cover images for favorited torrents that lack one. Uses batch image processing.

### Search Results Cache

- **Interval**: 6 hours
- **Action**: Pre-warms search result caches for common queries.

### Job Log Maintenance

- **Interval**: 24 hours (initial delay: 15 minutes)
- **Action**: Compresses log files older than `BACKGROUND_JOB_LOG_COMPRESS_AFTER_MS` (default 6 h) and deletes files older than `BACKGROUND_JOB_LOG_RETENTION_DAYS` (default 30 days).

### Redis Catalog Cache

- **Interval**: 25–35 minutes (jittered)
- **Action**: Pre-populates Stremio addon catalog keys in Redis for browse, trans, and studio catalogs. Disabled when `REDIS_URL` is unset.
- **Result shape**: `{ catalogsCached, torrentsCached, skipped, errors }`

### Search Query Cache

- **Interval**: 2 hours
- **Action**: Reads recent `search_queries` rows, re-scrapes each query, caches cover images, and writes result + catalog entries to Redis. Cleans up query rows older than 2 days. Disabled when `REDIS_URL` is unset.
- **Result shape**: `{ queriesProcessed, totalTorrents, coversCached, redisEntries, cleanedUp }`

### Cover Storage Maintenance

- **Interval**: 5 hours
- **Action**: Refreshes S3 presigned URLs for stored covers and deletes expired temporary cover objects. Disabled when S3 is not configured.
- **Result shape**: `{ refreshed, deletedTemp }`

## Secret Encryption

User Real-Debrid API keys are stored encrypted in MongoDB using AES-256-GCM (`internal/crypto`). The encryption key is derived from `REAL_DEBRID_ENCRYPTION_KEY` (or `SESSION_SECRET`) via SHA-256.

Format: `v1:<nonce_b64>:<tag_b64>:<ciphertext_b64>`

This format is compatible with Node AES-256-GCM implementations using the same key derivation, allowing seamless key migration between runtimes.

## Monitoring Dashboard

The browser-based monitoring dashboard (`/`) is a single-page HTML file served from `static/dashboard.html`. It is protected by `DASHBOARD_PASSWORD` (via `DashboardAuthMiddleware`) and optionally restricted by IP (`MONITORING_IP_ALLOWLIST`).

It surfaces:
- Background job last-run time, status, and log excerpts (now including Redis catalog, search query cache, and cover storage maintenance)
- API usage statistics (request count per endpoint)
- Database health and collection counts
- Manual job trigger buttons
- Stream refresh debug views
- Favorites debug panel and raw favorite entry lookup
