// Package models contains storage-agnostic types used by the persistence layer.
// These types were previously tied to the Turso client but are now neutral so
// the MongoDB-backed storage implementation can be used without a Turso dependency.
package models

import (
	"regexp"
	"strings"
	"time"
)

// BlockedIP represents an IP address that has been blocked from accessing the API.
type BlockedIP struct {
	IP           string `bson:"_id" json:"ip"`
	Reason       string `bson:"reason" json:"reason"` // "manual" | "auto"
	Notes        string `bson:"notes" json:"notes"`
	RequestCount int64  `bson:"request_count" json:"requestCount"`
	BlockedAt    int64  `bson:"blocked_at" json:"blockedAt"`
	IsActive     bool   `bson:"is_active" json:"isActive"`
	UpdatedAt    int64  `bson:"updated_at" json:"updatedAt"`
}

// IPTrafficStat is a per-IP request count snapshot used by the monitoring dashboard.
type IPTrafficStat struct {
	IP           string `json:"ip"`
	RequestCount int    `json:"requestCount"` // 1m window (backward compatible)
	Count1h      int    `json:"count1h"`
	Count6h      int    `json:"count6h"`
	Count1d      int    `json:"count1d"`
	Count1w      int    `json:"count1w"`
	Count1mo     int    `json:"count1mo"`
	IsBlocked    bool   `json:"isBlocked"`
}

// Stats holds database statistics.
type Stats struct {
	IsConnected  bool      `json:"isConnected"`
	DatabaseType string    `json:"databaseType"`
	Environment  string    `json:"environment"`
	LastCheck    time.Time `json:"lastCheck"`
}

// HealthStatus represents the health check result.
type HealthStatus struct {
	Status       string `json:"status"`
	Type         string `json:"type"`
	ResponseTime int64  `json:"responseTime"`
	Timestamp    string `json:"timestamp"`
}

// UserRow matches columns from the users table/collection.
type UserRow struct {
	ID               string  `json:"id"`
	Email            string  `json:"email"`
	Name             string  `json:"name"`
	Picture          *string `json:"picture"`
	GoogleID         *string `json:"googleId"`
	RealDebridAPIKey *string `json:"-"`
	CreatedAt        int64   `json:"createdAt"`
	UpdatedAt        int64   `json:"updatedAt"`
	LastLoginAt      *int64  `json:"lastLoginAt"`
	IsActive         int     `json:"isActive"`
}

// UserRealDebridKey pairs a user ID with an encrypted Real-Debrid API key.
type UserRealDebridKey struct {
	UserID       string `json:"userId"`
	EncryptedKey string `json:"encryptedKey"`
}

// SessionRow matches columns from user_sessions + joined user data.
type SessionRow struct {
	ID             string  `json:"id"`
	UserID         string  `json:"userId"`
	SessionToken   string  `json:"-"`
	ExpiresAt      int64   `json:"expiresAt"`
	UserAgent      *string `json:"userAgent"`
	IPAddress      *string `json:"ipAddress"`
	LastAccessedAt *int64  `json:"lastAccessedAt"`
	CreatedAt      int64   `json:"createdAt"`
	// Joined from users
	Email            string  `json:"email"`
	Name             string  `json:"name"`
	Picture          *string `json:"picture"`
	RealDebridAPIKey *string `json:"-"`
}

// FavoriteRow matches columns from the favorites table/collection.
type FavoriteRow struct {
	ID            string `json:"id"`
	UserID        string `json:"userId"`
	TorrentKey    string `json:"torrentKey"`
	TorrentName   string `json:"torrentName"`
	Website       string `json:"website"`
	TorrentData   string `json:"-"`
	MagnetLink    string `json:"magnetLink,omitempty"`
	CoverImageURL string `json:"coverImageUrl,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// FavoriteEntryRow matches columns from favorite_entries.
type FavoriteEntryRow struct {
	ID            string  `json:"id"`
	TorrentKey    string  `json:"torrentKey"`
	MagnetLink    *string `json:"magnetLink"`
	TorrentName   string  `json:"torrentName"`
	TorrentData   *string `json:"torrentData"`
	CoverImageURL *string `json:"coverImageUrl"`
	CreatedAt     int64   `json:"createdAt"`
	UpdatedAt     int64   `json:"updatedAt"`
}

// FavoriteRefreshItem is a single favorite to refresh.
type FavoriteRefreshItem struct {
	ID          string `json:"id"`
	MagnetLink  string `json:"magnetLink"`
	TorrentName string `json:"torrentName"`
}

// UserFavoritesRefresh groups favorites by user for stream URL refresh jobs.
type UserFavoritesRefresh struct {
	UserID    string                `json:"userId"`
	Favorites []FavoriteRefreshItem `json:"favorites"`
}

// TorrentDetailRow matches columns from torrent_details.
type TorrentDetailRow struct {
	ID            string  `json:"id"`
	FavoriteID    string  `json:"favoriteId"`
	Source        string  `json:"source"`
	DetailsData   string  `json:"detailsData"`
	CoverImageURL *string `json:"coverImageUrl"`
	CreatedAt     int64   `json:"createdAt"`
	UpdatedAt     int64   `json:"updatedAt"`
}

// ImageRow matches columns from the images collection.
type ImageRow struct {
	ID           string  `json:"id"`
	TorrentKey   string  `json:"torrentKey"`
	ImageType    string  `json:"imageType"`
	PixhostURL   string  `json:"pixhostUrl"`
	OriginalURL  *string `json:"originalUrl"`
	TorrentName  *string `json:"torrentName"`
	FallbackURLs *string `json:"fallbackUrls"`
	StorageKey   *string `json:"storageKey"`
	// Description, CoverSource and MetaID carry the TPDB/StashDB enrichment
	// persisted alongside a cover so torrent-search-ui can render an upgraded
	// cover + description. CoverSource is one of "tpdb"/"stashdb"/"nfo"/
	// "description"/"manual"; MetaID links the row to the shared_meta store.
	Description *string `json:"description,omitempty"`
	CoverSource *string `json:"coverSource,omitempty"`
	MetaID      *string `json:"metaId,omitempty"`
	CreatedAt   int64   `json:"createdAt"`
	// Separate cover slots so multiple sources coexist without overwriting each
	// other. TpdbURL holds the TPDB/StashDB cover; DetailsURL holds the cover
	// scraped from the torrent detail page (description or NFO).
	TpdbURL           *string `json:"tpdbUrl,omitempty"`
	TpdbStorageKey    *string `json:"tpdbStorageKey,omitempty"`
	DetailsURL        *string `json:"detailsUrl,omitempty"`
	DetailsStorageKey *string `json:"detailsStorageKey,omitempty"`
}

// SharedMetaPayload is the durable per-source (TPDB or StashDB) metadata record
// stored in the Mongo shared_meta collection. It mirrors the jobs.SharedMeta
// wire shape (which lives in internal/ and so cannot be referenced here) plus an
// UpdatedAt timestamp. Converters live in the jobs layer.
type SharedMetaPayload struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Poster      string   `json:"poster,omitempty"`
	Background  string   `json:"background,omitempty"`
	Year        string   `json:"year,omitempty"`
	Cast        []string `json:"cast,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Source      string   `json:"source,omitempty"`
	UpdatedAt   int64    `json:"updatedAt"`
}

// ObjectStorageCover identifies a cover row backed by object storage.
type ObjectStorageCover struct {
	TorrentKey string `json:"torrentKey"`
	StorageKey string `json:"storageKey"`
}

// PornripsEntry is one PornRips WordPress post durably stored in the
// pornrips_entries Mongo collection. Listing fields (slug/title/date/excerpt/
// wp_poster) are filled by the PornripsIngest job; enrichment fields
// (studio/tags/genres/performers/poster) are filled by the PornripsEnrich sweep
// from TPDB/StashDB scene data so the Studio/Tag catalogs can query Mongo-side.
// StudioNorm/TagsNorm are lowercase non-alphanumeric-stripped tokens used as
// indexed query keys (curated option names are normalized the same way before
// querying, so "Adult Time" matches a TPDB studio stored as "AdultTime").
type PornripsEntry struct {
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	DetailURL  string `json:"detailUrl"`
	Date       string `json:"date"`
	Excerpt    string `json:"excerpt"`
	WpPoster   string `json:"wpPoster"`
	Poster     string `json:"poster"`
	Studio     string `json:"studio"`
	StudioNorm string `json:"studioNorm"`
	// SceneGroup is the normalized-title group key shared by every resolution
	// variant of one scene (quality/codec/PRT tokens stripped, lowercased,
	// non-alphanumerics collapsed to spaces), so the catalog can group 720p/
	// 1080p/4K rips of the same scene into one jstrg: entry with one stream per
	// variant. Computed at ingest from Title; empty title falls back to
	// "pr:"+Slug (unique per doc, never groups). Indexed for the catalog
	// aggregation. See PornripsSceneGroup.
	SceneGroup string   `json:"sceneGroup"`
	Tags       []string `json:"tags"`
	TagsNorm   []string `json:"tagsNorm"`
	Genres     []string `json:"genres"`
	Performers []string `json:"performers"`
	// ResolvedTitle is the TPDB/Stash-resolved scene title (TPDB first, Stash
	// fallback - mirrors MergeShared used for catalog display) denormalized onto
	// the entry so SearchPornrips matches it alongside the original WP title and
	// performers in one Mongo query. Filled for new entries by the enrich sweep
	// and for existing entries by cmd/backfill-pr-resolved-title.
	ResolvedTitle string `json:"resolvedTitle"`
	MetaID        string `json:"metaId"`
	EnrichedTpdb  bool   `json:"enrichedTpdb"`
	EnrichedStash bool   `json:"enrichedStash"`
	EnrichedAt    int64  `json:"enrichedAt"`
	UpdatedAt     int64  `json:"updatedAt"`
	// InfoHash/TorrentURL are the resolved .torrent info-hash and the .torrent URL
	// the backfill sweep / lazy write-back fill so stream opens skip the live
	// Cloudflare-blocked detail-page fetch. Empty until resolved.
	InfoHash   string `json:"infoHash"`
	TorrentURL string `json:"torrentUrl"`
}

// PornripsGroup is one scene group for the PornRips catalog: the representative
// (highest-resolution member) plus every resolution variant of that scene. Backs
// the Mongo aggregation grouping (findPornripsGroups) so the catalog emits one
// jstrg: entry per scene with one stream per variant. Members are sorted by
// PornripsQualityRank desc; Members[0] == Representative.
type PornripsGroup struct {
	Representative PornripsEntry
	Members        []PornripsEntry
}

// HentaiEntry is one hentai series from HentaiMama (hmm), durably stored in
// the hentai_entries Mongo collection. All fields are filled by HentaiIngest
// from the source series page; the source rating (hentaimama, 0-10) is shown
// directly as the Stremio ImdbRating, so there is no external MAL/Jikan
// enrichment step. One doc per source id (hmm-…). (HentaiTV htv- was removed;
// any leftover htv docs are hidden by filtering reads to prefix "hmm".)
type HentaiEntry struct {
	ID          string          `json:"id"`     // "{prefix}-{slug}", e.g. "hmm-toshi-ie"
	Prefix      string          `json:"prefix"` // "hmm"
	Slug        string          `json:"slug"`   // source-local series slug
	Source      string          `json:"source"` // "hentaimama"
	Title       string          `json:"title"`
	Poster      string          `json:"poster"`
	Background  string          `json:"background"`
	Excerpt     string          `json:"excerpt"`
	ReleaseYear string          `json:"releaseYear"`
	Studio      string          `json:"studio"`
	StudioNorm  string          `json:"studioNorm"`
	Genres      []string        `json:"genres"`
	GenresNorm  []string        `json:"genresNorm"`
	Rating      float64         `json:"rating"`    // source rating 0-10
	RatingSrc   string          `json:"ratingSrc"` // which source supplied Rating
	DetailURL   string          `json:"detailUrl"`
	Episodes    []HentaiEpisode `json:"episodes"`
	UpdatedAt   int64           `json:"updatedAt"`
}

// HentaiEpisode is one episode of a hentai series. Slug/SourceURL are the
// source-local episode identifiers the live stream handler resolves to a
// direct mp4 (HentaiMama DooPlay AJAX/iframe flow).
type HentaiEpisode struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Slug      string `json:"slug"`      // source episode slug for live stream resolution
	SourceURL string `json:"sourceUrl"` // full episode page URL
	Thumbnail string `json:"thumbnail"`
	Released  string `json:"released"`
}

// PerverzijaEntry is one scene from tube.perverzija.com (a WordPress site),
// durably stored in the perverzija_entries Mongo collection. Listing fields
// (slug/title/date/excerpt/wp_poster/studios/tags) are filled by the ingest sweep
// from the WP REST API (/wp-json/wp/v2/posts?_embed); the enrich sweep scrapes
// the detail HTML for performers (Stars: block), description, full poster, and
// the xtremestream stream hash. No TPDB/StashDB - all metadata is scraped from
// the source site. One doc per scene (no quality grouping; stream qualities are
// resolved live from the xtremestream master m3u8).
//
// Studios holds every WP category display name for the post (e.g.
// ["AGirlKnows","LetsDoeIt"] - the subsite and its network). The WP embed omits
// category parent/count, so network vs subsite cannot be split at ingest time;
// instead both are indexed as a multikey studios_norm so the studio discover
// surfaces every level and the entry appears under each. TagsNorm/
// PerformersNorm/StudiosNorm are NormToken keys backing the tag/performer/
// studio catalogs.
type PerverzijaEntry struct {
	Slug           string   `json:"slug"` // WP post slug; doc id = "pvz:"+slug
	Title          string   `json:"title"`
	DetailURL      string   `json:"detailUrl"`
	Date           string   `json:"date"` // date_gmt from WP REST (sort key)
	Excerpt        string   `json:"excerpt"`
	WpPoster       string   `json:"wpPoster"`    // featured image full URL from REST
	Poster         string   `json:"poster"`      // full-size poster (strip -WxH suffix)
	Studios        []string `json:"studios"`     // all WP category display names
	StudiosNorm    []string `json:"studiosNorm"` // multikey; backs pvz_studio
	Tags           []string `json:"tags"`        // WP post_tag taxonomy
	TagsNorm       []string `json:"tagsNorm"`
	Performers     []string `json:"performers"` // from detail Stars: block
	PerformersNorm []string `json:"performersNorm"`
	Description    string   `json:"description"`
	Duration       string   `json:"duration"`      // best-effort (listing cards only)
	StreamHash     string   `json:"streamHash"`    // xtremestream data hash -> xs1.php?data=
	DetailScraped  bool     `json:"detailScraped"` // enrich gate
	UpdatedAt      int64    `json:"updatedAt"`
}

// FreepornvideosEntry is one scene from freepornvideos.xxx, durably stored in
// the freepornvideos_entries Mongo collection. Listing fields (id/slug/title/
// poster/duration/studio/performers/rating/views) are filled by the ingest sweep
// from /latest-updates/{N}/ card HTML; the enrich sweep fetches the detail page
// for the JSON-LD uploadDate (sort key) + duration + description + categories
// (a.btn_tag) + network. Stream mp4 URLs are NOT stored (rotating token); the
// stream resolver re-fetches the detail page on each open. No TPDB/StashDB -
// all metadata scraped from the source. One doc per scene.
type FreepornvideosEntry struct {
	VideoID        string   `json:"videoId"` // numeric id; doc id = "fpv:"+id
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	DetailURL      string   `json:"detailUrl"` // /videos/{id}/{slug}/
	Date           string   `json:"date"`      // JSON-LD uploadDate (sort key)
	Excerpt        string   `json:"excerpt"`
	Poster         string   `json:"poster"` // img.freepornvideos.xxx/.../1.jpg
	Studio         string   `json:"studio"` // a.btn_sponsor channel
	StudioNorm     string   `json:"studioNorm"`
	Network        string   `json:"network"`    // a.btn_sponsor_group
	Categories     []string `json:"categories"` // a.btn_tag (the "tags")
	CategoriesNorm []string `json:"categoriesNorm"`
	Performers     []string `json:"performers"` // a.btn_model
	PerformersNorm []string `json:"performersNorm"`
	Description    string   `json:"description"`
	Duration       string   `json:"duration"` // ISO8601 or "Full Video HH:MM:SS"
	Rating         string   `json:"rating"`   // % positive
	Views          string   `json:"views"`
	Has4K          bool     `json:"has4k"`
	DetailScraped  bool     `json:"detailScraped"` // enrich gate
	UpdatedAt      int64    `json:"updatedAt"`
}

// YespornEntry is one scene from yesporn.vip (a KernelTeam tube script site),
// durably stored in the yesporn_entries Mongo collection. Listing fields
// (id/slug/title/poster/duration) are filled by the ingest sweep from
// /latest-updates/{N}/ card HTML; the enrich sweep fetches the detail page for
// the og: release_date (sort key) + description + the player-config JS object
// (categories -> Tags, models -> Performers) + channel links (Studios, multi-key)
// + og:image (full poster). Stream mp4 URLs are NOT stored (rotating token in
// /get_file/{server}/{token}/...); the stream resolver re-fetches the detail page
// and parses the player-config video_url/video_alt_url[N] (strip the
// `function/0/` prefix) on each open. One doc per scene.
type YespornEntry struct {
	VideoID        string   `json:"videoId"` // numeric id; doc id = "ypv:"+id
	Slug           string   `json:"slug"`    // card slug incl. the -bjy9kc suffix
	Title          string   `json:"title"`
	DetailURL      string   `json:"detailUrl"` // /video/{id}/{slug}/ (trailing slash)
	Date           string   `json:"date"`      // video:release_date (sort key)
	Excerpt        string   `json:"excerpt"`
	Poster         string   `json:"poster"`  // yesnn.b-cdn.net (card data-original / og:image)
	Studios        []string `json:"studios"` // channels (multi-key)
	StudiosNorm    []string `json:"studiosNorm"`
	Tags           []string `json:"tags"` // categories (player-config video_categories)
	TagsNorm       []string `json:"tagsNorm"`
	Performers     []string `json:"performers"` // models (player-config video_models)
	PerformersNorm []string `json:"performersNorm"`
	Description    string   `json:"description"`   // og:description
	Duration       string   `json:"duration"`      // video:duration seconds -> HH:MM:SS
	Has4K          bool     `json:"has4k"`         // yesporn caps at 1080p; always false (kept for TubeEntry compat)
	DetailScraped  bool     `json:"detailScraped"` // enrich gate
	UpdatedAt      int64    `json:"updatedAt"`
}

// WatchpornEntry is one scene from watchporn.to (a KernelTeam tube script site),
// durably stored in the watchporn_entries Mongo collection. Listing fields
// (id/slug/title/poster/duration) are filled by the ingest sweep from
// /latest-updates/{N}/ card HTML; the enrich sweep fetches the detail page for
// the og: release_date (sort key) + description + og:image (full poster) and
// reads the single__info block's HTML links: /categories/ -> Studios (the
// site/network, multi-key), /models/ -> Performers, /tags/ -> Tags. Stream mp4
// URLs are NOT stored (a rotating v-acctoken query param in /get_file/...); the
// stream resolver re-fetches the detail page and parses the player-config
// video_url/video_alt_url[N] (no function/0/ prefix on this site) on each open.
// Populated by a Mac-side launchd cron (watchporn.to is TLS-blocked from prod).
type WatchpornEntry struct {
	VideoID        string   `json:"videoId"` // numeric id; doc id = "wpt:"+id
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	DetailURL      string   `json:"detailUrl"` // /video/{id}/{slug}/ (trailing slash)
	Date           string   `json:"date"`      // video:release_date (sort key)
	Excerpt        string   `json:"excerpt"`
	Poster         string   `json:"poster"`  // contents/videos_screenshots/.../preview.jpg
	Studios        []string `json:"studios"` // /categories/ links (site/network, multi-key)
	StudiosNorm    []string `json:"studiosNorm"`
	Tags           []string `json:"tags"` // /tags/ links
	TagsNorm       []string `json:"tagsNorm"`
	Performers     []string `json:"performers"` // /models/ links
	PerformersNorm []string `json:"performersNorm"`
	Description    string   `json:"description"`   // og:description
	Duration       string   `json:"duration"`      // video:duration seconds -> HH:MM:SS
	Has4K          bool     `json:"has4k"`         // watchporn caps at 1080p; always false (kept for TubeEntry compat)
	DetailScraped  bool     `json:"detailScraped"` // enrich gate
	UpdatedAt      int64    `json:"updatedAt"`
}

// PorneecEntry is one scene from porneec.com (a WordPress tube site), durably
// stored in the porneec_entries Mongo collection. The listing card (an
// <article.thumb-block> on /page/{N}/) carries most fields: WP post id, slug,
// title, poster (data-main-thumb, the same URL as og:image), duration, studios
// (the category-{slug} classes, humanized - "brazzers" -> "Brazzers"), and
// performers (the actors-{slug} classes, humanized - "brook-logan" -> "Brook
// Logan"). porneec obfuscates its tag slugs (tag-big-ass-porn-v565h4) and exposes
// no post-owned tag display names on the detail page, so Tags is left empty.
// The enrich sweep fetches the detail page for the article:published_time
// (sort key), og:description, and the stream mp4: the clean-tube-player WP
// plugin iframe's player-x.php?q={base64} param base64-decodes to a URL-encoded
// <video><source src="https://{sub}.b-cdn.net/{file}.mp4"></video>, a tokenless
// Bunny CDN mp4 (HTTP 200, no rotating token) stored on the doc. ResolveStream
// emits that stored URL directly - no re-fetch (unlike the rotating-token
// sources). One doc per scene (_id = "pec:" + slug).
type PorneecEntry struct {
	VideoID        string   `json:"videoId"` // WP post id; doc id = "pec:"+slug
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	DetailURL      string   `json:"detailUrl"` // https://porneec.com/{slug}/
	Date           string   `json:"date"`      // article:published_time (sort key)
	Poster         string   `json:"poster"`    // data-main-thumb / og:image
	Studios        []string `json:"studios"`   // category-{slug} classes, humanized
	StudiosNorm    []string `json:"studiosNorm"`
	Performers     []string `json:"performers"` // actors-{slug} classes, humanized
	PerformersNorm []string `json:"performersNorm"`
	Description    string   `json:"description"` // og:description
	Duration       string   `json:"duration"`    // card <span class="duration">
	StreamURL      string   `json:"streamUrl"`   // tokenless Bunny CDN mp4 (stored at enrich)
	DetailScraped  bool     `json:"detailScraped"`
	UpdatedAt      int64    `json:"updatedAt"`
}

// TubeEntry is the read-side superset for a tube-source scene, used by the
// generic stremio catalog/meta/stream handlers (internal/stremio/tube_catalog.go).
// The per-source Mongo stores keep their own BSON schemas (PerverzijaEntry /
// FreepornvideosEntry / future *_Entry); each source's adapter maps its entry
// to TubeEntry at read time, so no backfill is needed.
//
// SourceID is the per-source document id segment (pvz: slug, fpv: videoID);
// the Stremio id is IDPrefix+SourceID. Studios is multi-key (pvz: every WP
// category; fpv: {Studio, Network}); Tags is pvz.Tags / fpv.Categories. The
// remaining fields are optional and populated only by the sources that own
// them (StreamHash: pvz; StreamURL: porneec; Network/Rating/Views/Has4K: fpv
// and later mp4 sources; WpPoster: pvz).
type TubeEntry struct {
	SourceKey      string   `json:"sourceKey"` // matches normalizeSources key + catalog disable map
	IDPrefix       string   `json:"idPrefix"`  // "pvz:" / "fpv:" / ...
	SourceID       string   `json:"sourceId"`  // slug / videoID; Stremio id = IDPrefix+SourceID
	Slug           string   `json:"slug,omitempty"`
	Title          string   `json:"title"`
	DetailURL      string   `json:"detailUrl"`
	Date           string   `json:"date"`    // sort key
	Excerpt        string   `json:"excerpt"` // catalog-row description
	Poster         string   `json:"poster"`
	WpPoster       string   `json:"wpPoster,omitempty"` // pvz featured-image fallback
	Studios        []string `json:"studios"`            // multi-key; Cast/Studio link source
	StudiosNorm    []string `json:"studiosNorm,omitempty"`
	Tags           []string `json:"tags"` // genres; pvz.Tags / fpv.Categories
	TagsNorm       []string `json:"tagsNorm,omitempty"`
	Performers     []string `json:"performers"` // Cast links
	PerformersNorm []string `json:"performersNorm,omitempty"`
	Description    string   `json:"description,omitempty"` // full meta description
	Duration       string   `json:"duration,omitempty"`
	StreamHash     string   `json:"streamHash,omitempty"` // pvz xtremestream data hash
	StreamURL      string   `json:"streamUrl,omitempty"`  // porneec stored tokenless b-cdn mp4
	Network        string   `json:"network,omitempty"`    // fpv btn_sponsor_group
	Rating         string   `json:"rating,omitempty"`     // fpv % positive
	Views          string   `json:"views,omitempty"`
	Has4K          bool     `json:"has4k,omitempty"`
	DetailScraped  bool     `json:"detailScraped,omitempty"`
	UpdatedAt      int64    `json:"updatedAt,omitempty"`
}

// NormToken lowercases s and strips every non-alphanumeric character, so that
// "Adult Time", "AdultTime", and "adult-time" all collapse to "adulttime". Used
// to build indexed query keys (StudioNorm/TagsNorm) and to normalize a curated
// Studio/Tag option name before querying pornrips_entries.
func NormToken(s string) string {
	var b []byte
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b = append(b, byte(r))
		}
	}
	return string(b)
}

// PornripsQualityRE strips resolution/codec/release-group tokens from a PornRips
// WP title so the residual identifies the scene. Used by PornripsSceneGroup to
// build the scene_group key shared by every resolution variant of one scene.
// Canonical copy (formerly internal/stremio/pornrips_catalog.go:19); the stremio
// layer imports this so there is one source of truth.
var PornripsQualityRE = regexp.MustCompile(`(?i)\b(?:480p|540p|720p|1080p|1440p|2160p|4k|uhd|hevc|x265|x264|h\.?265|h\.?264|prt)\b`)

var (
	nonAlnumRE     = regexp.MustCompile(`[^a-z0-9]+`)
	qualityRank3RE = regexp.MustCompile(`(?i)\b(?:2160p|4k|uhd)\b`)
	qualityRank2RE = regexp.MustCompile(`(?i)\b(?:1080p|1440p)\b`)
)

// PornripsSceneGroup returns the normalized-title group key for a PornRips title:
// lowercase, strip quality/codec/PRT tokens, collapse non-alphanumerics to spaces,
// trim. Two titles that differ only by resolution/codec (e.g. "...1080p.WEB-DL.x265"
// vs "...720p.WEB-DL.x264") collapse to the same key, so the catalog groups their
// docs into one jstrg: entry. Empty result means the title had no scene-identifying
// tokens; the caller falls back to "pr:"+slug so each such doc is its own group
// (never collapses with another).
func PornripsSceneGroup(title string) string {
	s := strings.ToLower(title)
	s = PornripsQualityRE.ReplaceAllString(s, "")
	s = nonAlnumRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// PornripsQualityRank ranks a PornRips title by resolution for representative
// selection within a scene group: 3 = 4K/2160p/UHD, 2 = 1080p/1440p, 1 = other.
// The highest-ranked member becomes the group representative (catalog poster/title)
// and members[0] in the jstrg: group id.
func PornripsQualityRank(title string) int {
	switch {
	case qualityRank3RE.MatchString(title):
		return 3
	case qualityRank2RE.MatchString(title):
		return 2
	default:
		return 1
	}
}

// StreamURLInput carries the fields persisted for a cached Real-Debrid stream URL.
type StreamURLInput struct {
	MagnetHash            string
	MagnetLink            string
	StreamURL             string
	Filename              string
	Filesize              int64
	SupportsRangeRequests bool
	TorrentName           string
}

// StreamURLRow matches columns from stream_urls.
type StreamURLRow struct {
	ID                    string `json:"id"`
	MagnetHash            string `json:"magnetHash"`
	MagnetLink            string `json:"magnetLink"`
	StreamURL             string `json:"streamUrl"`
	Filename              string `json:"filename"`
	Filesize              int64  `json:"filesize"`
	SupportsRangeRequests bool   `json:"supportsRangeRequests"`
	TorrentName           string `json:"torrentName"`
	ExpiresAt             int64  `json:"expiresAt"`
	CreatedAt             int64  `json:"createdAt"`
	LastAccessedAt        int64  `json:"lastAccessedAt"`
	UpdatedAt             int64  `json:"updatedAt"`
}

// CachedLinkRow matches columns from cached_links.
type CachedLinkRow struct {
	ID                    string  `json:"id"`
	URL                   string  `json:"url"`
	Title                 *string `json:"title"`
	DateAdded             string  `json:"dateAdded"`
	StreamURL             *string `json:"streamUrl"`
	IsStreaming           bool    `json:"isStreaming"`
	Error                 *string `json:"error"`
	StreamURLCachedAt     *string `json:"streamUrlCachedAt"`
	SupportsRangeRequests bool    `json:"supportsRangeRequests"`
	Filename              *string `json:"filename"`
	CoverImageURL         *string `json:"coverImageUrl"`
	UserID                *string `json:"userId"`
}

// DBTableStats holds row counts per table/collection.
type DBTableStats struct {
	Users           int `json:"users"`
	Sessions        int `json:"sessions"`
	Favorites       int `json:"favorites"`
	FavoriteEntries int `json:"favoriteEntries"`
	Images          int `json:"images"`
	StreamURLs      int `json:"streamUrls"`
	CachedLinks     int `json:"cachedLinks"`
	TorrentDetails  int `json:"torrentDetails"`
	KVCache         int `json:"kvCache"`
}

// AddonStatusReport is the managed status, features, issues, and changelog for a single
// addon. Served by the public /api/addon-status endpoints and managed via the
// dashboard-password-gated /api/monitoring/addon-status write endpoints.
type AddonStatusReport struct {
	ID                 string           `bson:"_id"                  json:"-"`
	Addon              AddonMeta        `bson:"addon"                json:"addon"`
	Sources            []AddonSource    `bson:"sources"              json:"sources"`
	Issues             []AddonIssue     `bson:"issues"               json:"issues"`
	Changelog          []AddonChangelog `bson:"changelog"            json:"changelog"`
	Features           []AddonFeature   `bson:"features"             json:"features"`
	ChangelogSourceURL string           `bson:"changelog_source_url" json:"changelogSourceUrl,omitempty"`
}

// AddonMeta is the addon-level status header.
type AddonMeta struct {
	ID        string `bson:"id"         json:"id"`
	Name      string `bson:"name"       json:"name"`
	Status    string `bson:"status"     json:"status"`    // LIVE | DOWN | MAINTENANCE
	UpdatedAt string `bson:"updated_at" json:"updatedAt"` // YYYY-MM-DD
}

// AddonSource is one source row under the status section (e.g. TPB, PornRips).
type AddonSource struct {
	ID     string `bson:"id"     json:"id"`
	Name   string `bson:"name"   json:"name"`
	Note   string `bson:"note"   json:"note,omitempty"`
	Status string `bson:"status" json:"status"` // LIVE | DOWN | MAINTENANCE
	Detail string `bson:"detail" json:"detail"`
}

// AddonIssue is one known-issue row.
type AddonIssue struct {
	ID        string `bson:"id"         json:"id"`
	Title     string `bson:"title"      json:"title"`
	Status    string `bson:"status"     json:"status"` // investigating | identified | monitoring | resolved
	Summary   string `bson:"summary"    json:"summary"`
	UpdatedAt string `bson:"updated_at" json:"updatedAt"`
}

// AddonChangelog is one changelog entry.
type AddonChangelog struct {
	Version    string   `bson:"version"    json:"version"`
	Date       string   `bson:"date"       json:"date"`
	Highlights []string `bson:"highlights" json:"highlights"`
}

// AddonFeature is one feature card.
type AddonFeature struct {
	Title string `bson:"title" json:"title"`
	Body  string `bson:"body"  json:"body"`
}
