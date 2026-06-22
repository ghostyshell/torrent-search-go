package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"torrent-search-go/internal/models"
)

const sampleSukebeiRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:nyaa="https://sukebei.nyaa.si/xmlns/nyaa" version="2.0">
  <channel>
    <item>
      <title>FC2-PPV-123 Test Release</title>
      <link>https://sukebei.nyaa.si/download/100.torrent</link>
      <guid isPermaLink="true">https://sukebei.nyaa.si/view/100</guid>
      <nyaa:seeders>42</nyaa:seeders>
      <nyaa:leechers>3</nyaa:leechers>
      <nyaa:infoHash>aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</nyaa:infoHash>
      <nyaa:size>1.2 GiB</nyaa:size>
      <nyaa:category>Real Life - Videos</nyaa:category>
    </item>
    <item>
      <title>Older Release</title>
      <link>https://sukebei.nyaa.si/download/99.torrent</link>
      <guid isPermaLink="true">https://sukebei.nyaa.si/view/99</guid>
      <nyaa:seeders>5</nyaa:seeders>
      <nyaa:leechers>1</nyaa:leechers>
      <nyaa:infoHash>bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb</nyaa:infoHash>
      <nyaa:size>800 MiB</nyaa:size>
    </item>
  </channel>
</rss>`

func TestSukebeiBrowseSortsBySeeders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "rss" {
			t.Fatalf("expected RSS request, got %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(sampleSukebeiRSS))
	}))
	defer srv.Close()

	scraper := NewSukebeiScraper(srv.Client())
	scraper.baseURL = srv.URL

	got, err := scraper.Browse(context.Background(), "0_0", 1, "7", models.SearchOptions{})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(got))
	}
	if got[0].Seeders < got[1].Seeders {
		t.Fatalf("expected seeders desc, got %d then %d", got[0].Seeders, got[1].Seeders)
	}
	if got[0].Website != "sukebei" || got[0].MagnetLink == "" {
		t.Fatalf("unexpected torrent: %+v", got[0])
	}
}
