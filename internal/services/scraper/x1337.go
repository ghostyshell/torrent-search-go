package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/flaresolverr"
	"torrent-search-go/internal/services/images/extractors"
)

const (
	flareSolverrClientTimeout = 90 * time.Second
	flareSolverrMaxTimeoutMs  = 60000
)

// X1337Scraper scrapes torrents from 1337x
type X1337Scraper struct {
	client                   *http.Client
	flareClient              *http.Client
	baseURL                  string
	flareSolverrURL          string
	flareSolverrConfigured   bool
}

// FlareSolverr request/response
type FlareSolverrRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout,omitempty"`
}

type FlareSolverrResponse struct {
	Status   string               `json:"status"`
	Message  string               `json:"message"`
	Solution FlareSolverrSolution `json:"solution"`
}

type FlareSolverrSolution struct {
	URL       string               `json:"url"`
	Status    int                  `json:"status"`
	Headers   map[string]string    `json:"headers"`
	Response  string               `json:"response"`
	Cookies   []FlareSolverrCookie `json:"cookies"`
	UserAgent string               `json:"userAgent"`
}

type FlareSolverrCookie struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Domain  string `json:"domain"`
	Path    string `json:"path"`
	Expires int64  `json:"expirationDate,omitempty"`
}

// NewX1337Scraper creates a new 1337x scraper
func NewX1337Scraper(client *http.Client, flareSolverrURL string) *X1337Scraper {
	configured := flareSolverrURL != ""
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if flareSolverrURL == "" {
		flareSolverrURL = "http://localhost:8191"
	}
	return &X1337Scraper{
		client: client,
		flareClient: &http.Client{
			Timeout:   flareSolverrClientTimeout,
			Transport: transport,
		},
		baseURL:                "https://1337x.to",
		flareSolverrURL:        flareSolverrURL,
		flareSolverrConfigured: configured,
	}
}

// Search searches torrents on 1337x
func (s *X1337Scraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	searchURL := fmt.Sprintf("%s/search/%s/%d/", s.baseURL, url.PathEscape(query), page)

	html, err := s.fetchHTML(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	torrents := s.parseSearchHTML(html)
	if len(torrents) == 0 && !strings.Contains(html, "table-list") {
		return nil, &ScraperError{Message: "1337x response missing results table"}
	}

	return torrents, nil
}

func (s *X1337Scraper) parseSearchHTML(html string) []models.Torrent {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return nil
	}

	torrents := make([]models.Torrent, 0)
	doc.Find("table.table-list tbody tr").Each(func(_ int, row *goquery.Selection) {
		torrent := s.parseTorrentRow(row)
		if torrent != nil && torrent.Name != "" {
			torrents = append(torrents, *torrent)
		}
	})
	return torrents
}

// fetchHTML loads a 1337x page via FlareSolverr when configured; direct only for local dev.
func (s *X1337Scraper) fetchHTML(ctx context.Context, targetURL string) (string, error) {
	html, err := s.getWithFlareSolverr(ctx, targetURL)
	if err == nil {
		return html, nil
	}
	if s.flareSolverrConfigured {
		return "", err
	}
	return s.getDirect(ctx, targetURL)
}

// getWithFlareSolverr uses FlareSolverr to bypass Cloudflare protection
func (s *X1337Scraper) getWithFlareSolverr(ctx context.Context, targetURL string) (string, error) {
	if err := flaresolverr.Acquire(ctx); err != nil {
		return "", &ScraperError{Message: "FlareSolverr slot unavailable", Err: err}
	}
	defer flaresolverr.Release()
	reqBody := FlareSolverrRequest{
		Cmd:        "request.get",
		URL:        targetURL,
		MaxTimeout: flareSolverrMaxTimeoutMs,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", &ScraperError{Message: "failed to marshal FlareSolverr request", Err: err}
	}

	fsReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.flareSolverrURL+"/v1", bytes.NewReader(jsonBody))
	if err != nil {
		return "", &ScraperError{Message: "failed to create FlareSolverr request", Err: err}
	}

	fsReq.Header.Set("Content-Type", "application/json")
	fsReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.flareClient.Do(fsReq)
	if err != nil {
		return "", &ScraperError{Message: "FlareSolverr request failed", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &ScraperError{Message: fmt.Sprintf("FlareSolverr returned status %d", resp.StatusCode)}
	}

	var fsResp FlareSolverrResponse
	if err := json.NewDecoder(resp.Body).Decode(&fsResp); err != nil {
		return "", &ScraperError{Message: "failed to parse FlareSolverr response", Err: err}
	}

	if fsResp.Status != "ok" {
		return "", &ScraperError{Message: fmt.Sprintf("FlareSolverr error: %s", fsResp.Message)}
	}
	if fsResp.Solution.Status != 0 && fsResp.Solution.Status != http.StatusOK {
		return "", &ScraperError{Message: fmt.Sprintf("1337x returned HTTP %d via FlareSolverr", fsResp.Solution.Status)}
	}

	return fsResp.Solution.Response, nil
}

// getDirect makes a direct HTTP request without FlareSolverr
func (s *X1337Scraper) getDirect(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", &ScraperError{Message: "failed to create request", Err: err}
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", &ScraperError{Message: "failed to fetch search results", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &ScraperError{Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode)}
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", &ScraperError{Message: "failed to read response body", Err: err}
	}

	return buf.String(), nil
}

// parseTorrentRow parses a torrent row from 1337x HTML
func (s *X1337Scraper) parseTorrentRow(row *goquery.Selection) *models.Torrent {
	torrent := &models.Torrent{
		Website: "1337x",
	}

	// First <a> is the category icon; the torrent title is the next link.
	nameCell := row.Find("td.coll-1").First()
	nameLink := nameCell.Find("a[href*='/torrent/']").First()
	if nameLink.Length() == 0 {
		links := nameCell.Find("a")
		if links.Length() >= 2 {
			nameLink = links.Eq(1)
		} else if links.Length() == 1 {
			nameLink = links.First()
		}
	}
	torrent.Name = strings.TrimSpace(nameLink.Text())

	// Get torrent URL
	if href, exists := nameLink.Attr("href"); exists {
		if !strings.HasPrefix(href, "http") {
			torrent.TorrentURL = s.baseURL + href
		} else {
			torrent.TorrentURL = href
		}
	}

	// Get seeders / leechers (coll-* or newer class names)
	if seedersText := strings.TrimSpace(row.Find("td.coll-2, td.seeds").First().Text()); seedersText != "" {
		torrent.Seeders, _ = strconv.Atoi(seedersText)
	}
	if leechersText := strings.TrimSpace(row.Find("td.coll-3, td.leeches").First().Text()); leechersText != "" {
		torrent.Leechers, _ = strconv.Atoi(leechersText)
	}

	// Get size from td.coll-4 or td.size
	sizeCell := row.Find("td.coll-4, td.size").First()
	if sizeCell.Length() > 0 {
		torrent.Size = strings.TrimSpace(sizeCell.Text())
	}

	// Get upload time from td.coll-date (not coll-5)
	torrent.Time = strings.TrimSpace(row.Find("td.coll-date").First().Text())

	// Get uploader from td.coll-5
	uploader := strings.TrimSpace(row.Find("td.coll-5 a").First().Text())
	if uploader != "" {
		torrent.UploadedBy = uploader
	}

	// Get category from td.coll-1 span
	category := strings.TrimSpace(row.Find("td.coll-1 span").Text())
	if category != "" {
		torrent.Category = category
	}

	// Get magnet link from the torrent page (requires additional request)
	// For now, we'll construct it from the URL
	if torrent.TorrentURL != "" {
		// Magnet link will be fetched in GetDetails
	}

	// Validate minimum required data
	if torrent.Name == "" {
		return nil
	}

	return torrent
}

// GetDetails gets detailed information about a 1337x torrent in the Node wire format.
func (s *X1337Scraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	html, err := s.fetchHTML(ctx, torrentURL)
	if err != nil {
		return failedLegacyDetails(err.Error()), nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader([]byte(html)))
	if err != nil {
		return failedLegacyDetails(err.Error()), nil
	}

	details := &models.TorrentDetails{
		Website:    "1337x",
		TorrentURL: torrentURL,
		Files:      []models.File{},
		Comments:   []models.TorrentComment{},
		Images:     []models.TorrentImageLink{},
	}

	if descDiv := doc.Find(".torrent-description"); descDiv.Length() > 0 {
		details.Description = strings.TrimSpace(descDiv.Text())
	}
	if details.Description == "" {
		details.Description = "No description available"
	}

	doc.Find("a").Each(func(_ int, sel *goquery.Selection) {
		if details.MagnetLink != "" {
			return
		}
		if href, ok := sel.Attr("href"); ok && strings.HasPrefix(href, "magnet:") {
			details.MagnetLink = href
		}
	})
	details.InfoHash = extractBTIH(details.MagnetLink)

	for _, img := range extractors.ExtractImageLinks(ctx, s.client, details.Description) {
		details.Images = append(details.Images, models.TorrentImageLink{
			OriginalURL: img.OriginalURL,
			DirectURL:   img.DirectURL,
		})
	}

	doc.Find(".torrent-file-list ul li").Each(func(_ int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if text == "" {
			return
		}
		parts := strings.SplitN(text, " (", 2)
		name := strings.TrimSpace(parts[0])
		size := ""
		if len(parts) == 2 {
			size = strings.TrimSuffix(strings.TrimSpace(parts[1]), ")")
		}
		details.Files = append(details.Files, models.File{Name: name, Size: size})
	})

	if len(details.Files) == 0 {
		doc.Find(".torrent-file-list-table tr").Each(func(i int, sel *goquery.Selection) {
			if i == 0 {
				return
			}
			fileName := strings.TrimSpace(sel.Find("td").Eq(0).Text())
			fileSize := strings.TrimSpace(sel.Find("td").Eq(1).Text())
			if fileName != "" {
				details.Files = append(details.Files, models.File{Name: fileName, Size: fileSize})
			}
		})
	}

	return details, nil
}

func extractBTIH(magnet string) string {
	const marker = "btih:"
	lower := strings.ToLower(magnet)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	hash := magnet[idx+len(marker):]
	if amp := strings.Index(hash, "&"); amp >= 0 {
		hash = hash[:amp]
	}
	return strings.ToLower(strings.TrimSpace(hash))
}

// Diagnose runs diagnostic checks for 1337x scraper
func (s *X1337Scraper) Diagnose(ctx context.Context) map[string]interface{} {
	result := map[string]interface{}{
		"scraper":   "1337x",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"tests":     make(map[string]interface{}),
	}

	// Test FlareSolverr connectivity
	fsTest := s.testFlareSolverr(ctx)
	result["tests"].(map[string]interface{})["flaresolverr"] = fsTest

	// Test direct scraping
	directTest := s.testDirect(ctx)
	result["tests"].(map[string]interface{})["direct"] = directTest

	return result
}

func (s *X1337Scraper) testFlareSolverr(ctx context.Context) map[string]interface{} {
	testURL := s.baseURL + "/search/test/1/"

	_, err := s.getWithFlareSolverr(ctx, testURL)

	return map[string]interface{}{
		"success": err == nil,
		"message": func() string {
			if err == nil {
				return "FlareSolverr connectivity OK"
			}
			return "FlareSolverr error: " + err.Error()
		}(),
	}
}

func (s *X1337Scraper) testDirect(ctx context.Context) map[string]interface{} {
	testURL := s.baseURL + "/search/test/1/"

	_, err := s.getDirect(ctx, testURL)

	return map[string]interface{}{
		"success": err == nil,
		"message": func() string {
			if err == nil {
				return "Direct scraping OK"
			}
			return "Direct scraping error: " + err.Error()
		}(),
	}
}

// Ensure X1337Scraper implements the interfaces
var (
	_ Scraper        = (*X1337Scraper)(nil)
	_ DetailsScraper = (*X1337Scraper)(nil)
)
