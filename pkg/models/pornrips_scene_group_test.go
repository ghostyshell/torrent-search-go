package models

import "testing"

// TestPornripsSceneGroupCollapsesResolutions pins the grouping contract: two WP
// titles that differ only by resolution/codec/PRT tokens collapse to one
// scene_group key, so the catalog aggregation puts their docs in one jstrg: entry.
func TestPornripsSceneGroupCollapsesResolutions(t *testing.T) {
	cases := []struct{ a, b string }{
		{"Studio.25.06.25.Scene.PRT.1080p.WEB-DL.x265", "Studio.25.06.25.Scene.PRT.720p.WEB-DL.x264"},
		{"Scene.Name.2160p.UHD.WEB-DL.HEVC.x265", "Scene.Name.1080p.WEB-DL.x264"},
		{"Scene.Name.PRT.4K.WEB-DL", "scene.name.prt.1080p.web-dl.x264"},
	}
	for _, c := range cases {
		ka, kb := PornripsSceneGroup(c.a), PornripsSceneGroup(c.b)
		if ka == "" {
			t.Fatalf("PornripsSceneGroup(%q) = empty", c.a)
		}
		if ka != kb {
			t.Fatalf("pair did not collapse: %q -> %q  vs  %q -> %q", c.a, ka, c.b, kb)
		}
	}
}

// TestPornripsSceneGroupKeepsSceneIdentity asserts that genuinely different scenes
// do NOT collapse - the group key keeps studio/date/scene-name tokens.
func TestPornripsSceneGroupKeepsSceneIdentity(t *testing.T) {
	a := PornripsSceneGroup("Studio.25.06.25.SceneA.PRT.1080p")
	b := PornripsSceneGroup("Studio.25.06.25.SceneB.PRT.1080p")
	if a == b {
		t.Fatalf("different scenes collapsed: %q == %q", a, b)
	}
}

// TestPornripsSceneGroupEmptyReturnsEmpty: empty/garbage titles yield "" so the
// ingest caller falls back to "pr:"+slug (unique per doc, never groups).
func TestPornripsSceneGroupEmptyReturnsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", ".....", "720p 1080p 4k hevc x264 prt"} {
		if got := PornripsSceneGroup(in); got != "" {
			t.Fatalf("PornripsSceneGroup(%q) = %q, want empty (no scene tokens)", in, got)
		}
	}
}

// TestPornripsQualityRank pins representative selection: 4K > 1080p > other.
func TestPornripsQualityRank(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		{"Scene.2160p.UHD", 3},
		{"Scene.4K.WEB-DL", 3},
		{"Scene.1080p.WEB-DL", 2},
		{"Scene.1440p", 2},
		{"Scene.720p", 1},
		{"Scene.PRT", 1},
		{"", 1},
	}
	for _, c := range cases {
		if got := PornripsQualityRank(c.title); got != c.want {
			t.Fatalf("PornripsQualityRank(%q) = %d, want %d", c.title, got, c.want)
		}
	}
}