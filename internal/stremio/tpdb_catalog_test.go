package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appconfig "torrent-search-go/internal/config"
)

func tpdbTestHandler(t *testing.T, sceneID string) *Handler {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/scenes":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id":    sceneID,
						"title": "Example Scene",
						"date":  "2024-01-15",
						"poster": map[string]interface{}{
							"url": "https://example.com/poster.jpg",
						},
					},
				},
			})
		case r.URL.Path == "/scenes/"+sceneID:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"id":          sceneID,
					"title":       "Example Scene",
					"date":        "2024-01-15",
					"description": "A test scene.",
					"poster": map[string]interface{}{
						"url": "https://example.com/poster.jpg",
					},
					"background": map[string]interface{}{
						"url": "https://example.com/background.jpg",
					},
					"performers": []map[string]interface{}{
						{"name": "Eva Elfie"},
					},
					"tags": []map[string]interface{}{
						{"tag": "Blonde"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return &Handler{
		Env: &appconfig.Config{
			Metadata: appconfig.MetadataConfig{
				TPDBAPIKey: "test-key",
				TPDBAPIURL: srv.URL,
			},
		},
	}
}

func TestServeTPDBCatalogUsesPornType(t *testing.T) {
	h := tpdbTestHandler(t, "11093443")

	resp, err := h.serveTPDBCatalog(context.Background(), "tpdb_search", "eva elfie", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Metas) != 1 {
		t.Fatalf("expected 1 meta, got %d", len(resp.Metas))
	}
	if resp.Metas[0].Type != "Porn" {
		t.Errorf("catalog item type = %q, want %q", resp.Metas[0].Type, "Porn")
	}
	if resp.Metas[0].PosterShape != "landscape" {
		t.Errorf("catalog item posterShape = %q, want %q", resp.Metas[0].PosterShape, "landscape")
	}
	if resp.Metas[0].ID != "porndb:11093443" {
		t.Errorf("catalog item id = %q, want %q", resp.Metas[0].ID, "porndb:11093443")
	}
}

func TestServeMetaTPDBUsesPornType(t *testing.T) {
	h := tpdbTestHandler(t, "11093443")

	var resp MetaResponse
	for _, reqType := range []string{"Porn", "movie"} {
		var err error
		resp, err = h.ServeMeta(context.Background(), Config{}, reqType, "porndb:11093443")
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", reqType, err)
		}
		if resp.Meta == nil {
			t.Fatalf("type %q: expected meta, got nil", reqType)
		}
		if resp.Meta.Type != "Porn" {
			t.Errorf("type %q request: meta.Type = %q, want %q", reqType, resp.Meta.Type, "Porn")
		}
		if resp.Meta.PosterShape != "landscape" {
			t.Errorf("type %q request: meta.PosterShape = %q, want %q", reqType, resp.Meta.PosterShape, "landscape")
		}
	}
	if len(resp.Meta.Links) != 2 {
		t.Fatalf("expected cast+genre links, got %d", len(resp.Meta.Links))
	}
	for i, link := range resp.Meta.Links {
		if link.Category == "" {
			t.Errorf("link[%d].Category must not be empty (stremio-core requires it)", i)
		}
	}
}
