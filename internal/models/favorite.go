package models

// Favorite represents a user's favorite
type Favorite struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	TorrentKey  string `json:"torrentKey"`
	TorrentName string `json:"torrentName"`
	Website     string `json:"website"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// FavoriteEntry represents an entry in a favorite
type FavoriteEntry struct {
	ID            string `json:"id"`
	FavoriteID    string `json:"favoriteId"`
	TorrentKey    string `json:"torrentKey"`
	MagnetLink    string `json:"magnetLink"`
	TorrentName   string `json:"torrentName"`
	TorrentData   string `json:"torrentData"`
	CoverImageURL string `json:"coverImageUrl"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// FavoriteDetails represents detailed favorite information
type FavoriteDetails struct {
	Favorite       Favorite        `json:"favorite"`
	Entries        []FavoriteEntry `json:"entries"`
	TorrentDetails *TorrentDetails `json:"torrentDetails,omitempty"`
}

// FavoriteStats holds statistics about favorites
type FavoriteStats struct {
	Total       int `json:"total"`
	WithEntries int `json:"withEntries"`
	WithDetails int `json:"withDetails"`
}
