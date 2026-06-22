package magnetio

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Handler serves Magnetio Stremio protocol endpoints.
type Handler struct {
	BaseURL string

	// mochCache is a short in-memory cache for debrid library catalog pages.
	mochCache *mochCatalogCache
}

// NewHandler creates a Magnetio handler with internal caches.
func NewHandler(baseURL string) *Handler {
	return &Handler{
		BaseURL:   baseURL,
		mochCache: newMochCatalogCache(),
	}
}

// ServeManifest writes the configured manifest.json with CORS headers.
func (h *Handler) ServeManifest(w http.ResponseWriter, r *http.Request, configSegment string) {
	cfg := ParseConfig(configSegment)
	baseURL := h.BaseURL
	if edge := strings.TrimSpace(r.Header.Get("X-Addon-Base-Url")); edge != "" {
		baseURL = strings.TrimSuffix(edge, "/")
	}
	writeStremioJSON(w, http.StatusOK, BuildManifest(cfg, baseURL))
}

// ServeDummyManifest writes the pre-configuration manifest.
func (h *Handler) ServeDummyManifest(w http.ResponseWriter, r *http.Request) {
	baseURL := h.BaseURL
	if edge := strings.TrimSpace(r.Header.Get("X-Addon-Base-Url")); edge != "" {
		baseURL = strings.TrimSuffix(edge, "/")
	}
	writeStremioJSON(w, http.StatusOK, DummyManifest(baseURL))
}

// CatalogResponse is the Stremio catalog payload.
type CatalogResponse struct {
	Metas []map[string]interface{} `json:"metas"`
}

// MetaResponse is the Stremio meta payload.
type MetaResponse struct {
	Meta map[string]interface{} `json:"meta"`
}

// StreamResponse is the Stremio stream payload.
type StreamResponse struct {
	Streams []map[string]interface{} `json:"streams"`
}

// ServeCatalog returns catalog items for debrid library, TMDB streaming, and
// similar-content catalogs.
func (h *Handler) ServeCatalog(w http.ResponseWriter, r *http.Request, configSegment, contentType, catalogID, extra string) {
	cfg := ParseConfig(configSegment)
	ctx := r.Context()

	metas := []map[string]interface{}{}
	var err error

	switch {
	case strings.HasPrefix(catalogID, "magnetio_similar_"):
		metas, err = h.serveSimilarCatalog(ctx, cfg, contentType, catalogID, extra)
	case strings.HasPrefix(catalogID, "tmdb_"):
		metas, err = h.serveTMDBCatalog(ctx, cfg, catalogID)
	default:
		metas, err = h.serveDebridCatalog(ctx, cfg, contentType, catalogID, extra)
	}

	if err != nil {
		writeStremioJSON(w, http.StatusOK, CatalogResponse{Metas: []map[string]interface{}{}})
		return
	}
	writeStremioJSON(w, http.StatusOK, CatalogResponse{Metas: metas})
}

func (h *Handler) serveSimilarCatalog(ctx context.Context, cfg Config, contentType, catalogID, extra string) ([]map[string]interface{}, error) {
	if cfg.TMDBApiKey == "" {
		return nil, nil
	}
	params := parseExtra(extra)
	imdbID := params["genre"]
	if imdbID == "" {
		return nil, nil
	}
	tmdb := newTMDBClient(cfg.TMDBApiKey, cfg.RPDBApiKey, cfg.OMDBApiKey)
	return tmdb.fetchSimilarCatalog(ctx, imdbID, contentType)
}

func (h *Handler) serveTMDBCatalog(ctx context.Context, cfg Config, catalogID string) ([]map[string]interface{}, error) {
	if cfg.TMDBApiKey == "" {
		return nil, nil
	}
	tmdb := newTMDBClient(cfg.TMDBApiKey, cfg.RPDBApiKey, cfg.OMDBApiKey)
	return tmdb.fetchStreamingCatalog(ctx, catalogID)
}

func (h *Handler) serveDebridCatalog(ctx context.Context, cfg Config, contentType, catalogID, extra string) ([]map[string]interface{}, error) {
	parts := strings.Split(catalogID, "_")
	if len(parts) != 2 {
		return nil, nil
	}
	mochID := parts[0]
	requestedType := parts[1]
	if requestedType != "movie" && requestedType != "series" {
		return nil, nil
	}
	if !mochCatalogEnabled(cfg, mochID) {
		return nil, nil
	}
	apiKey := apiKeyForMoch(cfg, mochID)
	if len(apiKey) < minMochKeyLen {
		return nil, nil
	}

	skip := parseSkip(extra)
	if cached, ok := h.mochCache.get(ctx, mochID, apiKey, contentType, skip); ok {
		return cached, nil
	}
	metas, err := fetchMochCatalog(ctx, mochID, apiKey, contentType, skip)
	if err == nil {
		h.mochCache.set(mochID, apiKey, contentType, skip, metas)
	}
	return metas, err
}

// ServeMeta returns minimal metadata for debrid-prefixed items.
func (h *Handler) ServeMeta(w http.ResponseWriter, r *http.Request, configSegment, contentType, id string) {
	meta := mochItemMeta(ParseConfig(configSegment), contentType, id)
	writeStremioJSON(w, http.StatusOK, MetaResponse{Meta: meta})
}

// ServeStream intentionally returns an empty stream list. Per-user debrid
// resolution stays on the Node edge, so this route exists only to avoid a
// confusing static-file fallback HTML response.
func (h *Handler) ServeStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"streams": []map[string]interface{}{},
		"err":     "stream resolution is served by the Node edge, not the Go backend",
	})
}

func writeStremioJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
