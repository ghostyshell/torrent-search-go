package stremio

import "testing"

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
