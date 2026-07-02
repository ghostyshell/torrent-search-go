package stremio

import "testing"

func TestPropagateMetaByJAVCode(t *testing.T) {
	torrents := []catalogTorrent{
		{Title: "ACHJ-030-C"},
		{Title: "ACHJ-030: Madonna Dengeki Exclusive - Sumire Mizukawa"},
		{Title: "ACHJ-030-FHD"},
	}
	metas := []MetaPreview{
		{ID: "a", Name: "ACHJ-030-C"},
		{
			ID:          "b",
			Name:        "ACHJ-030: Madonna Dengeki Exclusive",
			Poster:      "https://stashdb.org/images/example.jpg",
			Background:  "https://stashdb.org/images/example.jpg",
			Description: "Studio: Achijo",
			ReleaseInfo: "2023",
		},
		{ID: "c", Name: "ACHJ-030-FHD"},
	}

	propagateMetaByJAVCode(torrents, metas)

	if metas[0].Poster == "" || metas[2].Poster == "" {
		t.Fatalf("expected poster propagation, got %#v", metas)
	}
	if metas[0].Name != metas[1].Name {
		t.Errorf("ACHJ-030-C name = %q, want donor title", metas[1].Name)
	}
	if metas[2].Name != metas[1].Name {
		t.Errorf("ACHJ-030-FHD name = %q, want donor title", metas[1].Name)
	}
	if metas[1].Poster == "" {
		t.Error("donor poster should remain")
	}
}

func TestPropagateMetaByJAVCodeSkipsWhenPosterPresent(t *testing.T) {
	torrents := []catalogTorrent{{Title: "SSIS-001"}, {Title: "SSIS-001-C"}}
	metas := []MetaPreview{
		{Poster: "https://example.com/a.jpg", Name: "Custom A"},
		{Poster: "https://example.com/b.jpg", Name: "Custom B"},
	}
	propagateMetaByJAVCode(torrents, metas)
	if metas[1].Name != "Custom B" {
		t.Errorf("should not overwrite row that already has a poster")
	}
}
