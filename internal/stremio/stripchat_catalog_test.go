package stremio

import (
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
	stripchatSortCatalogModels(filtered)
	if filtered[0].Username != "high" || filtered[2].Username != "low" {
		t.Fatalf("expected high,mid,low; got %s,%s,%s",
			filtered[0].Username, filtered[1].Username, filtered[2].Username)
	}
}

func TestStripchatSortCatalogModelsPrefersDesktop(t *testing.T) {
	models := []stripchatModel{
		{Username: "mobile_star", Status: "public", IsMobile: true, ViewersCount: 5000},
		{Username: "desktop", Status: "public", IsMobile: false, ViewersCount: 100},
		{Username: "mobile_mid", Status: "public", IsMobile: true, ViewersCount: 200},
	}
	stripchatSortCatalogModels(models)
	if models[0].Username != "desktop" {
		t.Fatalf("desktop should rank first, got %q", models[0].Username)
	}
	if models[1].Username != "mobile_star" {
		t.Fatalf("mobile sorted by viewers after desktop, got %q", models[1].Username)
	}
}

func TestStripchatPickPopularBlockID(t *testing.T) {
	if stripchatCatalogBlockID != "mostPopularModels" {
		t.Fatalf("catalog block id = %q", stripchatCatalogBlockID)
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
	if got := stripchatPoster(stripchatModel{Snapshot: "https://cdn/snap.jpg"}); got != "https://cdn/snap.jpg" {
		t.Errorf("absolute snapshot: %q", got)
	}
	if got := stripchatPoster(stripchatModel{PreviewURLThumbSmall: "/previews/a/b/c-thumb-small"}); got != stripchatCDNBase+"/previews/a/b/c" {
		t.Errorf("live preview without thumb suffix: %q", got)
	}
	if got := stripchatPoster(stripchatModel{AvatarURL: "/avatars/x.png"}); got != "" {
		t.Errorf("avatar must not be used as poster: %q", got)
	}
	if got := stripchatPoster(stripchatModel{}); got != "" {
		t.Errorf("empty model should yield empty poster: %q", got)
	}
}

func TestStripchatLivePreviewPath(t *testing.T) {
	if got := stripchatLivePreviewPath("/previews/x-thumb-small"); got != "/previews/x" {
		t.Errorf("strip suffix: %q", got)
	}
	if got := stripchatLivePreviewPath("/previews/x"); got != "/previews/x" {
		t.Errorf("unchanged path: %q", got)
	}
}

func TestStripchatAbsMediaURL(t *testing.T) {
	if got := stripchatAbsMediaURL(""); got != "" {
		t.Errorf("empty: %q", got)
	}
	if got := stripchatAbsMediaURL("https://x/y"); got != "https://x/y" {
		t.Errorf("already absolute: %q", got)
	}
	if got := stripchatAbsMediaURL("/previews/x"); got != stripchatCDNBase+"/previews/x" {
		t.Errorf("leading slash: %q", got)
	}
}
