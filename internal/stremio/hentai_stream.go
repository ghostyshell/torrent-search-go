package stremio

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"torrent-search-go/internal/services/hentai"
	"torrent-search-go/pkg/models"
)

// serveHentaiStream resolves direct mp4 streams for a hentai episode id (Phase
// C). The id is either a Stremio video id "{seriesId}:1:{N}" or a bare series
// id "hmm-…" (episode 1). The series + episode are read from the durable
// hentai_entries store, then the source scraper resolves the stream live;
// results are cached 5min. Returns nil (-> Stremio "no streams") on any
// parse/resolve failure rather than erroring, per the plan's risk mitigation.
func (h *Handler) serveHentaiStream(ctx context.Context, id string) []map[string]interface{} {
	seriesID, epNum := parseHentaiStreamID(id)
	if seriesID == "" || h.Hentai == nil || h.HentaiResolver == nil {
		return nil
	}

	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getHentaiStream(ctx, id); ok {
			return cached
		}
	}

	e, err := h.Hentai.GetHentaiEntry(ctx, seriesID)
	if err != nil || e == nil {
		return nil
	}
	ep := findHentaiEpisode(e.Episodes, epNum)
	if ep == nil {
		return nil
	}

	streams, err := h.HentaiResolver.ResolveEpisodeStream(ctx, e.Prefix, ep.Slug, ep.SourceURL)
	if err != nil {
		log.Printf("[hentai-stream] resolve %s ep%d (%s): %v", seriesID, epNum, ep.Slug, err)
		return nil
	}
	out := hentaiStreamsToStremio(streams, epNum)
	if store != nil && len(out) > 0 {
		_ = store.setHentaiStream(ctx, id, out)
	}
	return out
}

// parseHentaiStreamID splits a Stremio stream id into a series id + episode
// number. Accepts "{seriesId}:1:{N}" (video id form; seriesId has no colon) and
// bare "hmm-…" (defaults to episode 1). Returns ("", 0) for non-hentai.
func parseHentaiStreamID(id string) (seriesID string, epNum int) {
	if !strings.HasPrefix(id, "hmm-") {
		return "", 0
	}
	if parts := strings.Split(id, ":"); len(parts) == 3 {
		if n, err := strconv.Atoi(parts[2]); err == nil && n > 0 {
			return parts[0], n
		}
	}
	return id, 1
}

// findHentaiEpisode returns the episode with the given number, falling back to
// the first episode when epNum is 1 and no numbered-1 episode exists.
func findHentaiEpisode(eps []models.HentaiEpisode, epNum int) *models.HentaiEpisode {
	for i := range eps {
		if eps[i].Number == epNum {
			return &eps[i]
		}
	}
	if epNum == 1 && len(eps) > 0 {
		return &eps[0]
	}
	return nil
}

// hentaiStreamsToStremio maps resolved EpisodeStreams to Stremio stream objects.
// The name carries the source + episode + quality, e.g. "HentaiMama E3 1080P".
// Returns nil for no streams so the caller renders a clean "no streams" list.
func hentaiStreamsToStremio(streams []hentai.EpisodeStream, epNum int) []map[string]interface{} {
	if len(streams) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(streams))
	for _, s := range streams {
		label := s.Name
		if label == "" {
			label = "Hentai"
		}
		label = fmt.Sprintf("%s E%d", label, epNum)
		if s.Quality != "" {
			label += " " + s.Quality
		}
		out = append(out, map[string]interface{}{
			"url":           s.URL,
			"name":          label,
			"behaviorHints": map[string]interface{}{"notWebReady": true},
		})
	}
	return out
}