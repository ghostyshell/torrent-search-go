package models

import "time"

// CacheEntry represents a generic cache entry
type CacheEntry struct {
	Key       string      `json:"key"`
	Value     interface{} `json:"value"`
	Type      string      `json:"type"`
	ExpiresAt *int64      `json:"expiresAt,omitempty"`
	Metadata  interface{} `json:"metadata,omitempty"`
	CreatedAt int64       `json:"createdAt"`
}

// IsExpired checks if the cache entry is expired
func (c *CacheEntry) IsExpired() bool {
	if c.ExpiresAt == nil {
		return false
	}
	return time.Now().Unix() > *c.ExpiresAt
}

// StreamURL represents a cached Real-Debrid stream URL
type StreamURL struct {
	ID         string `json:"id"`
	MagnetHash string `json:"magnetHash"`
	MagnetLink string `json:"magnetLink"`
	StreamURL  string `json:"streamUrl"`
	ExpiresAt  int64  `json:"expiresAt"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// IsExpired checks if the stream URL is expired
func (s *StreamURL) IsExpired() bool {
	return time.Now().Unix() > s.ExpiresAt
}

// CachedLink represents a cached external link
type CachedLink struct {
	ID            string `json:"id"`
	UserID        string `json:"userId"`
	LinkType      string `json:"linkType"`
	OriginalURL   string `json:"originalUrl"`
	CachedURL     string `json:"cachedUrl"`
	CoverImageURL string `json:"coverImageUrl"`
	Metadata      string `json:"metadata"`
	ExpiresAt     *int64 `json:"expiresAt,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// Image represents a cached image
type Image struct {
	ID         string `json:"id"`
	TorrentKey string `json:"torrentKey"`
	ImageURL   string `json:"imageUrl"`
	ImageData  []byte `json:"imageData,omitempty"`
	MimeType   string `json:"mimeType"`
	Source     string `json:"source"`
	CreatedAt  int64  `json:"createdAt"`
}

// TorrentDetail represents cached torrent details
type TorrentDetail struct {
	ID            string `json:"id"`
	FavoriteID    string `json:"favoriteId"`
	Source        string `json:"source"`
	DetailsData   string `json:"detailsData"`
	CoverImageURL string `json:"coverImageUrl"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}
