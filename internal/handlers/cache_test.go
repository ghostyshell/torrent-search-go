package handlers

import "testing"

func TestMagnetCacheKeyMatchesNode(t *testing.T) {
	key := magnetCacheKey("1337x", "https://1337x.to/torrent/12345/some-slug/")
	expected := "magnet:1337x:aHR0cHM6Ly8xMzM3eC50by90b3JyZW50LzEyMzQ1L3NvbWUtc2x1Zy8="
	if key != expected {
		t.Fatalf("magnetCacheKey() = %q, want %q", key, expected)
	}
}

func TestCoverImagePathParam(t *testing.T) {
	got := coverImagePathParam("/api/cache/cover-image/favorite/fav-123", "favorite")
	if got != "fav-123" {
		t.Fatalf("favorite param = %q", got)
	}
	got = coverImagePathParam("/api/storage/cover-image/cached-link/link-9", "cached-link")
	if got != "link-9" {
		t.Fatalf("cached-link param = %q", got)
	}
}

func TestCoverImageTorrentDetailsParams(t *testing.T) {
	fav, src := coverImageTorrentDetailsParams("/api/cache/cover-image/torrent-details/fav-1/piratebay")
	if fav != "fav-1" || src != "piratebay" {
		t.Fatalf("torrent-details params = %q / %q", fav, src)
	}
}
