package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"torrent-search-go/internal/models"
)

// YTSScraper scrapes torrents from YTS (YIFY)
type YTSScraper struct {
	client  *http.Client
	baseURL string
}

// YTS API response structures
type YTSResponse struct {
	Status        string  `json:"status"`
	StatusMessage string  `json:"status_message"`
	Data          YTSData `json:"data"`
}

type YTSData struct {
	MovieCount int        `json:"movie_count"`
	Limit      int        `json:"limit"`
	PageNumber int        `json:"page_number"`
	Movies     []YTSMovie `json:"movies"`
}

type YTSMovie struct {
	ID                    int          `json:"id"`
	URL                   string       `json:"url"`
	IMDBCode              string       `json:"imdb_code"`
	Title                 string       `json:"title"`
	TitleLong             string       `json:"title_long"`
	Slug                  string       `json:"slug"`
	Year                  int          `json:"year"`
	Rating                float64      `json:"rating"`
	Runtime               int          `json:"runtime"`
	Genres                []string     `json:"genres"`
	Summary               string       `json:"summary"`
	DescriptionFull       string       `json:"description_full"`
	Synopsis              string       `json:"synopsis"`
	YTRating              string       `json:"yt_rating"`
	DateUploaded          string       `json:"date_uploaded"`
	DateUploadedUnix      int64        `json:"date_uploaded_unix"`
	State                 string       `json:"state"`
	Torrents              []YTSTorrent `json:"torrents"`
	DateUploadedFormatted string       `json:"date_uploaded_formatted"`
	LargeScreenshotImage  string       `json:"large_screenshot_image"`
	MediumCoverImage      string       `json:"medium_cover_image"`
}

type YTSTorrent struct {
	URL              string `json:"url"`
	Hash             string `json:"hash"`
	Quality          string `json:"quality"`
	Type             string `json:"type"`
	Seeds            int    `json:"seeds"`
	Peers            int    `json:"peers"`
	Size             string `json:"size"`
	SizeBytes        int64  `json:"size_bytes"`
	DateUploaded     string `json:"date_uploaded"`
	DateUploadedUnix int64  `json:"date_uploaded_unix"`
}

// NewYTSScraper creates a new YTS scraper
func NewYTSScraper(client *http.Client) *YTSScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &YTSScraper{
		client:  client,
		baseURL: "https://yts.mx/api/v2",
	}
}

// Search searches torrents on YTS using their API
func (s *YTSScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	// Build API URL
	apiURL := fmt.Sprintf("%s/list_movies.json?query_term=%s&page=%d&limit=20",
		s.baseURL, url.PathEscape(query), page)

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

	var ytsResp YTSResponse
	if err := json.NewDecoder(resp.Body).Decode(&ytsResp); err != nil {
		return nil, &ScraperError{Message: "failed to parse JSON response", Err: err}
	}

	if ytsResp.Status != "ok" {
		return nil, &ScraperError{Message: fmt.Sprintf("API error: %s", ytsResp.StatusMessage)}
	}

	torrents := make([]models.Torrent, 0)

	for _, movie := range ytsResp.Data.Movies {
		for _, torrent := range movie.Torrents {
			// Apply min seeders filter
			if options.MinSeeders > 0 && torrent.Seeds < options.MinSeeders {
				continue
			}

			t := models.Torrent{
				Name:       fmt.Sprintf("%s (%d) [%s] [%s]", movie.Title, movie.Year, torrent.Quality, torrent.Type),
				Size:       torrent.Size,
				Seeders:    torrent.Seeds,
				Leechers:   torrent.Peers,
				Time:       movie.DateUploaded,
				Category:   "Video",
				MagnetLink: fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", torrent.Hash, url.PathEscape(movie.Title)),
				TorrentURL: torrent.URL,
				Website:    "yts",
				UploadedBy: "YTS",
			}

			// Add cover image if available
			if movie.MediumCoverImage != "" {
				t.CoverImage = &models.CoverImage{
					Type:     "url",
					URL:      movie.MediumCoverImage,
					MimeType: "image/jpeg",
				}
			}

			torrents = append(torrents, t)
		}
	}

	// Apply max results limit
	if options.MaxResults > 0 && len(torrents) > options.MaxResults {
		torrents = torrents[:options.MaxResults]
	}

	return torrents, nil
}

// GetDetails gets detailed information about a YTS movie
func (s *YTSScraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	// Extract movie ID from URL or fetch from API
	// For YTS, we can use the movie details API endpoint
	// Example: https://yts.mx/api/v2/movie_details.json?movie_id=12345

	// This is a simplified implementation
	return &models.TorrentDetails{
		Name:       "YTS Movie",
		Category:   "Video",
		Website:    "yts",
		TorrentURL: torrentURL,
	}, nil
}

// Ensure YTSScraper implements the interfaces
var (
	_ Scraper        = (*YTSScraper)(nil)
	_ DetailsScraper = (*YTSScraper)(nil)
)
