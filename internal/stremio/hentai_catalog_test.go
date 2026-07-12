package stremio

import (
	"context"
	"testing"

	hmodels "torrent-search-go/pkg/models"
)

// fakeHentaiStore records the query each method received and returns canned
// entries, so we can assert serveHentaiCatalog dispatches to the right method
// with a normalized genre/studio token.
type fakeHentaiStore struct {
	recent        []hmodels.HentaiEntry
	top           []hmodels.HentaiEntry
	all           []hmodels.HentaiEntry
	studio        []hmodels.HentaiEntry
	year          []hmodels.HentaiEntry
	search        []hmodels.HentaiEntry
	byID          *hmodels.HentaiEntry

	lastGenreNorm  string
	lastStudioNorm string
	lastYear       string
	lastSearchQ    string
	lastSkip       int
	lastLimit      int
}

func (f *fakeHentaiStore) GetHentaiRecent(ctx context.Context, skip, limit int) ([]hmodels.HentaiEntry, error) {
	f.lastSkip, f.lastLimit = skip, limit
	return f.recent, nil
}
func (f *fakeHentaiStore) GetHentaiTop(ctx context.Context, genreNorm string, skip, limit int) ([]hmodels.HentaiEntry, error) {
	f.lastGenreNorm = genreNorm
	f.lastSkip, f.lastLimit = skip, limit
	return f.top, nil
}
func (f *fakeHentaiStore) GetHentaiAll(ctx context.Context, genreNorm string, skip, limit int) ([]hmodels.HentaiEntry, error) {
	f.lastGenreNorm = genreNorm
	f.lastSkip, f.lastLimit = skip, limit
	return f.all, nil
}
func (f *fakeHentaiStore) GetHentaiByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]hmodels.HentaiEntry, error) {
	f.lastStudioNorm = studioNorm
	f.lastSkip, f.lastLimit = skip, limit
	return f.studio, nil
}
func (f *fakeHentaiStore) GetHentaiByYear(ctx context.Context, year string, skip, limit int) ([]hmodels.HentaiEntry, error) {
	f.lastYear = year
	f.lastSkip, f.lastLimit = skip, limit
	return f.year, nil
}
func (f *fakeHentaiStore) SearchHentai(ctx context.Context, query string, skip, limit int) ([]hmodels.HentaiEntry, error) {
	f.lastSearchQ = query
	f.lastSkip, f.lastLimit = skip, limit
	return f.search, nil
}
func (f *fakeHentaiStore) GetHentaiEntry(ctx context.Context, id string) (*hmodels.HentaiEntry, error) {
	return f.byID, nil
}

func hentry(id, title, poster string, rating float64, prefix string) hmodels.HentaiEntry {
	return hmodels.HentaiEntry{
		ID: id, Prefix: prefix, Title: title, Poster: poster,
		Background: poster, Excerpt: "syn", ReleaseYear: "2024",
		Studio: "Studio X", Genres: []string{"Harem"}, Rating: rating,
		Episodes: []hmodels.HentaiEpisode{{Number: 1, Title: "Ep 1", Slug: "ep-1"}},
	}
}

func TestServeHentaiCatalogRecent(t *testing.T) {
	f := &fakeHentaiStore{recent: []hmodels.HentaiEntry{hentry("hmm-a", "A", "p", 9.9, "hmm")}}
	h := &Handler{Hentai: f}
	resp, err := h.serveHentaiCatalog(context.Background(), "hentai_new", "", "", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(resp.Metas) != 1 || resp.Metas[0].ID != "hmm-a" {
		t.Fatalf("metas = %+v", resp.Metas)
	}
	if resp.Metas[0].Type != "series" {
		t.Fatalf("type = %q, want series", resp.Metas[0].Type)
	}
	if resp.Metas[0].ImdbRating != "9.9" {
		t.Fatalf("imdbRating = %q, want 9.9", resp.Metas[0].ImdbRating)
	}
	if resp.Metas[0].Background != "p" {
		t.Fatalf("background should fall back to poster: %q", resp.Metas[0].Background)
	}
}

func TestServeHentaiCatalogTopNormalizesGenre(t *testing.T) {
	f := &fakeHentaiStore{top: []hmodels.HentaiEntry{hentry("hmm-b", "B", "p", 8.0, "hmm")}}
	h := &Handler{Hentai: f}
	if _, err := h.serveHentaiCatalog(context.Background(), "hentai_top", "Big Boobs", "", 0); err != nil {
		t.Fatalf("err: %v", err)
	}
	if f.lastGenreNorm != "bigboobs" {
		t.Fatalf("genre queried with %q, want normalized bigboobs", f.lastGenreNorm)
	}
}

func TestServeHentaiCatalogStudiosNormalizes(t *testing.T) {
	f := &fakeHentaiStore{studio: []hmodels.HentaiEntry{hentry("hmm-c", "C", "p", 7.7, "hmm")}}
	h := &Handler{Hentai: f}
	if _, err := h.serveHentaiCatalog(context.Background(), "hentai_studios", "Studio X", "", 0); err != nil {
		t.Fatalf("err: %v", err)
	}
	if f.lastStudioNorm != "studiox" {
		t.Fatalf("studio queried with %q, want studiox", f.lastStudioNorm)
	}
}

func TestServeHentaiCatalogYearsPassesRaw(t *testing.T) {
	f := &fakeHentaiStore{year: []hmodels.HentaiEntry{hentry("hmm-d", "D", "p", 0, "hmm")}}
	h := &Handler{Hentai: f}
	if _, err := h.serveHentaiCatalog(context.Background(), "hentai_years", "2023", "", 0); err != nil {
		t.Fatalf("err: %v", err)
	}
	if f.lastYear != "2023" {
		t.Fatalf("year queried with %q, want 2023 (no normalization)", f.lastYear)
	}
}

func TestServeHentaiCatalogSearch(t *testing.T) {
	f := &fakeHentaiStore{search: []hmodels.HentaiEntry{hentry("hmm-e", "E", "p", 0, "hmm")}}
	h := &Handler{Hentai: f}
	resp, err := h.serveHentaiCatalog(context.Background(), "hentai_search", "", "kuroinu", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f.lastSearchQ != "kuroinu" || len(resp.Metas) != 1 {
		t.Fatalf("search dispatch: q=%q metas=%d", f.lastSearchQ, len(resp.Metas))
	}
}

func TestServeHentaiCatalogSearchEmptyReturnsEmpty(t *testing.T) {
	f := &fakeHentaiStore{}
	h := &Handler{Hentai: f}
	resp, _ := h.serveHentaiCatalog(context.Background(), "hentai_search", "", "   ", 0)
	if len(resp.Metas) != 0 {
		t.Fatalf("empty search query should serve empty, got %d", len(resp.Metas))
	}
}

func TestServeHentaiCatalogNilStoreEmpty(t *testing.T) {
	h := &Handler{}
	resp, _ := h.serveHentaiCatalog(context.Background(), "hentai_new", "", "", 0)
	if len(resp.Metas) != 0 {
		t.Fatalf("nil store should serve empty, got %d", len(resp.Metas))
	}
}

func TestServeHentaiMetaBuildsVideosAndRating(t *testing.T) {
	e := hentry("hmm-toshi-ie", "Toshi Densetsu", "p", 9.9, "hmm")
	e.Episodes = []hmodels.HentaiEpisode{
		{Number: 1, Title: "Ep 1", Slug: "ep-1", Released: "2024-01-01", Thumbnail: "t1"},
		{Number: 2, Title: "", Slug: "ep-2"},
	}
	f := &fakeHentaiStore{byID: &e}
	h := &Handler{Hentai: f}
	meta, err := h.serveHentaiMeta(context.Background(), "hmm-toshi-ie")
	if err != nil || meta == nil {
		t.Fatalf("meta: %v %v", meta, err)
	}
	if meta.ImdbRating != "9.9" {
		t.Fatalf("imdbRating = %q, want 9.9", meta.ImdbRating)
	}
	if len(meta.Videos) != 2 {
		t.Fatalf("videos = %d, want 2", len(meta.Videos))
	}
	if meta.Videos[0].ID != "hmm-toshi-ie:1:1" {
		t.Fatalf("video[0] id = %q, want hmm-toshi-ie:1:1", meta.Videos[0].ID)
	}
	if meta.Videos[0].Season != 1 || meta.Videos[0].Episode != 1 {
		t.Fatalf("video[0] season/episode = %d/%d", meta.Videos[0].Season, meta.Videos[0].Episode)
	}
	if meta.Videos[1].Title != "Episode 2" {
		t.Fatalf("empty episode title should fall back, got %q", meta.Videos[1].Title)
	}
	if len(meta.Genres) != 1 || meta.Genres[0] != "Harem" {
		t.Fatalf("genres = %+v", meta.Genres)
	}
}

func TestServeHentaiMetaNilStoreNil(t *testing.T) {
	h := &Handler{}
	if m, err := h.serveHentaiMeta(context.Background(), "hmm-none"); err != nil || m != nil {
		t.Fatalf("nil store should return nil meta, got %v %v", m, err)
	}
}

func TestServeHentaiMetaNotFound(t *testing.T) {
	f := &fakeHentaiStore{byID: nil}
	h := &Handler{Hentai: f}
	if m, _ := h.serveHentaiMeta(context.Background(), "hmm-missing"); m != nil {
		t.Fatalf("missing entry should return nil meta, got %+v", m)
	}
}