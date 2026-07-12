package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/models"
)

// PornripsScraper scrapes adult scene releases from PornRips.to.
type PornripsScraper struct {
	client  *http.Client
	baseURL string
}

const pornripsBaseURL = "https://pornrips.to"

// PornripsTorrentURL builds the /torrents/{name}.torrent fallback URL for a PRT
// release. The WP post title is the .torrent filename stem. Shared by the
// backfill sweep and the jstrmStreams lazy write-back so both store the same URL.
func PornripsTorrentURL(name string) string {
	if name == "" {
		return ""
	}
	return pornripsBaseURL + "/torrents/" + url.PathEscape(name) + ".torrent"
}

// NewPornripsScraper creates a new PornRips scraper.
func NewPornripsScraper(client *http.Client) *PornripsScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &PornripsScraper{
		client:  client,
		baseURL: pornripsBaseURL,
	}
}

// Search searches PornRips releases. Magnet/torrent links are resolved lazily
// from the post detail page at stream time - not during catalog listing.
func (s *PornripsScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	return s.fetchListings(ctx, query, page, options)
}

// Browse returns the latest PornRips releases. Category and sort are ignored;
// listings are always newest-first.
func (s *PornripsScraper) Browse(ctx context.Context, category string, page int, sort string, options models.SearchOptions) ([]models.Torrent, error) {
	return s.fetchListings(ctx, "", page, options)
}

func (s *PornripsScraper) fetchListings(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	searchURL := s.buildListURL(query, page)

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

	bodyText := doc.Find("section#primary").Text()
	if bodyText == "" {
		bodyText = doc.Find("body").Text()
	}
	if regexp.MustCompile(`(?i)Nothing Found`).MatchString(bodyText) {
		return []models.Torrent{}, nil
	}

	torrents := make([]models.Torrent, 0)
	doc.Find("#content article, section#primary article, .site-content article").Each(func(_ int, article *goquery.Selection) {
		entry := s.parseArticle(article)
		if entry.title == "" || entry.detailURL == "" {
			return
		}

		torrents = append(torrents, models.Torrent{
			Name:       entry.title,
			Size:       entry.size,
			UploadedBy: entry.detailURL,
			Website:    "pornrips",
		})
	})

	if options.MaxResults > 0 && len(torrents) > options.MaxResults {
		torrents = torrents[:options.MaxResults]
	}

	return torrents, nil
}

func (s *PornripsScraper) buildListURL(query string, page int) string {
	qs := ""
	if query != "" {
		qs = "?s=" + url.QueryEscape(query)
	}
	pagePath := ""
	if page > 1 {
		pagePath = fmt.Sprintf("/page/%d", page)
	}
	return fmt.Sprintf("%s%s/%s", s.baseURL, pagePath, qs)
}

type articleEntry struct {
	title     string
	detailURL string
	size      string
}

var pornripsSizeRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?\s*(?:GB|MB|GiB|MiB|TB))`)

func (s *PornripsScraper) parseArticle(article *goquery.Selection) articleEntry {
	entry := articleEntry{size: "Unknown"}

	titleLink := article.Find("header h2 a, h2.entry-title a, .entry-title a").First()
	entry.title = strings.TrimSpace(titleLink.Text())
	if href, ok := titleLink.Attr("href"); ok && href != "" {
		entry.detailURL = absoluteURL(href, s.baseURL)
	}

	metaText := article.Find(".wrapper-excerpt-content p, .entry-summary p, p").Text()
	if metaText == "" {
		metaText = article.Text()
	}
	if m := pornripsSizeRe.FindString(metaText); m != "" {
		entry.size = m
	}

	return entry
}

type downloadLinks struct {
	magnetLink string
	torrentURL string
}

func (s *PornripsScraper) fetchDownloadLinks(ctx context.Context, detailURL, referer string) (downloadLinks, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return downloadLinks{}, err
	}
	s.setHeaders(req)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return downloadLinks{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return downloadLinks{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return downloadLinks{}, err
	}

	var dl downloadLinks
	doc.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, _ := a.Attr("href")
		if strings.HasPrefix(href, "magnet:?xt=urn:btih:") {
			dl.magnetLink = href
			return false
		}
		if strings.HasSuffix(href, ".torrent") {
			dl.torrentURL = absoluteURL(href, detailURL)
		}
		return true
	})

	return dl, nil
}

// FetchTorrentData downloads a .torrent file using the scraper's own HTTP client
// and headers. torrentName is the PRT dotted release name (e.g.
// "Private.26.06.14.Asteria.Jade.XXX.1080p.HEVC.x265.PRT"); postURL is used as
// the Referer. It first tries to resolve the URL from the detail page; if the
// page uses JS-rendered links, it falls back to the standard
// pornrips.to/torrents/{name}.torrent pattern.
func (s *PornripsScraper) FetchTorrentData(ctx context.Context, postURL, torrentName string) ([]byte, error) {
	postURL = strings.TrimSpace(postURL)
	torrentName = strings.TrimSpace(torrentName)
	if postURL == "" && torrentName == "" {
		return nil, fmt.Errorf("empty URL and name")
	}

	torrentURL := ""
	if postURL != "" {
		if dl, err := s.fetchDownloadLinks(ctx, postURL, postURL); err == nil {
			torrentURL = dl.torrentURL
		}
	}
	if torrentURL == "" && torrentName != "" {
		torrentURL = s.baseURL + "/torrents/" + url.PathEscape(torrentName) + ".torrent"
	}
	if torrentURL == "" {
		return nil, fmt.Errorf("cannot determine torrent URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, err
	}
	s.setHeaders(req)
	if postURL != "" {
		req.Header.Set("Referer", postURL)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrent fetch status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512*1024))
}

// ResolveDownloadURL resolves a PornRips post URL to a magnet or .torrent link.
func (s *PornripsScraper) ResolveDownloadURL(ctx context.Context, postURL string) string {
	postURL = strings.TrimSpace(postURL)
	if postURL == "" {
		return ""
	}
	if strings.HasPrefix(postURL, "magnet:") || strings.Contains(strings.ToLower(postURL), ".torrent") {
		return postURL
	}
	dl, err := s.fetchDownloadLinks(ctx, postURL, postURL)
	if err != nil {
		return ""
	}
	if dl.magnetLink != "" {
		return dl.magnetLink
	}
	return dl.torrentURL
}

func (s *PornripsScraper) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "+
			"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept",
		"text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
}

// Interface compliance checks.
var (
	_ Scraper       = (*PornripsScraper)(nil)
	_ BrowseScraper = (*PornripsScraper)(nil)
)
