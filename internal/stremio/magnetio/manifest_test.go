package magnetio

import (
	"testing"
)

func TestBuildManifestBasic(t *testing.T) {
	cfg := ParseConfig("")
	m := BuildManifest(cfg, "https://example.com")
	if m["id"] != addonID {
		t.Fatalf("expected id %q, got %v", addonID, m["id"])
	}
	if m["name"] != addonName {
		t.Fatalf("expected name %q, got %v", addonName, m["name"])
	}
	resources, ok := m["resources"].([]map[string]interface{})
	if !ok || len(resources) == 0 {
		t.Fatal("expected resources")
	}
	catalogs, ok := m["catalogs"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected catalogs slice")
	}
	if len(catalogs) != 0 {
		t.Fatalf("expected no catalogs without debrid/tmdb keys, got %d", len(catalogs))
	}
}

func TestBuildManifestWithDebrid(t *testing.T) {
	cfg := ParseConfig("rd=secretkey|rdcatalog=true")
	m := BuildManifest(cfg, "https://example.com")
	catalogs := m["catalogs"].([]map[string]interface{})
	if len(catalogs) != 2 {
		t.Fatalf("expected 2 debrid catalogs, got %d", len(catalogs))
	}
	name := m["name"].(string)
	if name != "Magnetio +RD" {
		t.Fatalf("expected name with RD suffix, got %q", name)
	}
}

func TestBuildManifestWithTMDB(t *testing.T) {
	cfg := ParseConfig("tmdb=tmdbkey|streamingservices=netflix,prime|streamingcountry=us")
	m := BuildManifest(cfg, "")
	catalogs := m["catalogs"].([]map[string]interface{})
	if len(catalogs) != 6 { // 2 services x 2 types + 2 similar
		t.Fatalf("expected 6 catalogs, got %d", len(catalogs))
	}
}

func TestDummyManifest(t *testing.T) {
	m := DummyManifest("https://example.com")
	if m["id"] != addonID {
		t.Fatal("unexpected dummy manifest id")
	}
	catalogs := m["catalogs"].([]map[string]interface{})
	if len(catalogs) != 0 {
		t.Fatal("expected dummy manifest to have no catalogs")
	}
}
