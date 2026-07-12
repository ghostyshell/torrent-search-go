package stremio

import (
	"context"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// PerverzijaStore reads durable perverzija_entries from Mongo for the catalog/
// meta/stream path. Read-only; the background sync job owns all writes via the
// full StorageProvider. Narrow so the full StorageProvider does not leak into
// the stremio package.
type PerverzijaStore interface {
	GetPerverzijaRecent(ctx context.Context, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PerverzijaEntry, error)
	SearchPerverzija(ctx context.Context, query string, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaEntry(ctx context.Context, slug string) (*models.PerverzijaEntry, error)
}

// PerverzijaStreamResolver resolves multi-quality HLS streams for a perverzija
// entry. Implemented by *scraper.PerverzijaScraper; nil on cold installs (the
// stream handler serves empty then).
type PerverzijaStreamResolver interface {
	ResolveStream(ctx context.Context, entry models.PerverzijaEntry) ([]scraper.Stream, error)
}

// perverzijaSource adapts the existing PerverzijaStore + scraper resolver to the
// generic TubeSource interface. Read methods map []PerverzijaEntry -> []TubeEntry
// at read time; ResolveStream reconstructs a minimal PerverzijaEntry (the
// scraper only reads StreamHash) for the resolver call.
type perverzijaSource struct {
	store    PerverzijaStore
	resolver PerverzijaStreamResolver
}

// NewPerverzijaSource wires the perverzija adapter. store is the Mongo
// StorageProvider (satisfies PerverzijaStore); resolver is the scraper (nil on
// cold installs).
func NewPerverzijaSource(store PerverzijaStore, resolver PerverzijaStreamResolver) TubeSource {
	return &perverzijaSource{store: store, resolver: resolver}
}

func (s *perverzijaSource) Key() string           { return "perverzija" }
func (s *perverzijaSource) IDPrefix() string      { return "pvz:" }
func (s *perverzijaSource) CatalogPrefix() string { return "pvz" }
func (s *perverzijaSource) DisplayName() string   { return "Perverzija" }
func (s *perverzijaSource) CachePrefixes() TubeCachePrefixes {
	return TubeCachePrefixes{
		Catalog: cache.PrefixPerverzijaCatalog,
		Meta:    cache.PrefixPerverzijaMeta,
		Stream:  cache.PrefixPerverzijaStream,
		Genres:  cache.PrefixPerverzijaGenres,
	}
}

func (s *perverzijaSource) Recent(ctx context.Context, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPerverzijaRecent(ctx, skip, limit)
	return mapPerverzijaEntries(es), err
}

func (s *perverzijaSource) ByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPerverzijaByStudio(ctx, studioNorm, skip, limit)
	return mapPerverzijaEntries(es), err
}

func (s *perverzijaSource) ByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPerverzijaByTag(ctx, tagsNorm, skip, limit)
	return mapPerverzijaEntries(es), err
}

func (s *perverzijaSource) ByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetPerverzijaByPerformer(ctx, performerNorm, skip, limit)
	return mapPerverzijaEntries(es), err
}

func (s *perverzijaSource) Search(ctx context.Context, query string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.SearchPerverzija(ctx, query, skip, limit)
	return mapPerverzijaEntries(es), err
}

func (s *perverzijaSource) GetEntry(ctx context.Context, sourceID string) (*models.TubeEntry, error) {
	e, err := s.store.GetPerverzijaEntry(ctx, sourceID)
	if err != nil || e == nil {
		return nil, err
	}
	t := mapPerverzijaEntry(*e)
	return &t, nil
}

func (s *perverzijaSource) ResolveStream(ctx context.Context, entry models.TubeEntry) ([]scraper.Stream, error) {
	if s.resolver == nil {
		return nil, nil
	}
	// The scraper's ResolveStream only reads StreamHash (it builds the
	// xs1.php?data= URL from it). Slug + DetailURL are passed for completeness.
	return s.resolver.ResolveStream(ctx, models.PerverzijaEntry{
		Slug:       entry.Slug,
		DetailURL:  entry.DetailURL,
		StreamHash: entry.StreamHash,
	})
}

func (s *perverzijaSource) StreamReferer(_ models.TubeEntry) string {
	return scraper.XtremeStreamReferer()
}

// mapPerverzijaEntry maps a durable PerverzijaEntry to the read-side TubeEntry.
// Studios is the multi-key WP category set; Tags is the WP post_tag taxonomy;
// Performers back the Cast links. WpPoster is the featured-image poster
// fallback used by the generic catalog/meta handlers.
func mapPerverzijaEntry(e models.PerverzijaEntry) models.TubeEntry {
	return models.TubeEntry{
		SourceKey:      "perverzija",
		IDPrefix:       "pvz:",
		SourceID:       e.Slug,
		Slug:           e.Slug,
		Title:          e.Title,
		DetailURL:      e.DetailURL,
		Date:           e.Date,
		Excerpt:        e.Excerpt,
		Poster:         e.Poster,
		WpPoster:       e.WpPoster,
		Studios:        e.Studios,
		StudiosNorm:    e.StudiosNorm,
		Tags:           e.Tags,
		TagsNorm:       e.TagsNorm,
		Performers:     e.Performers,
		PerformersNorm: e.PerformersNorm,
		Description:    e.Description,
		Duration:       e.Duration,
		StreamHash:     e.StreamHash,
		DetailScraped:  e.DetailScraped,
		UpdatedAt:      e.UpdatedAt,
	}
}

func mapPerverzijaEntries(es []models.PerverzijaEntry) []models.TubeEntry {
	out := make([]models.TubeEntry, len(es))
	for i := range es {
		out[i] = mapPerverzijaEntry(es[i])
	}
	return out
}
