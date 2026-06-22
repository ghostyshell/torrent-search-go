package extractors

import (
	"context"
	"net/http"
	"strings"
)

type imgurExtractor struct{}

func (e *imgurExtractor) Extract(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	if strings.Contains(imagePageURL, "i.imgur.com") {
		return imagePageURL, nil
	}

	parts := strings.Split(strings.TrimRight(imagePageURL, "/"), "/")
	if len(parts) == 0 {
		return "", nil
	}
	imgurID := parts[len(parts)-1]
	if imgurID == "" {
		return "", nil
	}

	extensions := []string{"jpg", "jpeg", "png", "gif", "webp"}
	for _, ext := range extensions {
		testURL := "https://i.imgur.com/" + imgurID + "." + ext
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, testURL, nil)
		if err != nil {
			continue
		}
		setCommonHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return testURL, nil
		}
	}

	return "https://i.imgur.com/" + imgurID + ".jpg", nil
}
