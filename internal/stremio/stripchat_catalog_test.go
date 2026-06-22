package stremio

import (
	"sort"
	"testing"
)

func TestStripchatPrimaryTagMap(t *testing.T) {
	want := map[string]string{
		"sc_girls":   "girls",
		"sc_couples": "couples",
		"sc_guys":    "men",
		"sc_trans":   "trans",
	}
	for id := range want {
		if _, ok := stripchatPrimaryTag[id]; !ok {
			t.Errorf("missing primaryTag for catalog %q", id)
		}
	}
	for id, tag := range stripchatPrimaryTag {
		if want[id] != tag {
			t.Errorf("primaryTag[%q] = %q, want %q", id, tag, want[id])
		}
	}
}

func TestStripchatCatalogDefsAllSearchable(t *testing.T) {
	defs := stripchatCatalogDefs()
	if len(defs) != 4 {
		t.Fatalf("expected 4 stripchat catalogs, got %d", len(defs))
	}
	for _, d := range defs {
		if !d.search {
			t.Errorf("catalog %q must be searchable", d.id)
		}
		if d.hideFromHome {
			t.Errorf("catalog %q must stay on Home (hideFromHome=false)", d.id)
		}
		if len(d.options) != 0 {
			t.Errorf("catalog %q must not declare genre options", d.id)
		}
	}
}

func TestStripchatFilterPublicLiveDropsOfflineAndPrivate(t *testing.T) {
	models := []stripchatModel{
		{Username: "a", Status: "public", IsLive: true, ViewersCount: 100},
		{Username: "b", Status: "private", IsLive: false, ViewersCount: 50},
		{Username: "c", Status: "offline", IsLive: false, ViewersCount: 0},
		{Username: "d", Status: "public", IsLive: true, ViewersCount: 200},
	}
	got := stripchatFilterPublicLive(models)
	if len(got) != 2 {
		t.Fatalf("expected 2 public models, got %d (%+v)", len(got), got)
	}
	for _, m := range got {
		if m.Status != "public" {
			t.Errorf("model %q leaked with status %q", m.Username, m.Status)
		}
	}
}

func TestStripchatListingSortedByViewersDesc(t *testing.T) {
	models := []stripchatModel{
		{Username: "low", Status: "public", ViewersCount: 5},
		{Username: "high", Status: "public", ViewersCount: 999},
		{Username: "mid", Status: "public", ViewersCount: 42},
	}
	filtered := stripchatFilterPublicLive(models)
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].ViewersCount > filtered[j].ViewersCount
	})
	if filtered[0].Username != "high" || filtered[2].Username != "low" {
		t.Fatalf("expected high,mid,low; got %s,%s,%s",
			filtered[0].Username, filtered[1].Username, filtered[2].Username)
	}
}

func TestStripchatModelToPreviewShape(t *testing.T) {
	m := stripchatModel{
		Username:     "alice",
		Status:       "public",
		IsLive:       true,
		ViewersCount: 42,
		Country:      "us",
		Snapshot:     "https://cdn/snap.jpg",
	}
	p := stripchatModelToPreview(m)
	if p.ID != "sc:alice" {
		t.Errorf("ID = %q, want sc:alice", p.ID)
	}
	if p.Type != "Porn" {
		t.Errorf("Type = %q, want Porn", p.Type)
	}
	if p.Poster != "https://cdn/snap.jpg" {
		t.Errorf("Poster = %q", p.Poster)
	}
	if p.PosterShape != "landscape" {
		t.Errorf("PosterShape = %q", p.PosterShape)
	}
	if p.Description != "live - 42 viewers - US" {
		t.Errorf("Description = %q", p.Description)
	}
}

func TestStripchatPosterFallbackOrder(t *testing.T) {
	if got := stripchatPoster(stripchatModel{Snapshot: "s"}); got != "s" {
		t.Errorf("snapshot first: %q", got)
	}
	if got := stripchatPoster(stripchatModel{PreviewURLThumbSmall: "p"}); got != "p" {
		t.Errorf("preview fallback: %q", got)
	}
	if got := stripchatPoster(stripchatModel{AvatarURL: "a"}); got != "a" {
		t.Errorf("avatar fallback: %q", got)
	}
	if got := stripchatPoster(stripchatModel{}); got != "" {
		t.Errorf("empty model should yield empty poster: %q", got)
	}
}
