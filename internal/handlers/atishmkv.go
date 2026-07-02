package handlers

import (
	"net/http"

	"torrent-search-go/internal/services/streams/providers"
)

// AtishmkvHandler exposes AtishMKV catalog sync / refresh / status endpoints.
type AtishmkvHandler struct {
	provider *providers.AtishmkvProvider
}

// NewAtishmkvHandler creates an AtishMKV handler.
func NewAtishmkvHandler(provider *providers.AtishmkvProvider) *AtishmkvHandler {
	return &AtishmkvHandler{provider: provider}
}

// Sync triggers a full catalog sync.
func (h *AtishmkvHandler) Sync(w http.ResponseWriter, r *http.Request) {
	if h.provider == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "AtishMKV not configured"})
		return
	}
	result, err := h.provider.SyncCatalog(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

// Refresh triggers a direct-link refresh. If a "name" query parameter is
// provided, only catalog entries whose normalized title contains that name are
// resolved, bypassing the usual max-age filter.
func (h *AtishmkvHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if h.provider == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "AtishMKV not configured"})
		return
	}
	var result map[string]interface{}
	var err error
	if name := r.URL.Query().Get("name"); name != "" {
		result, err = h.provider.RefreshDirectLinksForName(r.Context(), name)
	} else {
		result, err = h.provider.RefreshDirectLinks(r.Context())
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

// Status returns catalog stats.
func (h *AtishmkvHandler) Status(w http.ResponseWriter, r *http.Request) {
	if h.provider == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": false})
		return
	}
	stats, err := h.provider.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
