package stremio

import (
	"context"
	"strings"
	"testing"
)

func TestServeMetaSukebeiWithoutStashKeyUsesPlaceholder(t *testing.T) {
	id := EncodeItemID(TorrentRecord{
		Title:     "IPZZ-882 Test",
		InfoHash:  "ac09c79adfe4fd2160a8f64061fcb9d35e428e6c",
		Website:   "sukebei",
		DetailURL: "https://sukebei.nyaa.si/view/4630275",
	})

	h := &Handler{}
	resp, err := h.ServeMeta(context.Background(), Config{}, "Porn", id)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Meta == nil {
		t.Fatal("expected meta")
	}
	if !strings.HasPrefix(resp.Meta.Poster, "data:image/svg+xml") {
		t.Fatalf("expected placeholder poster without stash key, got %q", resp.Meta.Poster[:min(40, len(resp.Meta.Poster))])
	}
	if resp.Meta.PosterShape != "landscape" {
		t.Fatalf("expected landscape poster shape, got %q", resp.Meta.PosterShape)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
