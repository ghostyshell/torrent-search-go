# Changelog

All notable changes to **torrent-search-go** (Stremio protocol API, scrapers, jobs) are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Stremio manifest versions track the addon release line (`internal/stremio/manifest.go` `addonVersion`, overridable by `ADDON_VERSION`). The source of truth for the shipped addon version is `tpb-stremio-addon/package.json`.

---

## [Unreleased]

## [1.9.21] - 2026-06-22

### Added
- **Stripchat source** - live cam catalogs (`sc_girls`, `sc_couples`, `sc_guys`, `sc_trans`) with username search and `sc:` meta ids; 30s Redis listing cache.

## [1.9.20] - 2026-06-22

### Fixed
- **OnlyFans metadata** - parse `OnlyFans - Performer - Title` torrent names, probe TPDB/StashDB with performer+co-star terms, verify against the original release, and reuse poster art from sibling rows on the same catalog page.

## [1.9.19] - 2026-06-22

### Fixed
- **Metadata negative cache** - scope TPDB/StashDB miss sentinels per API credential so per-install keys are not blocked by server-key misses.

## [1.9.18] - 2026-06-22

### Fixed
- **Search catalog** - catalog id `search`, display name `Search` under Porn (was `jav_search` / `Porn · Search`).

## [1.9.17] - 2026-06-22

### Fixed
- **JAV search metadata** - parse product codes from release suffixes (`ACHJ-030-C`, `ACHJ-030-FHD`); propagate StashDB poster/title to sibling torrents sharing the same code on one catalog page.

## [1.9.16] - 2026-06-22

### Fixed
- **TPDB/StashDB catalog posters** - live metadata lookup now runs for HiddenBay/PirateBay catalog items (was PornRips-only), capped at 24 probes per page.
- **Release matching** - studio+date torrent titles require a matching release date; added studio aliases and broader search probes for compact site names (EvilAngel, JOIBabes, ALSScan, etc.).

## [1.9.15] - 2026-06-22

### Fixed
- **Compact catalogs** - paginate across underlying quality/sort pages so Stremio infinite scroll can load more than the first merged page (~60 scene groups).

## [1.9.14] - 2026-06-22

### Added
- **Trans catalog relevance filter** - reject non-adult "trans" noise from Knaben/Bitsearch (translation, anti-trans hate, etc.); broaden fanout query to adult synonyms.

## [1.9.13] - 2026-06-22

### Fixed
- **Catalog cache** - Redis keys include `extraIndexers` fanout tag so disabled indexers do not leak from warmed cache.
- **1080p compact catalogs** - drop 720p torrents from fhd-scoped compact merge and re-apply quality filters on cache reads.

## [1.9.12] - 2026-06-22

### Changed
- **Compact studios** - Trans catalog uses the same grouped `jstrg:` merge path as studio catalogs.

## [1.9.10] - 2026-06-22

### Changed
- **Compact studios** - main XXX catalog included in compact merged serving.

## [1.9.8] - 2026-06-22

### Added
- **Compact studio catalogs** - group scenes into one `jstrg:` meta with per-quality streams.

## [1.9.5] - 2026-06-22

### Changed
- **Copy** - replaced em/en dashes with ASCII hyphens in public-facing text.

## [1.9.4] - 2026-06-22

### Changed
- **Manifest version** - `addonVersion` env-overridable; documented version flow with Node edge.

## [1.9.0] - 2026-06-21

### Added
- **Extra indexers** - Knaben (adult), Bitsearch, XxxClub with opt-in flags, seeder filter, TPB-first ordering, quality scoping.
- **1337x** - FlareSolverr integration, cache-aside queue, serialized solver usage.
- **Meta cache** - no-match sentinels to avoid repeated TPDB/StashDB misses.

### Changed
- **Stremio offload** - catalog, meta, stream, and background jobs served from Go when configured.

## [1.5.3] - 2026-06-21

### Added
- **TPDB/StashDB catalogs** - `All` genre option; fixed indexer count in manifest.

### Fixed
- **PornRips search** - browse-and-filter when Cloudflare blocks search queries.

## [1.5.2] - 2026-06-20

### Fixed
- **PornRips metadata** - richer catalog and detail-page enrichment.

## [1.5.1] - 2026-06-20

### Fixed
- **PornRips search** - fallback path and live TPDB/StashDB enrichment.

## [1.5.0] - 2026-06-20

### Added
- **Sukebei** - Nyaa Sukebei scraper with StashDB-assisted top/recent catalogs.

### Fixed
- **PornRips catalogs** - list posts without serial detail fetches at scrape time (fixes empty catalogs).

### Changed
- **JAV search** - renamed to Porn; XXX catalogs browse-only.

## [1.2.1] - 2026-06-20

### Added
- **Sukebei magnets** - included in stream cache background prewarm.
- **Performer-aware search** - search by actress for JAV and scenes.
- **AtishMKV** - core HTTP stream provider with on-demand direct-link refresh.

### Fixed
- **JAV metadata** - TPDB/StashDB resolution for censored/uncensored codes (Heyzo, StashDB full-title).
- **Sukebei catalogs** - empty results when warmer cache cold; StashDB detail covers persisted.
- **Browse covers** - landscape posters for TPB and PornRips; TPDB/StashDB poster preference.

## [1.2.0] - 2026-06-17

### Changed
- **Manifest** - advertise TPDB/theStashDB metadata in Stremio description.
