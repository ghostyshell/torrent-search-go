package stremio

import "testing"

func TestMetadataTitlesForOnlyFans(t *testing.T) {
	titles := metadataTitlesForLookup("OnlyFans - Madison Ivy - Getting Stretched And Creampied By Girthmasterr rq mp4")
	seen := make(map[string]struct{})
	for _, title := range titles {
		seen[title] = struct{}{}
	}
	for _, want := range []string{
		"Madison Ivy Getting Stretched And Creampied By Girthmasterr",
		"Madison Ivy Girthmasterr",
	} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing probe %q in %v", want, titles)
		}
	}
	if _, ok := seen["Getting Stretched And Creampied By Girthmasterr"]; ok {
		t.Fatal("should not probe performer-less OnlyFans scene title")
	}
}

func TestPropagateMetaByOnlyFansPerformer(t *testing.T) {
	torrents := []catalogTorrent{
		{Title: "OnlyFans - Anna Ralphs - Family Dinner rq mp4"},
		{Title: "Anna Ralphs Couch Creampie"},
	}
	metas := []MetaPreview{
		{ID: "a", Name: "OnlyFans - Anna Ralphs - Family Dinner rq mp4"},
		{ID: "b", Name: "Anna Ralphs Couch Creampie", Poster: "https://example.com/anna.jpg"},
	}
	propagateMetaByOnlyFansPerformer(torrents, metas)
	if metas[0].Poster == "" {
		t.Fatal("expected poster from same-page Anna Ralphs donor")
	}
	if metas[0].Name != "OnlyFans - Anna Ralphs - Family Dinner rq mp4" {
		t.Fatalf("should not overwrite title, got %q", metas[0].Name)
	}
}
