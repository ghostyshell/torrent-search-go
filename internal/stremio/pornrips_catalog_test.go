package stremio

import (
	"testing"

	"torrent-search-go/internal/models"
)

func TestNormalizeModelTorrentPornripsDetailURL(t *testing.T) {
	got := normalizeModelTorrent(models.Torrent{
		Name:       "Scene",
		UploadedBy: "https://pornrips.to/some-slug/",
		Website:    "pornrips",
	})
	if got.DetailURL != "https://pornrips.to/some-slug/" {
		t.Fatalf("DetailURL = %q", got.DetailURL)
	}
	if got.Website != "pornrips" {
		t.Fatalf("Website = %q", got.Website)
	}
}

func TestNormalizePornripsSearchQuery(t *testing.T) {
	dotted := "MrLuckyPOV.2026.Sophia.Locke.Aria.Six.Horny.XXX.720p.HEVC.x265.PRT"
	got := normalizePornripsSearchQuery(dotted)
	want := "MrLuckyPOV 2026 Sophia Locke Aria Six Horny XXX 720p HEVC x265 PRT"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if normalizePornripsSearchQuery("Legal Porno") != "Legal Porno" {
		t.Fatal("short query should be unchanged")
	}
}
