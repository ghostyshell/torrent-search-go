package stremio

import (
	"testing"

	"torrent-search-go/internal/models"
)

func TestDedupeSukebeiTorrents(t *testing.T) {
	raw := []models.Torrent{
		{Name: "A", MagnetLink: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{Name: "B", MagnetLink: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{Name: "C", MagnetLink: "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	got := dedupeSukebeiTorrents(raw)
	if len(got) != 2 {
		t.Fatalf("deduped len = %d, want 2", len(got))
	}
}
