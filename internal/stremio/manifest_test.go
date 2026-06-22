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
	manifest := BuildManifest(cfg, "", nil, nil)
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
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil)

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
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil)
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
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil)
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

func TestBuildManifestPornSearchCatalog(t *testing.T) {
	cfg := Config{Sources: []string{"piratebay"}, EnabledSorts: []string{"top"}}
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil)
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
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil)
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
	manifest := BuildManifest(cfg, "", &appconfig.Config{}, nil)
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
