package stremio

import (
	"context"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// FreepornvideosStore reads durable freepornvideos_entries from Mongo for the
// catalog/meta/stream path. Read-only; the background sync job owns all writes
// via the full StorageProvider.
type FreepornvideosStore interface {
	GetFreepornvideosRecent(ctx context.Context, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.FreepornvideosEntry, error)
	SearchFreepornvideos(ctx context.Context, query string, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosEntry(ctx context.Context, videoID string) (*models.FreepornvideosEntry, error)
}

// FreepornvideosStreamResolver resolves multi-quality mp4 streams for a
// freepornvideos entry. Implemented by *scraper.FreepornvideosScraper.
type FreepornvideosStreamResolver interface {
	ResolveStream(ctx context.Context, entry models.FreepornvideosEntry) ([]scraper.Stream, error)
}

// freepornvideosSource adapts the existing FreepornvideosStore + scraper
// resolver to the generic TubeSource interface. The multi-key Studios set is
// built at read time ({Studio, Network} when distinct); Tags = Categories;
// SourceID = VideoID. ResolveStream reconstructs a minimal FreepornvideosEntry
// (the scraper re-fetches the detail page for the rotating token, reading only
// DetailURL).
type freepornvideosSource struct {
	store    FreepornvideosStore
	resolver FreepornvideosStreamResolver
}

func NewFreepornvideosSource(store FreepornvideosStore, resolver FreepornvideosStreamResolver) TubeSource {
	return &freepornvideosSource{store: store, resolver: resolver}
}

func (s *freepornvideosSource) Key() string           { return "freepornvideos" }
func (s *freepornvideosSource) IDPrefix() string      { return "fpv:" }
func (s *freepornvideosSource) CatalogPrefix() string { return "fpv" }
func (s *freepornvideosSource) DisplayName() string   { return "FreePornVideos" }
func (s *freepornvideosSource) CachePrefixes() TubeCachePrefixes {
	return TubeCachePrefixes{
		Catalog: cache.PrefixFreepornvideosCatalog,
		Meta:    cache.PrefixFreepornvideosMeta,
		Stream:  cache.PrefixFreepornvideosStream,
		Genres:  cache.PrefixFreepornvideosGenres,
	}
}

func (s *freepornvideosSource) Recent(ctx context.Context, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetFreepornvideosRecent(ctx, skip, limit)
	return mapFreepornvideosEntries(es), err
}

func (s *freepornvideosSource) ByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetFreepornvideosByStudio(ctx, studioNorm, skip, limit)
	return mapFreepornvideosEntries(es), err
}

func (s *freepornvideosSource) ByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetFreepornvideosByTag(ctx, tagsNorm, skip, limit)
	return mapFreepornvideosEntries(es), err
}

func (s *freepornvideosSource) ByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetFreepornvideosByPerformer(ctx, performerNorm, skip, limit)
	return mapFreepornvideosEntries(es), err
}

func (s *freepornvideosSource) Search(ctx context.Context, query string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.SearchFreepornvideos(ctx, query, skip, limit)
	return mapFreepornvideosEntries(es), err
}

func (s *freepornvideosSource) GetEntry(ctx context.Context, sourceID string) (*models.TubeEntry, error) {
	e, err := s.store.GetFreepornvideosEntry(ctx, sourceID)
	if err != nil || e == nil {
		return nil, err
	}
	t := mapFreepornvideosEntry(*e)
	return &t, nil
}

func (s *freepornvideosSource) ResolveStream(ctx context.Context, entry models.TubeEntry) ([]scraper.Stream, error) {
	if s.resolver == nil {
		return nil, nil
	}
	// The scraper's ResolveStream re-fetches the detail page (rotating token) and
	// reads only DetailURL. VideoID + Slug are passed for completeness.
	return s.resolver.ResolveStream(ctx, models.FreepornvideosEntry{
		VideoID:   entry.SourceID,
		Slug:      entry.Slug,
		DetailURL: entry.DetailURL,
	})
}

func (s *freepornvideosSource) StreamReferer(entry models.TubeEntry) string {
	// The Cloudflare gate keys on the source-site detail page; fall back to the
	// base URL when the entry has no detail URL (matches the pre-refactor path).
	if entry.DetailURL != "" {
		return entry.DetailURL
	}
	return scraper.FreepornvideosBaseURL()
}

// mapFreepornvideosEntry maps a durable FreepornvideosEntry to the read-side
// TubeEntry. Studios is built from {Studio, Network} (Network only when distinct
// from Studio, matching the pre-refactor meta builder); Tags = Categories.
func mapFreepornvideosEntry(e models.FreepornvideosEntry) models.TubeEntry {
	studios := []string{}
	if e.Studio != "" {
		studios = append(studios, e.Studio)
	}
	if e.Network != "" && e.Network != e.Studio {
		studios = append(studios, e.Network)
	}
	return models.TubeEntry{
		SourceKey:      "freepornvideos",
		IDPrefix:       "fpv:",
		SourceID:       e.VideoID,
		Slug:           e.Slug,
		Title:          e.Title,
		DetailURL:      e.DetailURL,
		Date:           e.Date,
		Excerpt:        e.Excerpt,
		Poster:         e.Poster,
		Studios:        studios,
		Tags:           e.Categories,
		TagsNorm:       e.CategoriesNorm,
		Performers:     e.Performers,
		PerformersNorm: e.PerformersNorm,
		Description:    e.Description,
		Duration:       e.Duration,
		Network:        e.Network,
		Rating:         e.Rating,
		Views:          e.Views,
		Has4K:          e.Has4K,
		DetailScraped:  e.DetailScraped,
		UpdatedAt:      e.UpdatedAt,
	}
}

func mapFreepornvideosEntries(es []models.FreepornvideosEntry) []models.TubeEntry {
	out := make([]models.TubeEntry, len(es))
	for i := range es {
		out[i] = mapFreepornvideosEntry(es[i])
	}
	return out
}
