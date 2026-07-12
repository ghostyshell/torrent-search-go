package jobs

import (
	"regexp"
	"strconv"
	"strings"

	"torrent-search-go/internal/models"
)

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

const scraperWebsite = "piratebay"

// TorrentKey builds the cover-cache key (mirrors Node generateTorrentKey).
func TorrentKey(t models.Torrent) string {
	source := t.Website
	if source == "" {
		source = scraperWebsite
	}
	key := strings.ToLower(nonAlnum.ReplaceAllString(t.Name+"_"+source+"_"+t.Size, "_"))
	if len(key) > 200 {
		key = key[:150] + "_" + simpleHash(key)
	}
	return key
}

func simpleHash(s string) string {
	var hash int32
	for _, c := range s {
		hash = (hash << 5) - hash + int32(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return strconv.FormatInt(int64(hash), 36)
}
