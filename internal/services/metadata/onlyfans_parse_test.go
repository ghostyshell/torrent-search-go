package metadata

import "testing"

func TestParseOnlyFansDashTitles(t *testing.T) {
	p := ParseRelease("OnlyFans - Anna Ralphs - Family Dinner rq mp4")
	if p.Studio != "OnlyFans" || p.Performer != "Anna Ralphs" || p.Scene != "Family Dinner" {
		t.Fatalf("Anna Ralphs parse = %+v", p)
	}
	if got := PrimaryPerformer("Dainty Wilder, LittlePolishAngel aka Lena Polanski"); got != "Dainty Wilder" {
		t.Fatalf("PrimaryPerformer = %q", got)
	}
	probes := OnlyFansCoStarProbes("Madison Ivy", "Getting Stretched And Creampied By Girthmasterr")
	if len(probes) != 1 || probes[0] != "Madison Ivy Girthmasterr" {
		t.Fatalf("co-star probes = %v", probes)
	}
}

func TestVerifyMatchOnlyFansMadison(t *testing.T) {
	parsed := ParseRelease("OnlyFans - Madison Ivy - Getting Stretched And Creampied By Girthmasterr rq mp4")
	cand := MatchCandidate{
		Title:      "Madison vs. Girthmaster!",
		Studio:     "FansDB: Madison420ivy (onlyfans)",
		Performers: []string{"Madison Ivy", "Girthmasterr"},
	}
	if !VerifyMatch(parsed, cand) {
		t.Fatal("expected match for Madison Ivy Girthmasterr scene")
	}
}

func TestVerifyMatchOnlyFansYasmina(t *testing.T) {
	parsed := ParseRelease("OnlyFans - Yasmina Khan - Romantic Baby Making Sex With Brady Bud rq mp4")
	cand := MatchCandidate{
		Title:      "Couple Swap With Leo&Lulu And Yasmina Khan & Brady Bud",
		Studio:     "yasmina-brady",
		Performers: []string{"Yasmina Khan", "Brady Bud"},
	}
	if !VerifyMatch(parsed, cand) {
		t.Fatal("expected performer overlap match")
	}
}

func TestVerifyMatchAnnaRalphsFamilyDinnerProbe(t *testing.T) {
	orig := ParseRelease("OnlyFans - Anna Ralphs - Family Dinner rq mp4")
	cands := []struct {
		title  string
		studio string
		perf   []string
		want   bool
	}{
		{"Family Dinner", "Teen Sex Mania", []string{"Soniy Sweet"}, false},
		{"Anna Ralphs Couch Creampie", "FansDB: Anna.ralphs (onlyfans)", []string{"Anna Ralphs"}, true},
	}
	for _, c := range cands {
		got := VerifyMatch(orig, MatchCandidate{Title: c.title, Studio: c.studio, Performers: c.perf})
		if got != c.want {
			t.Fatalf("VerifyMatch(%q) = %v want %v", c.title, got, c.want)
		}
	}
}

func TestVerifyMatchDaintyWilderFoursome(t *testing.T) {
	parsed := ParseRelease("OnlyFans - Dainty Wilder, LittlePolishAngel aka Lena Polanski - Foursome rq mp4")
	cand := MatchCandidate{
		Title:      "My First Foursome",
		Studio:     "some site",
		Performers: []string{"Someone Else"},
	}
	if VerifyMatch(parsed, cand) {
		t.Fatal("should not match unrelated foursome scene")
	}
}
