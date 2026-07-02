package streams

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MatchesEpisode tests whether a torrent title matches the requested episode.
// It supports SxxExx, loose sxep, absolute numbering, and arc batch ranges.
func MatchesEpisode(title string, req Request, absEpisode int) bool {
	if req.Season == nil || req.Episode == nil {
		return true
	}
	s, e := *req.Season, *req.Episode

	// Explicit SxxExx.
	if regexp.MustCompile(fmt.Sprintf(`(?i)\bs0*%d\s*e0*%d\b`, s, e)).MatchString(title) {
		return true
	}
	// Loose sxep.
	if regexp.MustCompile(fmt.Sprintf(`(?i)\b%dx0*%d\b`, s, e)).MatchString(title) {
		return true
	}
	// Season pack.
	if regexp.MustCompile(fmt.Sprintf(`(?i)\bseason\s*0*%d\b`, s)).MatchString(title) &&
		regexp.MustCompile(`(?i)\bcomplete\b`).MatchString(title) {
		return true
	}

	// Absolute episode match.
	if absEpisode > 0 {
		pat := fmt.Sprintf(`(?i)\b(%d)\b`, absEpisode)
		if regexp.MustCompile(pat).MatchString(title) {
			return true
		}
	}

	// Arc batch: "Name 252-279" or "Name E252-279".
	if absEpisode > 0 {
		batchRE := regexp.MustCompile(`(?i)\b(?:e?p?)(\d+)[\s-]+(\d+)\b`)
		for _, m := range batchRE.FindAllStringSubmatch(title, -1) {
			start, _ := strconv.Atoi(m[1])
			end, _ := strconv.Atoi(m[2])
			if start <= absEpisode && end >= absEpisode {
				return true
			}
		}
	}

	return false
}

// TitleLikelySeriesPack guesses whether a title is a season/series pack.
func TitleLikelySeriesPack(title string) bool {
	lower := strings.ToLower(title)
	return strings.Contains(lower, "complete") ||
		strings.Contains(lower, "season pack") ||
		regexp.MustCompile(`(?i)\bs\d+\s*(complete|full)\b`).MatchString(title)
}
