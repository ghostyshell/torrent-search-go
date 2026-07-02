package metadata

import "testing"

func TestParseReleaseStudioDateScene(t *testing.T) {
	cases := []struct {
		title  string
		studio string
		date   string
		scene  string
	}{
		{"EvilAngel 24 05 28 Summer Vixen XXX 2160p", "EvilAngel", "2024-05-28", "Summer Vixen"},
		{"PurgatoryX 24 10 11 Venus Vixen XXX 1080p", "PurgatoryX", "2024-10-11", "Venus Vixen"},
		{"BangBros18 24 03 24 Renee Rose XXX", "BangBros18", "2024-03-24", "Renee Rose"},
		{"JOIBabes 23 12 04 Venus Vixen JOI", "JOIBabes", "2023-12-04", "Venus Vixen JOI"},
		{"ALSScan 23 12 08 Venus Vixen", "ALSScan", "2023-12-08", "Venus Vixen"},
		{"Bang YNGR 24 04 19 Venus Vixen", "Bang YNGR", "2024-04-19", "Venus Vixen"},
	}
	for _, tc := range cases {
		p := ParseRelease(tc.title)
		if p.Studio != tc.studio {
			t.Errorf("%q studio=%q want %q", tc.title, p.Studio, tc.studio)
		}
		if p.Date != tc.date {
			t.Errorf("%q date=%q want %q", tc.title, p.Date, tc.date)
		}
		if p.Scene != tc.scene {
			t.Errorf("%q scene=%q want %q", tc.title, p.Scene, tc.scene)
		}
	}
}

func TestVerifyMatchRequiresDateWhenTorrentHasDate(t *testing.T) {
	parsed := ParseRelease("EvilAngel 24 05 28 Summer Vixen XXX 2160p")
	wrongDate := MatchCandidate{
		Title:  "Summer Vixen & Vince Karter",
		Studio: "Evil Angel",
		Date:   "2026-05-11",
	}
	if VerifyMatch(parsed, wrongDate) {
		t.Fatal("studio+performer overlap must not match when release dates differ by years")
	}
	rightDate := MatchCandidate{
		Title:  "Summer Vixen Squirting + Anal Gaping",
		Studio: "Evil Angel",
		Date:   "2024-05-28",
	}
	if !VerifyMatch(parsed, rightDate) {
		t.Fatal("expected studio+date+performer match")
	}
}

// TestVerifyMatchRejectsWrongStudioWrongDate covers the cover-mismatch leak:
// a candidate from a different studio with a date years off used to slip
// through on title-token overlap alone (PerformerOverlap falls back to the
// candidate title, so generic scene words counted as a performer hit).
func TestVerifyMatchRejectsWrongStudioWrongDate(t *testing.T) {
	parsed := ParseRelease("EvilAngel 24 05 28 Summer Vixen XXX 2160p")
	wrong := MatchCandidate{
		Title:  "Summer Vixen Solo",
		Studio: "Brazzers",
		Date:   "2026-01-01",
	}
	if VerifyMatch(parsed, wrong) {
		t.Fatal("different studio + years-off date must not match on title tokens")
	}
}

// TestVerifyMatchDatelessNeedsStudio: a candidate with no date (e.g. a movie
// record) must agree on studio before a title-token match is trusted.
func TestVerifyMatchDatelessNeedsStudio(t *testing.T) {
	parsed := ParseRelease("EvilAngel 24 05 28 Summer Vixen XXX 2160p")
	wrongStudio := MatchCandidate{Title: "Summer Vixen Compilation", Studio: "Brazzers"}
	if VerifyMatch(parsed, wrongStudio) {
		t.Fatal("dateless candidate with mismatched studio must not match")
	}
	rightStudio := MatchCandidate{Title: "Summer Vixen Compilation", Studio: "Evil Angel"}
	if !VerifyMatch(parsed, rightStudio) {
		t.Fatal("dateless candidate with matching studio + title should match")
	}
}

// TestVerifyMatchStudioNameMismatchButDateLinesUp guards the legit case the
// fix must preserve: TPDB/StashDB often name the parent network rather than the
// torrent's studio token, so a date-aligned scene with a good title must still
// match even when the studio string differs.
func TestVerifyMatchStudioNameMismatchButDateLinesUp(t *testing.T) {
	parsed := ParseRelease("Blacked 24 05 28 Hope Heaven Pull XXX 2160p")
	c := MatchCandidate{
		Title:  "Hope Heaven Pull",
		Studio: "Vixen Media Group",
		Date:   "2024-05-29", // 1 day off: dateClose but not exact, exercises the dated branch
	}
	if !VerifyMatch(parsed, c) {
		t.Fatal("date-aligned scene with good title must match despite studio-name mismatch")
	}
}
