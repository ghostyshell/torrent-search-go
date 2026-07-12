#!/bin/sh
# Installs launchd plists that run the prod-egress-blocked tube source syncs from the
# Mac. watchporn.to is TLS-blocked from Sliplane's Hetzner egress (Cloudflare
# Under-Attack JA3), so it is Mac-cron-populated; the deployed backend reads Mongo +
# the Redis genre blob only. Reachable tube sources (perverzija, freepornvideos,
# yesporn, porneec) use the deployed prod sync job, NOT this cron.
#
# watchporn runs every 4h (a bounded keep-fresh tick: newest ~20 pages + enrich 50 +
# genre precompute, ~30s when caught up). caffeinate -i (in mac-sync.sh) keeps the
# Mac awake for each run; flock -n blocks an overlap. StartCalendarInterval runs a
# missed job on wake, so a sleeping laptop catches up rather than skipping. Hours
# are staggered off the :00/:30 fleet marks. Re-runnable; unloads + reloads.
#
# Usage: sh scripts/install-mac-sync.sh
set -e
REPO=/Users/akshat/Code/torrent-search-go
LAUNCH=~/Library/LaunchAgents
LOGS="$REPO/logs"
mkdir -p "$LAUNCH" "$LOGS"

# source : hours : minute
# watchporn: 6x/day every 4h, staggered off :00/:30. Add a Tier-2 blocked source here
# in Phase 3 (pornhd3x/porn4days/hqporner) with its own staggered slot.
set -- \
  "watchporn:1,5,9,13,17,21:23"

for spec in "$@"; do
  src=${spec%%:*}
  rest=${spec#*:}
  hours=${rest%:*}
  minute=${rest#*:}
  label="com.akshat.tubesearch.${src}-sync"
  plist="$LAUNCH/${label}.plist"

  # Build the StartCalendarInterval array from the comma-separated hours.
  cal=""
  IFS=','
  for h in $hours; do
    cal="$cal
    <dict><key>Hour</key><integer>${h}</integer><key>Minute</key><integer>${minute}</integer></dict>"
  done
  unset IFS

  cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${label}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${REPO}/scripts/mac-sync.sh</string>
    <string>${src}</string>
  </array>
  <key>StartCalendarInterval</key>
  <array>${cal}
  </array>
  <key>StandardOutPath</key>
  <string>${LOGS}/mac-sync-${src}.log</string>
  <key>StandardErrorPath</key>
  <string>${LOGS}/mac-sync-${src}.log</string>
  <key>RunAtLoad</key>
  <false/>
</dict>
</plist>
EOF

  chmod +x "$REPO/scripts/mac-sync.sh"
  launchctl unload "$plist" 2>/dev/null || true
  launchctl load -w "$plist" 2>/dev/null || launchctl bootstrap "gui/$(id -u)" "$plist" 2>/dev/null || true
  echo "installed ${label} -> ${plist} (hours ${hours} :${minute})"
done

echo
echo "Loaded. Logs: ${LOGS}/mac-sync-<source>.log"
echo "List:    launchctl list | grep tubesearch"
echo "Run now: ${REPO}/scripts/mac-sync.sh watchporn"
echo "First fill (one-time, by hand): ${REPO}/scripts/mac-sync.sh watchporn -bulk"
echo "  (sets WATCHPORN_INGEST_PAGES_PER_TICK=20 by default; override in .env for the bulk walk)"