package stremio

import "testing"

func TestIsPornSearchCatalogID(t *testing.T) {
	if !isPornSearchCatalogID("search") {
		t.Fatal("search should be a porn search catalog id")
	}
	if !isPornSearchCatalogID("jav_search") {
		t.Fatal("jav_search should remain supported for legacy installs")
	}
	if isPornSearchCatalogID("xxx_top") {
		t.Fatal("browse catalog ids must not match")
	}
}

func TestGetHbParamsSearchCatalog(t *testing.T) {
	for _, id := range []string{"search", "jav_search"} {
		p := getHbParams(id)
		if p == nil {
			t.Fatalf("getHbParams(%q) = nil", id)
		}
		if p.Query != "" {
			t.Fatalf("getHbParams(%q).Query = %q, want empty browse query", id, p.Query)
		}
	}
}
