package stremio

import (
	"context"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// YespornStore reads durable yesporn_entries from Mongo for the catalog/meta/
// stream path. Read-only; the background sync job owns all writes via the full
// StorageProvider.
type YespornStore interface {
	GetYespornRecent(ctx context.Context, skip, limit int) ([]models.YespornEntry, error)
	GetYespornByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.YespornEntry, error)
	GetYespornByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.YespornEntry, error)
	GetYespornByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.YespornEntry, error)
	SearchYesporn(ctx context.Context, query string, skip, limit int) ([]models.YespornEntry, error)
	GetYespornEntry(ctx context.Context, videoID string) (*models.YespornEntry, error)
}

// YespornStreamResolver resolves multi-quality mp4 streams for a yesporn entry.
// Implemented by *scraper.YespornScraper.
type YespornStreamResolver interface {
	ResolveStream(ctx context.Context, entry models.YespornEntry) ([]scraper.Stream, error)
}

// yespornSource adapts the YespornStore + scraper resolver to the generic
// TubeSource interface. Studios is already multi-key (channel links), Tags =
// categories, Performers = models, SourceID = VideoID. ResolveStream
// reconstructs a minimal YespornEntry (the scraper re-fetches the detail page for
// the rotating token, reading only DetailURL).
type yespornSource struct {
	store    YespornStore
	resolver YespornStreamResolver
}

func NewYespornSource(store YespornStore, resolver YespornStreamResolver) TubeSource {
	return &yespornSource{store: store, resolver: resolver}
}

func (s *yespornSource) Key() string           { return "yesporn" }
func (s *yespornSource) IDPrefix() string      { return "ypv:" }
func (s *yespornSource) CatalogPrefix() string { return "ypv" }
func (s *yespornSource) DisplayName() string   { return "YesPorn" }
func (s *yespornSource) CachePrefixes() TubeCachePrefixes {
	return TubeCachePrefixes{
		Catalog: cache.PrefixYespornCatalog,
		Meta:    cache.PrefixYespornMeta,
		Stream:  cache.PrefixYespornStream,
		Genres:  cache.PrefixYespornGenres,
	}
}

func (s *yespornSource) Recent(ctx context.Context, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetYespornRecent(ctx, skip, limit)
	return mapYespornEntries(es), err
}

func (s *yespornSource) ByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetYespornByStudio(ctx, studioNorm, skip, limit)
	return mapYespornEntries(es), err
}

func (s *yespornSource) ByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetYespornByTag(ctx, tagsNorm, skip, limit)
	return mapYespornEntries(es), err
}

func (s *yespornSource) ByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetYespornByPerformer(ctx, performerNorm, skip, limit)
	return mapYespornEntries(es), err
}

func (s *yespornSource) Search(ctx context.Context, query string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.SearchYesporn(ctx, query, skip, limit)
	return mapYespornEntries(es), err
}

func (s *yespornSource) GetEntry(ctx context.Context, sourceID string) (*models.TubeEntry, error) {
	e, err := s.store.GetYespornEntry(ctx, sourceID)
	if err != nil || e == nil {
		return nil, err
	}
	t := mapYespornEntry(*e)
	return &t, nil
}

func (s *yespornSource) ResolveStream(ctx context.Context, entry models.TubeEntry) ([]scraper.Stream, error) {
	if s.resolver == nil {
		return nil, nil
	}
	// The scraper's ResolveStream re-fetches the detail page (rotating token) and
	// reads only DetailURL. VideoID + Slug are passed for completeness.
	return s.resolver.ResolveStream(ctx, models.YespornEntry{
		VideoID:   entry.SourceID,
		Slug:      entry.Slug,
		DetailURL: entry.DetailURL,
	})
}

func (s *yespornSource) StreamReferer(entry models.TubeEntry) string {
	// The Cloudflare gate keys on the source-site detail page; fall back to the
	// base URL when the entry has no detail URL (matches the pre-refactor path).
	if entry.DetailURL != "" {
		return entry.DetailURL
	}
	return scraper.YespornBaseURL()
}

// mapYespornEntry maps a durable YespornEntry to the read-side TubeEntry.
// Studios is multi-key (channel links); Tags = categories; Performers = models.
func mapYespornEntry(e models.YespornEntry) models.TubeEntry {
	return models.TubeEntry{
		SourceKey:      "yesporn",
		IDPrefix:       "ypv:",
		SourceID:       e.VideoID,
		Slug:           e.Slug,
		Title:          e.Title,
		DetailURL:      e.DetailURL,
		Date:           e.Date,
		Excerpt:        e.Excerpt,
		Poster:         e.Poster,
		Studios:        e.Studios,
		Tags:           e.Tags,
		TagsNorm:       e.TagsNorm,
		Performers:     e.Performers,
		PerformersNorm: e.PerformersNorm,
		Description:    e.Description,
		Duration:       e.Duration,
		Has4K:          e.Has4K,
		DetailScraped:  e.DetailScraped,
		UpdatedAt:      e.UpdatedAt,
	}
}

func mapYespornEntries(es []models.YespornEntry) []models.TubeEntry {
	out := make([]models.TubeEntry, len(es))
	for i := range es {
		out[i] = mapYespornEntry(es[i])
	}
	return out
}
