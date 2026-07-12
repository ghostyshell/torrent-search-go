package stremio

import (
	"context"
	"slices"
	"testing"

	prmodels "torrent-search-go/pkg/models"

	"torrent-search-go/internal/models"
)

func TestNormalizeModelTorrentPornripsDetailURL(t *testing.T) {
	got := normalizeModelTorrent(models.Torrent{
		Name:       "Scene",
		UploadedBy: "https://pornrips.to/some-slug/",
		Website:    "pornrips",
	})
	if got.DetailURL != "https://pornrips.to/some-slug/" {
		t.Fatalf("DetailURL = %q", got.DetailURL)
	}
	if got.Website != "pornrips" {
		t.Fatalf("Website = %q", got.Website)
	}
}

// fakePornripsStore records the query each method received and returns canned
// groups, so we can assert fetchPornripsFromStore dispatches to the right method
// with a normalized studio/tag token. The canned []PornripsEntry fields are
// wrapped one-entry-per-group via singleGroups (the real Mongo aggregation groups
// by scene_group; for these dispatch tests each canned entry is its own group).
type fakePornripsStore struct {
	recent             []prmodels.PornripsEntry
	studio             []prmodels.PornripsEntry
	tag                []prmodels.PornripsEntry
	search             []prmodels.PornripsEntry
	lastStudioNorm     string
	lastTagNorm        []string
	lastSearchQuery    string
	lastSkip           int
	lastLimit          int
	recentCalled       bool
	bySlug             []prmodels.PornripsEntry
	byPerformer        []prmodels.PornripsEntry
	lastPerformer      string
	byPerformers       []prmodels.PornripsEntry
	performersResolved map[string]bool
	lastPerformers     []string
}

// singleGroups wraps each entry as its own single-member group, mirroring the
// shape the Mongo aggregation returns. Used by the fake store so dispatch tests
// can keep feeding canned []PornripsEntry.
func singleGroups(entries []prmodels.PornripsEntry) []prmodels.PornripsGroup {
	out := make([]prmodels.PornripsGroup, len(entries))
	for i, e := range entries {
		out[i] = prmodels.PornripsGroup{Representative: e, Members: []prmodels.PornripsEntry{e}}
	}
	return out
}

func (f *fakePornripsStore) GetPornripsRecent(ctx context.Context, skip, limit int) ([]prmodels.PornripsGroup, error) {
	f.recentCalled = true
	f.lastSkip, f.lastLimit = skip, limit
	return singleGroups(f.recent), nil
}
func (f *fakePornripsStore) GetPornripsByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]prmodels.PornripsGroup, error) {
	f.lastStudioNorm = studioNorm
	f.lastSkip, f.lastLimit = skip, limit
	return singleGroups(f.studio), nil
}
func (f *fakePornripsStore) GetPornripsByTag(ctx context.Context, tags []string, skip, limit int) ([]prmodels.PornripsGroup, error) {
	f.lastTagNorm = tags
	f.lastSkip, f.lastLimit = skip, limit
	return singleGroups(f.tag), nil
}
func (f *fakePornripsStore) SearchPornrips(ctx context.Context, query string, skip, limit int) ([]prmodels.PornripsGroup, error) {
	f.lastSearchQuery = query
	f.lastSkip, f.lastLimit = skip, limit
	return singleGroups(f.search), nil
}

func (f *fakePornripsStore) GetPornripsEntryBySlug(ctx context.Context, slug string) (*prmodels.PornripsEntry, error) {
	if len(f.bySlug) == 0 {
		return nil, nil
	}
	e := f.bySlug[0]
	return &e, nil
}
func (f *fakePornripsStore) GetPornripsEntriesByPerformer(ctx context.Context, performer string, limit int) ([]prmodels.PornripsEntry, error) {
	f.lastPerformer = performer
	return f.byPerformer, nil
}
func (f *fakePornripsStore) GetPornripsEntriesByPerformers(ctx context.Context, performers []string, limit int) ([]prmodels.PornripsEntry, error) {
	f.lastPerformers = performers
	return f.byPerformers, nil
}
func (f *fakePornripsStore) PerformersWithTorrent(ctx context.Context, performers []string) (map[string]bool, error) {
	f.lastPerformers = performers
	out := make(map[string]bool, len(performers))
	for _, p := range performers {
		if f.performersResolved[p] {
			out[p] = true
		}
	}
	return out, nil
}

func entry(slug, title, poster, wp string) prmodels.PornripsEntry {
	return prmodels.PornripsEntry{Slug: slug, Title: title, Poster: poster, WpPoster: wp, DetailURL: "https://pornrips.to/" + slug + "/"}
}

func TestFetchPornripsFromStoreRecent(t *testing.T) {
	f := &fakePornripsStore{recent: []prmodels.PornripsEntry{entry("a", "A", "", "wp-a")}}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_recent", "", "", 0)
	if !f.recentCalled || len(got) != 1 || got[0].Title != "A" {
		t.Fatalf("recent dispatch failed: called=%v got=%+v", f.recentCalled, got)
	}
	if f.lastLimit != 20 || f.lastSkip != 0 {
		t.Fatalf("skip/limit = %d/%d", f.lastSkip, f.lastLimit)
	}
}

func TestFetchPornripsFromStoreStudioNormalizes(t *testing.T) {
	f := &fakePornripsStore{studio: []prmodels.PornripsEntry{entry("bb", "Bang Bros Scene", "", "")}}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_studio", "Bang Bros", "", 0)
	if len(got) != 1 {
		t.Fatalf("got %d items", len(got))
	}
	if f.lastStudioNorm != "bangbros" {
		t.Fatalf("studio queried with %q, want normalized bangbros", f.lastStudioNorm)
	}
}

func TestFetchPornripsFromStoreStudioAllFallsToRecent(t *testing.T) {
	f := &fakePornripsStore{recent: []prmodels.PornripsEntry{entry("r", "R", "", "")}}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_studio", "All", "", 0)
	if !f.recentCalled || len(got) != 1 {
		t.Fatalf("All studio should fall to recent: called=%v got=%d", f.recentCalled, len(got))
	}
}

func TestFetchPornripsFromStoreTagNormalizes(t *testing.T) {
	f := &fakePornripsStore{tag: []prmodels.PornripsEntry{entry("m", "MILF Scene", "", "")}}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_tag", "MILF", "", 0)
	// "MILF" resolves through prTagAliases to the milf30 compound token (the bare
	// "milf" token has zero on-disk entries), so the dispatch must carry the alias
	// set, not the bare normalized token.
	if len(got) != 1 || !slices.Contains(f.lastTagNorm, "milf30") || slices.Contains(f.lastTagNorm, "milf") {
		t.Fatalf("tag dispatch: got=%d tagNorm=%v", len(got), f.lastTagNorm)
	}
}

// TestFetchPornripsFromStoreTagPassthrough asserts a category with no alias
// still dispatches the bare normalized token (the original exact-match path).
func TestFetchPornripsFromStoreTagPassthrough(t *testing.T) {
	f := &fakePornripsStore{tag: []prmodels.PornripsEntry{entry("m", "BJ", "", "")}}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_tag", "Blowjob", "", 0)
	if len(got) != 1 || len(f.lastTagNorm) != 1 || f.lastTagNorm[0] != "blowjob" {
		t.Fatalf("tag passthrough: got=%d tagNorm=%v", len(got), f.lastTagNorm)
	}
}

// TestPrTagTokens asserts a pr_tag genre expands to its alias set. Unlike
// enrichedTagTokens, prTagTokens returns the aliases ONLY for aliased genres
// (the bare NormToken is dead in the pornrips vocabulary), so the bare token
// must NOT appear for aliased genres. Categories without an alias pass through
// the bare normalized token; unknown genres return nil.
func TestPrTagTokens(t *testing.T) {
	cases := []struct {
		genre   string
		wantHas  []string // tokens that must be in the set
		wantNot  []string // tokens that must NOT be in the set
		wantLen  int      // exact length if > 0
	}{
		{"MILF", []string{"milf30", "bustymilf"}, []string{"milf"}, 0},
		{"Stepfamily", []string{"stepmother", "stepdaughter", "stepsister"}, []string{"stepfamily"}, 0},
		{"Pissing", []string{"pissinmouth", "pissdrinking", "pissplay"}, []string{"pissing"}, 0},
		{"Rough Sex", []string{"rough"}, []string{"roughsex"}, 1},
		{"Redhead", []string{"redhairfemale", "redhair"}, []string{"redhead", "naturalredheads"}, 0},
		{"Ebony", []string{"blackwoman", "ebonyonebonysex", "blackonblack"}, []string{"ebony"}, 0},
		{"Blowjob", []string{"blowjob"}, nil, 1}, // no alias -> bare token passthrough
	}
	for _, c := range cases {
		got := prTagTokens(c.genre)
		if c.wantLen > 0 && len(got) != c.wantLen {
			t.Errorf("prTagTokens(%q) = %v, want len %d", c.genre, got, c.wantLen)
		}
		for _, w := range c.wantHas {
			if !slices.Contains(got, w) {
				t.Errorf("prTagTokens(%q) = %v, want %q in set", c.genre, got, w)
			}
		}
		for _, w := range c.wantNot {
			if slices.Contains(got, w) {
				t.Errorf("prTagTokens(%q) = %v, want %q NOT in set (aliased genres drop the bare token)", c.genre, got, w)
			}
		}
	}
	// Unknown / non-aliased genres fall through to the bare normalized token
	// (prTagTokens uses NormToken directly, not categoryTagNorm, so an unknown
	// name is not nil - it is its own bare token, which simply matches 0 docs).
	if got := prTagTokens("No Such Genre"); len(got) != 1 || got[0] != "nosuchgenre" {
		t.Errorf("prTagTokens(unknown) = %v, want [nosuchgenre]", got)
	}
	if got := prTagTokens(""); got != nil {
		t.Errorf("prTagTokens(empty) = %v, want nil", got)
	}
}

func TestFetchPornripsFromStoreSearch(t *testing.T) {
	f := &fakePornripsStore{search: []prmodels.PornripsEntry{entry("s", "S", "", "")}}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_search", "", "june liu", 24)
	if len(got) != 1 || f.lastSearchQuery != "june liu" || f.lastSkip != 24 {
		t.Fatalf("search dispatch: got=%d q=%q skip=%d", len(got), f.lastSearchQuery, f.lastSkip)
	}
}

func TestFetchPornripsFromStoreSearchEmptyReturnsNil(t *testing.T) {
	f := &fakePornripsStore{}
	h := &Handler{Pornrips: f}
	got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_search", "", "  ", 0)
	if got != nil {
		t.Fatalf("empty search query should return nil (caller falls through), got %+v", got)
	}
}

// Cold store (0 docs) returns nil so servePornripsCatalog falls through to the
// live WP/scrape paths - never an empty catalog.
func TestFetchPornripsFromStoreColdReturnsNil(t *testing.T) {
	f := &fakePornripsStore{}
	h := &Handler{Pornrips: f}
	if got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_studio", "Bang Bros", "", 0); got != nil {
		t.Fatalf("cold store should return nil, got %+v", got)
	}
}

func TestFetchPornripsFromStoreNilStoreReturnsNil(t *testing.T) {
	h := &Handler{}
	if got := h.fetchPornripsFromStore(context.Background(), Config{MaxResults: 20}, "pr_recent", "", "", 0); got != nil {
		t.Fatalf("nil store should return nil, got %+v", got)
	}
}

func TestEntriesToCatalogPosterFallback(t *testing.T) {
	out := groupsToCatalog(singleGroups([]prmodels.PornripsEntry{
		entry("enriched", "Enriched", "https://tpdb/p.jpg", "https://wp/p.jpg"), // poster wins
		entry("wp-only", "Wp Only", "", "https://wp/w.jpg"),                     // wp fallback
		{Slug: "bare", Title: "", Poster: "", WpPoster: ""},                     // title -> slug
	}))
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0].CoverImage != "https://tpdb/p.jpg" {
		t.Fatalf("enriched poster should win: %q", out[0].CoverImage)
	}
	if out[1].CoverImage != "https://wp/w.jpg" {
		t.Fatalf("wp_poster fallback: %q", out[1].CoverImage)
	}
	if out[2].Title != "bare" {
		t.Fatalf("empty title should fall back to slug: %q", out[2].Title)
	}
	if out[2].DetailURL != "https://pornrips.to/bare/" {
		t.Fatalf("bare detail url fallback: %q", out[2].DetailURL)
	}
	for _, c := range out {
		if c.Website != "pornrips" || c.Indexer != "pornrips" {
			t.Fatalf("website/indexer not set: %+v", c)
		}
		if c.Members != nil {
			t.Fatalf("single-member group should not carry Members: %+v", c)
		}
	}
}

// TestShouldLiveResolveGate pins the catalog live-resolve gate: PornRips is
// Mongo-only (the background PornripsSync job is the sole metadata populator, so
// pornrips rows never live-resolve), while other sources resolve when they lack
// a real cover and skip once they have one to avoid redundant TPDB calls.
func TestShouldLiveResolveGate(t *testing.T) {
	cases := []struct {
		website string
		poster  string
		want    bool
	}{
		{"pornrips", "https://wp/w.jpg", false},       // Mongo-only -> never live-resolve
		{"pornrips", "", false},                       // Mongo-only -> never live-resolve
		{"thepiratebay", "", true},                    // no poster -> resolve
		{"thepiratebay", "https://tpdb/p.jpg", false}, // real cover already present -> skip
		{"1337x", "  ", true},                         // whitespace poster -> resolve
	}
	for _, c := range cases {
		if got := shouldLiveResolve(c.website, c.poster); got != c.want {
			t.Fatalf("shouldLiveResolve(%q,%q) = %v, want %v", c.website, c.poster, got, c.want)
		}
	}
}

func TestEntriesToCatalogCarriesTorrent(t *testing.T) {
	e := prmodels.PornripsEntry{
		Slug: "s", Title: "Studio.25.06.25.PRT",
		DetailURL: "https://pornrips.to/s/", InfoHash: "abc123", TorrentURL: "https://pornrips.to/torrents/x.torrent",
	}
	got := groupsToCatalog([]prmodels.PornripsGroup{{Representative: e, Members: []prmodels.PornripsEntry{e}}})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].InfoHash != "abc123" || got[0].TorrentURL != "https://pornrips.to/torrents/x.torrent" {
		t.Fatalf("infoHash/torrentURL not propagated: %+v", got[0])
	}
	if got[0].Members != nil {
		t.Fatalf("single-member group should not carry Members: %+v", got[0])
	}
}

// TestGroupsToCatalogMultiResolution pins the scene-group feature: a 720p +
// 1080p pair of the same scene (same scene_group) arrives as one PornripsGroup
// with two members and leaves groupsToCatalog as one catalogTorrent carrying
// both variants in Members, so buildMetas emits a jstrg: group id with one
// stream per resolution. The representative (highest-res, members[0]) supplies
// the row's poster/title/infoHash.
func TestGroupsToCatalogMultiResolution(t *testing.T) {
	p720 := prmodels.PornripsEntry{
		Slug: "studio-scene-720p", Title: "Studio.25.06.25.Scene.PRT.720p.WEB-DL.x264",
		DetailURL: "https://pornrips.to/studio-scene-720p/",
		InfoHash: "hash720", TorrentURL: "https://pornrips.to/torrents/720.torrent",
	}
	p1080 := prmodels.PornripsEntry{
		Slug: "studio-scene-1080p", Title: "Studio.25.06.25.Scene.PRT.1080p.WEB-DL.x265",
		DetailURL: "https://pornrips.to/studio-scene-1080p/",
		InfoHash: "hash1080", TorrentURL: "https://pornrips.to/torrents/1080.torrent",
		Poster: "https://tpdb/p.jpg",
	}
	// Mongo returns members sorted by PornripsQualityRank desc -> 1080p first.
	gr := prmodels.PornripsGroup{
		Representative: p1080,
		Members:         []prmodels.PornripsEntry{p1080, p720},
	}
	got := groupsToCatalog([]prmodels.PornripsGroup{gr})
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 (the pair collapses)", len(got))
	}
	row := got[0]
	if len(row.Members) != 2 {
		t.Fatalf("Members len = %d, want 2", len(row.Members))
	}
	if row.Title != p1080.Title || row.InfoHash != "hash1080" || row.CoverImage != "https://tpdb/p.jpg" {
		t.Fatalf("representative fields wrong: %+v", row)
	}
	if row.Members[0].InfoHash != "hash1080" || row.Members[1].InfoHash != "hash720" {
		t.Fatalf("member order wrong: %v %v", row.Members[0].InfoHash, row.Members[1].InfoHash)
	}
	for _, m := range row.Members {
		if m.Website != "pornrips" || m.DetailURL == "" || m.Title == "" {
			t.Fatalf("member under-populated: %+v", m)
		}
	}
}

func TestPornripsSlugFromDetail(t *testing.T) {
	cases := map[string]string{
		"https://pornrips.to/some-slug/": "some-slug",
		"https://pornrips.to/some-slug":  "some-slug",
		"":                               "",
	}
	for in, want := range cases {
		if got := pornripsSlugFromDetail(in); got != want {
			t.Fatalf("pornripsSlugFromDetail(%q) = %q, want %q", in, got, want)
		}
	}
}
