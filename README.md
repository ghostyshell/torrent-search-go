# Torrent Search Go

[![Discord](https://img.shields.io/badge/Discord-5865F2?style=for-the-badge&logo=discord&logoColor=white)](https://discord.gg/EbHcTNAqca)
[![Ko-fi](https://img.shields.io/badge/Ko--fi-FF5E5B?style=for-the-badge&logo=ko-fi&logoColor=white)](https://ko-fi.com/ghosty99)

A high-performance Go backend for torrent search and streaming. Aggregates results from multiple public indexers, resolves premium streaming links via Real-Debrid, manages user sessions and favorites in MongoDB, and exposes a clean REST API.

**License: GPL-3.0**

## Features

- **Multi-source search** - aggregates torrents from 9 indexers (PirateBay/HiddenBay, 1337x, YTS, NyaaSI, LimeTorrents, TorrentProject, PornRips, Sukebei) in parallel using goroutines
- **Google OAuth** - secure user authentication with per-session token management
- **Real-Debrid integration** - premium streaming link resolution with server-side proxy and encrypted key storage (AES-256-GCM)
- **MongoDB persistence** - sessions, favorites, cover images, stream URL cache, and generic KV store
- **S3-compatible cover storage** - optional cover-image uploads with presigned URLs and maintenance
- **Background jobs** - automated stream URL refresh, image cache pre-warming, storage cleanup, log rotation, Redis catalog cache, search query cache, and S3 cover maintenance
- **Image processing** - Google Images search, Pixhost upload, cover image caching with batch support, and image extractor service for viewer-page resolution
- **Monitoring dashboard** - browser-based dashboard at `/` with job status, API usage stats, and log viewer
- **Production middleware** - security headers, request ID tracing, and IP-based rate limiting

## Documentation

| Document | Description |
|----------|-------------|
| [docs/architecture.md](docs/architecture.md) | System overview, request lifecycle, concurrency model, why Go |
| [docs/code-structure.md](docs/code-structure.md) | Package-by-package tour of the entire codebase |
| [docs/api-reference.md](docs/api-reference.md) | Full endpoint reference with request/response examples |
| [docs/services.md](docs/services.md) | Scrapers, Real-Debrid, image processing, background jobs |
| [docs/configuration.md](docs/configuration.md) | Every environment variable with defaults and descriptions |
| [docs/development.md](docs/development.md) | Build, test, Docker, adding scrapers/endpoints |
| [docs/api-compatibility.md](docs/api-compatibility.md) | Route aliases, auth compatibility, field names, limitations |

## Project Structure

```
torrent-search-go/
├── main.go                 Entry point - wires all packages
├── internal/
│   ├── config/             Configuration loading and validation
│   ├── handlers/           HTTP request handlers
│   ├── middleware/         Router, auth, CORS, logger, recovery
│   ├── models/             Data structs (Torrent, User, Favorite, …)
│   └── services/
│       ├── images/         Image extractor dispatcher and host extractors
│       ├── jobs/           Background job runner
│       ├── objectstorage/  S3-compatible object storage client
│       ├── realdebrid/     Real-Debrid API client
│       ├── redis/          Redis client for catalog/query cache jobs
│       └── scraper/        Torrent indexer scrapers
├── pkg/
│   ├── mongo/              MongoDB storage driver
│   └── storage/            Storage interface
├── deployments/            Dockerfile + docker-compose.yml
├── scripts/                Build and setup scripts
├── static/                 Monitoring dashboard HTML
└── docs/                   Full documentation
```

## Quick Start

### Prerequisites

- Go 1.21+
- MongoDB (local or cloud)
- Google Cloud project with OAuth2 and Custom Search API enabled

### Run Locally

```bash
git clone https://github.com/akshatsinghkaushik/torrent-search-go.git
cd torrent-search-go

go mod download

cp .env.example .env
# Edit .env with your credentials

go run .
# Server starts on http://localhost:3001
```

### Build Binary

```bash
go build -o server .
./server
```

## Configuration

Required environment variables:

| Variable | Description |
|----------|-------------|
| `MONGODB_URI` | MongoDB connection string |
| `GOOGLE_SERVICE_ACCOUNT_JSON` | Google service account JSON (includes OAuth credentials) |
| `GOOGLE_CUSTOM_SEARCH_ENGINE_ID` | Google Custom Search Engine ID |
| `SESSION_SECRET` | Secret for session token signing and encryption |

Optional but commonly needed:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3001` | HTTP listen port |
| `FRONTEND_URL` | `http://localhost:3000` | Allowed CORS origin |
| `GOOGLE_CALLBACK_URL` | `/api/auth/google/callback` | OAuth redirect URL |
| `FLARE_SOLVERR_URL` | - | FlareSolverr URL for 1337x scraping |
| `REAL_DEBRID_ENCRYPTION_KEY` | - | Encryption key for stored RD keys (AES-256-GCM) |
| `DASHBOARD_PASSWORD` | - | Password for the monitoring dashboard |

See [docs/configuration.md](docs/configuration.md) for the complete variable reference.

## API Endpoints

### Health
- `GET /health` - basic health check
- `GET /health/detailed` - DB + scraper status

### Authentication
- `GET /api/auth/google` - initiate Google OAuth
- `GET /api/auth/google/callback` - OAuth callback
- `POST /api/auth/logout` - logout
- `GET /api/auth/user` - current user (requires auth)

### Torrents
- `GET /api/torrents/websites` - list available indexers
- `GET /api/torrents/search/:website/:query/:page?` - search one indexer
- `POST /api/torrents/advanced-search` - search all indexers in parallel
- `GET /api/torrents/browse/:category/:page?` - browse by category
- `GET /api/torrents/details/:website/:torrentUrl` - torrent details

### Storage / Cache
- `GET|POST /api/storage/stream-url` - stream URL cache
- `POST /api/storage/stream-url/refresh` - re-resolve via Real-Debrid (auth)
- `GET|POST /api/storage/favorites` - favorites CRUD (auth)
- `GET|POST /api/storage/cover-image` - cover image cache

See [docs/api-reference.md](docs/api-reference.md) for the full endpoint reference.

## Deployment

### Docker

```bash
docker build -f deployments/Dockerfile -t torrent-search-go .
docker run -p 3001:3001 --env-file .env torrent-search-go
```

### Docker Compose (includes optional FlareSolverr)

```bash
docker compose -f deployments/docker-compose.yml up
```

### Any Container Platform

1. Build with `deployments/Dockerfile` or `go build -o server .`
2. Expose `$PORT` (the platform injects this; defaults to 8080 in Docker)
3. Configure a health check at `GET /health`
4. Set all required environment variables

See [docs/development.md](docs/development.md) for full deployment guidance.

## Background Jobs

| Job | Interval | Description |
|-----|----------|-------------|
| Storage cleanup | 1 hour | Remove expired sessions and cache entries |
| Stream URL refresh | 24 hours | Re-resolve Real-Debrid links for all user favorites |
| Image cache | 6 hours | Pre-fetch cover images for favorited torrents |
| Search cache | 6 hours | Pre-warm common search results |
| Redis catalog cache | 25-35 min | Pre-populate Stremio addon catalog keys in Redis |
| Search query cache | 2 hours | Re-scrape recent queries and warm Redis + covers |
| Cover storage maintenance | 5 hours | Refresh S3 presigned URLs and delete expired temp covers |
| Log maintenance | 24 hours | Rotate and compress job log files |

## Pre-Push Validation

```bash
./scripts/build-and-test.sh
```

Or manually:

```bash
go mod tidy && go build -o /tmp/test-server . && go vet ./... && rm /tmp/test-server
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Write tests - `go test ./...` must pass
4. Run `./scripts/build-and-test.sh` before pushing
5. Submit a pull request

## License

This project is licensed under the **GNU General Public License v3.0**. See [LICENSE](LICENSE) for the full text.
