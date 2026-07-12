package stremio

import (
	"strings"
	"testing"

	appconfig "torrent-search-go/internal/config"
)

func TestBuildManifestSukebeiWithUserStashKey(t *testing.T) {
	cfg := Config{
		Sources:      []string{"sukebei"},
		StashdbKey:   "user-stash-key",
		EnabledSorts: []string{"top"},
	}
	manifest := BuildManifest(cfg, "", nil, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})

	haveSukebei := false
	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if strings.HasPrefix(id, "sukebei_") {
			haveSukebei = true
			if !catalogHasSearchExtra(c) {
				t.Fatalf("sukebei catalog %q should expose search extra", id)
			}
			break
		}
	}
	if !haveSukebei {
		t.Fatal("expected sukebei catalogs when user stashdb key is set without server env key")
	}
}

func TestBuildManifestFiltersEnabledSorts(t *testing.T) {
	// Build a manifest with only the "top" sort variant enabled.
	cfg := Config{
		Sources:      []string{"piratebay"},
		EnabledSorts: []string{"top"},
	}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)

	catalogs := manifest["catalogs"].([]map[string]interface{})
	if len(catalogs) == 0 {
		t.Fatal("expected non-empty TPB catalogs")
	}

	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if id == "" {
			continue
		}
		// Search is a standalone catalog, not a sort variant.
		if isPornSearchCatalogID(id) {
			continue
		}
		if strings.HasSuffix(id, "_recent") {
			t.Fatalf("expected no recent catalogs, got %q", id)
		}
		if !strings.HasSuffix(id, "_top") {
			t.Fatalf("expected only top catalogs, got %q", id)
		}
	}

	// Every TPB base should still have its top catalog present.
	haveTop := make(map[string]struct{})
	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if strings.HasSuffix(id, "_top") {
			haveTop[id] = struct{}{}
		}
	}
	if len(haveTop) == 0 {
		t.Fatal("expected at least one top catalog")
	}
}

func TestBuildManifestDefaultSortsIncludesBoth(t *testing.T) {
	cfg := Config{
		Sources:      []string{"piratebay"},
		EnabledSorts: []string{"recent", "top"},
	}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})

	haveRecent, haveTop := false, false
	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if strings.HasSuffix(id, "_recent") {
			haveRecent = true
		}
		if strings.HasSuffix(id, "_top") {
			haveTop = true
		}
	}
	if !haveRecent {
		t.Fatal("expected recent catalogs with default sorts")
	}
	if !haveTop {
		t.Fatal("expected top catalogs with default sorts")
	}
}

func TestBuildManifestOnlyRecent(t *testing.T) {
	cfg := Config{
		Sources:      []string{"piratebay"},
		EnabledSorts: []string{"recent"},
	}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})

	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if strings.HasSuffix(id, "_top") {
			t.Fatalf("expected no top catalogs, got %q", id)
		}
	}
}

func TestIsCatalogAllowedRespectsEnabledSorts(t *testing.T) {
	cfg := Config{
		Sources:      []string{"piratebay"},
		EnabledSorts: []string{"top"},
	}
	if isCatalogAllowed(cfg, "xxx_recent") {
		t.Error("xxx_recent should be disallowed when only top is enabled")
	}
	if !isCatalogAllowed(cfg, "xxx_top") {
		t.Error("xxx_top should be allowed when top is enabled")
	}

	cfg.EnabledSorts = []string{"recent"}
	if !isCatalogAllowed(cfg, "xxx_recent") {
		t.Error("xxx_recent should be allowed when recent is enabled")
	}
	if isCatalogAllowed(cfg, "xxx_top") {
		t.Error("xxx_top should be disallowed when only recent is enabled")
	}
}

func TestBuildManifestCatalogOrderSorted(t *testing.T) {
	// Catalog order: the broad search-only catalogs (tpdb_search, then the
	// global "search") lead the array so stremio-web's search page loads them
	// in its initial visible range (empty first rows deadlock the lazy-load).
	// Then non-TPB-Studio catalogs (alphabetical), then TPB-Studio catalogs
	// (alphabetical): piratebay studio catalogs (IDs prefixed "xxx_studio_").
	// The main XXX/Trans browse catalogs are TPB-sourced but sort in the
	// non-studio block.
	cfg := Config{
		Sources:      []string{"piratebay", "pornrips", "hentai", "stripchat"},
		EnabledSorts: []string{"top", "recent"},
	}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})
	if len(catalogs) == 0 {
		t.Fatal("expected non-empty catalogs")
	}

	if len(catalogs) < 2 {
		t.Fatal("expected at least the two leading search catalogs")
	}
	if id, _ := catalogs[0]["id"].(string); id != "tpdb_search" {
		t.Fatalf("expected tpdb_search first, got %q", id)
	}
	if id, _ := catalogs[1]["id"].(string); id != PornSearchCatalogID {
		t.Fatalf("expected %q second, got %q", PornSearchCatalogID, catalogs[1]["id"])
	}
	catalogs = catalogs[2:]

	isStudio := func(c map[string]interface{}) bool {
		id, _ := c["id"].(string)
		return strings.HasPrefix(id, "xxx_studio_")
	}
	// XXX/Trans browse catalogs carry an "xxx"/"xxx_trans" base but are not
	// studios; they must land in the first (non-studio) block.
	isMainXxxTrans := func(c map[string]interface{}) bool {
		id, _ := c["id"].(string)
		return id == "xxx" || strings.HasPrefix(id, "xxx_recent") || strings.HasPrefix(id, "xxx_top") ||
			strings.HasPrefix(id, "xxx_fhd") || strings.HasPrefix(id, "xxx_trans")
	}

	var nonStudio, studio []string
	seenStudio := false
	sawMainXxxTransInNonStudio := false
	for _, c := range catalogs {
		name, _ := c["name"].(string)
		if isStudio(c) {
			seenStudio = true
			studio = append(studio, name)
		} else {
			if seenStudio {
				t.Fatalf("non-studio catalog %q appears after studio block", name)
			}
			if isMainXxxTrans(c) {
				sawMainXxxTransInNonStudio = true
			}
			nonStudio = append(nonStudio, name)
		}
	}
	if len(nonStudio) == 0 || len(studio) == 0 {
		t.Fatalf("expected both groups non-empty: non-studio=%d studio=%d", len(nonStudio), len(studio))
	}
	if !sawMainXxxTransInNonStudio {
		t.Fatal("expected XXX/Trans catalogs to sort in the non-studio block")
	}

	assertSorted := func(label string, names []string) {
		for i := 1; i < len(names); i++ {
			if strings.ToLower(names[i]) < strings.ToLower(names[i-1]) {
				t.Fatalf("%s block not sorted: %q before %q", label, names[i-1], names[i])
			}
		}
	}
	assertSorted("non-studio", nonStudio)
	assertSorted("studio", studio)
}

func catalogBaseID(catalogID string) string {
	base := catalogID
	for _, s := range []string{"_top", "_recent"} {
		base = strings.TrimSuffix(base, s)
	}
	return base
}

func catalogHasSearchExtra(c map[string]interface{}) bool {
	extras, _ := c["extra"].([]map[string]interface{})
	for _, e := range extras {
		if e["name"] == "search" {
			return true
		}
	}
	return false
}

func TestBuildManifestSearchOnlyOnPart1(t *testing.T) {
	// The dedicated Search catalog must appear only on part 1 (or an unsplit
	// install) so a multi-part install does not duplicate the Search row.
	hasSearch := func(catalogs []map[string]interface{}) bool {
		for _, c := range catalogs {
			if id, _ := c["id"].(string); id == PornSearchCatalogID {
				return true
			}
		}
		return false
	}

	// Unsplit (group 0): Search present.
	unsplit := BuildManifest(Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}}, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	if !hasSearch(unsplit["catalogs"].([]map[string]interface{})) {
		t.Fatal("expected Search catalog on unsplit install")
	}

	// Part 1 (group 1): Search present.
	part1 := BuildManifest(Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}, Group: 1, GroupTotal: 3}, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	if !hasSearch(part1["catalogs"].([]map[string]interface{})) {
		t.Fatal("expected Search catalog on part 1")
	}

	// Part 2 (group 2): Search absent.
	part2 := BuildManifest(Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}, Group: 2, GroupTotal: 3}, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	if hasSearch(part2["catalogs"].([]map[string]interface{})) {
		t.Fatal("Search catalog must not appear on parts after part 1")
	}
}

func TestBuildManifestTpdbSuppressedByEmptyCategories(t *testing.T) {
	// The Node edge sends tpdbCategories: [] on parts 2..N of a multi-part
	// install to suppress the TPDB catalog there: omitting the field would let
	// the backend fill default categories from the tpdb key and re-emit TPDB on
	// every part. Lock the contract the edge relies on - with a server TPDB key
	// active, an explicit empty TpdbCategories suppresses the TPDB catalog
	// while a non-empty slice emits it.
	env := &appconfig.Config{Metadata: appconfig.MetadataConfig{TPDBAPIKey: "server-key"}}

	hasTpdb := func(cats []map[string]interface{}) bool {
		for _, c := range cats {
			if id, _ := c["id"].(string); id == tpdbCatalogID {
				return true
			}
		}
		return false
	}

	empty := BuildManifest(Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}, TpdbCategories: []string{}}, "", env, nil, nil, tubeGenreOptions{}, nil)
	if hasTpdb(empty["catalogs"].([]map[string]interface{})) {
		t.Fatal("explicit empty TpdbCategories must suppress the TPDB catalog (parts 2..N rely on this)")
	}

	filled := BuildManifest(Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}, TpdbCategories: []string{"parody"}}, "", env, nil, nil, tubeGenreOptions{}, nil)
	if !hasTpdb(filled["catalogs"].([]map[string]interface{})) {
		t.Fatal("non-empty TpdbCategories with an active server key must emit the TPDB catalog")
	}
}

func TestBuildManifestPornSearchCatalog(t *testing.T) {
	cfg := Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})

	var pornSearch map[string]interface{}
	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if id == PornSearchCatalogID {
			pornSearch = c
			break
		}
	}
	if pornSearch == nil {
		t.Fatal("expected search catalog")
	}
	if name, _ := pornSearch["name"].(string); name != "Search" {
		t.Fatalf("search catalog name = %q, want %q", name, "Search")
	}
	extras, _ := pornSearch["extra"].([]map[string]interface{})
	if len(extras) == 0 || extras[0]["name"] != "search" || extras[0]["isRequired"] != true {
		t.Fatal("expected required search extra on Search catalog")
	}
}

func TestBuildManifestMainXxxBrowseOnly(t *testing.T) {
	cfg := Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top", "recent"}}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})

	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if isPornSearchCatalogID(id) {
			continue
		}
		base := catalogBaseID(id)
		if !isMainXxxBrowseCatalog(base) {
			continue
		}
		if catalogHasSearchExtra(c) {
			t.Fatalf("main XXX catalog %q should be browse-only (no search extra)", id)
		}
	}
}

func TestBuildManifestTransStillSearchable(t *testing.T) {
	cfg := Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil, nil, tubeGenreOptions{}, nil)
	catalogs := manifest["catalogs"].([]map[string]interface{})

	found := false
	for _, c := range catalogs {
		id, _ := c["id"].(string)
		if !strings.HasPrefix(id, "xxx_trans") {
			continue
		}
		found = true
		if !catalogHasSearchExtra(c) {
			t.Fatalf("trans catalog %q should still participate in global search", id)
		}
	}
	if !found {
		t.Fatal("expected at least one xxx_trans catalog")
	}
}
