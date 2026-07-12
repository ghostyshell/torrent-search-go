---
name: torrent-monorepo-context
description: TorrentSearch repo map and cross-repo coordination. Use when a task spans torrent-browse-ui and torrent-search-go, or when unsure which repo owns a feature.
---

You are working across the two TorrentSearch repos under `~/Code`:

## Repo map

| Directory | Purpose | Port |
|-----------|---------|------|
| `torrent-browse-ui/` | React frontend (search, streaming, Cast, favorites) | 3000 |
| `torrent-search-go/` | Go backend — Stremio manifest/catalog/meta, scrapers, REST API | 3001 |

> The former Node API (`Torrent-Search-API` / `torrent-search-node`) has been removed. `torrent-search-go` is now the sole backend.

## Coordination rules

1. **Scraper changes** live in `torrent-search-go/internal/services/scraper/`.
2. **UI changes** that depend on API shape must match `torrent-search-go`'s routes and response shapes.
3. **Never commit secrets** — `.env`, API keys, service account JSON.

## Delegation guide

- UI/React/streaming/Cast → stay in `torrent-browse-ui`, use `torrent-ui-context`
- Go handlers/scrapers/performance → `torrent-search-go`, use `torrent-go-context`
- Cross-repo refactors → coordinate with `codebase-orchestrator`

## Key integration points

- Frontend API base: `REACT_APP_API_URL` (default `http://localhost:3001`)
- Google OAuth callback: `/api/auth/google/callback`
- Real-Debrid: stream URL resolution (UI direct + API proxy)
- MongoDB: persistent storage (`torrent-search-go`)

When invoked, first identify which repo(s) are affected, then proceed in the correct directory with the repo-specific context agent.
