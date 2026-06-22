package magnetio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServeCatalogEmptyWithoutKeys(t *testing.T) {
	h := NewHandler("https://example.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/magnetio/default/catalog/movie/rd_movie.json", nil)
	h.ServeCatalog(w, r, "", "movie", "rd_movie", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp CatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Metas) != 0 {
		t.Fatalf("expected empty catalog without key, got %d", len(resp.Metas))
	}
}

func TestHandlerServeCatalogDebridDisabled(t *testing.T) {
	// Key present but catalog flag false.
	cfg := "rd=supersecretapikey12345|rdcatalog=false"
	h := NewHandler("https://example.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/magnetio/"+cfg+"/catalog/movie/rd_movie.json", nil)
	h.ServeCatalog(w, r, cfg, "movie", "rd_movie", "")

	var resp CatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Metas) != 0 {
		t.Fatalf("expected empty catalog when disabled, got %d", len(resp.Metas))
	}
}

func TestHandlerServeCatalogInvalidMoch(t *testing.T) {
	h := NewHandler("https://example.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/magnetio/default/catalog/movie/xx_movie.json", nil)
	h.ServeCatalog(w, r, "", "movie", "xx_movie", "")

	var resp CatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Metas) != 0 {
		t.Fatalf("expected empty catalog for invalid moch, got %d", len(resp.Metas))
	}
}

func TestHandlerServeCatalogTMDBNoKey(t *testing.T) {
	h := NewHandler("https://example.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/magnetio/default/catalog/movie/tmdb_netflix_movie_us.json", nil)
	h.ServeCatalog(w, r, "", "movie", "tmdb_netflix_movie_us", "")

	var resp CatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Metas) != 0 {
		t.Fatalf("expected empty TMDB catalog without key, got %d", len(resp.Metas))
	}
}

func TestHandlerServeCatalogSimilarNoKey(t *testing.T) {
	h := NewHandler("https://example.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/magnetio/default/catalog/movie/magnetio_similar_movie/genre=tt1234567.json", nil)
	h.ServeCatalog(w, r, "", "movie", "magnetio_similar_movie", "genre=tt1234567")

	var resp CatalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Metas) != 0 {
		t.Fatalf("expected empty similar catalog without key, got %d", len(resp.Metas))
	}
}

func TestMochItemMeta(t *testing.T) {
	cfg := ParseConfig("rd=supersecretapikey12345")
	meta := mochItemMeta(cfg, "movie", "rd:abc123")
	if meta == nil {
		t.Fatal("expected meta")
	}
	if meta["id"] != "rd:abc123" {
		t.Fatalf("unexpected id %v", meta["id"])
	}
	if meta["type"] != "movie" {
		t.Fatalf("unexpected type %v", meta["type"])
	}
	if name := meta["name"].(string); name == "" {
		t.Fatal("expected name")
	}
}

func TestMochItemMetaMissingKey(t *testing.T) {
	cfg := ParseConfig("")
	meta := mochItemMeta(cfg, "movie", "rd:abc123")
	if meta != nil {
		t.Fatal("expected nil meta without valid key")
	}
}

func TestMochItemMetaNonDebrid(t *testing.T) {
	cfg := ParseConfig("rd=supersecretapikey12345")
	meta := mochItemMeta(cfg, "movie", "tt1234567")
	if meta != nil {
		t.Fatal("expected nil meta for non-debrid id")
	}
}
