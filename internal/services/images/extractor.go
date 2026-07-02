package images

import (
	"context"
	"net/http"

	"torrent-search-go/internal/services/images/extractors"
)

// ExtractDirectImageURL returns the direct image URL for a supported image
// hosting page. If the URL already appears to be a direct image link, or no
// extractor matches the host, the original URL is returned unchanged.
func ExtractDirectImageURL(ctx context.Context, client *http.Client, imagePageURL string) (string, error) {
	return extractors.Extract(ctx, client, imagePageURL)
}
