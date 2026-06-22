package extractors

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var imageExtRE = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|gif|webp)(\?|$)`)

// Extractor resolves an image-hosting viewer page to a direct image URL.
type Extractor interface {
	Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error)
}

type hostExtractor struct {
	host      string
	extractor Extractor
}

var hostExtractors = []hostExtractor{
	{host: "trafficimage.club", extractor: &trafficimageExtractor{}},
	{host: "imgtraffic.com", extractor: &imgtrafficExtractor{}},
	{host: "imgbb.com", extractor: &imgbbExtractor{}},
	{host: "postimg.cc", extractor: &postimgExtractor{}},
	{host: "imgur.com", extractor: &imgurExtractor{}},
	{host: "fastpic.org", extractor: &fastpicExtractor{}},
	{host: "fastpic.ru", extractor: &fastpicExtractor{}},
	{host: "xxxwebdlxxx.top", extractor: &xxxwebdlxxxExtractor{}},
	{host: "xxxwebdlxxx.org", extractor: &xxxwebdlxxxExtractor{}},
}

// Extract returns the direct image URL for a supported image hosting page.
// If the URL already appears to be a direct image link, or no extractor
// matches the host, the original URL is returned unchanged.
func Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	if isDirectImageURL(imagePageURL) {
		return imagePageURL, nil
	}

	u, err := url.Parse(imagePageURL)
	if err != nil {
		return imagePageURL, fmt.Errorf("invalid image page URL: %w", err)
	}
	host := strings.ToLower(u.Hostname())

	for _, he := range hostExtractors {
		if host == he.host || strings.HasSuffix(host, "."+he.host) {
			direct, err := he.extractor.Extract(ctx, client, imagePageURL)
			if err != nil {
				return imagePageURL, err
			}
			if direct == "" {
				return imagePageURL, nil
			}
			return direct, nil
		}
	}

	return imagePageURL, nil
}

func isDirectImageURL(u string) bool {
	return imageExtRE.MatchString(u)
}

func fetchPage(ctx context.Context, client *http.Client, url string) (*goquery.Document, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d fetching %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
	return doc, nil
}

func fetchBody(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d fetching %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

func setCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
}

func metaImage(doc *goquery.Document) string {
	for _, attr := range []string{"property", "name"} {
		if v, ok := doc.Find(fmt.Sprintf("meta[%s=\"og:image\"]", attr)).Attr("content"); ok && v != "" {
			return v
		}
	}
	for _, attr := range []string{"property", "name"} {
		if v, ok := doc.Find(fmt.Sprintf("meta[%s=\"twitter:image\"]", attr)).Attr("content"); ok && v != "" {
			return v
		}
	}
	return ""
}

func firstImageSrc(doc *goquery.Document, pageURL string) string {
	var found string
	doc.Find("img").EachWithBreak(func(_ int, img *goquery.Selection) bool {
		src, ok := img.Attr("src")
		if !ok {
			return true
		}
		normalized := normalizeURL(src, pageURL)
		if isDirectImageURL(normalized) {
			found = normalized
			return false
		}
		return true
	})
	return found
}

func normalizeURL(src, pageURL string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return src
	}
	if strings.HasPrefix(src, "//") {
		return "https:" + src
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return src
	}

	if strings.HasPrefix(src, "/") {
		base.Path = src
		base.RawQuery = ""
		return base.String()
	}

	rel, err := url.Parse(src)
	if err != nil {
		return src
	}
	return base.ResolveReference(rel).String()
}
