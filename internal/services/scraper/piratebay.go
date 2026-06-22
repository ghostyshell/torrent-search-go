package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/models"
)

// PirateBayScraper scrapes torrents from ThePirateBay
type PirateBayScraper struct {
	client  *http.Client
	baseURL string
}

// PirateBay categories
const (
	PirateBayCategoryAll   = "0"
	PirateBayCategoryAudio = "100"
	PirateBayCategoryVideo = "200"
	PirateBayCategoryApps  = "300"
	PirateBayCategoryGames = "400"
	PirateBayCategoryPorn  = "500"
	PirateBayCategoryOther = "600"
)

// NewPirateBayScraper creates a new PirateBay scraper
func NewPirateBayScraper(client *http.Client) *PirateBayScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &PirateBayScraper{
		client:  client,
		baseURL: "https://thepiratebay.org",
	}
}

// Search searches torrents on PirateBay
func (s *PirateBayScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	// Build search URL
	searchURL := fmt.Sprintf("%s/search/%s/%d/7/0", s.baseURL, url.PathEscape(query), page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch search results", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode)}
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, &ScraperError{Message: "failed to parse HTML", Err: err}
	}

	torrents := make([]models.Torrent, 0)

	// Find torrent rows
	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		// Skip header rows
		if row.Find("th").Length() > 0 {
			return
		}

		torrent := s.parseTorrentRow(row)
		if torrent != nil && torrent.Name != "" {
			torrents = append(torrents, *torrent)
		}
	})

	return torrents, nil
}

// Browse browses torrents by category on PirateBay
func (s *PirateBayScraper) Browse(ctx context.Context, category string, page int, sort string, options models.SearchOptions) ([]models.Torrent, error) {
	// Build browse URL
	// Format: /browse/{category}/{page}/{sort}
	browseURL := fmt.Sprintf("%s/browse/%s/%d/%s", s.baseURL, category, page, sort)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, browseURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch browse results", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode)}
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, &ScraperError{Message: "failed to parse HTML", Err: err}
	}

	torrents := make([]models.Torrent, 0)

	doc.Find("tr").Each(func(i int, row *goquery.Selection) {
		if row.Find("th").Length() > 0 {
			return
		}

		torrent := s.parseTorrentRow(row)
		if torrent != nil && torrent.Name != "" {
			torrents = append(torrents, *torrent)
		}
	})

	return torrents, nil
}

// parseTorrentRow parses a torrent row from PirateBay HTML
func (s *PirateBayScraper) parseTorrentRow(row *goquery.Selection) *models.Torrent {
	torrent := &models.Torrent{
		Website: "piratebay",
	}

	// Get name and magnet link
	row.Find("a").Each(func(i int, link *goquery.Selection) {
		href, exists := link.Attr("href")
		if !exists {
			return
		}

		// Check for torrent name link (contains /torrent/ in URL)
		if strings.Contains(href, "/torrent/") {
			text := strings.TrimSpace(link.Text())
			if text != "" && torrent.Name == "" {
				torrent.Name = text
				// Build absolute URL
				if !strings.HasPrefix(href, "http") {
					torrent.TorrentURL = s.baseURL + href
				} else {
					torrent.TorrentURL = href
				}
			}
		}

		// Check for magnet link
		if strings.HasPrefix(href, "magnet:") {
			torrent.MagnetLink = href
		}
	})

	// Get size, seeders, leechers from cells
	cells := row.Find("td")
	if cells.Length() >= 5 {
		// Size is typically in the format "Size X.XX GiB (xxxxxxxxxx)"
		sizeText := cells.Eq(1).Text()
		sizeMatch := regexp.MustCompile(`Size\s+([\d.]+\s*\w+)`).FindStringSubmatch(sizeText)
		if len(sizeMatch) > 1 {
			torrent.Size = strings.TrimSpace(sizeMatch[1])
		}

		// Seeders and leechers
		if seedersText := strings.TrimSpace(cells.Eq(2).Text()); seedersText != "" {
			torrent.Seeders, _ = strconv.Atoi(seedersText)
		}
		if leechersText := strings.TrimSpace(cells.Eq(3).Text()); leechersText != "" {
			torrent.Leechers, _ = strconv.Atoi(leechersText)
		}

		// Uploaded by
		uploadedBy := strings.TrimSpace(cells.Eq(1).Find("a").Last().Text())
		if uploadedBy != "" && uploadedBy != "N/A" {
			torrent.UploadedBy = uploadedBy
		}
	}

	// Get upload time
	row.Find("font").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		// Time format: "Uploaded 2024-01-01 12:00, Size X.XX GiB..."
		if strings.HasPrefix(text, "Uploaded") {
			timeMatch := regexp.MustCompile(`Uploaded\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2})`).FindStringSubmatch(text)
			if len(timeMatch) > 1 {
				torrent.Time = timeMatch[1]
			}
		}
	})

	// Validate we have minimum required data
	if torrent.Name == "" || torrent.MagnetLink == "" {
		return nil
	}

	return torrent
}

// GetDetails gets detailed information about a torrent (not implemented for PirateBay)
func (s *PirateBayScraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	return nil, ErrDetailsNotSupported
}

// Ensure PirateBayScraper implements the interfaces
var (
	_ Scraper       = (*PirateBayScraper)(nil)
	_ BrowseScraper = (*PirateBayScraper)(nil)
)
