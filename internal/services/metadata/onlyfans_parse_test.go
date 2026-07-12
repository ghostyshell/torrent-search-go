package metadata

import (
	"strings"
	"testing"
)

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

// Flat multi-performer title whose scene field is a string of co-performer
// aliases. performerPairProbes must form the canonical pair ("June Liu Emiri
// Momota") the scene is indexed under, VerifyMatch must accept the real scene
// on date+performer overlap, and a wrong-group probe's scene (Spicygum,
// different date) must be rejected.
func TestPerformerPairProbesSpicyGum(t *testing.T) {
	parsed := ParseRelease("OnlyFans 23 06 17 June Liu SpicyGum Juneliu Emiri Momota Mizukaw")
	if parsed.Studio != "OnlyFans" || parsed.Performer != "" {
		t.Fatalf("parse = %+v", parsed)
	}
	probes := performerPairProbes(parsed)
	want := "June Liu Emiri Momota"
	found := false
	for _, p := range probes {
		if p == want {
			found = true
		}
		t.Logf("  probe=%q", p)
	}
	if !found {
		t.Fatalf("probes %v missing canonical pair %q", probes, want)
	}

	// VerifyMatch accepts the real scene (date 2023-06-17, June Liu + Emiri
	// Momota) via the perfOK && dateExact branch.
	target := MatchCandidate{
		Title:      "First Bgg Threesome for My Japanese Friend Sumire",
		Studio:     "OnlyFans",
		Performers: []string{"June Liu", "Emiri Momota", "Sumire Mizukawa"},
		Date:       "2023-06-17",
	}
	if !VerifyMatch(parsed, target) {
		t.Fatalf("VerifyMatch rejected target scene; parsed=%+v", parsed)
	}

	// A Spicygum co-star scene from a different date must be rejected (the
	// wrong-group probe can't pull in the wrong scene).
	wrong := MatchCandidate{
		Title:      "Asian & Ginger Hot 3Some",
		Studio:     "OnlyFans",
		Performers: []string{"June Liu", "Liu Yue", "Spicygum"},
		Date:       "2024-03-01",
	}
	if VerifyMatch(parsed, wrong) {
		t.Fatalf("VerifyMatch accepted wrong-date Spicygum scene; parsed=%+v", parsed)
	}

	// Explicit co-star (dash) titles and dated non-OnlyFans titles must not get
	// pair probes (they have their own path or resolve via studio+date).
	dash := ParseRelease("OnlyFans - Madison Ivy - Getting Stretched And Creampied By Girthmasterr rq mp4")
	if pp := performerPairProbes(dash); pp != nil {
		t.Fatalf("dash title got pair probes %v; want nil", pp)
	}
	nonOF := ParseRelease("Blacked 26 06 12 Hope Heaven Pull Chapter 5 First 2160p MP4-WRB")
	if pp := performerPairProbes(nonOF); pp != nil {
		t.Fatalf("dated non-OnlyFans title got pair probes %v; want nil", pp)
	}
}

// Extra indexers (bitsearch) return the multi-performer scene without the
// "OnlyFans" prefix and without the date. The pair-probe path must still fire
// (re-prepending the Studio token that ParseRelease put the first name into) and
// VerifyPairDescriptor must accept the real scene on pair+descriptor overlap
// while rejecting a scene that shares only one performer.
func TestPerformerPairProbesBitsearchNoDate(t *testing.T) {
	// #1: alias soup, no OnlyFans prefix, no date.
	parsed := ParseRelease("June.Liu.SpicyGum.Juneliu.Emiri.Momota.Mizukawasumire.BGG.threesome.mp4")
	if parsed.Date != "" || parsed.Performer != "" {
		t.Fatalf("parse = %+v", parsed)
	}
	probes := performerPairProbes(parsed)
	want := "June Liu Emiri Momota"
	found := false
	for _, p := range probes {
		t.Logf("  #1 probe=%q", p)
		if p == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("#1 probes %v missing canonical pair %q", probes, want)
	}

	// #5: OnlyFans prefix + "aka" separators, no date. The canonical pair sits
	// at an odd 2-token offset, so the sliding window (not every-2) must reach it.
	aka := ParseRelease("OnlyFans.Emiri.Momota.aka.Mizukawa.Sumire.June.Liu.aka.JuneLiu.SpicyGum.BG.Roleplay.Group.Sex.With.Chinese.And.Japanese.Girls.MeetMyLove.mp4")
	if aka.Date != "" || aka.Performer != "" || !strings.EqualFold(aka.Studio, "OnlyFans") {
		t.Fatalf("aka parse = %+v", aka)
	}
	akaProbes := performerPairProbes(aka)
	akaWant := "Emiri Momota June Liu"
	akaFound := false
	for _, p := range akaProbes {
		t.Logf("  #5 probe=%q", p)
		if p == akaWant {
			akaFound = true
		}
	}
	if !akaFound {
		t.Fatalf("#5 probes %v missing canonical pair %q", akaProbes, akaWant)
	}

	// VerifyPairDescriptor accepts the real scene: both probe performers match
	// distinct candidate performers, and a descriptor token (bgg/threesome)
	// from the scene appears in the candidate title.
	real := MatchCandidate{
		Title:      "First Bgg Threesome for My Japanese Friend Sumire",
		Studio:     "OnlyFans",
		Performers: []string{"June Liu", "Emiri Momota", "Sumire Mizukawa"},
	}
	if !VerifyPairDescriptor(parsed, "June Liu", "Emiri Momota", real) {
		t.Fatalf("VerifyPairDescriptor rejected real scene; parsed=%+v", parsed)
	}

	// A scene with only one of the two performers must be rejected (the pair is
	// the gate; a single-performer match is not enough without a date).
	onePerf := MatchCandidate{
		Title:      "June Liu Solo Bedroom",
		Studio:     "OnlyFans",
		Performers: []string{"June Liu"},
	}
	if VerifyPairDescriptor(parsed, "June Liu", "Emiri Momota", onePerf) {
		t.Fatalf("VerifyPairDescriptor accepted single-performer scene")
	}

	// A scene with both performers but no descriptor overlap (the title shares
	// no scene token beyond the names) must be rejected - the descriptor is the
	// disambiguator the missing date would otherwise provide.
	noDescriptor := MatchCandidate{
		Title:      "Totally Unrelated Title Words Here Now",
		Studio:     "OnlyFans",
		Performers: []string{"June Liu", "Emiri Momota"},
	}
	if VerifyPairDescriptor(parsed, "June Liu", "Emiri Momota", noDescriptor) {
		t.Fatalf("VerifyPairDescriptor accepted scene with no descriptor overlap")
	}

	// NoDatePairProbeTitle must flag both no-date soups (so loadTpdbMeta collapses
	// its variant loop to one call) but not the dated OnlyFans soup, the dash-form
	// title, or the dated non-OnlyFans title (those use the variant loop / their
	// own paths).
	if !NoDatePairProbeTitle(parsed) || !NoDatePairProbeTitle(aka) {
		t.Fatalf("NoDatePairProbeTitle false for no-date soup")
	}
	dated := ParseRelease("OnlyFans 23 06 17 June Liu SpicyGum Juneliu Emiri Momota Mizukaw")
	if NoDatePairProbeTitle(dated) {
		t.Fatalf("NoDatePairProbeTitle true for dated title")
	}
	dash := ParseRelease("OnlyFans - Madison Ivy - Getting Stretched And Creampied By Girthmasterr rq mp4")
	if NoDatePairProbeTitle(dash) {
		t.Fatalf("NoDatePairProbeTitle true for dash-form title")
	}
	nonOF := ParseRelease("Blacked 26 06 12 Hope Heaven Pull Chapter 5 First 2160p MP4-WRB")
	if NoDatePairProbeTitle(nonOF) {
		t.Fatalf("NoDatePairProbeTitle true for dated non-OnlyFans title")
	}
}
