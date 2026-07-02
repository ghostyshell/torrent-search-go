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

// LimeScraper scrapes torrents from LimeTorrent
type LimeScraper struct {
	client  *http.Client
	baseURL string
}

// NewLimeScraper creates a new LimeTorrent scraper
func NewLimeScraper(client *http.Client) *LimeScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &LimeScraper{
		client:  client,
		baseURL: "https://www.limetorrents.lol",
	}
}

// Search searches torrents on LimeTorrent
func (s *LimeScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	searchURL := fmt.Sprintf("%s/search?q=%s&p=%d", s.baseURL, url.PathEscape(query), page)

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
	doc.Find("table.table2 tr").Each(func(i int, row *goquery.Selection) {
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

// parseTorrentRow parses a torrent row from LimeTorrent HTML
func (s *LimeScraper) parseTorrentRow(row *goquery.Selection) *models.Torrent {
	torrent := &models.Torrent{
		Website: "limetorrent",
	}

	// Get name from first cell
	nameCell := row.Find("td.tdleft").First()
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

	// Get magnet link from download icon
	row.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists {
			if strings.HasPrefix(href, "magnet:") {
				torrent.MagnetLink = href
			}
		}
	})

	// Get size from last cell
	cells := row.Find("td")
	if cells.Length() >= 5 {
		// Size is typically in 4th cell
		torrent.Size = strings.TrimSpace(cells.Eq(3).Text())

		// Seeders in 5th cell
		if seedersText := strings.TrimSpace(cells.Eq(4).Text()); seedersText != "" {
			torrent.Seeders, _ = strconv.Atoi(seedersText)
		}

		// Leechers in 6th cell
		if leechersText := strings.TrimSpace(cells.Eq(5).Text()); leechersText != "" {
			torrent.Leechers, _ = strconv.Atoi(leechersText)
		}
	}

	// Get upload time from title or date element
	if timeElem := row.Find("font"); timeElem.Length() > 0 {
		torrent.Time = strings.TrimSpace(timeElem.Text())
	}

	// Validate minimum required data
	if torrent.Name == "" || torrent.MagnetLink == "" {
		return nil
	}

	return torrent
}

// GetDetails gets detailed information about a torrent
func (s *LimeScraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	return nil, ErrDetailsNotSupported
}

// Ensure LimeScraper implements the Scraper interface
var _ Scraper = (*LimeScraper)(nil)
