package stremio

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// serveTubeCatalog serves a {prefix}_* catalog for any registered TubeSource
// from its durable Mongo store. Catalog id suffixes: _recent / _studio / _tag /
// _performer / _search. Items are "{IDPrefix}{SourceID}" typed "Porn"
// (landscape). The _search branch runs the Mongo-regex Search method; Stremio's
// search page fans out across the enabled per-source _search catalogs and merges
// client-side, so this is the whole tube-search story (no separate cross-source
// catalog). Redis-cached 15min under src.CachePrefixes().Catalog.
func (h *Handler) serveTubeCatalog(ctx context.Context, src TubeSource, catalogID, genre, searchQ string, skip int) (CatalogResponse, error) {
	if src == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	limit := 50
	g := strings.TrimSpace(genre)
	if g == "All" {
		g = ""
	}
	cacheKey := fmt.Sprintf("%s|%s|%s|%d", catalogID, g, strings.TrimSpace(searchQ), skip)
	store := newRedisStore(h.Redis)
	var cached []models.TubeEntry
	if store != nil && store.getTubeCatalogEntries(ctx, src.CachePrefixes().Catalog, cacheKey, &cached) {
		return CatalogResponse{Metas: tubeEntriesToMetas(src, cached)}, nil
	}

	var entries []models.TubeEntry
	var err error
	switch catalogID {
	case src.CatalogPrefix() + "_recent":
		entries, err = src.Recent(ctx, skip, limit)
	case src.CatalogPrefix() + "_studio":
		entries, err = src.ByStudio(ctx, models.NormToken(g), skip, limit)
	case src.CatalogPrefix() + "_tag":
		entries, err = src.ByTag(ctx, []string{models.NormToken(g)}, skip, limit)
	case src.CatalogPrefix() + "_performer":
		entries, err = src.ByPerformer(ctx, models.NormToken(g), skip, limit)
	case src.CatalogPrefix() + "_search":
		q := strings.TrimSpace(searchQ)
		if q == "" {
			return CatalogResponse{Metas: []MetaPreview{}}, nil
		}
		entries, err = src.Search(ctx, q, skip, limit)
	default:
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	if store != nil {
		_ = store.setTubeCatalogEntries(ctx, src.CachePrefixes().Catalog, cacheKey, entries)
	}
	return CatalogResponse{Metas: tubeEntriesToMetas(src, entries)}, nil
}

// tubeEntriesToMetas maps durable TubeEntry rows to Stremio catalog previews.
// The Stremio id is IDPrefix+SourceID; poster is the full-size poster falling
// back to the WP featured image (pvz); releaseInfo is the upload year. Rows
// missing SourceID or Title are skipped (matches the old per-source guards).
func tubeEntriesToMetas(src TubeSource, entries []models.TubeEntry) []MetaPreview {
	out := make([]MetaPreview, 0, len(entries))
	for _, e := range entries {
		if e.SourceID == "" || e.Title == "" {
			continue
		}
		poster := e.Poster
		if poster == "" {
			poster = e.WpPoster
		}
		out = append(out, MetaPreview{
			ID:          src.IDPrefix() + e.SourceID,
			Type:        "Porn",
			Name:        e.Title,
			Poster:      poster,
			Background:  poster,
			Description: e.Excerpt,
			ReleaseInfo: releaseYear(e.Date),
			PosterShape: "landscape",
		})
	}
	return out
}

// serveTubeMeta serves full scene metadata for a "{prefix}{sourceID}" id from
// the source's durable store. Performers -> Cast links, Studios -> Studio links,
// Tags -> Genres. stremio-core rejects a Link with an empty Category, so every
// link is tagged (tubeMetaLinks). Redis-cached long TTL under Meta prefix.
func (h *Handler) serveTubeMeta(ctx context.Context, src TubeSource, id string) (*Meta, error) {
	if src == nil {
		return nil, nil
	}
	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getTubeMeta(ctx, src.CachePrefixes().Meta, id); ok {
			return cached, nil
		}
	}
	e, err := src.GetEntry(ctx, strings.TrimPrefix(id, src.IDPrefix()))
	if err != nil || e == nil {
		return nil, err
	}
	poster := e.Poster
	if poster == "" {
		poster = e.WpPoster
	}
	desc := e.Description
	if desc == "" {
		desc = e.Excerpt
	}
	meta := &Meta{
		ID:          id,
		Type:        "Porn",
		Name:        e.Title,
		Poster:      poster,
		Background:  poster,
		Description: desc,
		ReleaseInfo: releaseYear(e.Date),
		Runtime:     e.Duration,
		PosterShape: "landscape",
		Website:     e.DetailURL,
		Genres:      e.Tags,
		Links:       tubeMetaLinks(e.Performers, e.Studios),
	}
	if store != nil {
		_ = store.setTubeMeta(ctx, src.CachePrefixes().Meta, id, meta)
	}
	return meta, nil
}

// serveTubeStream resolves direct streams for a "{prefix}{sourceID}" id: read
// the entry, call the source resolver, emit one Stremio stream per resolved
// stream (best quality first) with proxyHeaders.request (browser UA + the
// source's player Referer via StreamReferer) so Stremio's streaming server
// re-fetches through the Cloudflare gate. Cached 5min under Stream prefix.
// Returns nil (clean "no streams") on any miss/resolve failure.
func (h *Handler) serveTubeStream(ctx context.Context, src TubeSource, id string) []map[string]interface{} {
	if src == nil {
		return nil
	}
	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getTubeStream(ctx, src.CachePrefixes().Stream, id); ok {
			return cached
		}
	}
	e, err := src.GetEntry(ctx, strings.TrimPrefix(id, src.IDPrefix()))
	if err != nil || e == nil {
		return nil
	}
	streams, err := src.ResolveStream(ctx, *e)
	if err != nil {
		log.Printf("[tube-stream] resolve %s: %v", id, err)
		return nil
	}
	out := tubeStreamsToStremio(streams, src.StreamReferer(*e))
	if store != nil && len(out) > 0 {
		_ = store.setTubeStream(ctx, src.CachePrefixes().Stream, id, out)
	}
	return out
}

// releaseYear returns the 4-digit year from an ISO-ish date string, or "".
func releaseYear(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

// tubeMetaLinks builds Cast (performers) + Studio (studios) search links, shared
// across every tube source. stremio-core rejects a Link with an empty Category,
// so every link is tagged.
func tubeMetaLinks(performers, studios []string) []Link {
	var links []Link
	for _, p := range performers {
		links = append(links, Link{
			Name:     p,
			Category: "Cast",
			// url.QueryEscape, not a bare space->"+" swap: a performer/studio name
			// carrying "&", "#", "?", or "+" would otherwise split or break the
			// stremio:///search query string.
			URL: "stremio:///search?search=" + url.QueryEscape(p),
		})
	}
	for _, s := range studios {
		links = append(links, Link{
			Name:     s,
			Category: "Studio",
			URL:      "stremio:///search?search=" + url.QueryEscape(s),
		})
	}
	return links
}

// tubeStreamsToStremio maps resolved scraper.Streams to Stremio stream objects
// with Cloudflare-gate proxyHeaders (browser UA + the source's player Referer).
// Best quality is first (the resolver sorts). Returns nil for no streams so the
// caller renders a clean "no streams" list. The Referer is stripped of CR/LF
// before it becomes a header value: a detail URL stored before the
// ingestion-time StripHeaderUnsafe fix could carry CRLF a compromised upstream
// injected.
func tubeStreamsToStremio(streams []scraper.Stream, referer string) []map[string]interface{} {
	if len(streams) == 0 {
		return nil
	}
	referer = scraper.StripHeaderUnsafe(referer)
	out := make([]map[string]interface{}, 0, len(streams))
	for _, s := range streams {
		name := s.Name
		if name == "" {
			name = "Stream"
		}
		out = append(out, map[string]interface{}{
			"url":  s.URL,
			"name": name,
			"behaviorHints": map[string]interface{}{
				"notWebReady": true,
				"proxyHeaders": map[string]interface{}{
					"request": map[string]string{
						"User-Agent": scraper.BrowserUA(),
						"Referer":    referer,
					},
				},
			},
		})
	}
	return out
}
