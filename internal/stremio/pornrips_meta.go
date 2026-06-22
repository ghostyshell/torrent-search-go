package stremio

import (
	"strings"
	"time"

	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
)

const (
	catalogLiveMetaConcurrency = 4
	catalogLiveMetaTimeout     = 22 * time.Second
	// ponytail: cap live TPDB/StashDB probes per catalog page to avoid 429 storms.
	catalogLiveMetaMaxPerPage = 24
)

// metadataTitlesForLookup returns title variants to probe TPDB/StashDB with.
func metadataTitlesForLookup(title string) []string {
	seen := make(map[string]string)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = s
	}

	add(title)
	if strings.Contains(title, ".") {
		add(strings.ReplaceAll(title, ".", " "))
	}
	parsed := metadata.ParseRelease(title)
	add(parsed.CleanQuery)
	if parsed.Performer != "" && parsed.Scene != "" {
		perf := metadata.PrimaryPerformer(parsed.Performer)
		add(perf + " " + parsed.Scene)
		add(parsed.Studio + " " + perf + " " + parsed.Scene)
		for _, probe := range metadata.OnlyFansCoStarProbes(parsed.Performer, parsed.Scene) {
			add(probe)
		}
	}
	if parsed.Studio != "" && parsed.Scene != "" {
		add(parsed.Studio + " " + parsed.Scene)
		if expanded := metadata.ExpandStudioToken(parsed.Studio); expanded != parsed.Studio {
			add(expanded + " " + parsed.Scene)
		}
	}
	if strings.Contains(parsed.Studio, " ") {
		parts := strings.Fields(parsed.Studio)
		if tail := parts[len(parts)-1]; tail != "" && parsed.Scene != "" {
			add(tail + " " + parsed.Scene)
			if expanded := metadata.ExpandStudioToken(tail); expanded != tail {
				add(expanded + " " + parsed.Scene)
			}
		}
	}
	if parsed.Scene != "" {
		if parsed.Performer == "" {
			add(parsed.Scene)
		}
	}

	out := make([]string, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	return out
}

func mergedHasPoster(m *jobs.SharedMeta) bool {
	return m != nil && strings.TrimSpace(m.Poster) != ""
}
