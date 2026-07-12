package stremio

import (
	"context"
	"fmt"

	hmodels "torrent-search-go/pkg/models"
)

// serveHentaiMeta serves full series metadata for a bare "hmm-" id from the
// durable hentai_entries Mongo store (Phase B). Legacy "hs:"/"hse-"/"hs-"/"htv-"
// ids do not map to Mongo docs and return no metadata (worker proxy removed in
// Phase C).
func (h *Handler) serveHentaiMeta(ctx context.Context, id string) (*Meta, error) {
	if h.Hentai == nil {
		return nil, nil
	}

	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getHentaiMeta(ctx, id); ok {
			return cached, nil
		}
	}

	e, err := h.Hentai.GetHentaiEntry(ctx, id)
	if err != nil || e == nil {
		return nil, err
	}

	bg := e.Background
	if bg == "" {
		bg = e.Poster
	}
	meta := &Meta{
		ID:          e.ID,
		Name:        e.Title,
		Poster:      e.Poster,
		Background:  bg,
		Description: e.Excerpt,
		ReleaseInfo: e.ReleaseYear,
		PosterShape: "landscape",
		Website:     e.DetailURL,
		Genres:      e.Genres,
		Videos:      hentaiVideos(*e),
	}
	if e.Rating > 0 {
		meta.ImdbRating = formatRating(e.Rating)
	}

	if store != nil {
		_ = store.setHentaiMeta(ctx, id, meta)
	}
	return meta, nil
}

// hentaiVideos maps a series' episodes to Stremio video entries. The video id
// is "{seriesId}:1:{N}" so the Phase C stream resolver can parse it back to a
// series + episode number.
func hentaiVideos(e hmodels.HentaiEntry) []Video {
	if len(e.Episodes) == 0 {
		return nil
	}
	out := make([]Video, 0, len(e.Episodes))
	for _, ep := range e.Episodes {
		title := ep.Title
		if title == "" {
			title = fmt.Sprintf("Episode %d", ep.Number)
		}
		out = append(out, Video{
			ID:        fmt.Sprintf("%s:1:%d", e.ID, ep.Number),
			Title:     title,
			Season:    1,
			Episode:   ep.Number,
			Released:  ep.Released,
			Thumbnail: ep.Thumbnail,
		})
	}
	return out
}