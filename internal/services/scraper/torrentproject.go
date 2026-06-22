package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"torrent-search-go/internal/models"
)

// TorrentProjectScraper scrapes torrents from TorrentProject
type TorrentProjectScraper struct {
	client  *http.Client
	baseURL string
}

// TorrentProject API response
type TorrentProjectResponse struct {
	Total    int                    `json:"total"`
	Torrents []TorrentProjectResult `json:"torrents"`
}

type TorrentProjectResult struct {
	Name     string               `json:"name"`
	InfoHash string               `json:"info_hash"`
	Leechers int                  `json:"leechers"`
	Seeders  int                  `json:"seeders"`
	LastDate string               `json:"last_date"`
	Size     int64                `json:"size_bytes"`
	NumFiles int                  `json:"num_files"`
	Files    []TorrentProjectFile `json:"files"`
}

type TorrentProjectFile struct {
	Name string `json:"name"`
	Size int64  `json:"size_bytes"`
}

// NewTorrentProjectScraper creates a new TorrentProject scraper
func NewTorrentProjectScraper(client *http.Client) *TorrentProjectScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &TorrentProjectScraper{
		client:  client,
		baseURL: "https://torrentproject2.com",
	}
}

// Search searches torrents on TorrentProject using their API
func (s *TorrentProjectScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	// TorrentProject API endpoint
	apiURL := fmt.Sprintf("https://torrentproject2.com/?t=%s&out=json", url.PathEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch search results", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode)}
	}

	var result TorrentProjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &ScraperError{Message: "failed to parse JSON response", Err: err}
	}

	torrents := make([]models.Torrent, 0)

	for _, t := range result.Torrents {
		// Skip if no info hash
		if t.InfoHash == "" {
			continue
		}

		// Apply min seeders filter
		if options.MinSeeders > 0 && t.Seeders < options.MinSeeders {
			continue
		}

		// Format size
		size := formatBytes(t.Size)

		torrent := models.Torrent{
			Name:       t.Name,
			Size:       size,
			Seeders:    t.Seeders,
			Leechers:   t.Leechers,
			Time:       t.LastDate,
			Category:   "Other",
			MagnetLink: fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", t.InfoHash, url.PathEscape(t.Name)),
			TorrentURL: fmt.Sprintf("%s/torrent/%s", s.baseURL, url.PathEscape(t.Name)),
			Website:    "torrentproject",
		}

		torrents = append(torrents, torrent)
	}

	// Apply max results limit
	if options.MaxResults > 0 && len(torrents) > options.MaxResults {
		torrents = torrents[:options.MaxResults]
	}

	return torrents, nil
}

// GetDetails gets detailed information about a torrent
func (s *TorrentProjectScraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	// TorrentProject doesn't have a detailed view API
	// Return basic info extracted from the torrent name
	name := extractNameFromURL(torrentURL)

	return &models.TorrentDetails{
		Name:       name,
		Website:    "torrentproject",
		TorrentURL: torrentURL,
		Category:   "Other",
	}, nil
}

// formatBytes formats bytes to human readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// extractNameFromURL extracts torrent name from URL
func extractNameFromURL(torrentURL string) string {
	parts := strings.Split(torrentURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "Unknown"
}

// Ensure TorrentProjectScraper implements the interfaces
var (
	_ Scraper        = (*TorrentProjectScraper)(nil)
	_ DetailsScraper = (*TorrentProjectScraper)(nil)
)
