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
	"torrent-search-go/internal/services/images/extractors"
)

// HiddenBayScraper scrapes adult torrents from TheHiddenBay (a TPB mirror)
type HiddenBayScraper struct {
	client  *http.Client
	baseURL string
}

// HiddenBay adult category constants
const (
	HBCategoryAll   = "500" // All Porn
	HBCategoryMovie = "501" // Movies
	HBCategoryDVDR  = "502" // Movies DVDR
	HBCategoryHD    = "505" // HD-Movies
	HBCategoryUHD   = "507" // UHD/4K-Movies
	HBCategoryClips = "506" // Movie Clips
)

// NewHiddenBayScraper creates a new HiddenBay scraper.
// baseURL defaults to https://thehiddenbay.com if empty.
func NewHiddenBayScraper(client *http.Client, baseURL string) *HiddenBayScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://thehiddenbay.com"
	}
	return &HiddenBayScraper{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// Search searches torrents on HiddenBay.
// Category is read from options.Category; defaults to HBCategoryAll.
func (s *HiddenBayScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	cat := options.Category
	if cat == "" {
		cat = HBCategoryAll
	}
	// Sort code (TPB orderby): 7 = seeders desc, 3 = date desc. Default 7.
	sort := options.Sort
	if sort == "" {
		sort = "7"
	}

	// TPB-mirror search path: /search/{query}/{page}/{sort}/{category}
	searchURL := fmt.Sprintf("%s/search/%s/%d/%s/%s",
		s.baseURL, url.PathEscape(query), page, sort, cat)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}
	s.setHeaders(req)

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

	return s.parseTorrentList(doc), nil
}

// Browse browses torrents by category on HiddenBay.
// URL format: /browse/{category}/{page}/3  (3 = sort by seeders)
func (s *HiddenBayScraper) Browse(ctx context.Context, category string, page int, sort string, options models.SearchOptions) ([]models.Torrent, error) {
	if category == "" {
		category = HBCategoryAll
	}
	// Sort code: 3 = newest first, 7 = seeders desc. Default 3.
	if sort == "" {
		sort = "3"
	}

	browseURL := fmt.Sprintf("%s/browse/%s/%d/%s", s.baseURL, category, page, sort)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, browseURL, nil)
	if err != nil {
		return nil, &ScraperError{Message: "failed to create request", Err: err}
	}
	s.setHeaders(req)

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

	return s.parseTorrentList(doc), nil
}

// GetDetails fetches full torrent details from a HiddenBay detail page in the
// Node-compatible wire format fields (description, files, comments, images).
func (s *HiddenBayScraper) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	absURL := torrentURL
	if !strings.HasPrefix(torrentURL, "http") {
		absURL = s.baseURL + torrentURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, absURL, nil)
	if err != nil {
		return failedLegacyDetails(err.Error()), nil
	}
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return failedLegacyDetails(err.Error()), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return failedLegacyDetails(fmt.Sprintf("unexpected status code: %d", resp.StatusCode)), nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return failedLegacyDetails(err.Error()), nil
	}

	details := &models.TorrentDetails{
		Website:    "hiddenbay",
		TorrentURL: absURL,
		Files:      []models.File{},
		Comments:   []models.TorrentComment{},
		Images:     []models.TorrentImageLink{},
	}

	details.Name = strings.TrimSpace(doc.Find("h1#title, h1").First().Text())

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if details.MagnetLink != "" {
			return
		}
		href, _ := a.Attr("href")
		if strings.HasPrefix(href, "magnet:") {
			details.MagnetLink = href
		}
	})

	doc.Find("dt").Each(func(_ int, dt *goquery.Selection) {
		label := strings.ToLower(strings.TrimSpace(dt.Text()))
		value := strings.TrimSpace(dt.Next().Text())
		switch {
		case strings.Contains(label, "uploaded"):
			details.UploadedAt = value
		case strings.Contains(label, "size"):
			details.Size = value
		case strings.Contains(label, "by"):
			details.UploadedBy = value
		case strings.Contains(label, "seeders"):
			details.Seeders, _ = strconv.Atoi(value)
		case strings.Contains(label, "leechers"):
			details.Leechers, _ = strconv.Atoi(value)
		case strings.Contains(label, "category"):
			details.Category = value
		}
	})

	description := strings.TrimSpace(doc.Find("#details .nfo pre, .nfo pre, #description, .description").First().Text())
	if description == "" {
		description = "No description available"
	}
	details.Description = description

	for _, img := range extractors.ExtractImageLinks(ctx, s.client, description) {
		details.Images = append(details.Images, models.TorrentImageLink{
			OriginalURL: img.OriginalURL,
			DirectURL:   img.DirectURL,
		})
	}

	doc.Find("table.torrentFileList tr").Each(func(_ int, row *goquery.Selection) {
		fileName := strings.TrimSpace(row.Find("td").First().Text())
		fileSize := strings.TrimSpace(row.Find("td").Eq(1).Text())
		if fileName == "" || fileName == "File Name" {
			return
		}
		details.Files = append(details.Files, models.File{Name: fileName, Size: fileSize})
	})

	nfoText := doc.Find("div.nfo pre").First().Text()
	if nfoText != "" {
		if candidate := extractNFOImageURL(nfoText); candidate != "" {
			details.CoverImageURL = s.resolveImageURL(ctx, candidate)
		}
	}

	return details, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// reSizeUploaded / reULed clean the TPB "detDesc" metadata line.
var reSizeUploaded = regexp.MustCompile(`(?i)(Size|Uploaded)`)
var reULed = regexp.MustCompile(`(?i)ULed`)

// parseTorrentList parses a TPB-style #searchResult table into torrents.
func (s *HiddenBayScraper) parseTorrentList(doc *goquery.Document) []models.Torrent {
	torrents := make([]models.Torrent, 0)
	doc.Find("table#searchResult tr").Each(func(_ int, row *goquery.Selection) {
		nameLink := row.Find("a.detLink").First()
		name := strings.TrimSpace(nameLink.Text())
		if name == "" {
			return
		}

		t := models.Torrent{Name: name, Website: "hiddenbay"}

		if href, ok := nameLink.Attr("href"); ok && href != "" {
			if strings.HasPrefix(href, "http") {
				t.TorrentURL = href
			} else {
				t.TorrentURL = s.baseURL + href
			}
		}

		// Magnet: sibling <a> after div.detName; fall back to any magnet link.
		magnet, _ := row.Find("td div.detName").Next().Attr("href")
		if !strings.HasPrefix(magnet, "magnet:") {
			row.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
				if h, _ := a.Attr("href"); strings.HasPrefix(h, "magnet:") {
					magnet = h
					return false
				}
				return true
			})
		}
		t.MagnetLink = magnet

		// detDesc metadata line: "Uploaded <date>, ULed <size>, by <uploader>".
		desc := row.Find("font.detDesc").Text()
		desc = reSizeUploaded.ReplaceAllString(desc, "")
		desc = reULed.ReplaceAllString(desc, "Uploaded")
		parts := strings.Split(desc, ",")
		if len(parts) > 0 {
			t.Time = strings.TrimSpace(parts[0])
		}
		if len(parts) > 1 {
			t.Size = strings.TrimSpace(parts[1])
		}
		t.UploadedBy = strings.TrimSpace(row.Find("font.detDesc a").Text())

		t.Seeders, _ = strconv.Atoi(strings.TrimSpace(row.Find("td").Eq(2).Text()))
		t.Leechers, _ = strconv.Atoi(strings.TrimSpace(row.Find("td").Eq(3).Text()))
		t.Category = strings.TrimSpace(row.Find("td.vertTh center a").First().Text())

		torrents = append(torrents, t)
	})
	return torrents
}

// reImageExt matches a URL ending in a recognised image extension.
var reImageExt = regexp.MustCompile(`(?i)https?://[^\s\n"<>]+?\.(?:jpg|jpeg|png|webp)(?:\?[^\s\n"<>]*)?`)

// reAnyURL matches any http(s) URL.
var reAnyURL = regexp.MustCompile(`(?i)https?://[^\s\n"<>]+`)

// reMdThumb matches a ".md" thumbnail marker before an image extension.
var reMdThumb = regexp.MustCompile(`(?i)\.md(\.(?:jpg|jpeg|png|webp))`)

// nonImageHostRe matches NFO URLs that are never the cover image (site chrome,
// trackers, forums, donations, proxies).
var nonImageHostRe = regexp.MustCompile(
	`(?i)(?:thehiddenbay|thepiratebay|piratebay|pirates?-?forum|bitcoin\.org|surferprotector|proxylist|proxy\.info|\.onion)`,
)

// extractNFOImageURL finds the first image (or image-viewer) URL embedded as
// plain text in an NFO block. Image hosts change over time, so instead of a
// fixed allow-list we take the first http(s) URL that isn't obvious site
// chrome; resolveImageURL turns a viewer page into a direct image.
func extractNFOImageURL(text string) string {
	// Priority 1: a URL that already ends in an image extension.
	if m := reImageExt.FindString(text); m != "" && !nonImageHostRe.MatchString(m) {
		return m
	}
	// Priority 2: the first non-chrome URL (usually an image-viewer page).
	for _, u := range reAnyURL.FindAllString(text, -1) {
		if !nonImageHostRe.MatchString(u) {
			return u
		}
	}
	return ""
}

// resolveImageURL turns an NFO image candidate into a direct image URL. If it
// already points at an image file it's returned (full-res, stripping any ".md"
// thumbnail marker); otherwise the viewer page is fetched and og:image / the
// first plausible <img> is extracted.
func (s *HiddenBayScraper) resolveImageURL(ctx context.Context, candidate string) string {
	if candidate == "" {
		return ""
	}
	if reImageExt.MatchString(candidate) {
		return reMdThumb.ReplaceAllString(candidate, "$1")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
	if err != nil {
		return ""
	}
	s.setHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return ""
	}

	// og:image / twitter:image is the most reliable signal on image hosts.
	for _, sel := range []string{`meta[property="og:image"]`, `meta[name="twitter:image"]`} {
		if c, ok := doc.Find(sel).First().Attr("content"); ok && c != "" {
			return reMdThumb.ReplaceAllString(absoluteURL(c, candidate), "$1")
		}
	}

	// Otherwise the first plausible <img> that looks like a real image.
	var found string
	doc.Find("img").EachWithBreak(func(_ int, img *goquery.Selection) bool {
		src, _ := img.Attr("src")
		if src == "" {
			src, _ = img.Attr("data-src")
		}
		if src == "" || strings.Contains(strings.ToLower(src), "logo") ||
			strings.Contains(strings.ToLower(src), "favicon") {
			return true
		}
		if reImageExt.MatchString(src) {
			found = reMdThumb.ReplaceAllString(absoluteURL(src, candidate), "$1")
			return false
		}
		return true
	})
	return found
}

// absoluteURL resolves a possibly-relative URL against a base.
func absoluteURL(ref, base string) string {
	if strings.HasPrefix(ref, "http") {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// setHeaders applies common browser-like request headers.
func (s *HiddenBayScraper) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "+
			"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept",
		"text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
}

// Interface compliance checks
var (
	_ Scraper        = (*HiddenBayScraper)(nil)
	_ BrowseScraper  = (*HiddenBayScraper)(nil)
	_ DetailsScraper = (*HiddenBayScraper)(nil)
)
