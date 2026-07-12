package stremio

import (
	"context"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// PorneecStore reads durable porneec_entries from Mongo for the catalog/meta/
// stream path. Read-only; the background sync job owns all writes via the full
// StorageProvider.
type PorneecStore interface {
	GetPorneecRecent(ctx context.Context, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PorneecEntry, error)
	SearchPorneec(ctx context.Context, query string, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecEntry(ctx context.Context, slug string) (*models.PorneecEntry, error)
}

// PorneecStreamResolver resolves the stored tokenless Bunny CDN mp4 for a
// porneec entry. Implemented by *scraper.PorneecScraper.
type PorneecStreamResolver interface {
	ResolveStream(ctx context.Context, entry models.PorneecEntry) ([]scraper.Stream, error)
}

// porneecSource adapts the PorneecStore + scraper resolver to the generic
// TubeSource interface. SourceID = Slug (the doc id segment, since porneec
// slugs carry no numeric id). ResolveStream emits the stored mp4 the enrich
// sweep wrote (carried on TubeEntry.StreamURL) directly - no re-fetch, since
// the Bunny CDN URL is tokenless and stable.
type porneecSource struct {
	store    PorneecStore
	resolver PorneecStreamResolver
}

func NewPorneecSource(store PorneecStore, resolver PorneecStreamResolver) TubeSource {
	return &porneecSource{store: store, resolver: resolver}
}

func (s *porneecSource) Key() string           { return "porneec" }
func (s *porneecSource) IDPrefix() string      { return "pec:" }
func (s *porneecSource) CatalogPrefix() string { return "pec" }
func (s *porneecSource) DisplayName() string   { return "Porneec" }
func (s *porneecSource) CachePrefixes() TubeCachePrefixes {
	return TubeCachePrefixes{
		Catalog: cache.PrefixPorneecCatalog,
		Meta:    cache.PrefixPorneecMeta,
		Stream:  cache.PrefixPorneecStream,
		Genres:  cache.PrefixPorneecGenres,
	}
}

func (s *porneecSource) Recent(ctx context.Context, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPorneecRecent(ctx, skip, limit)
	return mapPorneecEntries(es), err
}

func (s *porneecSource) ByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPorneecByStudio(ctx, studioNorm, skip, limit)
	return mapPorneecEntries(es), err
}

func (s *porneecSource) ByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPorneecByTag(ctx, tagsNorm, skip, limit)
	return mapPorneecEntries(es), err
}

func (s *porneecSource) ByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPorneecByPerformer(ctx, performerNorm, skip, limit)
	return mapPorneecEntries(es), err
}

func (s *porneecSource) Search(ctx context.Context, query string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.SearchPorneec(ctx, query, skip, limit)
	return mapPorneecEntries(es), err
}

func (s *porneecSource) GetEntry(ctx context.Context, sourceID string) (*models.TubeEntry, error) {
	e, err := s.store.GetPorneecEntry(ctx, sourceID)
	if err != nil || e == nil {
		return nil, err
	}
	t := mapPorneecEntry(*e)
	return &t, nil
}

func (s *porneecSource) ResolveStream(ctx context.Context, entry models.TubeEntry) ([]scraper.Stream, error) {
	if s.resolver == nil {
		return nil, nil
	}
	// The scraper emits the stored tokenless mp4 directly (no re-fetch). StreamURL
	// is carried on TubeEntry from the store read; DetailURL is passed for the
	// ResolveSafeStreamURL referer check.
	return s.resolver.ResolveStream(ctx, models.PorneecEntry{
		Slug:      entry.SourceID,
		DetailURL: entry.DetailURL,
		StreamURL: entry.StreamURL,
	})
}

func (s *porneecSource) StreamReferer(entry models.TubeEntry) string {
	// Bunny CDN is tokenless and does not key on a Referer, but set the source
	// detail page for parity with the other mp4 sources; fall back to the base
	// URL when the entry has no detail URL.
	if entry.DetailURL != "" {
		return entry.DetailURL
	}
	return scraper.PorneecBaseURL()
}

// mapPorneecEntry maps a durable PorneecEntry to the read-side TubeEntry.
// SourceID = Slug (the Stremio id is pec:{slug}). StreamURL is carried through
// so the stream path emits it without a re-fetch.
func mapPorneecEntry(e models.PorneecEntry) models.TubeEntry {
	return models.TubeEntry{
		SourceKey:      "porneec",
		IDPrefix:       "pec:",
		SourceID:       e.Slug,
		Slug:           e.Slug,
		Title:          e.Title,
		DetailURL:      e.DetailURL,
		Date:           e.Date,
		Poster:         e.Poster,
		Studios:        e.Studios,
		StudiosNorm:    e.StudiosNorm,
		Performers:     e.Performers,
		PerformersNorm: e.PerformersNorm,
		Description:    e.Description,
		Duration:       e.Duration,
		StreamURL:      e.StreamURL,
		DetailScraped:  e.DetailScraped,
		UpdatedAt:      e.UpdatedAt,
	}
}

func mapPorneecEntries(es []models.PorneecEntry) []models.TubeEntry {
	out := make([]models.TubeEntry, len(es))
	for i := range es {
		out[i] = mapPorneecEntry(es[i])
	}
	return out
}
