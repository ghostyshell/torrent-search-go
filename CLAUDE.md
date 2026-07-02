## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## Addon manifest version

The Stremio manifest `version` field is built in `internal/stremio/manifest.go` from the `addonVersion` const, overridable at runtime by the `ADDON_VERSION` env var. The const is a fallback for direct-backend hits only.

The single source of truth for the addon version is `tpb-stremio-addon/package.json`. The Node edge stamps its `ADDON_VERSION` onto the proxied manifest (`tpb-stremio-addon/src/utils/stremioGo.ts`), so Stremio sees the real version on edge deploy regardless of this const. When the addon version bumps in the Node repo, bump this const too (and/or set `ADDON_VERSION` on the backend deploy) so direct-backend hits and the fallback path stay aligned. Do not treat the const as the source of truth.

## Deploy & git sync

torrent-search-go ships to Sliplane via a GHCR image build, **not** `git push` (Sliplane's GitHub build integration is broken). Build the amd64 image, push to `ghcr.io/akshatsinghkaushik/torrent-search-go:latest`, and fire the Sliplane deploy hook per the gitignored `.claude/agents/deploy-registry.md` (creds + hook secret live there, uncommitted). **After every deploy, commit the shipped changes and push** so git stays in sync with what is running in prod; a GHCR deploy without a commit leaves prod ahead of git. Push origin granular (never force-push) and alt flattened via `sh ~/Code/scripts/push-alt-flatten.sh`.

## PornRips bulk-fill

The full ~355k-post PornRips archive is bulk-filled into prod `pornrips_entries` by a one-shot run locally on the Mac against the public Mongo endpoint. The bulk-fill tooling is **local-only and gitignored** - it was removed from git tracking and purged from history on 2026-06-27, and `cmd/` is in `.gitignore`. The runbook (two-pole setup, env vars, ETA monitoring, sweep indexes, resume procedure, Mongo CPU diagnostics) lives in the gitignored `.claude/agents/local-cmd-tools.md`; creds live in the gitignored `slipline-ssh.md`. The deployed `PornripsSync` job (post-fill) runs recent-only: `PORNRIPS_INGEST_RECENT_PAGES=5` + `PORNRIPS_SYNC_INTERVAL_MS=21600000` (6h) on the container, so it only scrapes the top 5 WP pages every 6h for new posts (enriching + torrent-backfilling them via the newest-first sweep).
