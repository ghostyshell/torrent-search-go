package stremio

import "testing"

func TestCountCompactSceneGroups(t *testing.T) {
	in := []catalogTorrent{
		{Title: "Scene A 2160p"},
		{Title: "Scene A 1080p"},
		{Title: "Scene B 2160p"},
	}
	if got := countCompactSceneGroups(in); got != 2 {
		t.Fatalf("countCompactSceneGroups = %d, want 2", got)
	}
}

func TestBuildCompactSceneGroupsRanksBySeeders(t *testing.T) {
	in := []catalogTorrent{
		{Title: "Low Seeders 2160p", Seeders: 5},
		{Title: "High Seeders 2160p", Seeders: 50},
		{Title: "High Seeders 1080p", Seeders: 10},
	}
	gs := buildCompactSceneGroups(in)
	if len(gs) != 2 {
		t.Fatalf("groups = %d, want 2", len(gs))
	}
	if gs[0].members[0].t.Title != "High Seeders 2160p" {
		t.Fatalf("top group = %q, want High Seeders 2160p", gs[0].members[0].t.Title)
	}
}

func TestBuildCompactSceneGroupsPaginationSlice(t *testing.T) {
	in := make([]catalogTorrent, 0, 5)
	for i := 0; i < 5; i++ {
		in = append(in, catalogTorrent{Title: "Scene " + string(rune('A'+i)) + " 2160p", Seeders: 100 - i})
	}
	gs := buildCompactSceneGroups(in)
	page := gs[2:4]
	if len(page) != 2 || page[0].members[0].t.Title != "Scene C 2160p" {
		t.Fatalf("page slice = %#v, want Scene C then Scene D", page)
	}
}
