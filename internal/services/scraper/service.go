package scraper

import (
	"context"
	"net/http"
	"time"

	"torrent-search-go/internal/models"
)

// Scraper defines the interface for a torrent scraper
type Scraper interface {
	Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error)
}

// DetailsScraper extends Scraper with details fetching capability
type DetailsScraper interface {
	Scraper
	GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error)
}

// BrowseScraper extends Scraper with category browsing capability
type BrowseScraper interface {
	Scraper
	Browse(ctx context.Context, category string, page int, sort string, options models.SearchOptions) ([]models.Torrent, error)
}

// Service coordinates all torrent scrapers
type Service struct {
	scrapers map[string]Scraper
	client   *http.Client
}

// NewService creates a new scraper service
func NewService() *Service {
	client := NewSafeClient(30 * time.Second)

	return &Service{
		scrapers: make(map[string]Scraper),
		client:   client,
	}
}

// RegisterScraper registers a scraper for a website
func (s *Service) RegisterScraper(name string, scraper Scraper) {
	s.scrapers[name] = scraper
}

// GetScraper returns a scraper by name
func (s *Service) GetScraper(name string) (Scraper, bool) {
	scraper, ok := s.scrapers[name]
	return scraper, ok
}

// GetAvailableScrapers returns list of available scraper names
func (s *Service) GetAvailableScrapers() []string {
	names := make([]string, 0, len(s.scrapers))
	for name := range s.scrapers {
		names = append(names, name)
	}
	return names
}

// Search searches torrents on a specific website
func (s *Service) Search(ctx context.Context, website, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	scraper, ok := s.GetScraper(website)
	if !ok {
		return nil, ErrScraperNotFound
	}

	return scraper.Search(ctx, query, page, options)
}

// SearchAll searches torrents across all websites in parallel
func (s *Service) SearchAll(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	results := make([]models.Torrent, 0)

	// Create channels for results and errors
	type result struct {
		torrents []models.Torrent
		err      error
		website  string
	}

	resultChan := make(chan result, len(s.scrapers))

	// Search all scrapers in parallel
	for name, scraper := range s.scrapers {
		go func(name string, scraper Scraper) {
			torrents, err := scraper.Search(ctx, query, page, options)
			resultChan <- result{
				torrents: torrents,
				err:      err,
				website:  name,
			}
		}(name, scraper)
	}

	// Collect results
	for range s.scrapers {
		r := <-resultChan
		if r.err == nil && len(r.torrents) > 0 {
			results = append(results, r.torrents...)
		}
	}

	// Apply filtering
	if options.MinSeeders > 0 {
		filtered := make([]models.Torrent, 0)
		for _, t := range results {
			if t.Seeders >= options.MinSeeders {
				filtered = append(filtered, t)
			}
		}
		results = filtered
	}

	// Apply max results limit
	if options.MaxResults > 0 && len(results) > options.MaxResults {
		results = results[:options.MaxResults]
	}

	return results, nil
}

// GetTorrentDetails gets detailed information about a torrent
func (s *Service) GetTorrentDetails(ctx context.Context, website, torrentURL string) (*models.TorrentDetails, error) {
	scraper, ok := s.GetScraper(website)
	if !ok {
		return nil, ErrScraperNotFound
	}

	detailsScraper, ok := scraper.(DetailsScraper)
	if !ok {
		return nil, ErrDetailsNotSupported
	}

	return detailsScraper.GetDetails(ctx, torrentURL)
}

// Browse browses torrents by category
func (s *Service) Browse(ctx context.Context, website, category string, page int, sort string, options models.SearchOptions) ([]models.Torrent, error) {
	scraper, ok := s.GetScraper(website)
	if !ok {
		return nil, ErrScraperNotFound
	}

	browseScraper, ok := scraper.(BrowseScraper)
	if !ok {
		return nil, ErrBrowseNotSupported
	}

	return browseScraper.Browse(ctx, category, page, sort, options)
}

// GetHTTPClient returns the HTTP client used by scrapers
func (s *Service) GetHTTPClient() *http.Client {
	return s.client
}

type downloadResolver interface {
	ResolveDownloadURL(context.Context, string) string
}

// ResolveDownloadURL fetches the magnet or torrent URL from a detail page.
// Returns "" if the scraper doesn't support it or the fetch fails.
func (s *Service) ResolveDownloadURL(ctx context.Context, website, postURL string) string {
	sc, ok := s.GetScraper(website)
	if !ok {
		return ""
	}
	if d, ok := sc.(downloadResolver); ok {
		return d.ResolveDownloadURL(ctx, postURL)
	}
	return ""
}

type torrentDataFetcher interface {
	FetchTorrentData(context.Context, string, string) ([]byte, error)
}

// FetchTorrentData fetches raw .torrent file bytes.
// postURL is the detail page URL (used as referer and to resolve the download link);
// torrentName is the PRT release name used as fallback for URL construction.
func (s *Service) FetchTorrentData(ctx context.Context, website, postURL, torrentName string) ([]byte, error) {
	sc, ok := s.GetScraper(website)
	if !ok {
		return nil, ErrScraperNotFound
	}
	if f, ok := sc.(torrentDataFetcher); ok {
		return f.FetchTorrentData(ctx, postURL, torrentName)
	}
	return nil, ErrDetailsNotSupported
}

// Scraper errors
var (
	ErrScraperNotFound     = &ScraperError{Message: "Scraper not found"}
	ErrDetailsNotSupported = &ScraperError{Message: "Details not supported for this scraper"}
	ErrBrowseNotSupported  = &ScraperError{Message: "Browse not supported for this scraper"}
)

// ScraperError represents a scraper error
type ScraperError struct {
	Message string
	Err     error
}

func (e *ScraperError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *ScraperError) Unwrap() error {
	return e.Err
}
