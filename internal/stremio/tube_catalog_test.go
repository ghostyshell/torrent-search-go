package stremio

import (
	"context"
	"reflect"
	"testing"

	"torrent-search-go/internal/services/scraper"
	"torrent-search-go/pkg/models"
)

// tube_catalog_test.go is the Phase 0 regression guard for the generic tube
// handlers (internal/stremio/tube_catalog.go). It runs the registered
// perverzija + freepornvideos adapters through serveTubeCatalog / serveTubeMeta
// / serveTubeStream and asserts the exact catalog/meta/stream output, so any
// divergence from the pre-refactor per-source behavior fails CI.
//
// The pre-refactor per-source handlers were deleted (plan 0.8); the expected
// values below are the byte-identical output the old handlers produced (verified
// by a before/after deep-compare gate before deletion). The stream Referer /
// User-Agent expectations use the real scraper constants so the test stays
// correct if those constants change.

// --- Perverzija stubs ---

type stubPvzStore struct {
	entries []models.PerverzijaEntry
	got     string // records the slug passed to GetPerverzijaEntry
}

func (s *stubPvzStore) GetPerverzijaRecent(ctx context.Context, skip, limit int) ([]models.PerverzijaEntry, error) {
	return s.entries, nil
}
func (s *stubPvzStore) GetPerverzijaByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PerverzijaEntry, error) {
	return s.entries, nil
}
func (s *stubPvzStore) GetPerverzijaByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PerverzijaEntry, error) {
	return s.entries, nil
}
func (s *stubPvzStore) GetPerverzijaByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PerverzijaEntry, error) {
	return s.entries, nil
}
func (s *stubPvzStore) SearchPerverzija(ctx context.Context, query string, skip, limit int) ([]models.PerverzijaEntry, error) {
	return s.entries, nil
}
func (s *stubPvzStore) GetPerverzijaEntry(ctx context.Context, slug string) (*models.PerverzijaEntry, error) {
	s.got = slug
	for i := range s.entries {
		if s.entries[i].Slug == slug {
			return &s.entries[i], nil
		}
	}
	return nil, nil
}

type stubPvzResolver struct {
	streams []scraper.Stream
	gotHash string // records the StreamHash the adapter reconstructed
}

func (r *stubPvzResolver) ResolveStream(ctx context.Context, e models.PerverzijaEntry) ([]scraper.Stream, error) {
	r.gotHash = e.StreamHash
	return r.streams, nil
}

func pvzEntries() []models.PerverzijaEntry {
	return []models.PerverzijaEntry{{
		Slug:        "scene-1",
		Title:       "Scene One",
		DetailURL:   "https://tube.perverzija.com/scene-1",
		Date:        "2026-05-10T12:00:00",
		Excerpt:     "short excerpt",
		Poster:      "https://img/p.jpg",
		WpPoster:    "https://img/wp.jpg",
		Studios:     []string{"Studio A"},
		StudiosNorm: []string{"studio a"},
		Tags:        []string{"tag1", "tag2"},
		Performers:  []string{"Perf A"},
		Description: "full desc",
		Duration:    "PT10M",
		StreamHash:  "hash123",
	}}
}

func pvzHandler(entries []models.PerverzijaEntry, streams []scraper.Stream) (*Handler, *TubeSourceRegistry, *stubPvzResolver) {
	store := &stubPvzStore{entries: entries}
	resolver := &stubPvzResolver{streams: streams}
	reg := NewTubeSourceRegistry()
	reg.Register(NewPerverzijaSource(store, resolver))
	h := &Handler{TubeSources: reg}
	return h, reg, resolver
}

func TestTubeCatalogPerverzija(t *testing.T) {
	h, reg, resolver := pvzHandler(pvzEntries(), []scraper.Stream{{URL: "https://hls/x.m3u8", Name: "HLS 720p"}})
	ctx := context.Background()

	// Catalog (pvz_recent): poster falls back from Poster -> WpPoster only when
	// Poster is empty; here Poster is set. ReleaseInfo is the upload year.
	got, err := h.serveTubeCatalog(ctx, reg.LookupByCatalogPrefix("pvz_recent"), "pvz_recent", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	wantMetas := []MetaPreview{{
		ID:          "pvz:scene-1",
		Type:        "Porn",
		Name:        "Scene One",
		Poster:      "https://img/p.jpg",
		Background:  "https://img/p.jpg",
		Description: "short excerpt",
		ReleaseInfo: "2026",
		PosterShape: "landscape",
	}}
	if !reflect.DeepEqual(got.Metas, wantMetas) {
		t.Fatalf("pvz_recent catalog:\ngot  %+v\nwant %+v", got.Metas, wantMetas)
	}

	// pvz_search empty query -> empty.
	gotSearch, _ := h.serveTubeCatalog(ctx, reg.LookupByCatalogPrefix("pvz_search"), "pvz_search", "", "  ", 0)
	if len(gotSearch.Metas) != 0 {
		t.Fatalf("pvz_search empty q: want 0 metas, got %+v", gotSearch.Metas)
	}

	// Meta: Genres=Tags, Links=Cast(performers)+Studio(studios), desc=Description.
	gotMeta, err := h.serveTubeMeta(ctx, reg.LookupByIDPrefix("pvz:scene-1"), "pvz:scene-1")
	if err != nil || gotMeta == nil {
		t.Fatalf("pvz meta: %+v %v", gotMeta, err)
	}
	wantMeta := &Meta{
		ID:          "pvz:scene-1",
		Type:        "Porn",
		Name:        "Scene One",
		Poster:      "https://img/p.jpg",
		Background:  "https://img/p.jpg",
		Description: "full desc",
		ReleaseInfo: "2026",
		Runtime:     "PT10M",
		PosterShape: "landscape",
		Website:     "https://tube.perverzija.com/scene-1",
		Genres:      []string{"tag1", "tag2"},
		Links: []Link{
			{Name: "Perf A", Category: "Cast", URL: "stremio:///search?search=Perf+A"},
			{Name: "Studio A", Category: "Studio", URL: "stremio:///search?search=Studio+A"},
		},
	}
	if !reflect.DeepEqual(gotMeta, wantMeta) {
		t.Fatalf("pvz meta:\ngot  %+v\nwant %+v", gotMeta, wantMeta)
	}

	// Stream: adapter reconstructs StreamHash for the resolver; referer is the
	// xtremestream player URL; proxyHeaders carry browser UA + stripped referer.
	resolver.gotHash = ""
	gotStream := h.serveTubeStream(ctx, reg.LookupByIDPrefix("pvz:scene-1"), "pvz:scene-1")
	wantStream := []map[string]interface{}{{
		"url":  "https://hls/x.m3u8",
		"name": "HLS 720p",
		"behaviorHints": map[string]interface{}{
			"notWebReady": true,
			"proxyHeaders": map[string]interface{}{
				"request": map[string]string{
					"User-Agent": scraper.BrowserUA(),
					"Referer":    scraper.StripHeaderUnsafe(scraper.XtremeStreamReferer()),
				},
			},
		},
	}}
	if !reflect.DeepEqual(gotStream, wantStream) {
		t.Fatalf("pvz stream:\ngot  %+v\nwant %+v", gotStream, wantStream)
	}
	if resolver.gotHash != "hash123" {
		t.Fatalf("adapter did not pass StreamHash to resolver, got %q", resolver.gotHash)
	}
}

// TestTubeCatalogPerverzijaPosterFallback guards the Poster -> WpPoster
// fallback used by both the catalog and meta handlers.
func TestTubeCatalogPerverzijaPosterFallback(t *testing.T) {
	e := pvzEntries()[0]
	e.Poster = "" // forces WpPoster fallback
	entries := []models.PerverzijaEntry{e}
	h, reg, _ := pvzHandler(entries, nil)
	ctx := context.Background()

	got, _ := h.serveTubeCatalog(ctx, reg.LookupByCatalogPrefix("pvz_recent"), "pvz_recent", "", "", 0)
	if got.Metas[0].Poster != "https://img/wp.jpg" {
		t.Fatalf("poster fallback: got %q want https://img/wp.jpg", got.Metas[0].Poster)
	}
	gotMeta, _ := h.serveTubeMeta(ctx, reg.LookupByIDPrefix("pvz:scene-1"), "pvz:scene-1")
	if gotMeta.Poster != "https://img/wp.jpg" {
		t.Fatalf("meta poster fallback: got %q want https://img/wp.jpg", gotMeta.Poster)
	}
}

// --- Freepornvideos stubs ---

type stubFpvStore struct {
	entries []models.FreepornvideosEntry
}

func (s *stubFpvStore) GetFreepornvideosRecent(ctx context.Context, skip, limit int) ([]models.FreepornvideosEntry, error) {
	return s.entries, nil
}
func (s *stubFpvStore) GetFreepornvideosByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	return s.entries, nil
}
func (s *stubFpvStore) GetFreepornvideosByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	return s.entries, nil
}
func (s *stubFpvStore) GetFreepornvideosByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	return s.entries, nil
}
func (s *stubFpvStore) SearchFreepornvideos(ctx context.Context, query string, skip, limit int) ([]models.FreepornvideosEntry, error) {
	return s.entries, nil
}
func (s *stubFpvStore) GetFreepornvideosEntry(ctx context.Context, videoID string) (*models.FreepornvideosEntry, error) {
	for i := range s.entries {
		if s.entries[i].VideoID == videoID {
			return &s.entries[i], nil
		}
	}
	return nil, nil
}

type stubFpvResolver struct {
	streams []scraper.Stream
	gotURL  string
}

func (r *stubFpvResolver) ResolveStream(ctx context.Context, e models.FreepornvideosEntry) ([]scraper.Stream, error) {
	r.gotURL = e.DetailURL
	return r.streams, nil
}

func fpvEntries() []models.FreepornvideosEntry {
	return []models.FreepornvideosEntry{{
		VideoID:     "4242",
		Slug:        "scene-slug",
		Title:       "FPV Scene",
		DetailURL:   "https://freepornvideos.xxx/videos/4242/scene-slug/",
		Date:        "2026-04-01",
		Excerpt:     "fpv excerpt",
		Poster:      "https://img.freepornvideos.xxx/1.jpg",
		Studio:      "Channel X",
		Network:     "Network Y",
		Categories:  []string{"cat1", "cat2"},
		Performers:  []string{"Model Z"},
		Description: "fpv full",
		Duration:    "PT20M",
		Rating:      "92",
		Views:       "1000",
		Has4K:       true,
	}}
}

func fpvHandler(entries []models.FreepornvideosEntry, streams []scraper.Stream) (*Handler, *TubeSourceRegistry, *stubFpvResolver) {
	store := &stubFpvStore{entries: entries}
	resolver := &stubFpvResolver{streams: streams}
	reg := NewTubeSourceRegistry()
	reg.Register(NewFreepornvideosSource(store, resolver))
	h := &Handler{TubeSources: reg}
	return h, reg, resolver
}

func TestTubeCatalogFreepornvideos(t *testing.T) {
	entries := fpvEntries()
	h, reg, resolver := fpvHandler(entries, []scraper.Stream{{URL: "https://cdn/720.mp4", Name: "720p"}})
	ctx := context.Background()

	// Catalog (fpv_recent): SourceID = VideoID.
	got, err := h.serveTubeCatalog(ctx, reg.LookupByCatalogPrefix("fpv_recent"), "fpv_recent", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	wantMetas := []MetaPreview{{
		ID:          "fpv:4242",
		Type:        "Porn",
		Name:        "FPV Scene",
		Poster:      "https://img.freepornvideos.xxx/1.jpg",
		Background:  "https://img.freepornvideos.xxx/1.jpg",
		Description: "fpv excerpt",
		ReleaseInfo: "2026",
		PosterShape: "landscape",
	}}
	if !reflect.DeepEqual(got.Metas, wantMetas) {
		t.Fatalf("fpv_recent catalog:\ngot  %+v\nwant %+v", got.Metas, wantMetas)
	}

	// Meta: Studios = {Channel X, Network Y} (distinct), Genres = Categories.
	gotMeta, err := h.serveTubeMeta(ctx, reg.LookupByIDPrefix("fpv:4242"), "fpv:4242")
	if err != nil || gotMeta == nil {
		t.Fatalf("fpv meta: %+v %v", gotMeta, err)
	}
	wantMeta := &Meta{
		ID:          "fpv:4242",
		Type:        "Porn",
		Name:        "FPV Scene",
		Poster:      "https://img.freepornvideos.xxx/1.jpg",
		Background:  "https://img.freepornvideos.xxx/1.jpg",
		Description: "fpv full",
		ReleaseInfo: "2026",
		Runtime:     "PT20M",
		PosterShape: "landscape",
		Website:     "https://freepornvideos.xxx/videos/4242/scene-slug/",
		Genres:      []string{"cat1", "cat2"},
		Links: []Link{
			{Name: "Model Z", Category: "Cast", URL: "stremio:///search?search=Model+Z"},
			{Name: "Channel X", Category: "Studio", URL: "stremio:///search?search=Channel+X"},
			{Name: "Network Y", Category: "Studio", URL: "stremio:///search?search=Network+Y"},
		},
	}
	if !reflect.DeepEqual(gotMeta, wantMeta) {
		t.Fatalf("fpv meta:\ngot  %+v\nwant %+v", gotMeta, wantMeta)
	}

	// Stream: adapter reconstructs DetailURL; referer = the detail URL.
	resolver.gotURL = ""
	gotStream := h.serveTubeStream(ctx, reg.LookupByIDPrefix("fpv:4242"), "fpv:4242")
	wantStream := []map[string]interface{}{{
		"url":  "https://cdn/720.mp4",
		"name": "720p",
		"behaviorHints": map[string]interface{}{
			"notWebReady": true,
			"proxyHeaders": map[string]interface{}{
				"request": map[string]string{
					"User-Agent": scraper.BrowserUA(),
					"Referer":    scraper.StripHeaderUnsafe(entries[0].DetailURL),
				},
			},
		},
	}}
	if !reflect.DeepEqual(gotStream, wantStream) {
		t.Fatalf("fpv stream:\ngot  %+v\nwant %+v", gotStream, wantStream)
	}
	if resolver.gotURL != entries[0].DetailURL {
		t.Fatalf("adapter did not pass DetailURL to resolver, got %q", resolver.gotURL)
	}
}

// TestTubeCatalogFreepornvideosStudioNetworkDedup guards the {Studio, Network}
// dedup: when Network == Studio the meta carries a single Studio link, and the
// stream referer falls back to the base URL when DetailURL is empty.
func TestTubeCatalogFreepornvideosStudioNetworkDedup(t *testing.T) {
	e := fpvEntries()[0]
	e.Network = e.Studio // collapse: Studios should be {Channel X}, not duplicated
	e.DetailURL = ""     // forces base-URL referer fallback
	entries := []models.FreepornvideosEntry{e}
	h, reg, _ := fpvHandler(entries, []scraper.Stream{{URL: "https://cdn/480.mp4", Name: "480p"}})
	ctx := context.Background()

	gotMeta, _ := h.serveTubeMeta(ctx, reg.LookupByIDPrefix("fpv:4242"), "fpv:4242")
	studioLinks := 0
	for _, l := range gotMeta.Links {
		if l.Category == "Studio" {
			studioLinks++
		}
	}
	if studioLinks != 1 {
		t.Fatalf("expected 1 Studio link after dedup, got %d (%+v)", studioLinks, gotMeta.Links)
	}

	gotStream := h.serveTubeStream(ctx, reg.LookupByIDPrefix("fpv:4242"), "fpv:4242")
	wantReferer := scraper.StripHeaderUnsafe(scraper.FreepornvideosBaseURL())
	bh := gotStream[0]["behaviorHints"].(map[string]interface{})
	req := bh["proxyHeaders"].(map[string]interface{})["request"].(map[string]string)
	if req["Referer"] != wantReferer {
		t.Fatalf("referer fallback: got %q want %q", req["Referer"], wantReferer)
	}
}