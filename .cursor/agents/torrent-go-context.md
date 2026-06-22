---
name: torrent-go-context
description: torrent-search-go context. Use for Go handlers, goquery scrapers, MongoDB persistence, OAuth auth, Real-Debrid, background jobs, or API work.
---

You are working in **torrent-search-go** — a high-performance Go backend for torrent search and streaming.

## Stack

- Go 1.25, standard library HTTP with custom router middleware
- goquery for HTML scraping
- MongoDB for persistent storage
- Google OAuth, session management
- Real-Debrid, Google Images
- Docker deployment

## Key paths

| Area | Path |
|------|------|
| Entry point | `main.go` |
| Config | `internal/config/config.go`, `internal/config/environment.go` |
| Handlers | `internal/handlers/` (torrents, auth, cache, favorites, images, proxy, health, monitoring, storage) |
| Middleware | `internal/middleware/` (router, auth, cors, logger, recovery, ip_allowlist) |
| Scrapers | `internal/services/scraper/` (piratebay, x1337, yts, nyaa, lime, torrentproject, hiddenbay) |
| Stremio addon | `internal/stremio/` (manifest, catalog, meta, stream handlers) |
| Models | `internal/models/` (torrent, user, favorite, cache) |
| MongoDB client | `pkg/mongo/` |
| Tests | `internal/models/torrent_test.go`, `tests/` |
| Docker | `Dockerfile`, `deployments/Dockerfile` |

## Environment

- `PORT=3001`, `MONGODB_URI`, `MONGODB_DB`
- `GOOGLE_SERVICE_ACCOUNT_JSON`, `GOOGLE_CUSTOM_SEARCH_ENGINE_ID`
- `GOOGLE_CALLBACK_URL`, `SESSION_SECRET`, `FRONTEND_URL`
- `REAL_DEBRID_API_KEY` (see `.env.example`)

## Commands

```bash
go run .                 # start server
go test ./...            # all tests
go build -o server .     # build binary
```

## Conventions

- Routes and JSON responses follow the documented REST API contract (`docs/api-reference.md`)
- Use existing middleware chain; add new routes via the route-registration functions in `main.go`
- Scraper service is centralized in `internal/services/scraper/service.go`
- Stremio manifest `version` comes from the `addonVersion` const in `internal/stremio/manifest.go`, overridable by the `ADDON_VERSION` env var. The const is a fallback; the single source of truth is `tpb-stremio-addon/package.json`, and the Node edge stamps that version onto the proxied manifest. When the addon version bumps in the Node repo, bump this const too (and/or set `ADDON_VERSION` on deploy).
- Never commit `.env` or credentials
