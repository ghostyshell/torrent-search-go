package extractors

import (
	"context"
	"net/http"
)

type postimgExtractor struct{}

func (e *postimgExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	doc, err := fetchPage(ctx, client, imagePageURL)
	if err != nil {
		return "", err
	}

	if meta := metaImage(doc); meta != "" {
		if normalized := normalizeURL(meta, imagePageURL); isDirectImageURL(normalized) {
			return normalized, nil
		}
	}

	for _, sel := range []string{"#main-image", "img.imagefield", "img[src*=\"postimg.cc\"]"} {
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
