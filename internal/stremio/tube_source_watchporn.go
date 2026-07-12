package stremio

import (
	"context"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// WatchpornStore reads durable watchporn_entries from Mongo for the catalog/meta/
// stream path. Read-only; the Mac-side launchd cron owns all writes via the full
// StorageProvider (watchporn.to is TLS-blocked from prod, so there is no deployed
// sync job - the cron populates Mongo and the genre-option Redis blob).
type WatchpornStore interface {
	GetWatchpornRecent(ctx context.Context, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.WatchpornEntry, error)
	SearchWatchporn(ctx context.Context, query string, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornEntry(ctx context.Context, videoID string) (*models.WatchpornEntry, error)
}

// WatchpornStreamResolver resolves multi-quality mp4 streams for a watchporn
// entry. Implemented by *scraper.WatchpornScraper.
type WatchpornStreamResolver interface {
	ResolveStream(ctx context.Context, entry models.WatchpornEntry) ([]scraper.Stream, error)
}

// watchpornSource adapts the WatchpornStore + scraper resolver to the generic
// TubeSource interface. Studios is multi-key (/categories/ links = the site/
// network), Tags = /tags/ links, Performers = /models/ links, SourceID = VideoID.
// ResolveStream reconstructs a minimal WatchpornEntry (the scraper re-fetches the
// detail page for the rotating v-acctoken, reading only DetailURL).
type watchpornSource struct {
	store    WatchpornStore
	resolver WatchpornStreamResolver
}

func NewWatchpornSource(store WatchpornStore, resolver WatchpornStreamResolver) TubeSource {
	return &watchpornSource{store: store, resolver: resolver}
}

func (s *watchpornSource) Key() string           { return "watchporn" }
func (s *watchpornSource) IDPrefix() string      { return "wpt:" }
func (s *watchpornSource) CatalogPrefix() string { return "wpt" }
func (s *watchpornSource) DisplayName() string   { return "WatchPorn" }
func (s *watchpornSource) CachePrefixes() TubeCachePrefixes {
	return TubeCachePrefixes{
		Catalog: cache.PrefixWatchpornCatalog,
		Meta:    cache.PrefixWatchpornMeta,
		Stream:  cache.PrefixWatchpornStream,
		Genres:  cache.PrefixWatchpornGenres,
	}
}

func (s *watchpornSource) Recent(ctx context.Context, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetWatchpornRecent(ctx, skip, limit)
	return mapWatchpornEntries(es), err
}

func (s *watchpornSource) ByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetWatchpornByStudio(ctx, studioNorm, skip, limit)
	return mapWatchpornEntries(es), err
}

func (s *watchpornSource) ByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetWatchpornByTag(ctx, tagsNorm, skip, limit)
	return mapWatchpornEntries(es), err
}

func (s *watchpornSource) ByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.GetWatchpornByPerformer(ctx, performerNorm, skip, limit)
	return mapWatchpornEntries(es), err
}

func (s *watchpornSource) Search(ctx context.Context, query string, skip, limit int) ([]models.TubeEntry, error) {
	es, err := s.store.SearchWatchporn(ctx, query, skip, limit)
	return mapWatchpornEntries(es), err
}

func (s *watchpornSource) GetEntry(ctx context.Context, sourceID string) (*models.TubeEntry, error) {
	e, err := s.store.GetWatchpornEntry(ctx, sourceID)
	if err != nil || e == nil {
		return nil, err
	}
	t := mapWatchpornEntry(*e)
	return &t, nil
}

func (s *watchpornSource) ResolveStream(ctx context.Context, entry models.TubeEntry) ([]scraper.Stream, error) {
	if s.resolver == nil {
		return nil, nil
	}
	// The scraper's ResolveStream re-fetches the detail page (rotating v-acctoken)
	// and reads only DetailURL. VideoID + Slug are passed for completeness.
	return s.resolver.ResolveStream(ctx, models.WatchpornEntry{
		VideoID:   entry.SourceID,
		Slug:      entry.Slug,
		DetailURL: entry.DetailURL,
	})
}

func (s *watchpornSource) StreamReferer(entry models.TubeEntry) string {
	// The Cloudflare gate keys on the source-site detail page; fall back to the
	// base URL when the entry has no detail URL (matches the pre-refactor path).
	if entry.DetailURL != "" {
		return entry.DetailURL
	}
	return scraper.WatchpornBaseURL()
}

// mapWatchpornEntry maps a durable WatchpornEntry to the read-side TubeEntry.
// Studios is multi-key (/categories/ = site/network); Tags = /tags/; Performers = /models/.
func mapWatchpornEntry(e models.WatchpornEntry) models.TubeEntry {
	return models.TubeEntry{
		SourceKey:      "watchporn",
		IDPrefix:       "wpt:",
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

func mapWatchpornEntries(es []models.WatchpornEntry) []models.TubeEntry {
	out := make([]models.TubeEntry, len(es))
	for i := range es {
		out[i] = mapWatchpornEntry(es[i])
	}
	return out
}
