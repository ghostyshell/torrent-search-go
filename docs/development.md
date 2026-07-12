# Development Guide

## Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| Go | 1.25+ | Matches `go.mod` (`go 1.25.0`) |
| Docker | 24+ | Optional - for container builds |
| MongoDB | 6+ | Or use a cloud instance (e.g., MongoDB Atlas free tier) |

## Quick Start

```bash
# 1. Clone
git clone https://github.com/akshatsinghkaushik/torrent-search-go.git
cd torrent-search-go

# 2. Install dependencies
go mod download

# 3. Configure environment
cp .env.example .env
# Edit .env - at minimum: MONGODB_URI, GOOGLE_SERVICE_ACCOUNT_JSON,
#              GOOGLE_CUSTOM_SEARCH_ENGINE_ID, SESSION_SECRET

# 4. Run
go run .
# Server starts on http://localhost:3001
```

## Automated Setup

`scripts/setup.sh` handles the above plus installs dev tools:

```bash
./scripts/setup.sh
```

This installs: `golangci-lint`, `mockgen`, `errcheck`, `govulncheck`, and pre-commit hooks.

## Environment Variables

See [configuration.md](configuration.md) for the full variable reference.

For local development the minimum viable `.env` is:

```env
NODE_ENV=development
MONGODB_URI=mongodb://localhost:27017
SESSION_SECRET=dev-secret-change-in-production
GOOGLE_SERVICE_ACCOUNT_JSON={"type":"service_account","oauth_client_id":"...","oauth_client_secret":"..."}
GOOGLE_CUSTOM_SEARCH_ENGINE_ID=your-cse-id
```

Image processing (Google CSE) is optional - cover images primarily come from torrent description scraping.

## Build

```bash
# Build binary
go build -o server .

# Build with optimisations (matches Dockerfile)
CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server .

# Quick compile check (no output binary)
go build ./...
```

## Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Specific package
go test ./internal/models/...

# Verbose
go test -v ./...
```

## Pre-Push Validation

Run before pushing to ensure the build is clean:

```bash
./scripts/build-and-test.sh
```

This script:
1. Checks Go version compatibility
2. Runs `go mod tidy` and verifies no uncommitted changes
3. Builds the application
4. Runs `go test ./...`
5. Runs `go vet ./...`
6. Checks `gofmt` formatting

Alternatively, run manually:

```bash
go mod tidy && go build -o /tmp/test-server . && go vet ./... && rm /tmp/test-server
```

## Linting

```bash
golangci-lint run
```

The pre-commit hooks (installed by `setup.sh`) run `go vet` and `gofmt` on every commit.

## Docker

### Local build

```bash
# Build image
docker build -f deployments/Dockerfile -t torrent-search-go .

# Run
docker run -p 3001:3001 --env-file .env torrent-search-go
```

### Docker Compose

```bash
# Copy env
cp .env.example .env
# Edit .env

# Start
docker compose -f deployments/docker-compose.yml up
```

The compose file mounts a named volume for logs and runs a health check on `/health`.

## Project Layout

```
torrent-search-go/
├── main.go               Entry point
├── internal/
│   ├── config/           Config loading and validation
│   ├── handlers/         HTTP handlers (one file per route group)
│   ├── middleware/        Router, auth, CORS, logger, recovery
│   ├── models/           Data structs
│   └── services/
│       ├── jobs/         Background job runner
│       ├── realdebrid/   Real-Debrid API client
│       └── scraper/      Torrent indexer scrapers
├── pkg/
│   ├── mongo/            MongoDB storage implementation
│   ├── storage/          Storage interface
│   └── turso/            Legacy types (HealthStatus, Stats)
├── scripts/              Developer tooling
├── deployments/          Dockerfile + docker-compose
├── static/               Dashboard HTML
└── docs/                 Documentation
```

See [code-structure.md](code-structure.md) for a deeper package-by-package tour.

## Adding a New Scraper

1. Create `internal/services/scraper/<name>.go` implementing `Scraper` (and optionally `DetailsScraper`, `BrowseScraper`).
2. Register it in `main.go`:
   ```go
   scraperService.RegisterScraper("mysite", scraper.NewMySiteScraper(scraperService.GetHTTPClient()))
   ```
3. Return `*models.Torrent` values with all fields populated consistently with existing scrapers.
4. Add tests in `internal/services/scraper/<name>_test.go` using recorded HTTP fixtures.

## Adding a New API Endpoint

1. Add a handler method to the appropriate handler file in `internal/handlers/`.
2. Register the route in the matching `register*Routes` function in `main.go`.
3. Apply middleware inline: `router.Get("/api/foo", handler.Foo, authMiddleware.RequireAuth)`.
4. Document the endpoint in [api-reference.md](api-reference.md).

## Deployment

### Generic Docker

Any platform that runs containers works without modification. The binary listens on `$PORT` (default 8080). Provide all required environment variables to the container.

```bash
docker build -f deployments/Dockerfile -t torrent-search-go .
docker push your-registry/torrent-search-go
```

### Railway

`railway.json` and `railpack.toml` configure Railway to use `deployments/Dockerfile`. Set all environment variables in the Railway dashboard. `PORT` is injected automatically.

### Other Platforms (Render, Fly.io, Cloud Run, etc.)

1. Point the build at `deployments/Dockerfile` or use `go build` directly.
2. Set `$PORT` to whatever port the platform exposes (or let the platform inject it).
3. Configure a health check at `GET /health`.
4. Set all required environment variables.

## Monitoring

The web dashboard at `http://localhost:3001/` (served from `static/dashboard.html`) shows:
- Background job status and logs
- API usage statistics
- Database health

Protect it with `DASHBOARD_PASSWORD` and `MONITORING_IP_ALLOWLIST` in production.

## Security Notes

- Session tokens are random 32-byte hex strings stored as `session_token` cookies (HttpOnly, SameSite=Lax).
- User Real-Debrid keys are encrypted with AES-256-GCM before storage. See [services.md](services.md#secret-encryption).
- The monitoring dashboard is gated by IP allowlist + password - do not expose it publicly without both.
- `ALLOWED_EMAILS` restricts OAuth login to specific Google accounts.
