package handlers

import (
	"net/http"
	"strings"

	"torrent-search-go/internal/services/streams"
)

// StreamsHandler serves Magnetio-compatible stream resolution endpoints.
type StreamsHandler struct {
	service *streams.Service
}

// NewStreamsHandler creates a streams handler.
func NewStreamsHandler(service *streams.Service) *StreamsHandler {
	return &StreamsHandler{service: service}
}

// GetStreams handles GET /streams/:type/:id?providers=...
func (h *StreamsHandler) GetStreams(w http.ResponseWriter, r *http.Request) {
	typ := r.PathValue("type")
	id := r.PathValue("id")

	if typ != "movie" && typ != "series" && typ != "anime" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid type. Use movie, series, or anime.",
		})
		return
	}

	var providerIDs []string
	if p := r.URL.Query().Get("providers"); p != "" {
		parts := strings.Split(p, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				providerIDs = append(providerIDs, part)
			}
		}
	}

	result, err := h.service.Scrape(r.Context(), typ, id, providerIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error":   err.Error(),
			"streams": []streams.Stream{},
		})
		return
	}

	streams.SortByQualityAndSeeders(result.Streams)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"streams": result.Streams,
		"cached":  result.Cached,
	})
}

// ListProviders handles GET /providers
func (h *StreamsHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": h.service.ListProviders(),
	})
}
