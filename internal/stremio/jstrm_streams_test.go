package stremio

import (
	"context"
	"testing"
)

// TestJstrmStreamsWithHash emits the carried infoHash directly (no scraper needed).
func TestJstrmStreamsWithHash(t *testing.T) {
	id := EncodeItemID(TorrentRecord{
		InfoHash: "abcdef0123456789abcdef0123456789abcdef01",
		Title:    "Studio.26.06.23.Scene.PRT",
		Website:  "pornrips",
	})
	h := &Handler{}
	streams := h.jstrmStreams(context.Background(), id)
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if got := streams[0]["infoHash"]; got != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("infoHash = %v, want lowercased hash", got)
	}
	if got := streams[0]["name"]; got != "PRT" {
		t.Errorf("name = %v, want PRT for pornrips", got)
	}
	if got := streams[0]["title"]; got != "Studio.26.06.23.Scene.PRT" {
		t.Errorf("title = %v, want original title", got)
	}
}

// TestJstrmStreamsPornripsNoScraper returns nil (no panic) when a pornrips
// catalog ID carries only a detailUrl and no scraper is wired.
func TestJstrmStreamsPornripsNoScraper(t *testing.T) {
	id := EncodeItemID(TorrentRecord{
		Title:     "Studio.26.06.23.Scene.PRT",
		Website:   "pornrips",
		DetailURL: "https://pornrips.to/some-slug/",
	})
	h := &Handler{}
	if streams := h.jstrmStreams(context.Background(), id); streams != nil {
		t.Fatalf("expected nil streams when Scrapers is nil, got %v", streams)
	}
}

// TestJstrmStreamsBogusID returns nil for a non-jstrm / undecodable id.
func TestJstrmStreamsBogusID(t *testing.T) {
	h := &Handler{}
	if streams := h.jstrmStreams(context.Background(), "jstrm:!!!not-base64!!!"); streams != nil {
		t.Fatalf("expected nil for undecodable id, got %v", streams)
	}
	if streams := h.jstrmStreams(context.Background(), "porndb:123"); streams != nil {
		t.Fatalf("expected nil for non-jstrm id, got %v", streams)
	}
}

// TestJstrmGroupStreams emits one stream per member that carries an infoHash.
func TestJstrmGroupStreams(t *testing.T) {
	id := EncodeGroupID([]TorrentRecord{
		{InfoHash: "1111111111111111111111111111111111111111", Title: "Scene 4K", Website: "pornrips", Quality: "4k"},
		{InfoHash: "2222222222222222222222222222222222222222", Title: "Scene 1080p", Website: "pornrips", Quality: "fhd"},
	})
	h := &Handler{}
	streams := h.jstrmOrGroupStreams(context.Background(), id)
	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}
}

// TestJstrmGroupStreamsBogus returns nil for an undecodable group id.
func TestJstrmGroupStreamsBogus(t *testing.T) {
	h := &Handler{}
	if streams := h.jstrmOrGroupStreams(context.Background(), "jstrg:!!!not-base64!!!"); streams != nil {
		t.Fatalf("expected nil for undecodable group id, got %v", streams)
	}
}

// TestJstrmGroupStreamsPornripsLabels pins the user-facing behavior for a
// PornRips scene group: one stream per resolution variant, each labeled "PRT"
// with its own infoHash and the full dotted filename (which carries 720p/1080p)
// as the title so Stremio shows two distinguishable stream rows.
func TestJstrmGroupStreamsPornripsLabels(t *testing.T) {
	id := EncodeGroupID([]TorrentRecord{
		{InfoHash: "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1", Title: "Studio.25.06.25.Scene.PRT.1080p.WEB-DL.x265", Website: "pornrips"},
		{InfoHash: "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2", Title: "Studio.25.06.25.Scene.PRT.720p.WEB-DL.x264", Website: "pornrips"},
	})
	h := &Handler{}
	streams := h.jstrmOrGroupStreams(context.Background(), id)
	if len(streams) != 2 {
		t.Fatalf("expected 2 streams (one per resolution), got %d", len(streams))
	}
	seen := make(map[string]bool, 2)
	for _, s := range streams {
		if s["name"] != "PRT" {
			t.Fatalf("stream name = %v, want PRT", s["name"])
		}
		ih, _ := s["infoHash"].(string)
		if ih == "" || seen[ih] {
			t.Fatalf("duplicate/empty infoHash: %v", s["infoHash"])
		}
		seen[ih] = true
		if got, _ := s["title"].(string); got == "" {
			t.Fatalf("stream title empty, want the dotted filename")
		}
	}
}