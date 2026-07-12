# Skill: Changelog (torrent-search-go)

> Global workflow: `~/.claude/skills/changelog/SKILL.md`

## Repo paths

| Item | Path |
|------|------|
| Changelog | `CHANGELOG.md` (repo root) |
| Version source | `internal/stremio/manifest.go` `addonVersion` (align with `tpb-stremio-addon/package.json`) |
| Pre-commit | `.githooks/pre-commit` runs `scripts/changelog-check.sh` |

## Before every commit

1. Update `CHANGELOG.md` → `[Unreleased]` for staged `internal/`, `cmd/`, `main.go`, scraper, or Stremio handler changes.
2. Stage `CHANGELOG.md` with the code change.
3. When addon version bumps in the Node repo, bump `addonVersion` here in the same release.

Pre-commit **fails** if product code is staged without a staged `CHANGELOG.md` diff.

## Release

When `addonVersion` bumps:

1. Move `[Unreleased]` into `## [x.y.z] - YYYY-MM-DD`.
2. Reset `[Unreleased]`.
3. Keep bullets in sync with the matching `tpb-stremio-addon` release where both repos ship together.

## Shim sync

After editing this file:

```bash
sh scripts/sync-agent-skills.sh
```
