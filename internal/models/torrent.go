package models

import (
	"encoding/json"
	"strconv"
)

// Torrent represents a torrent search result.
//
// Internally fields are strongly typed (e.g. Seeders is an int for sorting and
// filtering), but the JSON wire format uses a clean, well-documented shape
// (Url/Magnet/DateUploaded, Seeders and Leechers as strings, lowercase coverImage).
// See MarshalJSON below.
type Torrent struct {
	Name       string      `json:"-"`
	Size       string      `json:"-"`
	Seeders    int         `json:"-"`
	Leechers   int         `json:"-"`
	Time       string      `json:"-"`
	UploadedBy string      `json:"-"`
	Category   string      `json:"-"`
	MagnetLink string      `json:"-"`
	TorrentURL string      `json:"-"`
	Website    string      `json:"-"`
	CoverImage *CoverImage `json:"-"`
}

// torrentWire is the on-the-wire JSON representation.
type torrentWire struct {
	Name         string      `json:"Name"`
	Size         string      `json:"Size"`
	DateUploaded string      `json:"DateUploaded"`
	Category     string      `json:"Category"`
	Seeders      string      `json:"Seeders"`
	Leechers     string      `json:"Leechers"`
	UploadedBy   string      `json:"UploadedBy"`
	Url          string      `json:"Url"`
	Magnet       string      `json:"Magnet"`
	Source       string      `json:"Source,omitempty"`
	CoverImage   *CoverImage `json:"coverImage,omitempty"`
}

// MarshalJSON renders the wire format.
func (t Torrent) MarshalJSON() ([]byte, error) {
	return json.Marshal(torrentWire{
		Name:         t.Name,
		Size:         t.Size,
		DateUploaded: t.Time,
		Category:     t.Category,
		Seeders:      strconv.Itoa(t.Seeders),
		Leechers:     strconv.Itoa(t.Leechers),
		UploadedBy:   t.UploadedBy,
		Url:          t.TorrentURL,
		Magnet:       t.MagnetLink,
		Source:       t.Website,
		CoverImage:   t.CoverImage,
	})
}

// CoverImage represents a torrent cover image
type CoverImage struct {
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// TorrentComment is a user comment on a torrent detail page.
type TorrentComment struct {
	Author  string `json:"author"`
	Comment string `json:"comment"`
	Date    string `json:"date"`
}

// TorrentImageLink is an image extracted from a torrent description.
type TorrentImageLink struct {
	OriginalURL string `json:"originalUrl"`
	DirectURL   string `json:"directUrl"`
}

// TorrentDetails represents detailed torrent information
type TorrentDetails struct {
	Name          string             `json:"name"`
	Size          string             `json:"size"`
	UploadedAt    string             `json:"uploadedAt"`
	UploadedBy    string             `json:"uploadedBy"`
	Category      string             `json:"category"`
	Seeders       int                `json:"seeders"`
	Leechers      int                `json:"leechers"`
	Description   string             `json:"description"`
	Files         []File             `json:"files"`
	Comments      []TorrentComment   `json:"-"`
	Images        []TorrentImageLink `json:"-"`
	MagnetLink    string             `json:"magnetLink"`
	InfoHash      string             `json:"-"`
	TorrentURL    string             `json:"torrentUrl"`
	Website       string             `json:"website"`
	CoverImageURL string             `json:"coverImageUrl,omitempty"`
	Error         string             `json:"-"`
}

// LegacyWire returns the Node-compatible torrent details JSON shape used by
// torrent-browse-ui (/api/torrent-details and /api/torrents/details).
func (d *TorrentDetails) LegacyWire() map[string]interface{} {
	if d == nil {
		return map[string]interface{}{
			"description": "Failed to load description",
			"files":       []File{},
			"comments":    []TorrentComment{},
			"images":      []TorrentImageLink{},
		}
	}

	files := d.Files
	if files == nil {
		files = []File{}
	}
	comments := d.Comments
	if comments == nil {
		comments = []TorrentComment{}
	}
	images := d.Images
	if images == nil {
		images = []TorrentImageLink{}
	}

	wire := map[string]interface{}{
		"description": d.Description,
		"files":       files,
		"comments":    comments,
		"images":      images,
	}
	if d.MagnetLink != "" {
		wire["magnet"] = d.MagnetLink
	}
	if d.InfoHash != "" {
		wire["hash"] = d.InfoHash
	}
	if d.Error != "" {
		wire["error"] = d.Error
	}
	return wire
}

// File represents a file within a torrent
type File struct {
	Name string `json:"name"`
	Size string `json:"size"`
}

// SearchOptions holds options for torrent search
type SearchOptions struct {
	MinSeeders         int    `json:"minSeeders"`
	MaxResults         int    `json:"maxResults"`
	IncludeCoverImages bool   `json:"includeCoverImages"`
	Sort               string `json:"sort"`
	Category           string `json:"category"`
}

// SearchResult holds the result of a torrent search
type SearchResult struct {
	Success bool      `json:"success"`
	Website string    `json:"website"`
	Query   string    `json:"query"`
	Page    int       `json:"page"`
	Results []Torrent `json:"results"`
}

// AdvancedSearchRequest holds the request for advanced search
type AdvancedSearchRequest struct {
	Query              string   `json:"query"`
	Websites           []string `json:"websites"`
	MinSeeders         int      `json:"minSeeders"`
	MaxResults         int      `json:"maxResults"`
	SortBy             string   `json:"sortBy"`
	SortOrder          string   `json:"sortOrder"`
	IncludeCoverImages bool     `json:"includeCoverImages"`
}

// AdvancedSearchResponse holds the response for advanced search
type AdvancedSearchResponse struct {
	Success      bool          `json:"success"`
	Query        string        `json:"query"`
	Websites     []string      `json:"websites"`
	Filters      SearchOptions `json:"filters"`
	TotalResults int           `json:"totalResults"`
	Results      []Torrent     `json:"results"`
}
