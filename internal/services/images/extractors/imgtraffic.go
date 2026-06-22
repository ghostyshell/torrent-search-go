package extractors

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type imgtrafficExtractor struct{}

var imgtrafficDirectRE = regexp.MustCompile(`imgtraffic\.com/1/[^/]+/`)

func (e *imgtrafficExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	if imgtrafficDirectRE.MatchString(imagePageURL) && isDirectImageURL(imagePageURL) {
		return imagePageURL, nil
	}

	pageURL := imagePageURL
	if !strings.HasSuffix(imagePageURL, ".html") {
		if regexp.MustCompile(`/(i-1|z-1|1s)/`).MatchString(imagePageURL) {
			pageURL = imagePageURL + ".html"
		} else if isDirectImageURL(imagePageURL) {
			return imagePageURL, nil
		}
	}

	body, err := fetchBody(ctx, client, pageURL)
	if err != nil {
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", nil
	}

	if meta := metaImage(doc); meta != "" {
		if normalized := normalizeURL(meta, imagePageURL); isDirectImageURL(normalized) {
			return normalized, nil
		}
	}

	selectors := []string{
		"img[src*=\"imgtraffic.com/1/\"]",
		"img#image",
		"img.main-image",
		".image-container img",
		"#image-viewer img",
		"img[src*=\"imgtraffic.com\"]",
	}
	for _, sel := range selectors {
		if src, ok := doc.Find(sel).First().Attr("src"); ok {
			if normalized := normalizeURL(src, imagePageURL); isDirectImageURL(normalized) {
				return normalized, nil
			}
		}
	}

	if m := regexp.MustCompile(`https?://imgtraffic\.com/1/[^\s"'<>]+\.(jpg|jpeg|png|gif|webp)`).Find(body); m != nil {
		return string(m), nil
	}

	if strings.Contains(imagePageURL, "/i-1/") {
		fallback := strings.Replace(imagePageURL, "/i-1/", "/1/", 1)
		fallback = strings.TrimSuffix(fallback, ".html")
		if isDirectImageURL(fallback) {
			return fallback, nil
		}
	}

	return "", nil
}
