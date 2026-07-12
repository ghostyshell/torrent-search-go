package extractors

import (
	"context"
	"net/http"
)

type trafficimageExtractor struct{}

func (e *trafficimageExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	doc, err := fetchPage(ctx, client, imagePageURL)
	if err != nil {
		return "", err
	}

	if meta := metaImage(doc); meta != "" {
		if normalized := normalizeURL(meta, imagePageURL); isDirectImageURL(normalized) {
			return normalized, nil
		}
	}

	selectors := []string{
		"img#image",
		".image-container img",
		"#image-viewer img",
		"img.img-fluid",
		"img.main-image",
		"img[src*=\"trafficimage\"]",
	}
	for _, sel := range selectors {
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
