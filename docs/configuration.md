# Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` and fill in values for local development.

## Core Server

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `PORT` | `3001` | No | HTTP listen port. In containerized deployments, the hosting platform typically injects this. |
| `HOST` | `0.0.0.0` | No | Bind address. Leave as `0.0.0.0` to accept connections on all interfaces. |
| `NODE_ENV` | `development` | No | `"production"` enables production hardening: rate limiting, stricter CORS, trust-proxy headers. |

## Database - MongoDB

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MONGODB_URI` | - | **Yes** | MongoDB connection string. Supports `mongodb://` and `mongodb+srv://` schemes. |
| `MONGODB_DB` | `torrent_search` | No | Database name. |
| `MONGO_USERNAME` | - | No | If provided alongside `MONGO_PASSWORD`, credentials are injected into `MONGODB_URI` automatically (safe URL encoding applied). |
| `MONGO_PASSWORD` | - | No | See `MONGO_USERNAME`. |
| `MONGO_URL` | - | No | Alias for `MONGODB_URI` (checked as fallback). |

## Google APIs

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `GOOGLE_SERVICE_ACCOUNT_JSON` | - | **Yes** | Full JSON content of a Google service account with OAuth2 and Custom Search API enabled. |
| `GOOGLE_CUSTOM_SEARCH_ENGINE_ID` | - | **Yes** | Custom Search Engine ID (cx parameter) for Google Images queries. |
| `GOOGLE_CALLBACK_URL` | `/api/auth/google/callback` | No | Absolute URL for the OAuth redirect. Must match the URI registered in Google Cloud Console. Example: `https://api.example.com/api/auth/google/callback`. |
| `GOOGLE_OAUTH_CLIENT_ID` | - | Prod only | Google OAuth client ID. Can be embedded in `GOOGLE_SERVICE_ACCOUNT_JSON` under the `oauth_client_id` key. |
| `GOOGLE_OAUTH_CLIENT_SECRET` | - | Prod only | Google OAuth client secret. Can be embedded in `GOOGLE_SERVICE_ACCOUNT_JSON` under `oauth_client_secret`. |

## Session & Security

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `SESSION_SECRET` | - | **Yes** | Random secret used to sign session tokens and as fallback encryption key. Use at least 32 random bytes in production. |
| `REAL_DEBRID_ENCRYPTION_KEY` | - | No | Preferred key for encrypting user Real-Debrid API keys at rest (AES-256-GCM). Falls back to `SESSION_SECRET` if unset. |
| `ALLOWED_EMAILS` | - | No | Comma-separated list of Google account emails permitted to log in. Leave empty to allow any Google account. Example: `alice@example.com,bob@example.com`. |
| `MONITORING_IP_ALLOWLIST` | - | No | Comma-separated IPs or CIDR ranges allowed to access `/api/monitoring/*`. Leave empty to rely on `DASHBOARD_PASSWORD` only. Example: `127.0.0.1,10.0.0.0/8`. |
| `DASHBOARD_PASSWORD` | - | No | Password protecting the monitoring dashboard (`/`). Empty disables password auth (not recommended in production). |

## CORS

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `FRONTEND_URL` | `http://localhost:3000` | No | Primary allowed CORS origin. Set to your frontend URL in production. |
| `ADDITIONAL_CORS_ORIGINS` | - | No | Comma-separated additional allowed origins. Example: `https://app.example.com,https://admin.example.com`. |

In development mode (`NODE_ENV != "production"`), `localhost:3000` and `localhost:3001` are always allowed. In production, only `FRONTEND_URL` and `ADDITIONAL_CORS_ORIGINS` are allowed (falls back to `*` if both are empty).

## External Integrations

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `REAL_DEBRID_API_KEY` | - | No | Real-Debrid API key for server-side stream resolution (non-user-specific). Usually left empty - user keys are stored per-user in the database. |
| `HIDDENBAY_URL` | - | No | Override URL for the HiddenBay/PirateBay scraper. Useful if the default domain is blocked. |
| `ADDON_API_TOKEN` | - | No | Shared secret sent by the addon as `X-Addon-Token` to skip IP-based rate limiting. Cache/storage endpoints use session auth; this token is **not** used as primary auth. |
| `ADDON_CACHE_BASE_URL` | - | No | Public URL of this API as seen by the Stremio addon (`BACKEND_URL`). Used as the first segment of `cat:v1:` Redis keys. Falls back to `BASE_URL`. **Must match the addon's `BACKEND_URL`.** |
| `TPDB_API_KEY` | - | No | ThePornDB API key. Enables the category warmer for TPDB browse catalogs. |
| `STASHDB_API_KEY` | - | No | StashDB API key. Enables the category warmer for StashDB browse catalogs. |
| `TPDB_API_URL` | `https://api.theporndb.net` | No | TPDB API base URL. |
| `STASHDB_API_URL` | `https://stashdb.org` | No | StashDB GraphQL base URL. |
| `CATEGORY_WARMER_INITIAL_MS` | `300000` (5 min) | No | Delay before the first category warmer run after startup. |
| `CATEGORY_WARMER_INTERVAL_MS` | `10800000` (3 h) | No | Interval between category warmer runs. |
| `META_ENRICHER_INTERVAL_MS` | `60000` (1 min) | No | Interval between meta enricher queue drains. |
| `META_ENRICHER_MAX_PER_TICK` | `150` | No | Max metadata lookups processed per enricher tick. |
| `REF_WARMER_INTERVAL_MS` | `3600000` (1 h) | No | Interval between reference metadata warmer runs. |

## Logging

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `LOG_LEVEL` | `debug` (dev) / `info` (prod) | No | Log verbosity: `debug`, `info`, `warn`, `error`. |
| `LOG_DIR` | `./logs` | No | Directory for application and background-job log files. Created automatically if absent. |
| `BACKGROUND_JOBS_LOG_VERSION` | `v1` | No | Log format version tag written to each job log file. |
| `BACKGROUND_JOB_LOG_RETENTION_DAYS` | `30` | No | Delete job log files older than this many days. Minimum: 1. |
| `BACKGROUND_JOB_LOG_COMPRESS_AFTER_MS` | `21600000` (6 h) | No | Compress log files after this many milliseconds. Minimum: 60000. |
| `BACKGROUND_JOB_LOG_MAINTENANCE_INTERVAL_MS` | `86400000` (24 h) | No | How often to run log maintenance. Minimum: 3600000 (1 h). |
| `BACKGROUND_JOB_LOG_MAINTENANCE_INITIAL_DELAY_MS` | `900000` (15 min) | No | Delay before first log maintenance run after startup. Minimum: 300000 (5 min). |

## Redis

Required by the Redis catalog cache and search query cache background jobs.

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `REDIS_URL` | - | No | Redis connection URL, e.g. `redis://localhost:6379/0`. When unset, the Redis-dependent jobs are disabled. |
| `REDIS_PASSWORD` | - | No | Password override applied on top of any password in `REDIS_URL`. |

## S3-Compatible Object Storage

Optional. When configured, cover images are uploaded to the bucket and served via presigned URLs instead of being stored as direct URLs in MongoDB.

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `S3_ENDPOINT` | - | No | S3 endpoint, e.g. `https://s3.example.com` or `https://xxx.r2.cloudflarestorage.com`. |
| `S3_REGION` | `auto` | No | S3 region. |
| `S3_BUCKET` | - | No | Bucket name. |
| `S3_ACCESS_KEY_ID` | - | No | Access key. |
| `S3_SECRET_ACCESS_KEY` | - | No | Secret key. |
| `S3_KEY_PREFIX` | `covers/` | No | Prefix for stored cover objects. |
| `S3_TEMP_EXPIRE_DAYS` | `7` | No | Temp covers (e.g., description-image-cache results) are deleted after this many days. |
| `S3_PRESIGN_DAYS` | `7` | No | Presigned URL lifetime in days; the maintenance job refreshes URLs before they expire. |

## Addon / Catalog

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `BASE_URL` | - | No | Canonical public URL of the API, used as a prefix for Stremio catalog Redis keys. Falls back to `FRONTEND_URL` or `RAILWAY_PUBLIC_DOMAIN`. |

## Cache

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `STREAM_URL_TTL_SECONDS` | `72000` (20 h) | No | How long a cached Real-Debrid stream URL is considered valid. The stream URL refresh job uses this as its re-resolution window. Minimum: 60. |

## Rate Limiting

Rate limiting is enabled automatically in production (`NODE_ENV=production`). Health endpoints and requests bearing a valid `X-Addon-Token` are skipped.

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `RATE_LIMIT_MAX` | `1000` | No | Maximum requests per 15-minute window per IP. Minimum enforced: 100. |

## Railway / Platform

| Variable | Auto-set | Description |
|----------|----------|-------------|
| `RAILWAY_ENVIRONMENT` | By platform | Detected to enable Railway-specific behaviours. |
| `RAILWAY_STATIC_URL` | By platform | Static asset CDN URL (Railway). |
| `RAILWAY_PUBLIC_DOMAIN` | By platform | Public-facing domain assigned by Railway. |

These variables are set automatically by the Railway platform and do not need to be configured manually.

## Minimal Production `.env`

```env
NODE_ENV=production

# MongoDB
MONGODB_URI=mongodb+srv://user:pass@cluster.mongodb.net
MONGODB_DB=torrent_search

# Google
GOOGLE_SERVICE_ACCOUNT_JSON={"type":"service_account","oauth_client_id":"...","oauth_client_secret":"...",...}
GOOGLE_CUSTOM_SEARCH_ENGINE_ID=xxxxxxxxxxxxxxxxx
GOOGLE_CALLBACK_URL=https://api.example.com/api/auth/google/callback

# Security
SESSION_SECRET=<64-char random hex>
REAL_DEBRID_ENCRYPTION_KEY=<64-char random hex>
ALLOWED_EMAILS=you@example.com

# CORS
FRONTEND_URL=https://app.example.com

# Optional
DASHBOARD_PASSWORD=<strong-password>
MONITORING_IP_ALLOWLIST=10.0.0.0/8
```
