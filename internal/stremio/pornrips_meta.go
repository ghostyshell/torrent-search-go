package stremio

import (
	"strings"
	"time"

	"torrent-search-go/internal/services/jobs"
)

const (
	catalogLiveMetaConcurrency = 4
	catalogLiveMetaTimeout     = 22 * time.Second
	// ponytail: cap live TPDB/StashDB probes per catalog page to avoid 429 storms.
	catalogLiveMetaMaxPerPage = 24
)

func mergedHasPoster(m *jobs.SharedMeta) bool {
	return m != nil && strings.TrimSpace(m.Poster) != ""
}
