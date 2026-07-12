package scraper

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/models"
)

const xxxclubBaseURL = "https://xxxclub.to"

type XxxClubScraper struct {
	client  *http.Client
	baseURL string
}

func NewXxxClubScraper(client *http.Client) *XxxClubScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &XxxClubScraper{client: client, baseURL: xxxclubBaseURL}
}

func (s *XxxClubScraper) Search(ctx context.Context, query string, page int, _ models.SearchOptions) ([]models.Torrent, error) {
	return s.scrapePage(ctx, "/torrents/browse/all/")
}

func (s *XxxClubScraper) Browse(ctx context.Context, category string, page int, _ string, _ models.SearchOptions) ([]models.Torrent, error) {
	cat := xxxclubCategory(category)
	return s.scrapePage(ctx, fmt.Sprintf("/torrents/browse/%s/", cat))
}

func (s *XxxClubScraper) scrapePage(ctx context.Context, path string) ([]models.Torrent, error) {
	url := s.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &ScraperError{Message: "xxxclub request failed", Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "xxxclub fetch failed", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("xxxclub returned %d", resp.StatusCode)}
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, &ScraperError{Message: "xxxclub parse failed", Err: err}
	}

	out := make([]models.Torrent, 0, 30)
	doc.Find(".browsetablediv li").Each(func(i int, li *goquery.Selection) {
		if len(out) >= 30 {
			return
		}
		if li.Find("span").Length() < 3 {
			return
		}
		link := li.Find(".toral a[href*='/torrents/details/']")
		if link.Length() == 0 {
			return
		}
		href, _ := link.Attr("href")
		id, _ := link.Attr("id")
		title := strings.TrimSpace(link.Text())
		if title == "" {
			return
		}
		infoHash := extractXxxClubHash(id)
		if infoHash == "" {
			return
		}
		detailURL := s.baseURL + href
		size := strings.TrimSpace(li.Find(".siz").Text())
		seeders := atoiSafe(strings.TrimSpace(li.Find(".see").Text()))
		leechers := atoiSafe(strings.TrimSpace(li.Find(".lee").Text()))
		uploader := strings.TrimSpace(li.Find(".uploadertable").Text())
		out = append(out, models.Torrent{
			Name:       title,
			MagnetLink: buildMagnetLink(infoHash, title),
			Seeders:    seeders,
			Leechers:   leechers,
			Size:       size,
			Website:    "xxxclub",
			TorrentURL: detailURL,
			UploadedBy: uploader,
		})
	})

	return out, nil
}

func extractXxxClubHash(elementID string) string {
	id := strings.TrimSpace(elementID)
	id = strings.TrimPrefix(id, "#")
	id = strings.TrimPrefix(id, "id")
	id = strings.TrimPrefix(id, "i")
	if len(id) == 40 {
		if matched, _ := regexp.MatchString(`^[a-f0-9]{40}$`, strings.ToLower(id)); matched {
			return strings.ToLower(id)
		}
	}
	return ""
}

func xxxclubCategory(tpbCategory string) string {
	switch tpbCategory {
	case "507":
		return "4"
	case "505":
		return "2"
	default:
		return "all"
	}
}

func atoiSafe(s string) int {
	s = regexp.MustCompile(`[^\d]`).ReplaceAllString(s, "")
	if s == "" {
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

var (
	_ Scraper       = (*XxxClubScraper)(nil)
	_ BrowseScraper = (*XxxClubScraper)(nil)
)
