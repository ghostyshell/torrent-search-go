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

const bitsearchBaseURL = "https://bitsearch.eu"

type BitsearchScraper struct {
	client  *http.Client
	baseURL string
}

func NewBitsearchScraper(client *http.Client) *BitsearchScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &BitsearchScraper{client: client, baseURL: bitsearchBaseURL}
}

func (s *BitsearchScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	sortParam := "seeders"
	if options.Sort == "3" {
		sortParam = "date"
	}
	searchURL := fmt.Sprintf("%s/search?q=%s&sort=%s", s.baseURL, url.QueryEscape(query), sortParam)
	if page > 1 {
		searchURL = fmt.Sprintf("%s/search?q=%s&sort=%s&page=%d", s.baseURL, url.QueryEscape(query), sortParam, page)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch bitsearch", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("bitsearch returned %d", resp.StatusCode)}
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, &ScraperError{Message: "failed to parse HTML", Err: err}
	}

	seen := make(map[string]bool)
	out := make([]models.Torrent, 0, 30)

	doc.Find(`a[href^="magnet:"]`).Each(func(i int, sel *goquery.Selection) {
		if len(out) >= 30 {
			return
		}
		card := sel.Closest("div.bg-white")
		if card.Length() == 0 {
			return
		}
		magnet, _ := sel.Attr("href")
		infoHash := extractBitsearchHash(magnet)
		if infoHash == "" || seen[infoHash] {
			return
		}
		title := strings.TrimSpace(card.Find(`a[href^="/torrent/"]`).First().Text())
		title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")
		if title == "" {
			return
		}
		seen[infoHash] = true
		text := regexp.MustCompile(`\s+`).ReplaceAllString(card.Text(), " ")
		out = append(out, models.Torrent{
			Name:       title,
			MagnetLink: magnet,
			Seeders:    parseBitsearchInt(text, "seeders"),
			Leechers:   parseBitsearchInt(text, "leechers"),
			Size:       parseBitsearchSizeStr(text),
			Website:    "bitsearch",
			TorrentURL: magnet,
		})
	})

	return out, nil
}

func extractBitsearchHash(magnet string) string {
	m := regexp.MustCompile(`(?i)xt=urn:btih:([a-fA-F0-9]{40}|[a-z2-7]{32})`).FindStringSubmatch(magnet)
	if len(m) < 2 {
		return ""
	}
	raw := strings.ToLower(m[1])
	if len(raw) == 32 {
		if hex, err := base32ToHex(raw); err == nil {
			return hex
		}
	}
	return raw
}

func parseBitsearchInt(text, label string) int {
	re := regexp.MustCompile(`(?i)([\d,]+)\s+` + regexp.QuoteMeta(label))
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	return n
}

func parseBitsearchSizeStr(text string) string {
	matches := regexp.MustCompile(`(?i)([\d.]+)\s*(KB|MB|GB|TB|B)\b`).FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return ""
	}
	m := matches[len(matches)-1]
	return fmt.Sprintf("%s %sB", m[1], strings.ToUpper(m[2]))
}

const base32Alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

func base32ToHex(b32 string) (string, error) {
	var bits, val int
	var out strings.Builder
	for _, c := range strings.ToUpper(b32) {
		idx := strings.IndexRune(base32Alpha, c)
		if idx < 0 {
			return "", fmt.Errorf("invalid base32 char: %c", c)
		}
		val = (val << 5) | idx
		bits += 5
		if bits >= 8 {
			out.WriteByte(byte((val >> (bits - 8)) & 0xff))
			bits -= 8
		}
	}
	return out.String(), nil
}

var _ Scraper = (*BitsearchScraper)(nil)
