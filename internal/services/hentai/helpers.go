package hentai

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// helpers.go holds the small shared utilities the HentaiMama scraper uses for
// slug/number parsing, dedup, rating parse, and stream quality detect.

var (
	// episodeSlugNumRe pulls the episode number off a HentaiMama episode slug
	// like "series-name-episode-3" -> 3.
	episodeSlugNumRe = regexp.MustCompile(`-episode-(\d+)$`)
	// episodeTextNumRe pulls the number out of link text like "Episode 3" / "EP 3".
	episodeTextNumRe = regexp.MustCompile(`(?i)(?:episode|ep\.?)\s*(\d+)`)
	// qualityRe detects a quality token in a stream URL/path.
	qualityRe = regexp.MustCompile(`(?i)\b(4k|2160p|1440p|1080p|720p|480p|360p)\b`)
)

// parseRating parses a 0-10 rating string ("8.5", "8.5/10", "8,5"). Returns 0
// on parse failure.
func parseRating(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if i := strings.IndexAny(s, "/ "); i > 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if f < 0 {
		return 0
	}
	if f > 10 {
		// some sites emit 0-100; normalize to 0-10
		if f <= 100 {
			return f / 10
		}
		return 0
	}
	return f
}

// collapseSpaces trims and collapses runs of whitespace (newlines, tabs,
// repeated spaces) into single spaces - for scraped link/heading text that
// spans formatted HTML with indentation.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// dedupStrings returns s with duplicates removed, order preserved.
func dedupStrings(s []string) []string {
	if len(s) < 2 {
		return s
	}
	seen := make(map[string]struct{}, len(s))
	out := s[:0]
	for _, v := range s {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// episodeNumFromSlug returns the episode number encoded in a slug like
// "series-name-episode-3", or 0 if none.
func episodeNumFromSlug(slug string) int {
	if m := episodeSlugNumRe.FindStringSubmatch(slug); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// parseEpisodeNumber extracts an episode number from human text ("Episode 3").
func parseEpisodeNumber(text string) int {
	if m := episodeTextNumRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// sortEpisodes sorts episodes by Number ascending (0s last).
func sortEpisodes(eps []EpisodeInfo) {
	sort.SliceStable(eps, func(i, j int) bool {
		if eps[i].Number == 0 || eps[j].Number == 0 {
			return eps[i].Number != 0 && eps[j].Number == 0
		}
		return eps[i].Number < eps[j].Number
	})
}

// detectQuality returns a quality label parsed from a stream URL, or "".
func detectQuality(url string) string {
	if m := qualityRe.FindStringSubmatch(url); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// dedupStreams removes duplicate URLs and sorts highest-quality-first.
func dedupStreams(s []EpisodeStream) []EpisodeStream {
	if len(s) < 2 {
		return s
	}
	seen := make(map[string]struct{}, len(s))
	out := make([]EpisodeStream, 0, len(s))
	for _, st := range s {
		if st.URL == "" {
			continue
		}
		if _, ok := seen[st.URL]; ok {
			continue
		}
		seen[st.URL] = struct{}{}
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return qualityRank(out[i].Quality) > qualityRank(out[j].Quality)
	})
	return out
}

// qualityRank orders quality labels for sorting (higher = better).
func qualityRank(q string) int {
	switch strings.ToUpper(q) {
	case "4K", "2160P":
		return 50
	case "1440P":
		return 40
	case "1080P":
		return 30
	case "720P":
		return 20
	case "480P":
		return 10
	case "360P":
		return 5
	}
	return 0
}