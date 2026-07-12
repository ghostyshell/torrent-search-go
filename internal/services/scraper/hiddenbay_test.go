package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"torrent-search-go/internal/models"
)

// tpbSearchPage is a minimal TPB search-results page the parser accepts: a
// #searchResult table with one detLink row, a magnet sibling, a detDesc line,
// and seeder/leecher cells. base is substituted into the relative torrent link.
func tpbSearchPage(name, hash string) string {
	return `<html><body><table id="searchResult"><tr>
  <td><div class="detName"><a class="detLink" href="/torrent/1/` + name + `">` + name + `</a></div>
  <a href="magnet:?xt=urn:btih:` + hash + `">magnet</a>
  <font class="detDesc">Uploaded 2024-01-01, ULed by uploader</font></td>
  <td>5</td><td>2</td></tr></table></body></html>`
}

// Primary returning a Cloudflare-style interstitial (200, no #searchResult table)
// must make the scraper fall through to the next mirror, whose results win and
// whose host is used for absolute torrent URLs.
func TestHiddenBayFailoverOnBlock(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>cf challenge</body></html>`))
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(tpbSearchPage("FooTitle", "aaa")))
	}))
	defer fallback.Close()

	s := NewHiddenBayScraper(http.DefaultClient, primary.URL+","+fallback.URL)
	// Pin baseURLs to only the test servers so a failover never reaches the
	// baked-in production mirrors (thepiratebay0.org / piratebay.live).
	s.baseURLs = []string{primary.URL, fallback.URL}
	got, err := s.Search(context.Background(), "foo", 1, models.SearchOptions{Category: HBCategoryAll})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "FooTitle" {
		t.Fatalf("failover results = %+v, want 1 row named FooTitle", got)
	}
	if !strings.HasPrefix(got[0].MagnetLink, "magnet:") {
		t.Fatalf("magnet = %q", got[0].MagnetLink)
	}
	// TorrentURL must be built against the fallback host that answered, not the
	// blocked primary, so later detail fetches hit a live mirror.
	if !strings.HasPrefix(got[0].TorrentURL, fallback.URL) {
		t.Fatalf("TorrentURL = %q, want fallback host %s", got[0].TorrentURL, fallback.URL)
	}
}

// A genuine zero-result page (200, table present, no rows) must NOT trigger
// failover: the scraper returns the primary's empty set rather than hitting the
// baked-in production mirrors.
func TestHiddenBayNoFailoverOnEmptyResults(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><table id="searchResult"></table></body></html>`))
	}))
	defer primary.Close()

	s := NewHiddenBayScraper(http.DefaultClient, primary.URL)
	s.baseURLs = []string{primary.URL} // isolate from baked-in production mirrors
	got, err := s.Search(context.Background(), "zzqqxxwwzzqq", 1, models.SearchOptions{Category: HBCategoryAll})
	if err != nil {
		t.Fatalf("Search error on empty results: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty search returned %d rows, want 0 (no failover)", len(got))
	}
}

// A detail page that is a Cloudflare "Just a moment..." challenge (200, a bare
// <h1>, no magnet) must fail over to a mirror serving the real detail page with
// a magnet. Guards against detailPageOK accepting challenge pages.
func TestHiddenBayDetailsFailoverOnChallenge(t *testing.T) {
	challenge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><h1>Just a moment...</h1></body></html>`))
	}))
	defer challenge.Close()
	realHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
			<h1 id="title">Real Torrent</h1>
			<a href="magnet:?xt=urn:btih:bbbb">magnet</a>
			<dl><dt>Size</dt><dd>1.4 GB</dd></dl>
			</body></html>`))
	}))
	defer realHost.Close()

	s := NewHiddenBayScraper(http.DefaultClient, challenge.URL+","+realHost.URL)
	s.baseURLs = []string{challenge.URL, realHost.URL}
	// Torrent URL points at the (blocked) challenge host; GetDetails should retry
	// the same path on the real host and parse its magnet.
	d, err := s.GetDetails(context.Background(), challenge.URL+"/torrent/1/foo")
	if err != nil {
		t.Fatalf("GetDetails error: %v", err)
	}
	if d.MagnetLink != "magnet:?xt=urn:btih:bbbb" {
		t.Fatalf("magnet = %q, want the real host's magnet (failover did not happen)", d.MagnetLink)
	}
	if d.Name != "Real Torrent" {
		t.Fatalf("name = %q, want Real Torrent", d.Name)
	}
}
