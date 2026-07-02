package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"torrent-search-go/internal/models"
)

const samplePornripsHTML = `<!doctype html><html><body>
<section id="primary">
  <article>
    <header><h2><a href="https://pornrips.to/test-scene-1080p-prt/">Test Scene 1080p</a></h2></header>
    <div class="wrapper-excerpt-content"><p>Size: 2.5 GB</p></div>
  </article>
  <article>
    <header><h2><a href="/another-scene-4k-prt/">Another Scene 4K</a></h2></header>
    <p>1.2 GiB</p>
  </article>
</section>
</body></html>`

func TestPornripsFetchListingsListingOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(samplePornripsHTML))
	}))
	defer srv.Close()

	sc := NewPornripsScraper(srv.Client())
	sc.baseURL = srv.URL

	got, err := sc.Browse(context.Background(), "all", 1, "", models.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].MagnetLink != "" || got[0].TorrentURL != "" {
		t.Fatal("expected listing-only rows without download links")
	}
	if got[0].UploadedBy != "https://pornrips.to/test-scene-1080p-prt/" {
		t.Fatalf("detail url = %q", got[0].UploadedBy)
	}
	if got[1].UploadedBy != srv.URL+"/another-scene-4k-prt/" {
		t.Fatalf("relative href = %q", got[1].UploadedBy)
	}
}
