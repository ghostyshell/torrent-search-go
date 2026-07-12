package extractors

import (
	"context"
	"net/http"
	"strconv"

	"github.com/PuerkitoBio/goquery"
)

type xxxwebdlxxxExtractor struct{}

func (e *xxxwebdlxxxExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imagePageURL, nil)
	if err != nil {
		return "", err
	}
	setCommonHeaders(req)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", nil
	}

	if meta := metaImage(doc); meta != "" {
		if normalized := normalizeURL(meta, imagePageURL); isDirectImageURL(normalized) {
			return normalized, nil
		}
	}

	selectors := []string{
		"img#img",
		"img.image",
		"img[src*=\".jpg\"]",
		"img[src*=\".jpeg\"]",
		"img[src*=\".png\"]",
		"img[src*=\".gif\"]",
		"img[src*=\".webp\"]",
		".image img",
		"#image img",
		"img.main-image",
		"img[alt*=\"image\"]",
		"img[title*=\"image\"]",
	}
	for _, sel := range selectors {
		if src, ok := doc.Find(sel).First().Attr("src"); ok {
			if normalized := normalizeURL(src, imagePageURL); isDirectImageURL(normalized) {
				return normalized, nil
			}
		}
	}

	var best string
	doc.Find("img").EachWithBreak(func(_ int, img *goquery.Selection) bool {
		src, ok := img.Attr("src")
		if !ok {
			return true
		}
		normalized := normalizeURL(src, imagePageURL)
		if !isDirectImageURL(normalized) {
			return true
		}

		wStr, _ := img.Attr("width")
		hStr, _ := img.Attr("height")
		w, _ := strconv.Atoi(wStr)
		h, _ := strconv.Atoi(hStr)
		if (wStr == "" || hStr == "") || (w > 100 && h > 100) {
			best = normalized
			return false
		}
		return true
	})

	return best, nil
}
