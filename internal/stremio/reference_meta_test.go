package stremio

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"torrent-search-go/internal/services/metadata"
)

func TestPornripsSlug(t *testing.T) {
	slug := PornripsSlug("https://pornrips.to/some-release-1080p-prt/")
	if slug != "some-release-1080p-prt" {
		t.Fatalf("slug = %q", slug)
	}
	if PornripsSlug("https://example.com/x") != "" {
		t.Fatal("expected empty slug for non-pornrips URL")
	}
}

func TestReferenceMetaCacheNegative(t *testing.T) {
	// Exercise cache entry serialization used by reference meta lookups.
	entry := referenceMetaCacheEntry{Found: false}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded referenceMetaCacheEntry
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Found {
		t.Fatal("expected negative cache entry")
	}
}

func TestReferenceToMerged(t *testing.T) {
	ref := &metadata.ReferenceMeta{Name: "Scene", Poster: "http://x/p.jpg", Year: "2024"}
	merged := referenceToMerged(ref)
	if merged == nil || merged.Title != "Scene" || merged.Poster != "http://x/p.jpg" {
		t.Fatalf("unexpected merged: %+v", merged)
	}
}

func TestReferenceMetaForSlugCacheOnly(t *testing.T) {
	h := &Handler{}
	if got := h.referenceMetaForSlug(context.Background(), "", false); got != nil {
		t.Fatal("expected nil for empty slug")
	}
	_ = ttlRefNegative
	_ = time.Second
}
