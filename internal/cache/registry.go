// Package cache holds the single source of truth for Redis key prefixes and
// the registry of cache groups exposed to the admin dashboard for inspection
// and on-demand busting.
//
// Prefix values must match tpb-stremio-addon src/utils/cache.js (the Node edge
// reads/writes the same keys). Keep them here only; reference cache.PrefixX
// from every writer/reader instead of re-declaring local string constants.
package cache

// Redis key prefixes. Versioned (name:vN:) so payload-shape changes can bump
// the version without collisions.
const (
	PrefixTorrentStore      = "torrent:v1:"
	PrefixTPDBShared        = "tpdb-shared:v1:"
	PrefixStashDBShared     = "stashdb-shared:v1:"
	PrefixTPDBSharedMiss    = "tpdb-miss:v1:"
	PrefixStashDBSharedMiss = "stashdb-miss:v1:"
	PrefixCategoryCache     = "catcat:v1:"
	PrefixCatalogList       = "cat:v1:"
	PrefixPornripsCatalog   = "cat:pr:v7:"
	PrefixTPDBCatalog       = "cat:tpdb:v4:"
	PrefixHentaiCatalog     = "cat:hs:v2:"
	PrefixSukebeiCatalog    = "cat:sb:v1:"
	PrefixStripchatCatalog  = "cat:sc:v4:"
	PrefixHentaiMeta        = "meta:hs:v2:"
	PrefixHentaiStream      = "hstream:v1:"
	PrefixStashTag          = "stashtag:v1:"
	PrefixSearchQuery       = "sqc:v1:"
	PrefixAtishmkvDirect    = "atishmkv:direct:"
	// New caches added with the admin cache viewer/buster (2026-06).
	PrefixPornripsMeta     = "prmeta:v1:"
	PrefixPornStreams      = "pstreams:v2:"
	PrefixPerformerTorrent = "pperf:v1:"
	// Tube sources (perverzija / freepornvideos) added 2026-07. Backend-internal
	// only (the Node edge proxies these catalog/meta/stream paths); distinct
	// prefixes so they do not collide with the hentai/pornrips ones.
	PrefixPerverzijaCatalog     = "cat:pvz:v1:"
	PrefixPerverzijaMeta        = "meta:pvz:v1:"
	PrefixPerverzijaStream      = "stream:pvz:v1:"
	PrefixFreepornvideosCatalog = "cat:fpv:v1:"
	PrefixFreepornvideosMeta    = "meta:fpv:v1:"
	PrefixFreepornvideosStream  = "stream:fpv:v1:"
	PrefixYespornCatalog        = "cat:ypv:v1:"
	PrefixYespornMeta           = "meta:ypv:v1:"
	PrefixYespornStream         = "stream:ypv:v1:"
	PrefixWatchpornCatalog      = "cat:wpt:v1:"
	PrefixWatchpornMeta         = "meta:wpt:v1:"
	PrefixWatchpornStream       = "stream:wpt:v1:"
	PrefixPorneecCatalog        = "cat:pec:v1:"
	PrefixPorneecMeta           = "meta:pec:v1:"
	PrefixPorneecStream         = "stream:pec:v1:"
	// Genre-option blobs for the tube discover catalogs. The sync job precomputes
	// top-N studios/tags/performers (store distinct-aggregations) and writes them
	// here so the manifest path reads a cheap KV blob instead of running an
	// un-indexable $unwind/$group on every manifest fetch (which drove Mongo CPU
	// to 100% under manifest re-fetch traffic - see manifest.go pr_tag note).
	PrefixPerverzijaGenres     = "genres:pvz:v1:"
	PrefixFreepornvideosGenres = "genres:fpv:v1:"
	PrefixYespornGenres        = "genres:ypv:v1:"
	PrefixWatchpornGenres      = "genres:wpt:v1:"
	PrefixPorneecGenres        = "genres:pec:v1:"
)

// Group describes one Redis cache group for the admin dashboard: a label, a
// short description, and the nominal TTL used for display. The real TTL is
// applied at each write site; this TTLSeconds is informational only.
type Group struct {
	Prefix      string `json:"prefix"`
	Label       string `json:"label"`
	Description string `json:"description"`
	TTLSeconds  int64  `json:"ttlSeconds"`
}

// Groups is the complete list of Redis cache groups the dashboard can inspect
// and bust. Every prefix written anywhere in the backend should appear here.
var Groups = []Group{
	{
		Prefix:      PrefixTorrentStore,
		Label:       "Torrent records",
		Description: "Decoded jstrm: item records keyed by id; used by ServeMeta to enrich title/cover/website.",
		TTLSeconds:  6 * 60 * 60,
	},
	{
		Prefix:      PrefixTPDBShared,
		Label:       "TPDB shared meta",
		Description: "Enriched PornRips/TPDB shared metadata (title, poster, background) keyed by metaID.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixStashDBShared,
		Label:       "StashDB shared meta",
		Description: "Enriched Sukebei/StashDB shared metadata keyed by metaID.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixTPDBSharedMiss,
		Label:       "TPDB negative cache",
		Description: "Confirmed no-match markers so the on-demand MetaEnricher does not re-probe.",
		TTLSeconds:  60 * 60,
	},
	{
		Prefix:      PrefixStashDBSharedMiss,
		Label:       "StashDB negative cache",
		Description: "Confirmed no-match markers so the on-demand MetaEnricher does not re-probe.",
		TTLSeconds:  60 * 60,
	},
	{
		Prefix:      PrefixCategoryCache,
		Label:       "Category meta previews",
		Description: "Per-source/slug category meta previews warmed by the CategoryWarmer job.",
		TTLSeconds:  6 * 60 * 60,
	},
	{
		Prefix:      PrefixCatalogList,
		Label:       "Catalog lists (HiddenBay/search)",
		Description: "Catalog page results for xxx_* and search-result catalogs, keyed by backend|catalog|type|query|genre|skip|fanout.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixPornripsCatalog,
		Label:       "PornRips catalog lists",
		Description: "PornRips catalog page results (recent/studio/tag) keyed by catalog|genre|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixTPDBCatalog,
		Label:       "TPDB proxied catalog",
		Description: "Proxied TPDB performer/browse catalog pages.",
		TTLSeconds:  60 * 60,
	},
	{
		Prefix:      PrefixHentaiCatalog,
		Label:       "Hentai catalog lists",
		Description: "Hentai catalog page results (new/top/all/studios/years/search) keyed by catalog|genre|query|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixSukebeiCatalog,
		Label:       "Sukebei catalog lists",
		Description: "Sukebei catalog page results keyed by catalog|genre|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixStripchatCatalog,
		Label:       "Stripchat live catalog",
		Description: "Live cam listing; very short TTL because the list churns constantly.",
		TTLSeconds:  30,
	},
	{
		Prefix:      PrefixHentaiMeta,
		Label:       "Hentai meta",
		Description: "Full hentai ServeMeta responses keyed by hmm- id.",
		TTLSeconds:  7 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixHentaiStream,
		Label:       "Hentai streams",
		Description: "Resolved direct mp4 streams for hmm- ids.",
		TTLSeconds:  5 * 60,
	},
	{
		Prefix:      PrefixStashTag,
		Label:       "Stash tag cache",
		Description: "Stash tag lookups used during shared-meta enrichment.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixSearchQuery,
		Label:       "Search query cache",
		Description: "Per website/category/query scrape results warmed by the SearchQueryCache job.",
		TTLSeconds:  60 * 60,
	},
	{
		Prefix:      PrefixAtishmkvDirect,
		Label:       "AtishMKV direct links",
		Description: "Resolved AtishMKV direct download URLs keyed by slug:quality.",
		TTLSeconds:  5 * 60 * 60,
	},
	{
		Prefix:      PrefixPornripsMeta,
		Label:       "PornRips meta (slug)",
		Description: "PornRips entry lookup by slug used as the ServeMeta poster fallback.",
		TTLSeconds:  30 * 60,
	},
	{
		Prefix:      PrefixPornStreams,
		Label:       "porndb: streams",
		Description: "Resolved infoHash stream lists for porndb:<scene> ids (TPDB scene + PornRips performer match).",
		TTLSeconds:  30 * 60,
	},
	{
		Prefix:      PrefixPerformerTorrent,
		Label:       "Performer torrent membership",
		Description: "Per-performer 'has a resolved pornrips_entries torrent' flag backing the tpdb_new/tpdb_search catalog filter. Hit 60min (sticky-correct), miss 30min.",
		TTLSeconds:  60 * 60,
	},
	{
		Prefix:      PrefixPerverzijaCatalog,
		Label:       "Perverzija catalog lists",
		Description: "Perverzija catalog page results (recent/studio/tag/performer/search) keyed by catalog|genre|query|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixPerverzijaMeta,
		Label:       "Perverzija meta",
		Description: "Full Perverzija ServeMeta responses keyed by pvz:{slug}.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixPerverzijaStream,
		Label:       "Perverzija streams",
		Description: "Resolved HLS variant streams for pvz:{slug} ids (one per quality).",
		TTLSeconds:  5 * 60,
	},
	{
		Prefix:      PrefixFreepornvideosCatalog,
		Label:       "FreePornVideos catalog lists",
		Description: "FreePornVideos catalog page results (recent/studio/tag/performer/search) keyed by catalog|genre|query|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixFreepornvideosMeta,
		Label:       "FreePornVideos meta",
		Description: "Full FreePornVideos ServeMeta responses keyed by fpv:{id}.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixFreepornvideosStream,
		Label:       "FreePornVideos streams",
		Description: "Resolved mp4 streams for fpv:{id} ids (one per quality).",
		TTLSeconds:  5 * 60,
	},
	{
		Prefix:      PrefixYespornCatalog,
		Label:       "YesPorn catalog lists",
		Description: "YesPorn catalog page results (recent/studio/tag/performer/search) keyed by catalog|genre|query|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixYespornMeta,
		Label:       "YesPorn meta",
		Description: "Full YesPorn ServeMeta responses keyed by ypv:{id}.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixYespornStream,
		Label:       "YesPorn streams",
		Description: "Resolved mp4 streams for ypv:{id} ids (one per quality).",
		TTLSeconds:  5 * 60,
	},
	{
		Prefix:      PrefixWatchpornCatalog,
		Label:       "WatchPorn catalog lists",
		Description: "WatchPorn catalog page results (recent/studio/tag/performer/search) keyed by catalog|genre|query|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixWatchpornMeta,
		Label:       "WatchPorn meta",
		Description: "Full WatchPorn ServeMeta responses keyed by wpt:{id}.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixWatchpornStream,
		Label:       "WatchPorn streams",
		Description: "Resolved mp4 streams for wpt:{id} ids (one per quality).",
		TTLSeconds:  5 * 60,
	},
	{
		Prefix:      PrefixPorneecCatalog,
		Label:       "Porneec catalog lists",
		Description: "Porneec catalog page results (recent/studio/tag/performer/search) keyed by catalog|genre|query|skip.",
		TTLSeconds:  15 * 60,
	},
	{
		Prefix:      PrefixPorneecMeta,
		Label:       "Porneec meta",
		Description: "Full Porneec ServeMeta responses keyed by pec:{slug}.",
		TTLSeconds:  30 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixPorneecStream,
		Label:       "Porneec streams",
		Description: "Resolved tokenless Bunny CDN mp4 streams for pec:{slug} ids (single quality, stored at enrich).",
		TTLSeconds:  5 * 60,
	},
	{
		Prefix:      PrefixPerverzijaGenres,
		Label:       "Perverzija genre options",
		Description: "Precomputed top-N studio/tag/performer option lists for the Perverzija discover catalogs; written by the sync job, read at manifest time.",
		TTLSeconds:  7 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixFreepornvideosGenres,
		Label:       "FreePornVideos genre options",
		Description: "Precomputed top-N studio/tag/performer option lists for the FreePornVideos discover catalogs; written by the sync job, read at manifest time.",
		TTLSeconds:  7 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixYespornGenres,
		Label:       "YesPorn genre options",
		Description: "Precomputed top-N studio/tag/performer option lists for the YesPorn discover catalogs; written by the sync job, read at manifest time.",
		TTLSeconds:  7 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixWatchpornGenres,
		Label:       "WatchPorn genre options",
		Description: "Precomputed top-N studio/tag/performer option lists for the WatchPorn discover catalogs; written by the Mac-cron ingest tool, read at manifest time.",
		TTLSeconds:  7 * 24 * 60 * 60,
	},
	{
		Prefix:      PrefixPorneecGenres,
		Label:       "Porneec genre options",
		Description: "Precomputed top-N studio/performer option lists for the Porneec discover catalogs; written by the sync job, read at manifest time. Tags are empty for porneec (obfuscated slugs).",
		TTLSeconds:  7 * 24 * 60 * 60,
	},
}

// Lookup returns the Group for an exact prefix match, or ok=false. Used as the
// bust whitelist so callers cannot delete arbitrary keys outside the registry.
func Lookup(prefix string) (Group, bool) {
	for _, g := range Groups {
		if g.Prefix == prefix {
			return g, true
		}
	}
	return Group{}, false
}
