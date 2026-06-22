package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	appconfig "torrent-search-go/internal/config"
)

func TestServeProxiedMetaHentaiPreservesVideos(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/meta/series/hse-example.json" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"meta": map[string]interface{}{
				"id":          "hse-example",
				"type":        "series",
				"name":        "Example Hentai",
				"poster":      "https://example.com/poster.jpg",
				"background":  "https://example.com/bg.jpg",
				"description": "An example.",
				"releaseInfo": 2020,
				"posterShape": "poster",
				"runtime":     "★ 4.5",
				"genres":      []string{"3D Hentai"},
				"videos": []map[string]interface{}{
					{
						"id":        "hse-example-episode-1",
						"title":     "Episode 1",
						"season":    1,
						"episode":   1,
						"released":  "2020-01-01T00:00:00.000Z",
						"thumbnail": "https://example.com/thumb.jpg",
					},
				},
			},
		})
	}))
	defer upstream.Close()

	h := &Handler{
		External: &ExternalProxy{
			HentaiURL: upstream.URL,
		},
	}

	meta, err := h.serveProxiedMeta(context.Background(), "hs:hse-example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta == nil {
		t.Fatal("expected meta, got nil")
	}
	if meta.Name != "Example Hentai" {
		t.Errorf("name = %q, want %q", meta.Name, "Example Hentai")
	}
	if meta.ReleaseInfo != "2020" {
		t.Errorf("releaseInfo = %q, want %q", meta.ReleaseInfo, "2020")
	}
	wantVideos := []Video{
		{
			ID:        "hse-example-episode-1",
			Title:     "Episode 1",
			Season:    1,
			Episode:   1,
			Released:  "2020-01-01T00:00:00.000Z",
			Thumbnail: "https://example.com/thumb.jpg",
		},
	}
	if !reflect.DeepEqual(meta.Videos, wantVideos) {
		t.Errorf("videos = %#v, want %#v", meta.Videos, wantVideos)
	}
}

func TestServeProxiedMetaHentaiBareID(t *testing.T) {
	// New-style ids carry no "hs:" prefix; the bare upstream id must route to the
	// Hentai upstream unchanged (a colon prefix breaks Stremio's series detail page).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/meta/series/hse-example.json" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"meta": map[string]interface{}{
				"id":     "hse-example",
				"type":   "series",
				"name":   "Example Hentai",
				"videos": []map[string]interface{}{{"id": "hse-example-episode-1", "episode": 1}},
			},
		})
	}))
	defer upstream.Close()

	h := &Handler{External: &ExternalProxy{HentaiURL: upstream.URL}}

	meta, err := h.serveProxiedMeta(context.Background(), "hse-example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta == nil {
		t.Fatal("expected meta, got nil")
	}
	// The meta id must stay bare so it matches the episode ids' series prefix.
	if meta.ID != "hse-example" {
		t.Errorf("meta.ID = %q, want %q", meta.ID, "hse-example")
	}
	if len(meta.Videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(meta.Videos))
	}
}

func TestServeProxiedMetaHentaiLinksHaveCategory(t *testing.T) {
	// stremio-core requires Link.category; dropping it fails the whole meta
	// ("No metadata was found") even on a 200 response. Upstream categories must
	// be preserved, and links missing one must still get a non-empty default.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"meta": map[string]interface{}{
				"id":   "hse-example",
				"type": "series",
				"name": "Example Hentai",
				"links": []map[string]interface{}{
					{"name": "Big Boobs", "category": "Genres", "url": "stremio:///search?search=Big%20Boobs"},
					{"name": "NoCategory", "url": "stremio:///search?search=NoCategory"},
				},
				"videos": []map[string]interface{}{{"id": "hse-example-episode-1", "episode": 1}},
			},
		})
	}))
	defer upstream.Close()

	h := &Handler{External: &ExternalProxy{HentaiURL: upstream.URL}}
	meta, err := h.serveProxiedMeta(context.Background(), "hse-example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta == nil || len(meta.Links) != 2 {
		t.Fatalf("expected 2 links, got %#v", meta)
	}
	if meta.Links[0].Category != "Genres" {
		t.Errorf("link[0].Category = %q, want %q", meta.Links[0].Category, "Genres")
	}
	if meta.Links[1].Category == "" {
		t.Errorf("link[1].Category must not be empty (stremio-core requires it)")
	}
}

func TestFetchCatalogHentaiEmitsBareIDs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"metas": []map[string]interface{}{
				{"id": "hse-example", "name": "Example Hentai", "poster": "https://example.com/p.jpg"},
			},
		})
	}))
	defer upstream.Close()

	p := &ExternalProxy{HentaiURL: upstream.URL}
	metas, err := p.fetchCatalog(context.Background(), upstream.URL, "hentai", "hentai-monthly", "", "", 0, "hs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("expected 1 meta, got %d", len(metas))
	}
	if metas[0].ID != "hse-example" {
		t.Errorf("catalog item id = %q, want bare %q (no hs: prefix)", metas[0].ID, "hse-example")
	}
	// Items are typed "series" (matching the meta type) even though the catalog is
	// declared type "hentai"; a "hentai"-typed item with a "series" meta is
	// rejected by Stremio as "No metadata found".
	if metas[0].Type != "series" {
		t.Errorf("catalog item type = %q, want %q", metas[0].Type, "series")
	}
}


func TestServeMetaHentaiViaHandler(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"meta": map[string]interface{}{
				"id":     "hse-example",
				"type":   "series",
				"name":   "Example Hentai",
				"videos": []map[string]interface{}{{"id": "hse-example-ep-1", "episode": 1}},
			},
		})
	}))
	defer upstream.Close()

	h := &Handler{
		External: &ExternalProxy{HentaiURL: upstream.URL},
		Env:      &appconfig.Config{},
	}

	resp, err := h.ServeMeta(context.Background(), Config{}, "Porn", "hs:hse-example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Meta == nil {
		t.Fatal("expected meta, got nil")
	}
	if len(resp.Meta.Videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(resp.Meta.Videos))
	}
	if resp.Meta.Type != "series" {
		t.Errorf("type = %q, want series", resp.Meta.Type)
	}
}

func TestParseVideosIgnoresInvalidEntries(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"id": "valid", "episode": 2},
		"not a map",
		map[string]interface{}{"episode": 3},
	}
	got := parseVideos(input)
	want := []Video{{ID: "valid", Episode: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseVideos = %#v, want %#v", got, want)
	}
}

func TestStrMetaValConvertsNumbers(t *testing.T) {
	if got := strMetaVal(float64(2020)); got != "2020" {
		t.Errorf("strMetaVal(float64) = %q, want 2020", got)
	}
	if got := strMetaVal(2020); got != "2020" {
		t.Errorf("strMetaVal(int) = %q, want 2020", got)
	}
	if got := strMetaVal(json.Number("2020")); got != "2020" {
		t.Errorf("strMetaVal(json.Number) = %q, want 2020", got)
	}
	if got := strMetaVal("text"); got != "text" {
		t.Errorf("strMetaVal(string) = %q, want text", got)
	}
}
