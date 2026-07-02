# API Reference

Base URL: `http://localhost:3001` (development) / `https://<your-domain>` (production)

All endpoints return JSON. Successful responses include `"success": true`; error responses include `"success": false, "error": "<message>"`.

Authentication uses a session cookie (`session_token`) set by the OAuth flow, or a `Bearer <token>` `Authorization` header for programmatic access.

---

## Health

### `GET /health`

Basic liveness check. No auth.

**Response 200**
```json
{ "status": "ok", "timestamp": "2024-01-15T10:00:00Z" }
```

### `GET /health/detailed`

Full system health including database and scraper status.

**Response 200**
```json
{
  "status": "ok",
  "database": { "status": "healthy", "type": "mongodb", "responseTime": 4, "timestamp": "..." },
  "scrapers": { "piratebay": true, "yts": true, ... },
  "uptime": 3600
}
```

### `GET /health/ready`

Readiness probe — returns 503 if the database is not reachable.

### `GET /health/live`

Liveness probe — always 200 as long as the process is running.

### `GET /health/1337x`

Tests connectivity to the 1337x scraper (direct fetch against the 1337xx.to mirror).

---

## Authentication

### `GET /api/auth/google`

Redirects the browser to Google's OAuth consent screen. No auth required.

**Query params**: none (callback URL configured via `GOOGLE_CALLBACK_URL`)

### `GET /api/auth/google/callback`

OAuth redirect target. Sets a `session_token` cookie and redirects to `FRONTEND_URL`. No auth required.

### `POST /api/auth/exchange`

Exchange a short-lived auth code for a session (used by mobile / SPA clients that cannot follow cookie redirects).

**Body**
```json
{ "code": "<auth_exchange_code>" }
```

**Response 200**
```json
{ "success": true, "sessionToken": "<token>", "user": { "id": "...", "email": "...", "name": "..." } }
```

### `POST /api/auth/logout`

Destroys the current session. Requires auth.

**Response 200**
```json
{ "success": true }
```

### `GET /api/auth/user`

Returns the currently authenticated user. Requires auth.

**Response 200**
```json
{ "success": true, "user": { "id": "...", "email": "...", "name": "...", "picture": "..." } }
```

### `POST /api/auth/validate`

Validates a session token without requiring a cookie.

**Body**
```json
{ "sessionToken": "<token>" }
```

**Response 200**
```json
{ "success": true, "valid": true, "user": { ... } }
```

### `GET /api/auth/sessions`

Lists all active sessions for the current user. Requires auth.

### Real-Debrid Key Management

All three endpoints require auth.

#### `GET /api/auth/realdebrid/api-key`

Returns whether the user has a Real-Debrid key stored (never returns the key itself).

```json
{ "success": true, "hasApiKey": true }
```

#### `POST /api/auth/realdebrid/api-key`

Stores an encrypted Real-Debrid API key for the current user.

**Body**: `{ "apiKey": "<rd_key>" }`

#### `DELETE /api/auth/realdebrid/api-key`

Removes the stored Real-Debrid key.

---

## Torrents

No auth required on any torrent endpoint unless noted.

### `GET /api/torrents/websites`

Returns the list of registered scraper names.

```json
{ "success": true, "websites": ["piratebay", "hiddenbay", "1337x", "yts", "nyaasi", "limetorrent", "torrentproject", "pornrips"] }
```

### `GET /api/torrents/search/:website/:query/:page?`

Search a single indexer.

| Param | Type | Description |
|-------|------|-------------|
| `website` | path | Indexer name (see `/api/torrents/websites`) |
| `query` | path | URL-encoded search query |
| `page` | path (optional) | Page number (default: 1) |
| `minSeeders` | query | Filter results with fewer seeders |
| `maxResults` | query | Cap result count |
| `category` | query | Category filter (indexer-specific) |

**Response 200**
```json
{
  "success": true,
  "torrents": [
    {
      "name": "Example Title",
      "size": "2.1 GB",
      "seeders": 342,
      "leechers": 12,
      "magnetLink": "magnet:?xt=urn:btih:...",
      "source": "piratebay",
      "category": "Movies",
      "uploadDate": "2024-01-10",
      "coverImage": "https://..."
    }
  ],
  "total": 1,
  "page": 1
}
```

### `GET /api/torrents/browse/:category/:page?`

Browse by category on a supporting indexer. Not all scrapers implement `BrowseScraper`; unsupported scrapers return an error.

### `POST /api/torrents/advanced-search`

Search across all registered indexers in parallel.

**Body**
```json
{
  "query": "example title",
  "page": 1,
  "minSeeders": 10,
  "maxResults": 50
}
```

**Response**: same shape as single-site search, with `source` field distinguishing each result's origin.

### `GET /api/torrents/details/:website/:torrentUrl`

Fetch detailed torrent information (file list, description, cover images). Only available on scrapers that implement `DetailsScraper`.

| Param | Type | Description |
|-------|------|-------------|
| `website` | path | Indexer name |
| `torrentUrl` | path | URL-encoded torrent detail page URL |

---

## Cache / Storage

The `/api/cache/*` and `/api/storage/*` prefixes are equivalent aliases for the same handlers. All cache/storage endpoints **require session authentication**, matching the NodeJS backend. The `ADDON_API_TOKEN` (sent as `X-Addon-Token`) is used only to bypass rate limiting for trusted addon traffic.

### Stream URLs

#### `GET /api/cache/stream-url/:magnetHash`

Retrieve a cached stream URL by magnet hash. Requires auth.

**Response 200**
```json
{ "success": true, "streamUrl": "https://...", "expires": 1705312800 }
```

**Response 404**: no cached URL for this hash.

#### `POST /api/cache/stream-url`

Store a stream URL. Requires auth.

**Body**: `{ "magnetHash": "...", "magnetLink": "...", "streamUrl": "...", "expires": 1705312800 }`

#### `POST /api/cache/stream-url/refresh`

Re-resolve a stream URL via Real-Debrid using the authenticated user's key. Requires auth.

**Body**: `{ "magnetLink": "magnet:?xt=urn:btih:..." }`

**Response 200**: `{ "success": true, "streamUrl": "https://..." }`

### Cover Images

#### `GET /api/cache/cover-image/:torrentKey`

Retrieve a cached cover image URL for a torrent key. Requires auth.

#### `POST /api/cache/cover-image`

Store a cover image URL. Requires auth.

**Body**: `{ "torrentKey": "...", "imageUrl": "...", "imageType": "cover" }`

#### `POST /api/cache/cover-image/torrent`

Retrieve cover image for a torrent by name/metadata (used by the UI). Requires auth.

### Magnet Links

#### `POST /api/cache/magnet`

Store a magnet link by hash. Requires auth.

#### `GET /api/cache/magnet`

Retrieve a magnet link by hash. `?hash=<magnetHash>`. Requires auth.

### Generic KV Cache

Requires auth.

- `POST /api/cache/set` — `{ "key": "...", "value": "...", "ttl": 3600 }`
- `GET /api/cache/get/:key` — returns `{ "value": "..." }`
- `DELETE /api/cache/delete/:key`

### Statistics

#### `GET /api/cache/stats`

Returns database collection counts and connection health. No auth.

---

## Favorites

All favorites endpoints require auth.

### `GET /api/storage/favorites`

Returns all favorites for the authenticated user.

```json
{
  "success": true,
  "favorites": [
    { "id": "...", "torrentKey": "...", "torrentName": "...", "magnetLink": "...", "createdAt": "..." }
  ]
}
```

### `POST /api/storage/favorites`

Add a favorite.

**Body**: `{ "torrentKey": "...", "torrentName": "...", "magnetLink": "..." }`

### `DELETE /api/storage/favorites`

Remove a favorite.

**Body**: `{ "torrentKey": "..." }`

### Favorite Details

- `GET  /api/favorites/:favoriteId/details` — cached torrent details
- `POST /api/favorites/:favoriteId/details` — store details
- `POST /api/favorites/check` — check if a torrent is favorited: `{ "torrentKey": "..." }`
- `POST /api/favorites/entry` — upsert a full favorite entry

### Stored / Cached Links

Requires auth. Alias: `/api/cache/cached-links/*`

- `GET    /api/storage/stored-links` — list
- `POST   /api/storage/stored-links` — add
- `PUT    /api/storage/stored-links/:id` — update
- `DELETE /api/storage/stored-links/:id` — remove

### Update Magnet on Favorite Entry

`PUT /api/storage/favorites/:favoriteId/magnet` — Requires auth.

**Body**: `{ "magnetLink": "magnet:?xt=urn:btih:..." }`

---

## Images

No auth required.

### `GET /api/images/google-images/search`

Search Google Images for cover art.

**Query**: `?q=<title>&num=<count>`

**Response**: `{ "success": true, "images": [{ "url": "...", "thumbnail": "..." }] }`

### `GET /api/images/google-images/suggestions`

Type-ahead search suggestions.

**Query**: `?q=<partial>`

### `POST /api/images/pixhost/upload`

Upload an image to Pixhost by URL.

**Body**: `{ "imageUrl": "https://...", "torrentKey": "..." }`

**Response**: `{ "success": true, "pixhostUrl": "https://img.pixhost.to/..." }`

### `GET /api/images/pixhost/fallbacks`

Get alternative CDN URLs for a Pixhost image.

**Query**: `?url=<pixhost_url>`

### `POST /api/images/batch-process`

Process cover images for multiple torrents in one call.

**Body**: `{ "torrents": [{ "torrentKey": "...", "title": "..." }] }`

---

## Proxy

### `ALL /api/proxy/real-debrid/*`

Authenticated reverse proxy to the Real-Debrid API. Requires auth. The user's decrypted RD key is injected as an `Authorization` header before forwarding.

**Example**: `GET /api/proxy/real-debrid/torrents/info/ABCDEF` → proxied to `https://api.real-debrid.com/rest/1.0/torrents/info/ABCDEF`

---

## Monitoring

All monitoring endpoints require both IP allowlist (`MONITORING_IP_ALLOWLIST`) and dashboard password (`DASHBOARD_PASSWORD`) authentication.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/monitoring/dashboard` | Aggregate dashboard data |
| `GET` | `/api/monitoring/logs` | Recent application logs |
| `GET` | `/api/monitoring/tasks` | Background task stats |
| `GET` | `/api/monitoring/api-usage` | Request count per endpoint |
| `GET` | `/api/monitoring/stream-url-refresh-logs` | Stream refresh job logs |
| `POST` | `/api/monitoring/stream-url-refresh-trigger` | Trigger stream refresh immediately |
| `GET` | `/api/monitoring/description-image-cache-logs` | Image cache job logs |
| `POST` | `/api/monitoring/description-image-cache-trigger` | Trigger image cache job |
| `POST` | `/api/monitoring/description-image-cache-force-refresh` | Force full image re-cache |
| `GET` | `/api/monitoring/search-results-cache-logs` | Search cache job logs |
| `POST` | `/api/monitoring/search-results-cache-trigger` | Trigger search cache job |
| `GET` | `/api/monitoring/redis-catalog-cache-logs` | Redis catalog cache job logs |
| `POST` | `/api/monitoring/redis-catalog-cache-trigger` | Trigger Redis catalog cache job |
| `GET` | `/api/monitoring/search-query-cache-logs` | Search query cache job logs |
| `POST` | `/api/monitoring/search-query-cache-trigger` | Trigger search query cache job |
| `POST` | `/api/monitoring/cover-storage-maintenance-trigger` | Trigger cover storage maintenance |
| `GET` | `/api/monitoring/job-logs/list` | List job log files |
| `GET` | `/api/monitoring/job-logs/search` | Search job logs |
| `GET` | `/api/monitoring/job-logs/file` | Download a log file |
| `POST` | `/api/monitoring/job-logs/maintenance` | Trigger log rotation/cleanup |
| `GET` | `/api/monitoring/debug-favorites` | Favorites debug stats |
| `GET` | `/api/monitoring/debug-stream-refresh` | Stream refresh debug info |

### Debug

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/debug/favorite-entry/:favoriteEntryId` | IP-restricted raw lookup of a favorite entry |

---

## REST Contract Notes

- Pagination starts at `page=1` (not 0).
- All dates are ISO 8601 strings.
- `magnetLink` values are full `magnet:?xt=urn:btih:...` strings.
- `torrentKey` is a stable, URL-safe identifier derived from title + source.
- Endpoints under `/api/cache/*` and `/api/storage/*` are identical aliases; both prefixes are supported for backward compatibility.
