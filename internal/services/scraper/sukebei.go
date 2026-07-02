package scraper

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"torrent-search-go/internal/models"
)

var (
	sukebeiItemRE     = regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	sukebeiTitleRE    = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	sukebeiLinkRE     = regexp.MustCompile(`(?s)<link>(.*?)</link>`)
	sukebeiGuidRE     = regexp.MustCompile(`(?s)<guid[^>]*>(.*?)</guid>`)
	sukebeiPubDateRE  = regexp.MustCompile(`(?s)<pubDate>(.*?)</pubDate>`)
	sukebeiSeedersRE  = regexp.MustCompile(`(?s)<nyaa:seeders>(\d+)</nyaa:seeders>`)
	sukebeiLeechersRE = regexp.MustCompile(`(?s)<nyaa:leechers>(\d+)</nyaa:leechers>`)
	sukebeiInfoHashRE = regexp.MustCompile(`(?i)<nyaa:infoHash>([a-f0-9]{40})</nyaa:infoHash>`)
	sukebeiSizeRE     = regexp.MustCompile(`(?s)<nyaa:size>(.*?)</nyaa:size>`)
	sukebeiCategoryRE = regexp.MustCompile(`(?s)<nyaa:category>(.*?)</nyaa:category>`)
)

const sukebeiBaseURL = "https://sukebei.nyaa.si"

// SukebeiScraper lists torrents from sukebei.nyaa.si via its RSS feed.
type SukebeiScraper struct {
	client  *http.Client
	baseURL string
}

// NewSukebeiScraper creates a Sukebei scraper.
func NewSukebeiScraper(client *http.Client) *SukebeiScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &SukebeiScraper{
		client:  client,
		baseURL: sukebeiBaseURL,
	}
}

// Search searches Sukebei via RSS.
func (s *SukebeiScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	if page < 1 {
		page = 1
	}
	params := url.Values{
		"page": {"rss"},
		"f":    {"0"},
		"c":    {"0_0"},
		"p":    {strconv.Itoa(page)},
	}
	if q := strings.TrimSpace(query); q != "" {
		params.Set("q", q)
	}
	return s.fetchRSS(ctx, params, options.Sort)
}

// Browse lists Sukebei torrents. sort follows the addon convention: "7" = top
// seeders, "3" = recent (RSS publish order).
func (s *SukebeiScraper) Browse(ctx context.Context, category string, page int, sortCode string, options models.SearchOptions) ([]models.Torrent, error) {
	_ = category
	if page < 1 {
		page = 1
	}
	params := url.Values{
		"page": {"rss"},
		"f":    {"0"},
		"c":    {"0_0"},
		"p":    {strconv.Itoa(page)},
	}
	return s.fetchRSS(ctx, params, sortCode)
}

func (s *SukebeiScraper) fetchRSS(ctx context.Context, params url.Values, sortCode string) ([]models.Torrent, error) {
	reqURL := s.baseURL + "/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &ScraperError{Message: "failed to fetch RSS", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &ScraperError{Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &ScraperError{Message: "failed to read RSS body", Err: err}
	}

	torrents := parseSukebeiRSSBody(body)
	if sortCode == "7" {
		sort.Slice(torrents, func(i, j int) bool { return torrents[i].Seeders > torrents[j].Seeders })
	}
	return torrents, nil
}

func parseSukebeiRSSBody(body []byte) []models.Torrent {
	raw := string(body)
	items := sukebeiItemRE.FindAllStringSubmatch(raw, -1)
	torrents := make([]models.Torrent, 0, len(items))
	for _, match := range items {
		if len(match) < 2 {
			continue
		}
		block := match[1]
		item := sukebeiRSSItem{
			Title:    sukebeiFirstMatch(sukebeiTitleRE, block),
			Link:     sukebeiFirstMatch(sukebeiLinkRE, block),
			GUID:     sukebeiFirstMatch(sukebeiGuidRE, block),
			PubDate:  sukebeiFirstMatch(sukebeiPubDateRE, block),
			Size:     sukebeiFirstMatch(sukebeiSizeRE, block),
			Category: sukebeiFirstMatch(sukebeiCategoryRE, block),
		}
		if m := sukebeiSeedersRE.FindStringSubmatch(block); len(m) > 1 {
			item.Seeders, _ = strconv.Atoi(m[1])
		}
		if m := sukebeiLeechersRE.FindStringSubmatch(block); len(m) > 1 {
			item.Leechers, _ = strconv.Atoi(m[1])
		}
		if m := sukebeiInfoHashRE.FindStringSubmatch(block); len(m) > 1 {
			item.InfoHash = m[1]
		}
		if t := item.toTorrent(""); t != nil {
			torrents = append(torrents, *t)
		}
	}
	if len(torrents) > 0 {
		return torrents
	}

	// Fallback for feeds that omit nyaa: namespaced tags.
	var feed sukebeiRSSFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	for _, item := range feed.Channel.Items {
		if t := item.toTorrent(""); t != nil {
			torrents = append(torrents, *t)
		}
	}
	return torrents
}

func sukebeiFirstMatch(re *regexp.Regexp, block string) string {
	if m := re.FindStringSubmatch(block); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

type sukebeiRSSFeed struct {
	Channel sukebeiRSSChannel `xml:"channel"`
}

type sukebeiRSSChannel struct {
	Items []sukebeiRSSItem `xml:"item"`
}

type sukebeiRSSItem struct {
	Title    string `xml:"title"`
	Link     string `xml:"link"`
	GUID     string `xml:"guid"`
	PubDate  string `xml:"pubDate"`
	Seeders  int    `xml:"seeders"`
	Leechers int    `xml:"leechers"`
	InfoHash string `xml:"infoHash"`
	Size     string `xml:"size"`
	Category string `xml:"category"`
}

func (item sukebeiRSSItem) toTorrent(_ string) *models.Torrent {
	title := strings.TrimSpace(item.Title)
	infoHash := strings.ToLower(strings.TrimSpace(item.InfoHash))
	if title == "" || infoHash == "" {
		return nil
	}

	detailURL := strings.TrimSpace(item.GUID)
	if detailURL == "" {
		detailURL = strings.TrimSpace(item.Link)
	}
	torrentURL := strings.TrimSpace(item.Link)
	if torrentURL == "" && detailURL != "" {
		torrentURL = detailURL
	}

	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(title))

	return &models.Torrent{
		Name:       title,
		Size:       strings.TrimSpace(item.Size),
		Seeders:    item.Seeders,
		Leechers:   item.Leechers,
		MagnetLink: magnet,
		TorrentURL: torrentURL,
		UploadedBy: detailURL, // view page URL for StashDB resolution
		Website:    "sukebei",
		Category:   strings.TrimSpace(item.Category),
		Time:       strings.TrimSpace(item.PubDate),
	}
}

var (
	_ Scraper       = (*SukebeiScraper)(nil)
	_ BrowseScraper = (*SukebeiScraper)(nil)
)
