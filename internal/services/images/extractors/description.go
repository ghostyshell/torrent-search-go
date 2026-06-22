package extractors

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// ImageLink is the wire shape returned by torrent detail scrapers.
type ImageLink struct {
	OriginalURL string `json:"originalUrl"`
	DirectURL   string `json:"directUrl"`
}

var descriptionImagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://(?:img\.)?trafficimage\.club/image/[a-zA-Z0-9]+`),
	regexp.MustCompile(`(?i)https?://imgtraffic\.com/[a-zA-Z0-9\-/]+\.(?:jpg|jpeg|png|gif|webp)\.html`),
	regexp.MustCompile(`(?i)https?://imgtraffic\.com/[a-zA-Z0-9\-/]+\.(?:jpg|jpeg|png|gif|webp)(?:\?[^\s]*)?`),
	regexp.MustCompile(`(?i)https?://imgbb\.com/[a-zA-Z0-9]+`),
	regexp.MustCompile(`(?i)https?://postimg\.cc/[a-zA-Z0-9]+`),
	regexp.MustCompile(`(?i)https?://imgur\.com/[a-zA-Z0-9]+`),
	regexp.MustCompile(`(?i)https?://i\.imgur\.com/[a-zA-Z0-9]+\.(?:jpg|jpeg|png|gif|webp)`),
	regexp.MustCompile(`(?i)https?://i\.postimg\.cc/[a-zA-Z0-9]+/[^.\s]+\.(?:jpg|jpeg|png|gif|webp)`),
	regexp.MustCompile(`(?i)https?://fastpic\.org/view/\d+/\d{4}/\d{4}/_[a-zA-Z0-9]+\.(?:jpg|jpeg|png|gif|webp)\.html`),
	regexp.MustCompile(`(?i)https?://i\d+\.fastpic\.org/[^.\s]+\.(?:jpg|jpeg|png|gif|webp)(?:\?[^\s]*)?`),
	regexp.MustCompile(`(?i)https?://xxxwebdlxxx\.(?:top|org)/img-[a-zA-Z0-9]+\.html`),
	regexp.MustCompile(`(?i)https?://[^.\s]+\.(?:jpg|jpeg|png|gif|webp)(?:\?[^\s]*)?`),
}

// ExtractImageLinks finds image-hosting URLs in a torrent description and resolves
// them to direct image URLs, mirroring the Node imageExtractorService.
func ExtractImageLinks(ctx context.Context, client *http.Client, description string) []ImageLink {
	if strings.TrimSpace(description) == "" {
		return []ImageLink{}
	}

	found := make([]string, 0)
	seen := make(map[string]struct{})
	for _, pattern := range descriptionImagePatterns {
		for _, match := range pattern.FindAllString(description, -1) {
			match = strings.TrimSpace(match)
			if match == "" {
				continue
			}
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			found = append(found, match)
		}
	}

	links := make([]ImageLink, 0, len(found))
	seenDirect := make(map[string]struct{})
	for _, original := range found {
		if isDirectImageURL(original) {
			if _, ok := seenDirect[original]; !ok {
				seenDirect[original] = struct{}{}
				links = append(links, ImageLink{OriginalURL: original, DirectURL: original})
			}
			continue
		}

		direct, err := Extract(ctx, client, original)
		if err != nil || direct == "" {
			continue
		}
		if _, ok := seenDirect[direct]; ok {
			continue
		}
		seenDirect[direct] = struct{}{}
		links = append(links, ImageLink{OriginalURL: original, DirectURL: direct})
	}

	if links == nil {
		return []ImageLink{}
	}
	return links
}
