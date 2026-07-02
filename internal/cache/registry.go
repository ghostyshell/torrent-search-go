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
	PrefixPornripsCatalog   = "cat:pr:v6:"
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
