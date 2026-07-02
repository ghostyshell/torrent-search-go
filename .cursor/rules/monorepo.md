# TorrentSearch — Cursor Rules

## Repositories

| Repo | Role | Default port | Stack |
|------|------|--------------|-------|
| `torrent-browse-ui/` | React frontend | 3000 | React 19, TypeScript, MUI, CRA/Craco |
| `torrent-search-go/` | Go backend (sole API) | 3001 | Go 1.25, goquery scrapers, MongoDB, Redis, S3 |

The UI talks to the backend at `REACT_APP_API_URL` (default `http://localhost:3001`).

> The former Node API (`Torrent-Search-API` / `torrent-search-node`) has been removed; `torrent-search-go` is the only backend.

## Cross-repo constraints

- **Scrapers**: live in `torrent-search-go/internal/services/scraper/`. When fixing a source site, update there.
- **API compatibility**: UI handler routes and response shapes must stay aligned with `torrent-search-go`.
- **Auth**: Google OAuth + session/JWT. Callback URL: `http://localhost:3001/api/auth/google/callback`.
- **Real-Debrid**: Used for unrestricted stream URLs. UI may call RD directly (`REACT_APP_REAL_DEBRID_API_KEY`); the backend also integrates RD server-side.
- **Secrets**: Never commit `.env`, API keys, or service account JSON. Use `.env.example` as reference.

## Recommended subagents by repo

| Repo | Primary agents |
|------|----------------|
| `torrent-browse-ui` | `torrent-ui-context`, `react-specialist`, `typescript-pro`, `test-automator` |
| `torrent-search-go` | `torrent-go-context`, `golang-pro`, `backend-developer`, `test-automator` |
| Cross-repo | `torrent-monorepo-context`, `code-reviewer`, `codebase-orchestrator` |

## Testing

| Repo | Command | Location |
|------|---------|----------|
| UI unit | `npm test` | `torrent-browse-ui/` |
| UI e2e | `npm run test:e2e` | `torrent-browse-ui/tests/e2e/` |
| Go | `go test ./...` | `torrent-search-go/` |

## Deployment

- **Go backend**: Docker (`Dockerfile`, `deployments/Dockerfile`); ships by pushing `main` (deploy is push-triggered).
- **UI**: Docker, static build (`npm run build`), Android variant (`npm run build:android`)

## When making changes

1. Identify which repo owns the behavior (UI vs backend vs scraper).
2. Run the relevant test suite before finishing.
3. Use `code-reviewer` after substantive changes.
