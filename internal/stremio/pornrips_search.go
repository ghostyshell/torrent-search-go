package stremio

import (
	"context"
	"regexp"
	"strings"
)

const (
	pornripsSearchMaxPages = 12
	pornripsSearchPageSize = 30
)

var pornripsSearchStopwords = map[string]struct{}{
	"xxx": {}, "and": {}, "the": {}, "prt": {}, "hevc": {}, "x265": {}, "x264": {},
	"720p": {}, "1080p": {}, "2160p": {}, "4k": {}, "uhd": {},
}

// pornripsSearchTokens extracts match tokens from a user search query.
func pornripsSearchTokens(query string) []string {
	norm := pornripsNormalizeTitle(query)
	parts := strings.Fields(norm)
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, w := range parts {
		if len(w) < 2 {
			continue
		}
		if _, stop := pornripsSearchStopwords[w]; stop {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

func pornripsNormalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	s = pornripsQualityRE.ReplaceAllString(s, "")
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// pornripsMatchesSearch reports whether a listing matches enough query tokens.
func pornripsMatchesSearch(t catalogTorrent, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	hay := pornripsNormalizeTitle(t.Title)
	if hay == "" {
		hay = pornripsNormalizeTitle(PornripsSlug(t.DetailURL))
	}
	if hay == "" {
		return false
	}
	matched := 0
	for _, tok := range tokens {
		if strings.Contains(hay, tok) {
			matched++
		}
	}
	minMatch := len(tokens) * 7 / 10
	if minMatch < 2 {
		minMatch = 2
	}
	if minMatch > len(tokens) {
		minMatch = len(tokens)
	}
	return matched >= minMatch
}

// fetchPornripsBrowseSearch scans browse pages (not Cloudflare-blocked ?s= search)
// and returns releases whose titles match the query tokens.
func (h *Handler) fetchPornripsBrowseSearch(ctx context.Context, query string, skip, maxResults int) []catalogTorrent {
	tokens := pornripsSearchTokens(query)
	if len(tokens) == 0 {
		return nil
	}
	if maxResults <= 0 {
		maxResults = 20
	}
	need := skip + maxResults
	matches := make([]catalogTorrent, 0, need)
	seen := make(map[string]struct{})

	for page := 1; page <= pornripsSearchMaxPages; page++ {
		pageSkip := (page - 1) * pornripsSearchPageSize
		batch := h.fetchPornripsBrowse(ctx, pageSkip)
		if len(batch) == 0 {
			break
		}
		for _, t := range batch {
			if !pornripsMatchesSearch(t, tokens) {
				continue
			}
			key := t.DetailURL
			if key == "" {
				key = t.Title
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			matches = append(matches, t)
		}
		if len(matches) >= need {
			break
		}
	}
	if skip >= len(matches) {
		return nil
	}
	end := skip + maxResults
	if end > len(matches) {
		end = len(matches)
	}
	return matches[skip:end]
}
