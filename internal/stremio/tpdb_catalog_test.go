package stremio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	appconfig "torrent-search-go/internal/config"
	prmodels "torrent-search-go/pkg/models"
)

// fakeEnrichedScenesStore is an in-memory EnrichedScenesStore double for the
// store-backed catalog/meta/stream tests. It records the last query so a test
// can assert the source/tag/sources gate.
type fakeEnrichedScenesStore struct {
	scenes                []enrichedTestScene
	lastSource            string
	lastTags              []string
	lastSources           []string
	lastID                string
}

type enrichedTestScene struct {
	id, title, source string
	matched           []string
	torrents          map[string]prmodels.TorrentRef
}

func (f *fakeEnrichedScenesStore) GetEnrichedScenesByMatchedSources(ctx context.Context, source string, tags []string, sources []string, skip, limit int) ([]prmodels.EnrichedScene, error) {
	f.lastSource, f.lastTags, f.lastSources = source, tags, sources
	allowed := make(map[string]bool, len(sources))
	for _, s := range sources {
		allowed[s] = true
	}
	var out []prmodels.EnrichedScene
	for _, s := range f.scenes {
		if s.source != source {
			continue
		}
		matched := false
		for _, m := range s.matched {
			if allowed[m] {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		out = append(out, prmodels.EnrichedScene{ID: s.id, Title: s.title, Source: s.source, MatchedSources: s.matched, Torrents: s.torrents})
	}
	return out, nil
}

func (f *fakeEnrichedScenesStore) GetEnrichedSceneByID(ctx context.Context, id string) (*prmodels.EnrichedScene, error) {
	f.lastID = id
	for _, s := range f.scenes {
		if s.id == id {
			esc := prmodels.EnrichedScene{ID: s.id, Title: s.title, Source: s.source, MatchedSources: s.matched, Torrents: s.torrents}
			return &esc, nil
		}
	}
	return nil, nil
}

func (f *fakeEnrichedScenesStore) EnrichedScenesCount(ctx context.Context) (int64, error) {
	return int64(len(f.scenes)), nil
}

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

	resp, err := h.serveTPDBCatalog(context.Background(), Config{}, "movie", "tpdb_search", "eva elfie", 0)
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

// TestServeTPDBCatalogSearchIsUnfiltered asserts tpdb_search returns live TPDB
// results unfiltered: the old source-resolvable filter (which dropped scenes
// without a resolved pornrips performer) is gone - the matched_sources gate now
// lives on the store-backed tpdb_new browse, not the live search. The pornrips
// store is not consulted on a TPDB hit.
func TestServeTPDBCatalogSearchIsUnfiltered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "1", "title": "Resolved Scene", "date": "2024-01-01", "performers": []map[string]interface{}{{"name": "Eva Elfie"}}},
				{"id": "2", "title": "Unresolved Scene", "date": "2024-01-02", "performers": []map[string]interface{}{{"name": "Nobody Here"}}},
				{"id": "3", "title": "No Performer Scene", "date": "2024-01-03"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	store := &fakePornripsStore{performersResolved: map[string]bool{"Eva Elfie": true}}
	h := &Handler{
		Env:      &appconfig.Config{Metadata: appconfig.MetadataConfig{TPDBAPIKey: "k", TPDBAPIURL: srv.URL}},
		Pornrips: store,
	}

	resp, err := h.serveTPDBCatalog(context.Background(), Config{}, "movie", "tpdb_search", "eva", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Metas) != 3 {
		t.Fatalf("expected 3 unfiltered live metas, got %d", len(resp.Metas))
	}
	// The pornrips store is not consulted on a TPDB hit (no source-resolvable
	// filter, no fallback).
	if store.lastPerformers != nil {
		t.Errorf("PerformersWithTorrent should not be called on a TPDB hit, got %v", store.lastPerformers)
	}
}

// TestServeTPDBBrowseGatesByMatchedSources asserts tpdb_new browse reads the
// enriched_scenes store and gates by the user's configured sources: with
// piratebay configured, a scene matched on piratebay surfaces and one matched
// only on an unconfigured source is dropped. The pornrips store is not involved.
func TestServeTPDBBrowseGatesByMatchedSources(t *testing.T) {
	store := &fakeEnrichedScenesStore{
		scenes: []enrichedTestScene{
			{id: "porndb:1", title: "Pirate Bay Scene", source: "tpdb", matched: []string{"piratebay"}},
			{id: "porndb:2", title: "Knaben Scene", source: "tpdb", matched: []string{"knaben_adult"}},
		},
	}
	h := &Handler{EnrichedScenes: store}

	resp, err := h.serveTPDBCatalog(context.Background(), Config{Sources: []string{"piratebay"}}, "movie", "tpdb_new", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Metas) != 1 {
		t.Fatalf("expected 1 source-gated meta, got %d", len(resp.Metas))
	}
	if resp.Metas[0].ID != "porndb:1" {
		t.Errorf("gated meta id = %q, want porndb:1", resp.Metas[0].ID)
	}
	if store.lastSource != "tpdb" {
		t.Errorf("store queried source = %q, want tpdb", store.lastSource)
	}
	if len(store.lastTags) != 0 {
		t.Errorf("browse tags = %v, want empty (no tag filter)", store.lastTags)
	}
}

// TestEnrichedTagTokens asserts a tpdb_cat/stashdb_cat genre expands to its
// compound tags_norm tokens so a category whose content lives under compound
// tokens (milf30, analsex, blondhair, footfetish ...) surfaces instead of
// returning zero. The bare NormToken(StashTag) is always included; unknown
// genres return nil.
func TestEnrichedTagTokens(t *testing.T) {
	cases := []struct {
		genre   string
		wantHas  []string // tokens that must be in the set
		wantLen  int      // exact length if > 0
	}{
		{"MILF", []string{"milf", "milf30"}, 0},
		{"Anal", []string{"anal", "analsex"}, 0},
		{"Feet", []string{"feet", "footfetish"}, 0},
		{"Blonde", []string{"blonde", "blondhair"}, 0},
		{"Stepfamily", []string{"stepfamily", "stepmother"}, 0},
		{"Rough Sex", []string{"roughsex", "rough"}, 0}, // bare rough, not roughsex
		{"POV", []string{"pov", "cowgirlpov"}, 0},        // stashdb has no bare pov
		{"Blowjob", []string{"blowjob"}, 1},              // no aliases -> just the bare token
		{"Big Tits", []string{"bigtits"}, 1},
	}
	for _, c := range cases {
		got := enrichedTagTokens(c.genre)
		if c.wantLen > 0 && len(got) != c.wantLen {
			t.Errorf("enrichedTagTokens(%q) = %v, want len %d", c.genre, got, c.wantLen)
		}
		for _, w := range c.wantHas {
			if !slices.Contains(got, w) {
				t.Errorf("enrichedTagTokens(%q) = %v, want %q in set", c.genre, got, w)
			}
		}
	}
	if got := enrichedTagTokens("No Such Genre"); got != nil {
		t.Errorf("enrichedTagTokens(unknown) = %v, want nil", got)
	}
}

// TestServeCategoryCatalogExpandsGenreAliases asserts the store-backed category
// catalog passes the expanded tags_norm $in set to the store (MILF -> milf30,
// stashdb Anal -> analsex) and "All" passes no tag filter.
func TestServeCategoryCatalogExpandsGenreAliases(t *testing.T) {
	store := &fakeEnrichedScenesStore{
		scenes: []enrichedTestScene{
			{id: "porndb:1", title: "MILF Scene", source: "tpdb", matched: []string{"piratebay"}},
		},
	}
	h := &Handler{EnrichedScenes: store}
	cfg := Config{Sources: []string{"piratebay"}}

	if _, err := h.serveCategoryCatalog(context.Background(), tpdbCatalogID, "MILF", 0, 20, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastSource != "tpdb" {
		t.Errorf("MILF source = %q, want tpdb", store.lastSource)
	}
	if !slices.Contains(store.lastTags, "milf30") || !slices.Contains(store.lastTags, "milf") {
		t.Errorf("MILF tags = %v, want milf+milf30 in the $in set", store.lastTags)
	}

	if _, err := h.serveCategoryCatalog(context.Background(), stashdbCatalogID, "Anal", 0, 20, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.lastSource != "stashdb" {
		t.Errorf("Anal stash source = %q, want stashdb", store.lastSource)
	}
	if !slices.Contains(store.lastTags, "analsex") || !slices.Contains(store.lastTags, "anal") {
		t.Errorf("Anal stash tags = %v, want anal+analsex in the $in set", store.lastTags)
	}

	if _, err := h.serveCategoryCatalog(context.Background(), tpdbCatalogID, "All", 0, 20, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.lastTags) != 0 {
		t.Errorf("All tags = %v, want empty (no tag filter)", store.lastTags)
	}
}
