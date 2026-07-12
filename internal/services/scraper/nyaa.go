package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/models"
)

// NyaaScraper scrapes torrents from NyaaSI
type NyaaScraper struct {
	client  *http.Client
	baseURL string
}

// NewNyaaScraper creates a new NyaaSI scraper
func NewNyaaScraper(client *http.Client) *NyaaScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &NyaaScraper{
		client:  client,
		baseURL: "https://nyaa.si",
	}
}

// Search searches torrents on NyaaSI
func (s *NyaaScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	searchURL := fmt.Sprintf("%s/?f=0&s=0&o=desc&p=%d&q=%s", s.baseURL, page, url.PathEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch search results", Err: err}
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

	// Find torrent rows in the table
	doc.Find("table.torrent-list tbody tr").Each(func(i int, row *goquery.Selection) {
		torrent := s.parseTorrentRow(row)
		if torrent != nil && torrent.Name != "" {
			torrents = append(torrents, *torrent)
		}
	})

	return torrents, nil
}

// parseTorrentRow parses a torrent row from NyaaSI HTML
func (s *NyaaScraper) parseTorrentRow(row *goquery.Selection) *models.Torrent {
	torrent := &models.Torrent{
		Website: "nyaasi",
	}

	// Get name from second cell
	nameCell := row.Find("td").Eq(1)
	nameLink := nameCell.Find("a").First()
	torrent.Name = strings.TrimSpace(nameLink.Text())

	// Get torrent URL
	if href, exists := nameLink.Attr("href"); exists {
		if !strings.HasPrefix(href, "http") {
			torrent.TorrentURL = s.baseURL + href
		} else {
			torrent.TorrentURL = href
		}
	}

	// Get magnet link from download icons
	row.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists {
			if strings.HasPrefix(href, "magnet:") {
				torrent.MagnetLink = href
			}
		}
	})

	// Get other info from cells
	cells := row.Find("td")
	if cells.Length() >= 10 {
		// Size is in 4th cell
		torrent.Size = strings.TrimSpace(cells.Eq(3).Text())

		// Seeders in 9th cell
		if seedersText := strings.TrimSpace(cells.Eq(8).Text()); seedersText != "" {
			torrent.Seeders, _ = strconv.Atoi(seedersText)
		}

		// Leechers in 10th cell
		if leechersText := strings.TrimSpace(cells.Eq(9).Text()); leechersText != "" {
			torrent.Leechers, _ = strconv.Atoi(leechersText)
		}
	}

	// Get upload time from title attribute or date cell
	if dateCell := cells.Eq(5); dateCell.Length() > 0 {
		torrent.Time = strings.TrimSpace(dateCell.Text())
	}

	// Get category
	if categoryCell := cells.Eq(0); categoryCell.Length() > 0 {
		torrent.Category = strings.TrimSpace(categoryCell.Text())
	}

	// Validate minimum required data
	if torrent.Name == "" || torrent.MagnetLink == "" {
		return nil
	}

	return torrent
}

// GetDetails gets detailed information about a torrent
func (s *NyaaScraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	// Fetch the torrent page
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch torrent details", Err: err}
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, &ScraperError{Message: "failed to parse HTML", Err: err}
	}

	details := &models.TorrentDetails{}

	// Get description from torrent-description div
	if descDiv := doc.Find(".torrent-description"); descDiv.Length() > 0 {
		details.Description = strings.TrimSpace(descDiv.Text())
	}

	// Get files from torrent-files table
	doc.Find(".torrent-files-table tr").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			return // Skip header
		}
		fileName := strings.TrimSpace(s.Find("td").Eq(0).Text())
		fileSize := strings.TrimSpace(s.Find("td").Eq(1).Text())
		if fileName != "" {
			details.Files = append(details.Files, models.File{
				Name: fileName,
				Size: fileSize,
			})
		}
	})

	return details, nil
}

// Ensure NyaaScraper implements the interfaces
var (
	_ Scraper        = (*NyaaScraper)(nil)
	_ DetailsScraper = (*NyaaScraper)(nil)
)
