package models

// EnrichedScene is one TPDB or StashDB scene durably stored in the
// enriched_scenes Mongo collection, pre-matched to a resolvable torrent from
// one or more configured sources. The tpdb_new / tpdb_cat / stashdb_cat
// catalogs, porndb: meta/stream, and the category catalogs read from this store
// instead of hitting the live TPDB/StashDB APIs.
//
// ID is the scene's Stremio metaID (the same string the catalog emits and the
// meta/stream handler receives): "porndb:<num>" for TPDB scenes, a stash-scene
// id for StashDB scenes (the prefix is fixed when the job wires StashDB scene
// IDs into the manifest). Source is "tpdb" or "stashdb" - the catalog
// source-filter key, stored explicitly so catalogs query by source without
// depending on the _id prefix.
//
// MatchedSources holds the torrent sources that resolved a torrent for this
// scene (piratebay/knaben_adult/bitsearch/xxxclub/1337x/sukebei/pornrips); it
// is the indexable gate - a catalog filtered by the user's configured sources
// is a {matched_sources: {$in: configured}} query. AttemptedSources holds the
// sources the sweep has tried (hit or miss) so a clean miss is not re-scraped
// every tick; a transient scrape failure MUST stay out of both so it retries.
// Torrents holds the per-source best torrent so the stream path emits without a
// second query. A scene matched on multiple sources is stored once with all
// sources unioned - the scene metadata is shared across sources.
//
// Scene metadata (title/poster/cast/tags/date) is filled at discovery time
// (TPDB BrowseScenes / Stash FetchScenes already return it), so there is no
// separate enrichment step and no enriched_* flags - the work post-discovery is
// torrent matching, tracked by AttemptedSources/MatchedSources.
type EnrichedScene struct {
	ID      string `json:"id"`
	Source  string `json:"source"` // "tpdb" or "stashdb"
	Title   string `json:"title"`
	Poster  string `json:"poster"`
	// Background defaults to Poster when the source has no separate background.
	Background  string `json:"background"`
	Description string `json:"description"`
	Cast        []string `json:"cast"`
	// Tags are the source's scene tags; they double as Stremio genres. TagsNorm
	// is the lowercase normalized form backing the category-catalog tag filter.
	Tags     []string `json:"tags"`
	TagsNorm []string `json:"tagsNorm"`
	Date     string `json:"date"`
	// Studio is the scene's producing studio (TPDB site.name / Stash studio),
	// populated at discovery. findMatchingTorrent's pattern-B recall queries
	// (category_warmer tpbQueries) and VerifyMatch's studio gate depend on it;
	// empty studio degrades match precision to performer+date+title only.
	Studio string `json:"studio"`
	// MatchedSources: sources with a resolved torrent (the catalog gate).
	// AttemptedSources: sources the sweep has tried, hit or miss (skip on retry).
	MatchedSources   []string `json:"matchedSources"`
	AttemptedSources []string `json:"attemptedSources"`
	// Torrents is the per-source best torrent; keyed by source name.
	Torrents  map[string]TorrentRef `json:"torrents"`
	UpdatedAt int64                 `json:"updatedAt"`
}

// TorrentRef is the best matched torrent for one source on an EnrichedScene.
// The stream path emits one stream per matched source from InfoHash; Title is
// the stream display name. TorrentURL backs direct-backend P2P resolution.
type TorrentRef struct {
	InfoHash   string `json:"infoHash"`
	TorrentURL string `json:"torrentUrl"`
	Title      string `json:"title"`
	Seeders    int    `json:"seeders"`
	Quality    string `json:"quality"`
}