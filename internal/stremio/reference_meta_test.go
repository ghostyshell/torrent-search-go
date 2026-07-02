package stremio

import "testing"

func TestPornripsSlug(t *testing.T) {
	slug := PornripsSlug("https://pornrips.to/some-release-1080p-prt/")
	if slug != "some-release-1080p-prt" {
		t.Fatalf("slug = %q", slug)
	}
	if PornripsSlug("https://example.com/x") != "" {
		t.Fatal("expected empty slug for non-pornrips URL")
	}
}