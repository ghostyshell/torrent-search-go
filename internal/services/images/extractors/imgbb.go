package extractors

import (
	"context"
	"net/http"
)

type imgbbExtractor struct{}

func (e *imgbbExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	doc, err := fetchPage(ctx, client, imagePageURL)
	if err != nil {
		return "", err
	}

	if meta := metaImage(doc); meta != "" {
		if normalized := normalizeURL(meta, imagePageURL); isDirectImageURL(normalized) {
			return normalized, nil
		}
	}

	for _, sel := range []string{"img.image", "#image", "img[src*=\"ibb.co\"]"} {
		if src, ok := doc.Find(sel).First().Attr("src"); ok {
			if normalized := normalizeURL(src, imagePageURL); isDirectImageURL(normalized) {
				return normalized, nil
			}
		}
	}

	if src := firstImageSrc(doc, imagePageURL); src != "" {
		return src, nil
	}

	return "", nil
}
