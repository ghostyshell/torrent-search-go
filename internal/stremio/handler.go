package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	appconfig "torrent-search-go/internal/config"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/internal/services/redis"
	"torrent-search-go/internal/services/scraper"
)

// MetaEnqueuer enqueues background TPDB/StashDB metadata lookups.
type MetaEnqueuer func(ctx context.Context, items []jobs.MetaEnqueueItem)

// SukebeiCatalogStore loads durable Sukebei catalog blobs from Mongo.
type SukebeiCatalogStore interface {
	GetSukebeiCatalog(ctx context.Context, catalogID string) ([]byte, bool, error)
}

// Handler serves Stremio addon protocol endpoints.
type Handler struct {
	Scrapers     *scraper.Service
	Redis        *redis.Client
	Env          *appconfig.Config
	MetaEnqueuer MetaEnqueuer
	CatalogStore SukebeiCatalogStore
	BaseURL      string
	Reference    *metadata.ReferenceClient
	External     *ExternalProxy
	Studios      StudioProvider
	Cover        CoverProvider
}

// Manifest builds the full Stremio manifest for the install config.
func (h *Handler) Manifest(ctx context.Context, cfg Config) map[string]interface{} {
	extraStudios := []string(nil)
	if h.Studios != nil {
		if studios, err := h.Studios.ExtraStudios(ctx); err == nil && len(studios) > 0 {
			extraStudios = studios
		}
	}
	return BuildManifest(cfg, h.BaseURL, h.Env, extraStudios)
}

// ServeHTTPManifest writes manifest.json with CORS headers.
func (h *Handler) ServeHTTPManifest(w http.ResponseWriter, r *http.Request, configSegment string) {
	cfg := ParseConfig(configSegment)
	baseURL := h.BaseURL
	if edge := strings.TrimSpace(r.Header.Get("X-Addon-Base-Url")); edge != "" {
		baseURL = strings.TrimSuffix(edge, "/")
	}
	writeStremioJSON(w, http.StatusOK, BuildManifest(cfg, baseURL, h.Env, h.extraStudios(r.Context())))
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
