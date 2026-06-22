// Package models contains storage-agnostic types used by the persistence layer.
// These types were previously tied to the Turso client but are now neutral so
// the MongoDB-backed storage implementation can be used without a Turso dependency.
package models

import "time"

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
	// "description"; MetaID links the row to the shared_meta store.
	Description *string `json:"description,omitempty"`
	CoverSource *string `json:"coverSource,omitempty"`
	MetaID      *string `json:"metaId,omitempty"`
	CreatedAt   int64   `json:"createdAt"`
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
