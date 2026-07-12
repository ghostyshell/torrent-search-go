package stremio

import (
	"context"
	"strings"
	"sync"

	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// TubeSource is the contract every direct-play tube source implements
// (Perverzija, FreePornVideos, yesporn, watchporn, porneec, and the later
// pornhd3x / porn4days / hqporner sources). The generic catalog/meta/stream
// handlers in tube_catalog.go talk to a source through this interface, so
// adding a source is one adapter file + a Register call instead of a third
// copy of the per-source handler stack.
//
// Key/CatalogPrefix/IDPrefix identify the source: Key matches the
// normalizeSources accepted set and the per-source disable name; CatalogPrefix
// is the chars before the first "_" in a catalog id ("pvz"); IDPrefix is the
// meta/stream id prefix ("pvz:"). The Stremio id of an entry is
// IDPrefix+entry.SourceID.
//
// Recent/ByStudio/ByTag/ByPerformer/Search read the durable Mongo store (the
// background sync job owns writes). Search is the Mongo-regex query backing the
// per-source _search catalog (Stremio's search page fans out across the enabled
// per-source _search catalogs and merges client-side, so no separate cross-source
// search catalog is needed). ResolveStream resolves direct streams;
// StreamReferer is the Referer stamped on emitted Stremio streams via
// proxyHeaders.request.
type TubeSource interface {
	Key() string
	IDPrefix() string
	CatalogPrefix() string
	DisplayName() string
	CachePrefixes() TubeCachePrefixes

	Recent(ctx context.Context, skip, limit int) ([]models.TubeEntry, error)
	ByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.TubeEntry, error)
	ByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.TubeEntry, error)
	ByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.TubeEntry, error)
	Search(ctx context.Context, query string, skip, limit int) ([]models.TubeEntry, error)
	GetEntry(ctx context.Context, sourceID string) (*models.TubeEntry, error)

	ResolveStream(ctx context.Context, entry models.TubeEntry) ([]scraper.Stream, error)
	StreamReferer(entry models.TubeEntry) string
}

// TubeCachePrefixes are the four Redis key prefixes a source uses (catalog list
// pages, full meta, resolved streams, precomputed discover-genre options). The
// generic handlers pass these to the prefix-arg tube cache helpers in
// redis_store.go, so a new source reuses those helpers with its own prefixes.
type TubeCachePrefixes struct {
	Catalog string
	Meta    string
	Stream  string
	Genres  string
}

// TubeSourceRegistry holds every registered TubeSource, keyed three ways for
// the three dispatch sites: by Key (normalizeSources / manifest / disable map),
// by IDPrefix (meta + stream dispatch on "pvz:"-style ids), and by CatalogPrefix
// (catalog dispatch on "pvz_"-style ids). Built once at main.go wiring; read on
// every request. All() returns sources in registration order (the manifest
// appends catalogs in that order; tests rely on stable order).
type TubeSourceRegistry struct {
	mu       sync.RWMutex
	byKey    map[string]TubeSource
	byPrefix map[string]TubeSource // IDPrefix, e.g. "pvz:"
	byCat    map[string]TubeSource // CatalogPrefix, e.g. "pvz"
	order    []TubeSource
}

func NewTubeSourceRegistry() *TubeSourceRegistry {
	return &TubeSourceRegistry{
		byKey:    map[string]TubeSource{},
		byPrefix: map[string]TubeSource{},
		byCat:    map[string]TubeSource{},
	}
}

// defaultTubeRegistry is set once at main.go wiring via
// SetDefaultTubeSourceRegistry. The registry-unaware free functions
// normalizeSources and isCatalogAllowed read it so a new tube source just needs
// to be registered - no edit to those functions. nil in bare-handler tests; tube
// sources are then unrecognized, which is fine (tests don't use tube catalogs).
var defaultTubeRegistry *TubeSourceRegistry

// SetDefaultTubeSourceRegistry wires the package-level default registry used by
// normalizeSources / isCatalogAllowed. Must be called exactly once, before the
// HTTP server starts: the package var is unsynchronized, so reads from request
// goroutines rely on the server-start happens-before edge. A second call after
// startup would race those reads.
func SetDefaultTubeSourceRegistry(r *TubeSourceRegistry) { defaultTubeRegistry = r }

func (r *TubeSourceRegistry) Register(s TubeSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[s.Key()] = s
	r.byPrefix[s.IDPrefix()] = s
	r.byCat[s.CatalogPrefix()] = s
	r.order = append(r.order, s)
}

func (r *TubeSourceRegistry) LookupByKey(key string) TubeSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byKey[key]
}

func (r *TubeSourceRegistry) LookupByIDPrefix(id string) TubeSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if i := strings.IndexByte(id, ':'); i > 0 {
		return r.byPrefix[id[:i+1]]
	}
	return nil
}

func (r *TubeSourceRegistry) LookupByCatalogPrefix(catalogID string) TubeSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if i := strings.IndexByte(catalogID, '_'); i > 0 {
		return r.byCat[catalogID[:i]]
	}
	return nil
}

func (r *TubeSourceRegistry) All() []TubeSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TubeSource, len(r.order))
	copy(out, r.order)
	return out
}
