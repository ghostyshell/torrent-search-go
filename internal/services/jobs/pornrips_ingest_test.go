package jobs

import (
	"testing"

	"torrent-search-go/internal/services/metadata"
)

func TestPornripsEntryFromItem(t *testing.T) {
	item := metadata.ReferenceRecentItem{
		Slug: "some-release-slug",
		Date: "2026-06-24T10:00:00",
		Meta: &metadata.ReferenceMeta{
			Name:   "Studio Performer Title",
			Poster: "https://pornrips.to/wp-content/poster.jpg",
			Studio: "BrazzersExxtra",
		},
	}
	e := pornripsEntryFromItem(item)
	if e.Slug != "some-release-slug" {
		t.Fatalf("Slug = %q", e.Slug)
	}
	if e.Title != "Studio Performer Title" {
		t.Fatalf("Title = %q", e.Title)
	}
	if e.Studio != "BrazzersExxtra" {
		t.Fatalf("Studio = %q; want the WP post_tag carried through for pr_studio", e.Studio)
	}
	if e.StudioNorm != "brazzersexxtra" {
		t.Fatalf("StudioNorm = %q, want brazzersexxtra", e.StudioNorm)
	}
	if e.Date != "2026-06-24T10:00:00" {
		t.Fatalf("Date = %q; want the full WP date for the sort index", e.Date)
	}
	if e.WpPoster != "https://pornrips.to/wp-content/poster.jpg" {
		t.Fatalf("WpPoster = %q", e.WpPoster)
	}
	if e.MetaID != "pr:some-release-slug" {
		t.Fatalf("MetaID = %q", e.MetaID)
	}
	if e.DetailURL != "https://pornrips.to/some-release-slug/" {
		t.Fatalf("DetailURL = %q", e.DetailURL)
	}
}

func TestPornripsEntryFromItemNilMeta(t *testing.T) {
	e := pornripsEntryFromItem(metadata.ReferenceRecentItem{Slug: "no-meta"})
	if e.Title != "" || e.WpPoster != "" {
		t.Fatalf("nil Meta should leave Title/WpPoster empty: %+v", e)
	}
	if e.Slug != "no-meta" || e.MetaID != "pr:no-meta" {
		t.Fatalf("slug/metaID should still populate: %+v", e)
	}
}