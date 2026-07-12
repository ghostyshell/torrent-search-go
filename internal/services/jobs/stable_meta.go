package jobs

import (
	"regexp"
	"strings"
)

var pornripsSlugRE = regexp.MustCompile(`(?i)pornrips\.to/([^/?#]+)`)

// StableMetaID returns the cross-install metadata cache key for a torrent record.
func StableMetaID(website, detailURL, infoHash string) string {
	if website == "pornrips" {
		if m := pornripsSlugRE.FindStringSubmatch(detailURL); len(m) > 1 {
			return "pr:" + m[1]
		}
		return ""
	}
	return strings.ToLower(infoHash)
}

// MetaEnqueueItem is a pending metadata lookup request.
type MetaEnqueueItem struct {
	Title      string `json:"title"`
	DetailURL  string `json:"detailUrl"`
	Website    string `json:"website"`
	InfoHash   string `json:"infoHash"`
	Name       string `json:"name"`
	T            string `json:"t"`
	D            string `json:"d"`
	W            string `json:"w"`
	H            string `json:"h"`
	Priority    bool   `json:"priority"`
}

// NormalizeMetaEnqueueItem extracts fields from a flexible wire record.
func NormalizeMetaEnqueueItem(it MetaEnqueueItem) (id, title, detailURL, website string) {
	title = it.Title
	if title == "" {
		title = it.Name
	}
	if title == "" {
		title = it.T
	}
	detailURL = it.DetailURL
	if detailURL == "" {
		detailURL = it.D
	}
	website = it.Website
	if website == "" {
		website = it.W
	}
	infoHash := it.InfoHash
	if infoHash == "" {
		infoHash = it.H
	}
	id = StableMetaID(website, detailURL, infoHash)
	return id, title, detailURL, website
}
