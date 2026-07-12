#!/bin/sh
# Mac-side source sync for the tube sources that are unreachable from prod egress.
# watchporn.to is TLS-blocked from Sliplane's Hetzner egress (Cloudflare Under-Attack
# JA3), so prod can't scrape it; the Mac can. This wrapper sources prod Mongo + Redis
# creds from .env and runs cmd/<source>ingest against prod, wrapped in `caffeinate -i`
# so a laptop doesn't idle-sleep mid-run, and `flock -n` so a still-running tick blocks
# the next one instead of double-running. Fired by the launchd plists installed by
# install-mac-sync.sh.
#
# Default mode is a bounded keep-fresh tick (one recent-pages ingest + one enrich +
# genre precompute); pass -bulk for the one-time full-archive fill run by hand.
#
# Usage: mac-sync.sh <source> [-bulk]   (source in {watchporn})
set -e
cd /Users/akshat/Code/torrent-search-go
set -a
. ./.env
set +a
# launchd runs with a minimal PATH; ensure go is findable.
export PATH=/usr/local/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH

src="$1"
shift || true
case "$src" in
  watchporn) ;;
  *) echo "mac-sync: unknown source '$src' (want watchporn)" >&2; exit 2 ;;
esac

# Keep-fresh: walk the newest WATCHPORN_INGEST_PAGES_PER_TICK pages each fire so new
# posts are picked up within one cron interval (not the full-archive cursor walk).
# -bulk overrides this to a high page count for the one-time full fill.
: "${WATCHPORN_INGEST_PAGES_PER_TICK:=20}"

echo "[$(date -u +%FT%TZ)] mac-sync $src start (pages=$WATCHPORN_INGEST_PAGES_PER_TICK)"
# flock -n: exit 0 (skip) if another tick is still running; caffeinate -i: keep awake.
exec flock -n /tmp/tube-sync-${src}.lock caffeinate -i go run ./cmd/"${src}ingest" "$@"