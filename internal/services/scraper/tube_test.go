package scraper

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"torrent-search-go/pkg/models"
)

const samplePerverzijaWPJSON = `[{
"id":1252962,"date_gmt":"2026-07-10T23:23:04","slug":"agirlknows-ariela-donovan-and-leanne-lace-step-sister-fun",
"link":"https://tube.perverzija.com/agirlknows-ariela-donovan-and-leanne-lace-step-sister-fun/",
"title":{"rendered":"AGirlKnows &#8211; Ariela Donovan and Leanne Lace &#8211; Step-Sister Fun"},
"excerpt":{"rendered":"<p>Beautiful young babes Ariela and Lee Anne Lace want to practice [&hellip;]<\/p>"},
"_embedded":{"wp:featuredmedia":[{"source_url":"https://tube.perverzija.com/wp-content/uploads/2020/11/bg-step-sister.webp"}],
"wp:term":[
  [{"taxonomy":"category","name":"AGirlKnows","slug":"agirlknows"},{"taxonomy":"category","name":"LetsDoeIt","slug":"letsdoeit"}],
  [{"taxonomy":"post_tag","name":"Lesbian","slug":"lesbian"},{"taxonomy":"post_tag","name":"Kissing","slug":"kissing"}]
]}}]`

func TestPerverzijaIngestPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/posts") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(samplePerverzijaWPJSON))
	}))
	defer srv.Close()

	sc := NewPerverzijaScraper(srv.Client())
	sc.baseURL = srv.URL

	got, err := sc.IngestPage(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	e := got[0]
	if e.Slug != "agirlknows-ariela-donovan-and-leanne-lace-step-sister-fun" {
		t.Errorf("slug: %q", e.Slug)
	}
	if e.Title != "AGirlKnows - Ariela Donovan and Leanne Lace - Step-Sister Fun" {
		t.Errorf("title (entities decoded, dash kept): %q", e.Title)
	}
	if e.Date != "2026-07-10T23:23:04" {
		t.Errorf("date: %q", e.Date)
	}
	if e.WpPoster != "https://tube.perverzija.com/wp-content/uploads/2020/11/bg-step-sister.webp" {
		t.Errorf("wp poster: %q", e.WpPoster)
	}
	if !contains(e.Studios, "LetsDoeIt") || !contains(e.Studios, "AGirlKnows") {
		t.Errorf("studios (both wp categories): %v", e.Studios)
	}
	if !contains(e.Tags, "Lesbian") {
		t.Errorf("tags: %v", e.Tags)
	}
	if strings.Contains(e.Excerpt, "[&hellip;]") || strings.Contains(e.Excerpt, "<p>") {
		t.Errorf("excerpt not cleaned: %q", e.Excerpt)
	}
}

const samplePerverzijaDetail = `<!doctype html><html><body>
<meta property="og:image" content="https://tube.perverzija.com/wp-content/uploads/2020/11/bg-step-sister.webp"/>
<meta property="og:description" content="A steamy lesbian show with Ariela and Leanne."/>
<iframe src="https://j2.xtremestream.xyz/player/index.php?data=94bdf49dcb9b7357c377c7310c411343"></iframe>
<a href="https://tube.perverzija.com/stars/ariela-donovan/" rel="tag">Ariela Donovan</a>
<a href="https://tube.perverzija.com/stars/leanne-lace/" rel="tag">Leanne Lace</a>
<a href="https://tube.perverzija.com/stars/nicolette-shea/" class="menu-link sub-menu-link">Nicolette Shea</a>
</body></html>`

func TestPerverzijaEnrichEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(samplePerverzijaDetail))
	}))
	defer srv.Close()

	sc := NewPerverzijaScraper(srv.Client())
	e := &models.PerverzijaEntry{DetailURL: srv.URL + "/scene"}
	if err := sc.EnrichEntry(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if !contains(e.Performers, "Ariela Donovan") || !contains(e.Performers, "Leanne Lace") {
		t.Errorf("performers (rel=tag only, not menu): %v", e.Performers)
	}
	if contains(e.Performers, "Nicolette Shea") {
		t.Errorf("menu-link star leaked into performers: %v", e.Performers)
	}
	if e.StreamHash != "94bdf49dcb9b7357c377c7310c411343" {
		t.Errorf("stream hash: %q", e.StreamHash)
	}
	if e.Poster != "https://tube.perverzija.com/wp-content/uploads/2020/11/bg-step-sister.webp" {
		t.Errorf("poster: %q", e.Poster)
	}
	if e.Description != "A steamy lesbian show with Ariela and Leanne." {
		t.Errorf("description: %q", e.Description)
	}
	if !e.DetailScraped {
		t.Error("DetailScraped not set")
	}
}

const sampleMasterM3U8 = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=709080,RESOLUTION=854x480,CLOSED-CAPTIONS=NONE
https://j2.xtremestream.xyz/player/xs1.php?data=HASH&q=480
#EXT-X-STREAM-INF:BANDWIDTH=1423113,RESOLUTION=1280x720,CLOSED-CAPTIONS=NONE
https://j2.xtremestream.xyz/player/xs1.php?data=HASH&q=720
#EXT-X-STREAM-INF:BANDWIDTH=3202679,RESOLUTION=1920x1080,CLOSED-CAPTIONS=NONE
https://j2.xtremestream.xyz/player/xs1.php?data=HASH&q=1080
`

func TestParseHLSVariants(t *testing.T) {
	got := parseHLSVariants([]byte(sampleMasterM3U8), "Perverzija", "https://j2.xtremestream.xyz/player/xs1.php?data=HASH")
	if len(got) != 3 {
		t.Fatalf("want 3 variants, got %d", len(got))
	}
	if got[0].Quality != "1080p" {
		t.Errorf("best-first: want 1080p, got %q", got[0].Quality)
	}
	if got[2].Quality != "480p" {
		t.Errorf("last: want 480p, got %q", got[2].Quality)
	}
	if got[0].URL != "https://j2.xtremestream.xyz/player/xs1.php?data=HASH&q=1080" {
		t.Errorf("variant url: %q", got[0].URL)
	}
}

// TestParseHLSVariantsRejectsInternalURL locks the SSRF guard on the stream
// resolver: a variant line pointing at a cloud-metadata / loopback / private
// host is dropped, never emitted to Stremio (which would fetch it via
// proxyHeaders on the user's behalf).
func TestParseHLSVariantsRejectsInternalURL(t *testing.T) {
	body := []byte("#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=854x480\n" +
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2,RESOLUTION=1280x720\n" +
		"http://localhost:8080/admin\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=3,RESOLUTION=1920x1080\n" +
		"https://j2.xtremestream.xyz/player/xs1.php?data=HASH&q=1080\n")
	got := parseHLSVariants(body, "Perverzija", "https://j2.xtremestream.xyz/player/xs1.php?data=HASH")
	if len(got) != 1 {
		t.Fatalf("want 1 safe variant (internal URLs dropped), got %d: %v", len(got), got)
	}
	if got[0].Quality != "1080p" {
		t.Errorf("only the public variant should survive: %q", got[0].Quality)
	}
}

const sampleFreepornvideosCards = `<!doctype html><html><body>
<div class="item">
  <a href="https://www.freepornvideos.xxx/videos/93816671/thr3e-10-obedience-and-desire/" target="_blank" title="Thr3e #10 - Obedience and desire" class="thumb_img">
    <div class="img thumb__img"><img class="thumb thumb_img" src="https://img.freepornvideos.xxx/93816000/93816671/medium@2x/1.jpg" alt="Thr3e #10 - Obedience and desire" width="744" height="420"/>
    <span class="duration">Full Video 33:21</span></div>
  </a>
  <div class="item-info">
    <a class="thumb_title" href="https://www.freepornvideos.xxx/videos/93816671/thr3e-10-obedience-and-desire/" title="Thr3e #10 - Obedience and desire"><strong class="title">Thr3e #10 - Obedience and desire</strong></a>
    <div class="models">
      <a class="models__item thumb_cs" href="https://www.freepornvideos.xxx/sites/dorcel-club/"><span>Dorcel Club</span></a>
      <a class="models__item thumb_model" href="https://www.freepornvideos.xxx/models/kristof-cale/"><span>Kristof Cale</span></a>
      <a class="models__item thumb_model" href="https://www.freepornvideos.xxx/models/bella-mur/"><span>Bella Mur</span></a>
    </div>
    <div class="wrap">
      <div class="k4"><span class="icon"></span></div>
      <div class="rating positive ">100%</div>
      <div class="views">2.5K</div>
    </div>
  </div>
</div>
<div class="item">
  <a href="https://www.freepornvideos.xxx/some-sponsor-ad/">Sponsor Ad</a>
</div>
</body></html>`

func TestFreepornvideosIngestPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFreepornvideosCards))
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	sc.baseURL = srv.URL

	got, err := sc.IngestPage(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 video card (ad filtered), got %d", len(got))
	}
	e := got[0]
	if e.VideoID != "93816671" {
		t.Errorf("videoID: %q", e.VideoID)
	}
	if e.Slug != "thr3e-10-obedience-and-desire" {
		t.Errorf("slug: %q", e.Slug)
	}
	if e.Studio != "Dorcel Club" {
		t.Errorf("studio (thumb_cs): %q", e.Studio)
	}
	if !contains(e.Performers, "Kristof Cale") || !contains(e.Performers, "Bella Mur") {
		t.Errorf("performers (thumb_model): %v", e.Performers)
	}
	if e.Duration != "33:21" {
		t.Errorf("duration (Full Video prefix stripped): %q", e.Duration)
	}
	if !e.Has4K {
		t.Error("Has4K should be true (div.k4 present)")
	}
	if e.Rating != "100%" {
		t.Errorf("rating: %q", e.Rating)
	}
	if e.Views != "2.5K" {
		t.Errorf("views: %q", e.Views)
	}
	if e.Poster != "https://img.freepornvideos.xxx/93816000/93816671/medium@2x/1.jpg" {
		t.Errorf("poster: %q", e.Poster)
	}
}

const sampleFreepornvideosDetail = `<!doctype html><html><body>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"VideoObject","name":"Thr3e #10",
"uploadDate":"2026-07-10T00:00:00Z","duration":"PT0H33M21S",
"embedUrl":"https://www.freepornvideos.xxx/embed/93816671"}
</script>
<meta property="og:description" content="A steamy threesome scene."/>
<a class="btn_tag" class="link" href="https://www.freepornvideos.xxx/categories/anal/">Anal</a>
<a class="btn_tag" class="link" href="https://www.freepornvideos.xxx/categories/threesome/">Threesome</a>
<a class="btn_sponsor_group" href="https://www.freepornvideos.xxx/networks/dorcel/"><span>Dorcel Club</span></a>
<video class="video-js" poster="https://img.freepornvideos.xxx/93816000/93816671/medium@2x/1.jpg">
<source src='https://www.freepornvideos.xxx/get_file/8512/TOKEN1/93816000/93816671/93816671_2160m.mp4/' type='video/mp4' label="2160p">
<source src='https://www.freepornvideos.xxx/get_file/8512/TOKEN2/93816000/93816671/93816671_720m.mp4/' type='video/mp4' label="720p" selected="true">
<source src='https://www.freepornvideos.xxx/get_file/8512/TOKEN3/93816000/93816671/93816671_480m.mp4/' type='video/mp4' label="480p">
</video>
</body></html>`

func TestFreepornvideosEnrichEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFreepornvideosDetail))
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	e := &models.FreepornvideosEntry{DetailURL: srv.URL + "/scene"}
	if err := sc.EnrichEntry(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if e.Date != "2026-07-10T00:00:00Z" {
		t.Errorf("date (JSON-LD uploadDate): %q", e.Date)
	}
	if e.Duration != "00:33:21" {
		t.Errorf("duration (ISO8601 -> HH:MM:SS): %q", e.Duration)
	}
	if !contains(e.Categories, "Anal") || !contains(e.Categories, "Threesome") {
		t.Errorf("categories (btn_tag): %v", e.Categories)
	}
	if e.Network != "Dorcel Club" {
		t.Errorf("network (btn_sponsor_group): %q", e.Network)
	}
	if !e.DetailScraped {
		t.Error("DetailScraped not set")
	}
}

func TestFreepornvideosResolveStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFreepornvideosDetail))
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	e := models.FreepornvideosEntry{DetailURL: srv.URL + "/scene"}
	got, err := sc.ResolveStream(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 mp4 streams, got %d", len(got))
	}
	if got[0].Quality != "2160p" {
		t.Errorf("best-first: want 2160p, got %q", got[0].Quality)
	}
	if !strings.Contains(got[0].URL, "93816671_2160m.mp4") {
		t.Errorf("2160p url: %q", got[0].URL)
	}
}

// TestFreepornvideosResolveStreamRejectsInternalURL locks the SSRF guard: a
// <source> pointing at a private/loopback/metadata host is dropped before it
// reaches Stremio (which would fetch it via proxyHeaders on the user's behalf).
func TestFreepornvideosResolveStreamRejectsInternalURL(t *testing.T) {
	const detail = `<!doctype html><html><body>
<video class="video-js">
<source src='http://169.254.169.254/latest/meta-data/' type='video/mp4' label="2160p">
<source src='http://192.168.1.1/router-config' type='video/mp4' label="1080p">
<source src='https://www.freepornvideos.xxx/get_file/8512/TOK/93816000/93816671/93816671_720m.mp4/' type='video/mp4' label="720p">
</video>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(detail))
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	e := models.FreepornvideosEntry{DetailURL: srv.URL + "/scene"}
	got, err := sc.ResolveStream(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 safe stream (internal URLs dropped), got %d: %v", len(got), got)
	}
	if got[0].Quality != "720p" {
		t.Errorf("only the public source should survive: %q", got[0].Quality)
	}
}

// TestPerverzijaEnrichEntryGone locks the permanent-404 handling: a deleted post
// (HTTP 410/404) is marked detail_scraped and returns no error so the enrich sweep
// does not re-fetch it every tick (which would livelock the newest-first queue as
// 404s accumulate). Transient 5xx is covered by the retry path (error returned,
// detail_scraped left false) - not asserted here since the success path already
// exercises non-error returns.
func TestPerverzijaEnrichEntryGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	sc := NewPerverzijaScraper(srv.Client())
	e := &models.PerverzijaEntry{DetailURL: srv.URL + "/deleted"}
	if err := sc.EnrichEntry(context.Background(), e); err != nil {
		t.Fatalf("gone page should not return error, got %v", err)
	}
	if !e.DetailScraped {
		t.Error("DetailScraped should be true so a deleted post is not retried every tick")
	}
}

// TestFreepornvideosEnrichEntryGone mirrors TestPerverzijaEnrichEntryGone.
func TestFreepornvideosEnrichEntryGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	e := &models.FreepornvideosEntry{DetailURL: srv.URL + "/deleted"}
	if err := sc.EnrichEntry(context.Background(), e); err != nil {
		t.Fatalf("gone page should not return error, got %v", err)
	}
	if !e.DetailScraped {
		t.Error("DetailScraped should be true so a deleted post is not retried every tick")
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// TestFreepornvideosIngestPageGone locks the feed-tail behavior: pages past the
// archive end return 404 (errPageGone), and IngestPage must treat that as
// end-of-feed (empty slice, nil error) so the ingest loop sets hitEmpty and
// stops. Without this, the listing walk retries the gone page forever and never
// reaches hitEmpty - the livelock that bit the real backfill at page 6001.
func TestFreepornvideosIngestPageGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	sc.baseURL = srv.URL

	got, err := sc.IngestPage(context.Background(), 6001)
	if err != nil {
		t.Fatalf("gone listing page should be end-of-feed (nil err), got %v", err)
	}
	if got != nil {
		t.Fatalf("want nil entries for gone page, got %d", len(got))
	}
}

// TestFreepornvideosCardDropsCRLFFromSlug locks the header-injection fix: a card
// href whose slug carries &#13;&#10; (goquery decodes to literal CRLF) must not
// store CRLF in Slug/DetailURL, since the detail URL later becomes the Referer in
// proxyHeaders.request and unsanitized CRLF could inject a header on Stremio's
// fetch of the stream URL. Any slug with a control char (partial or all-CRLF) is
// dropped at ingestion; a clean slug is kept unchanged.
func TestFreepornvideosCardDropsCRLFFromSlug(t *testing.T) {
	// goquery decodes &#13;&#10; in the href attribute to literal \r\n.
	const cards = `<!doctype html><html><body>
<div class="item">
  <a href="https://www.freepornvideos.xxx/videos/123/legit&#13;&#10;X-Injected: evil/" title="T" class="thumb_img"><img class="thumb" src="https://img.freepornvideos.xxx/1/2/3.jpg"/></a>
</div>
<div class="item">
  <a href="https://www.freepornvideos.xxx/videos/456/&#13;&#10;&#13;&#10;/" title="T2" class="thumb_img"><img class="thumb" src="https://img.freepornvideos.xxx/1/3/4.jpg"/></a>
</div>
<div class="item">
  <a href="https://www.freepornvideos.xxx/videos/789/clean-slug/" title="T3" class="thumb_img"><img class="thumb" src="https://img.freepornvideos.xxx/1/4/5.jpg"/></a>
</div>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(cards))
	}))
	defer srv.Close()

	sc := NewFreepornvideosScraper(srv.Client())
	sc.baseURL = srv.URL

	got, err := sc.IngestPage(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry (both CRLF-slug cards dropped, clean card kept), got %d", len(got))
	}
	e := got[0]
	if e.VideoID != "789" || e.Slug != "clean-slug" {
		t.Errorf("only the clean card should survive: id=%q slug=%q", e.VideoID, e.Slug)
	}
	if strings.ContainsAny(e.DetailURL, "\r\n") {
		t.Errorf("detail URL has CRLF: %q", e.DetailURL)
	}
}

// TestResolveSafeStreamURLRejectsPrivateDNS locks the SSRF guard on the stream
// resolver: a hostname (not a literal IP, not in the metadata denylist) whose DNS
// resolves to a private/loopback/link-local/metadata IP is dropped before it
// leaves the addon, so a compromised upstream can't point an attacker-controlled
// A record at an internal service on the user's network (Stremio fetches the URL
// via proxyHeaders). A public resolve is accepted; a failed resolve is rejected
// (fail closed). The resolver is stubbed so the test needs no network.
func TestResolveSafeStreamURLRejectsPrivateDNS(t *testing.T) {
	orig := resolveStreamHost
	defer func() { resolveStreamHost = orig }()

	resolveStreamHost = func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	if _, ok := ResolveSafeStreamURL("https://evil.example.com/x", "https://www.freepornvideos.xxx/"); ok {
		t.Error("hostname resolving to link-local metadata IP must be rejected")
	}

	resolveStreamHost = func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.1.1")}, nil
	}
	if _, ok := ResolveSafeStreamURL("https://evil.example.com/x", "https://www.freepornvideos.xxx/"); ok {
		t.Error("hostname resolving to RFC1918 must be rejected")
	}

	// A round-robin with one private IP is rejected (any non-public fails).
	resolveStreamHost = func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("10.0.0.1")}, nil
	}
	if _, ok := ResolveSafeStreamURL("https://evil.example.com/x", "https://www.freepornvideos.xxx/"); ok {
		t.Error("round-robin with any private IP must be rejected")
	}

	// A fully public resolve is accepted.
	resolveStreamHost = func(_ context.Context, _ string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.1")}, nil
	}
	got, ok := ResolveSafeStreamURL("https://evil.example.com/x", "https://www.freepornvideos.xxx/")
	if !ok {
		t.Error("hostname resolving to a public IP should be accepted")
	}
	if !strings.HasPrefix(got, "https://evil.example.com/") {
		t.Errorf("accepted URL wrong: %q", got)
	}

	// A failed resolve is rejected (fail closed).
	resolveStreamHost = func(_ context.Context, _ string) ([]net.IP, error) {
		return nil, fmt.Errorf("no such host")
	}
	if _, ok := ResolveSafeStreamURL("https://evil.example.com/x", "https://www.freepornvideos.xxx/"); ok {
		t.Error("unresolvable host should be rejected (fail closed)")
	}
}
