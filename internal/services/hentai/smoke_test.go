package hentai

import (
	"context"
	"os"
	"testing"
	"time"
)

// Smoke test against the live HentaiMama endpoint. Skipped unless
// HENTAI_SMOKE=1, so it never runs in CI. Run manually to validate the scraper
// after selector/site changes:
//
//	HENTAI_SMOKE=1 go test ./internal/services/hentai/ -run TestSmoke -v -timeout 120s
func TestSmoke(t *testing.T) {
	if os.Getenv("HENTAI_SMOKE") != "1" {
		t.Skip("set HENTAI_SMOKE=1 to run live scraper smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	svc := NewService(nil)

	// HentaiMama: list page 1, fetch first series, resolve its first episode.
	mama, err := svc.ListSeries(ctx, "hentaimama", 1)
	if err != nil {
		t.Fatalf("mama ListSeries: %v", err)
	}
	t.Logf("mama list page1: %d series", len(mama))
	if len(mama) == 0 {
		t.Fatal("mama: no series listed - selectors broken?")
	}
	t.Logf("mama first: slug=%q title=%q poster=%q", mama[0].Slug, mama[0].Title, mama[0].Poster)
	d, err := svc.FetchSeries(ctx, "hentaimama", mama[0].Slug)
	if err != nil || d == nil {
		t.Fatalf("mama FetchSeries(%q): %v", mama[0].Slug, err)
	}
	t.Logf("mama series: title=%q year=%q studio=%q genres=%v episodes=%d rating=%.1f",
		d.Title, d.ReleaseYear, d.Studio, d.Genres, len(d.Episodes), d.Rating)
	if len(d.Episodes) > 0 {
		t.Logf("mama ep[0]: slug=%q num=%d", d.Episodes[0].Slug, d.Episodes[0].Number)
		streams, serr := svc.ResolveEpisodeStream(ctx, "hmm", d.Episodes[0].Slug, d.Episodes[0].SourceURL)
		if serr != nil {
			t.Logf("mama resolve ep[0]: err=%v", serr)
		} else {
			for _, s := range streams {
				t.Logf("mama stream: %s [%s]", s.URL, s.Quality)
			}
		}
	}
}