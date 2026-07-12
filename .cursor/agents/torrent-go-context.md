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
| Ops tools | `cmd/` is gitignored local-only tooling - not in the repo; runbook in the gitignored `.claude/agents/local-cmd-tools.md` |

## Environment

- `PORT=3001`, `MONGODB_URI`, `MONGODB_DB`
- `GOOGLE_SERVICE_ACCOUNT_JSON`, `GOOGLE_CUSTOM_SEARCH_ENGINE_ID`
- `GOOGLE_CALLBACK_URL`, `SESSION_SECRET`, `FRONTEND_URL`
- `REAL_DEBRID_API_KEY` (see `.env.example`)
- PornRips sync: `PORNRIPS_SYNC_INTERVAL_MS`, `PORNRIPS_INGEST_RECENT_PAGES`, `PORNRIPS_BACKFILL_DIRECT`, `PORNRIPS_ENRICH_PER_TICK`/`PORNRIPS_ENRICH_CONCURRENCY`, comma-separated `TPDB_API_KEY`/`STASHDB_API_KEY`

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

## Deploy & git sync

torrent-search-go ships to Sliplane via a GHCR image build, **not** `git push` (Sliplane's GitHub build integration is broken). Build the amd64 image, push to `ghcr.io/akshatsinghkaushik/torrent-search-go:latest`, and fire the Sliplane deploy hook per the gitignored `.cursor/agents/deploy-registry.md` (creds + hook secret live there, uncommitted). **After every deploy, commit the shipped changes and push** so git stays in sync with what is running in prod; a GHCR deploy without a commit leaves prod ahead of git. Push origin granular (never force-push) and alt flattened via `sh ~/Code/scripts/push-alt-flatten.sh`.

## PornRips bulk-fill

The full ~355k-post PornRips archive is bulk-filled into prod `pornrips_entries` by a one-shot run locally on the Mac against the public Mongo endpoint. The bulk-fill tooling is **local-only and gitignored** - it was removed from git tracking and purged from history on 2026-06-27, and `cmd/` is in `.gitignore`. The runbook (two-pole setup, env vars, ETA monitoring, sweep indexes, resume procedure, Mongo CPU diagnostics) lives in the gitignored `.claude/agents/local-cmd-tools.md`; creds live in the gitignored `slipline-ssh.md`. The deployed `PornripsSync` job (post-fill) runs recent-only: `PORNRIPS_INGEST_RECENT_PAGES=5` + `PORNRIPS_SYNC_INTERVAL_MS=21600000` (6h) on the container, so it only scrapes the top 5 WP pages every 6h for new posts (enriching + torrent-backfilling them via the newest-first sweep).
