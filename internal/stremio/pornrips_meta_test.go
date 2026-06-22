package stremio

import "testing"

func TestMetadataTitlesForLookup(t *testing.T) {
	titles := metadataTitlesForLookup("FrolicMe.26.06.12.Julia.North.Squirter.XXX.1080p.HEVC.x265.PRT")
	if len(titles) < 3 {
		t.Fatalf("expected multiple probes, got %v", titles)
	}
	seen := make(map[string]struct{})
	for _, title := range titles {
		seen[title] = struct{}{}
	}
	if _, ok := seen["FrolicMe.26.06.12.Julia.North.Squirter.XXX.1080p.HEVC.x265.PRT"]; !ok {
		t.Fatal("expected raw title probe")
	}
	if _, ok := seen["FrolicMe 26 06 12 Julia North Squirter XXX 1080p HEVC x265 PRT"]; !ok {
		t.Fatal("expected humanized title probe")
	}
}
