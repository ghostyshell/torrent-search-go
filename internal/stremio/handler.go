package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	appconfig "torrent-search-go/internal/config"
	"torrent-search-go/internal/services/hentai"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/redis"
	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// MetaEnqueuer enqueues background TPDB/StashDB metadata lookups.
type MetaEnqueuer func(ctx context.Context, items []jobs.MetaEnqueueItem)

// EnrichedScenesOnDemand is the on-demand store populator fired after a
// successful tpdb_search live hit: it upserts the live TPDB scene maps as stubs
// and torrent-matches them against the user's configured sources, so the scene
// surfaces in the store-backed tpdb_new browse on the next open. Fire-and-forget;
// the caller bounds it with a timeout. Nil when no Runner is wired (cold installs).
type EnrichedScenesOnDemand func(ctx context.Context, items []map[string]interface{}, sources []string)

// SukebeiCatalogStore loads durable Sukebei catalog blobs from Mongo.
type SukebeiCatalogStore interface {
	GetSukebeiCatalog(ctx context.Context, catalogID string) ([]byte, bool, error)
}

// PornripsStore reads durable PornRips entries from Mongo for the Mongo-only
// catalog/meta/stream path. Read-only; the background PornripsSync job owns all
// writes via the full StorageProvider. Kept narrow so the full StorageProvider
// does not leak into the stremio package.
type PornripsStore interface {
	GetPornripsRecent(ctx context.Context, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsByTag(ctx context.Context, tags []string, skip, limit int) ([]models.PornripsGroup, error)
	SearchPornrips(ctx context.Context, query string, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsEntryBySlug(ctx context.Context, slug string) (*models.PornripsEntry, error)
	GetPornripsEntriesByPerformer(ctx context.Context, performer string, limit int) ([]models.PornripsEntry, error)
	GetPornripsEntriesByPerformers(ctx context.Context, performers []string, limit int) ([]models.PornripsEntry, error)
	PerformersWithTorrent(ctx context.Context, performers []string) (map[string]bool, error)
}

// EnrichedScenesStore reads durable enriched_scenes from Mongo for the
// store-backed tpdb_new / tpdb_cat / stashdb_cat catalogs and the porndb: meta/
// stream path. Read-only; the background EnrichedScenesSync job owns all writes
// via the full StorageProvider. Kept narrow so the full StorageProvider does
// not leak into the stremio package.
type EnrichedScenesStore interface {
	GetEnrichedScenesByMatchedSources(ctx context.Context, source string, tags []string, sources []string, skip, limit int) ([]models.EnrichedScene, error)
	GetEnrichedScenesByMatchedSourcesAndIDs(ctx context.Context, source string, ids, sources []string, limit int) ([]models.EnrichedScene, error)
	GetEnrichedSceneByID(ctx context.Context, id string) (*models.EnrichedScene, error)
	EnrichedScenesCount(ctx context.Context) (int64, error)
}

// SharedMetaStore reads durable TPDB/StashDB shared_meta from Mongo so the
// Mongo-only request path rehydrates from the durable store on a Redis miss
// without a live TPDB/Stash probe.
type SharedMetaStore interface {
	GetSharedMetaPair(ctx context.Context, metaID string) (*models.SharedMetaPayload, *models.SharedMetaPayload, error)
}

// HentaiStore reads durable hentai_entries from Mongo for the Mongo-only
// catalog/meta path. Read-only; the background HentaiSync job owns all writes
// via the full StorageProvider. Kept narrow so the full StorageProvider does
// not leak into the stremio package.
type HentaiStore interface {
	GetHentaiRecent(ctx context.Context, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiTop(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiAll(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiByYear(ctx context.Context, year string, skip, limit int) ([]models.HentaiEntry, error)
	SearchHentai(ctx context.Context, query string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiEntry(ctx context.Context, id string) (*models.HentaiEntry, error)
}

// Handler serves Stremio addon protocol endpoints.
type Handler struct {
	Scrapers               *scraper.Service
	Redis                  *redis.Client
	Env                    *appconfig.Config
	MetaEnqueuer           MetaEnqueuer
	EnrichedScenesOnDemand EnrichedScenesOnDemand
	CatalogStore           SukebeiCatalogStore
	BaseURL                string
	Studios                StudioProvider
	Cover                  CoverProvider
	Pornrips               PornripsStore
	SharedMeta             SharedMetaStore
	Hentai                 HentaiStore
	// EnrichedScenes is the store-backed source for tpdb_new / tpdb_cat /
	// stashdb_cat catalogs and porndb: meta/stream. nil on cold installs (the
	// background EnrichedScenesSync job populates it); reads return empty then.
	EnrichedScenes EnrichedScenesStore
	// HentaiResolver resolves direct mp4 streams for hmm- episode ids (Phase C).
	// nil when no background runner is wired (cold stores serve empty streams).
	HentaiResolver hentai.EpisodeStreamResolver
	// TubeSources is the registry of all direct-play tube sources, built once at
	// main.go wiring. Catalog/meta/stream dispatch looks up by id/catalog prefix
	// and routes through the generic handlers in tube_catalog.go. The per-source
	// stores + resolvers live inside each TubeSource adapter.
	TubeSources *TubeSourceRegistry
}

// Manifest builds the full Stremio manifest for the install config.
func (h *Handler) Manifest(ctx context.Context, cfg Config) map[string]interface{} {
	extraStudios := []string(nil)
	if h.Studios != nil {
		if studios, err := h.Studios.ExtraStudios(ctx); err == nil && len(studios) > 0 {
			extraStudios = studios
		}
	}
	return BuildManifest(cfg, h.BaseURL, h.Env, extraStudios, prTagOptions(cfg), h.loadTubeGenreOptions(ctx), h.tubeSourcesAll())
}

// ServeHTTPManifest writes manifest.json with CORS headers.
func (h *Handler) ServeHTTPManifest(w http.ResponseWriter, r *http.Request, configSegment string) {
	cfg := ParseConfig(configSegment)
	baseURL := h.BaseURL
	if edge := strings.TrimSpace(r.Header.Get("X-Addon-Base-Url")); edge != "" {
		baseURL = strings.TrimSuffix(edge, "/")
	}
	writeStremioJSON(w, http.StatusOK, BuildManifest(cfg, baseURL, h.Env, h.extraStudios(r.Context()), prTagOptions(cfg), h.loadTubeGenreOptions(r.Context()), h.tubeSourcesAll()))
}

// tubeSourcesAll returns the registered tube sources, or nil when no registry
// is wired (bare-handler tests / cold path). Manifest catalog emission and
// idPrefixes are registry-driven.
func (h *Handler) tubeSourcesAll() []TubeSource {
	if h.TubeSources == nil {
		return nil
	}
	return h.TubeSources.All()
}

// loadTubeGenreOptions reads the precomputed tube-source discover option blobs
// from KV (written by the background sync jobs), one GET per registered source.
// Empty on a cold install; the Studio/Tag/Performer catalogs stay hidden until
// the first sync tick populates KV. Cheap: no Mongo aggregation on the manifest
// path. Per-source (registry-driven) so a new source needs no edit here.
func (h *Handler) loadTubeGenreOptions(ctx context.Context) tubeGenreOptions {
	store := newRedisStore(h.Redis)
	if store == nil || h.TubeSources == nil {
		return tubeGenreOptions{}
	}
	opts := tubeGenreOptions{}
	// Source-gating is handled in BuildManifest (it only emits tube catalogs when
	// the source is enabled); we always load every source's blob so an install
	// that enables any source gets its options without a second pass. The GETs
	// are cheap.
	for _, src := range h.TubeSources.All() {
		if s, t, p, ok := store.getTubeGenreOptions(ctx, src.CachePrefixes().Genres); ok {
			opts[src.Key()] = tubeGenreSet{Studios: s, Tags: t, Performers: p}
		}
	}
	return opts
}

func (h *Handler) extraStudios(ctx context.Context) []string {
	if h.Studios == nil {
		return nil
	}
	studios, err := h.Studios.ExtraStudios(ctx)
	if err != nil || len(studios) == 0 {
		return nil
	}
	return studios
}

// ServeHTTPCatalog writes catalog JSON with CORS headers.
func (h *Handler) ServeHTTPCatalog(w http.ResponseWriter, r *http.Request, configSegment, contentType, catalogID, extra string) {
	cfg := ParseConfig(configSegment)
	resp, err := h.ServeCatalog(r.Context(), cfg, contentType, catalogID, extra)
	if err != nil {
		writeStremioJSON(w, http.StatusOK, CatalogResponse{Metas: []MetaPreview{}})
		return
	}
	writeStremioJSON(w, http.StatusOK, resp)
}

// ServeHTTPMeta writes meta JSON with CORS headers.
func (h *Handler) ServeHTTPMeta(w http.ResponseWriter, r *http.Request, configSegment, contentType, id string) {
	cfg := ParseConfig(configSegment)
	resp, err := h.ServeMeta(r.Context(), cfg, contentType, id)
	if err != nil {
		writeStremioJSON(w, http.StatusOK, MetaResponse{Meta: nil})
		return
	}
	if resp.Meta == nil && strings.HasPrefix(id, "jstrm:") {
		writeStremioJSON(w, http.StatusNotFound, resp)
		return
	}
	writeStremioJSON(w, http.StatusOK, resp)
}

func writeStremioJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
