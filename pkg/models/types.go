// Package models contains storage-agnostic types used by the persistence layer.
// These types were previously tied to the Turso client but are now neutral so
// the MongoDB-backed storage implementation can be used without a Turso dependency.
package models

import (
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
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	DetailURL  string   `json:"detailUrl"`
	Date       string   `json:"date"`
	Excerpt    string   `json:"excerpt"`
	WpPoster   string   `json:"wpPoster"`
	Poster     string   `json:"poster"`
	Studio     string   `json:"studio"`
	StudioNorm string   `json:"studioNorm"`
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

// HentaiEntry is one hentai series from HentaiMama (hmm), durably stored in
// the hentai_entries Mongo collection. All fields are filled by HentaiIngest
// from the source series page; the source rating (hentaimama, 0-10) is shown
// directly as the Stremio ImdbRating, so there is no external MAL/Jikan
// enrichment step. One doc per source id (hmm-…). (HentaiTV htv- was removed;
// any leftover htv docs are hidden by filtering reads to prefix "hmm".)
type HentaiEntry struct {
	ID          string          `json:"id"`          // "{prefix}-{slug}", e.g. "hmm-toshi-ie"
	Prefix      string          `json:"prefix"`      // "hmm"
	Slug        string          `json:"slug"`        // source-local series slug
	Source      string          `json:"source"`      // "hentaimama"
	Title       string          `json:"title"`
	Poster      string          `json:"poster"`
	Background  string          `json:"background"`
	Excerpt     string          `json:"excerpt"`
	ReleaseYear string          `json:"releaseYear"`
	Studio      string          `json:"studio"`
	StudioNorm  string          `json:"studioNorm"`
	Genres      []string        `json:"genres"`
	GenresNorm  []string        `json:"genresNorm"`
	Rating      float64         `json:"rating"`   // source rating 0-10
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
	ID                 string            `bson:"_id"                  json:"-"`
	Addon              AddonMeta         `bson:"addon"                json:"addon"`
	Sources            []AddonSource     `bson:"sources"              json:"sources"`
	Issues             []AddonIssue      `bson:"issues"               json:"issues"`
	Changelog          []AddonChangelog  `bson:"changelog"            json:"changelog"`
	Features           []AddonFeature    `bson:"features"             json:"features"`
	ChangelogSourceURL string            `bson:"changelog_source_url" json:"changelogSourceUrl,omitempty"`
}

// AddonMeta is the addon-level status header.
type AddonMeta struct {
	ID        string `bson:"id"         json:"id"`
	Name      string `bson:"name"       json:"name"`
	Status    string `bson:"status"     json:"status"`     // LIVE | DOWN | MAINTENANCE
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
