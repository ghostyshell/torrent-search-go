package metadata

import "testing"

func TestMetadataTitlesForLookup(t *testing.T) {
	titles := MetadataTitlesForLookup("FrolicMe.26.06.12.Julia.North.Squirter.XXX.1080p.HEVC.x265.PRT")
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

func TestMetadataTitlesForOnlyFans(t *testing.T) {
	titles := MetadataTitlesForLookup("OnlyFans - Madison Ivy - Getting Stretched And Creampied By Girthmasterr rq mp4")
	seen := make(map[string]struct{})
	for _, title := range titles {
		seen[title] = struct{}{}
	}
	for _, want := range []string{
		"Madison Ivy Getting Stretched And Creampied By Girthmasterr",
		"Madison Ivy Girthmasterr",
	} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing probe %q in %v", want, titles)
		}
	}
	if _, ok := seen["Getting Stretched And Creampied By Girthmasterr"]; ok {
		t.Fatal("should not probe performer-less OnlyFans scene title")
	}
}