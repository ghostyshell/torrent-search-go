package hentai

import (
	"context"
	"fmt"
	"net/http"
)

// service.go assembles the HentaiMama scraper into one HentaiService. The
// Runner builds it via NewService and hands the resolver to the stremio
// Handler for live stream resolution (Phase C). Ratings come straight from
// the scraped source (hentaimama), so there is no Jikan/MAL client here.

// EpisodeStreamResolver resolves a direct stream list for an episode, given the
// series id prefix (hmm), the source episode slug, and the episode's source URL
// (unused by hmm). Implemented by HentaiService; declared here so the stremio
// Handler depends on the narrow interface, not the whole service.
type EpisodeStreamResolver interface {
	ResolveEpisodeStream(ctx context.Context, prefix, epSlug, sourceURL string) ([]EpisodeStream, error)
}

// HentaiService owns the HentaiMama scraper.
type HentaiService struct {
	mama *MamaScraper
}

// NewService builds a HentaiService from a stdlib HTTP client (may be nil; a
// 20s default is used).
func NewService(c *http.Client) *HentaiService {
	hc := newHTTPClient(c)
	return &HentaiService{mama: newMamaScraper(hc)}
}

// ListSeries lists HentaiMama series for a listing page.
func (s *HentaiService) ListSeries(ctx context.Context, source string, page int) ([]SeriesListing, error) {
	switch source {
	case "hentaimama":
		return s.mama.ListSeries(ctx, page)
	}
	return nil, fmt.Errorf("hentai: unknown source %q", source)
}

// FetchSeries fetches full HentaiMama series detail for a slug.
func (s *HentaiService) FetchSeries(ctx context.Context, source, slug string) (*SeriesDetail, error) {
	switch source {
	case "hentaimama":
		return s.mama.FetchSeries(ctx, slug)
	}
	return nil, fmt.Errorf("hentai: unknown source %q", source)
}

// ResolveEpisodeStream dispatches to the source scraper by series id prefix.
// sourceURL is the episode's source URL (unused by hmm).
func (s *HentaiService) ResolveEpisodeStream(ctx context.Context, prefix, epSlug, sourceURL string) ([]EpisodeStream, error) {
	switch prefix {
	case "hmm":
		return s.mama.ResolveEpisodeStream(ctx, epSlug, sourceURL)
	}
	return nil, fmt.Errorf("hentai: unknown prefix %q", prefix)
}