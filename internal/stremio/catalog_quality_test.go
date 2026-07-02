package stremio

import "testing"

func TestFilterByQualityScopeDrops720pFromFhdCatalog(t *testing.T) {
	in := []catalogTorrent{{Title: "Scene 1080p WEB-DL"}, {Title: "Scene 720p WEB-DL"}}
	got := filterByQualityScope(in, "fhd")
	if len(got) != 1 || got[0].Title != "Scene 1080p WEB-DL" {
		t.Fatalf("filterByQualityScope(fhd) = %#v, want only 1080p", got)
	}
}

func TestFilterByStampedQualityDrops720pFhdMember(t *testing.T) {
	in := []catalogTorrent{
		{Title: "Trans.Active.35.2026.720p", Quality: "fhd"},
		{Title: "Scene 1080p WEB-DL", Quality: "fhd"},
	}
	got := filterByStampedQuality(in)
	if len(got) != 1 || got[0].Title != "Scene 1080p WEB-DL" {
		t.Fatalf("filterByStampedQuality = %#v, want only 1080p fhd member", got)
	}
}

func TestFilterByQualityScopeReappliedOnCachedFhdList(t *testing.T) {
	cached := []catalogTorrent{{Title: "Only 720p release WEB-DL 720p"}}
	got := filterByQualityScope(cached, catalogQualityScope("xxx_trans_fhd_recent"))
	if len(got) != 0 {
		t.Fatalf("cached fhd list should drop 720p, got %#v", got)
	}
}

// "FULL HD" (space) must tag 1080p, not fall through to the 720p \bhd\b rule.
// The prior `full\.?hd` (literal dot) missed it; myporn.club browse results
// were tagged 720p and dropped out of every quality-scoped xxx_ catalog.
func TestDetectQualityFullHDWithSpaceIs1080p(t *testing.T) {
	cases := map[string]string{
		"Scene FULL HD -> https://lulustream.com/x":  "1080p",
		"Scene Full.HD WEB-DL":                       "1080p",
		"Scene fullhd":                                "1080p",
		"Scene 1080p WEB-DL":                          "1080p",
		"Scene 720p WEB-DL":                           "720p",
		"Scene with no quality tag":                   "unknown",
	}
	for title, want := range cases {
		if got := detectQuality(title); got != want {
			t.Errorf("detectQuality(%q) = %q, want %q", title, got, want)
		}
	}
}
