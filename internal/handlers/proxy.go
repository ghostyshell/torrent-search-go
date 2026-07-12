package handlers

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/middleware"
)

// ProxyHandler handles proxy endpoints
type ProxyHandler struct {
	config *config.Config
	client *http.Client
}

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(cfg *config.Config) *ProxyHandler {
	return &ProxyHandler{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RealDebridProxy proxies requests to the Real-Debrid API.
// It forwards the Authorization header (or adds one from the stored user API key),
// rewrites the path, and streams the response back to the client.
func (h *ProxyHandler) RealDebridProxy(w http.ResponseWriter, r *http.Request) {
	// Strip /api/proxy/real-debrid from path
	rdPath := strings.TrimPrefix(r.URL.Path, "/api/proxy/real-debrid")
	if rdPath == "" {
		rdPath = "/"
	}

	// Preserve query string
	targetURL := "https://api.real-debrid.com/rest/1.0" + rdPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Read original body
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error":   "Failed to read request body",
				"message": err.Error(),
			})
			return
		}
	}

	// Build upstream request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error":   "Real-Debrid API error",
			"message": err.Error(),
		})
		return
	}

	// Forward relevant headers
	if ct := r.Header.Get("Content-Type"); ct != "" {
		proxyReq.Header.Set("Content-Type", ct)
	} else {
		proxyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if ua := r.Header.Get("User-Agent"); ua != "" {
		proxyReq.Header.Set("User-Agent", ua)
	} else {
		proxyReq.Header.Set("User-Agent", "TorrentSearch-Proxy/1.0")
	}
	if auth := middleware.GetRealDebridKey(r); auth != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+auth)
	} else if auth := r.Header.Get("Authorization"); auth != "" {
		proxyReq.Header.Set("Authorization", auth)
	}
	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
		proxyReq.Header.Set("Range", rangeHdr)
	}

	// Execute upstream request
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		writeJSON(w, http.StatusGatewayTimeout, map[string]interface{}{
			"error":   "Real-Debrid API error",
			"message": "Error occurred while proxying: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Forward content-type
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	// Read body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Handle empty body
	if len(respBody) == 0 || resp.StatusCode == http.StatusNoContent {
		w.WriteHeader(resp.StatusCode)
		return
	}

	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}
