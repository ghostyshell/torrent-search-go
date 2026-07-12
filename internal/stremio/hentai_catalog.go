package stremio

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	hmodels "torrent-search-go/pkg/models"
)

// serveHentaiCatalog serves the hentai_* catalogs from the durable
// hentai_entries Mongo store (Phase B). A cold store (0 docs, before the first
// HentaiSync tick) serves empty - no live scrape fallback. The store filters to
// HentaiMama (prefix "hmm") so this path emits bare "hmm-" ids that map
// straight back to Mongo for meta.
func (h *Handler) serveHentaiCatalog(ctx context.Context, catalogID, genre, searchQ string, skip int) (CatalogResponse, error) {
	if h.Hentai == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	limit := 50 // hentai catalogs page 50-per; manifest skip stepping matches.

	g := strings.TrimSpace(genre)
	if g == "All" {
		g = ""
	}

	// Cache key mirrors the other catalog list paths: catalog|genre|query|skip.
	// An empty hentai_search query still does one Redis miss below before the
	// switch short-circuits; harmless (no Mongo read) and keeps the path uniform.
	cacheKey := fmt.Sprintf("%s|%s|%s|%d", catalogID, g, strings.TrimSpace(searchQ), skip)
	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getHentaiCatalogEntries(ctx, cacheKey); ok {
			return CatalogResponse{Metas: hentaiEntriesToMetas(cached)}, nil
		}
	}

	var entries []hmodels.HentaiEntry
	var err error
	switch catalogID {
	case "hentai_new":
		entries, err = h.Hentai.GetHentaiRecent(ctx, skip, limit)
	case "hentai_top":
		entries, err = h.Hentai.GetHentaiTop(ctx, hmodels.NormToken(g), skip, limit)
	case "hentai_all":
		entries, err = h.Hentai.GetHentaiAll(ctx, hmodels.NormToken(g), skip, limit)
	case "hentai_studios":
		entries, err = h.Hentai.GetHentaiByStudio(ctx, hmodels.NormToken(g), skip, limit)
	case "hentai_years":
		entries, err = h.Hentai.GetHentaiByYear(ctx, g, skip, limit)
	case "hentai_search":
		q := strings.TrimSpace(searchQ)
		if q == "" {
			return CatalogResponse{Metas: []MetaPreview{}}, nil
		}
		entries, err = h.Hentai.SearchHentai(ctx, q, skip, limit)
	default:
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}

	if store != nil {
		_ = store.setHentaiCatalogEntries(ctx, cacheKey, entries)
	}

	metas := hentaiEntriesToMetas(entries)
	return CatalogResponse{Metas: metas}, nil
}

// hentaiEntriesToMetas maps durable hentai_entries to Stremio catalog previews.
// Items are typed "series" (the catalog declares type "hentai"; items typed
// "hentai" are rejected by stremio-core as "No metadata found"). The source
// rating (0-10) is surfaced as ImdbRating.
func hentaiEntriesToMetas(entries []hmodels.HentaiEntry) []MetaPreview {
	out := make([]MetaPreview, 0, len(entries))
	for _, e := range entries {
		if e.ID == "" || e.Title == "" {
			continue
		}
		bg := e.Background
		if bg == "" {
			bg = e.Poster
		}
		mp := MetaPreview{
			ID:          e.ID,
			Type:        "series",
			Name:        e.Title,
			Poster:      e.Poster,
			Background:  bg,
			Description: e.Excerpt,
			ReleaseInfo: e.ReleaseYear,
			PosterShape: "landscape",
		}
		if e.Rating > 0 {
			mp.ImdbRating = formatRating(e.Rating)
		}
		out = append(out, mp)
	}
	return out
}

func formatRating(r float64) string {
	return strconv.FormatFloat(r, 'f', 1, 64)
}