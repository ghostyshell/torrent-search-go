package metadata

import "testing"

func TestParseJAVCode(t *testing.T) {
	cases := map[string]string{
		// Labeled censored codes: separator/case canonicalized, padding kept.
		"SSIS-001 Some Title Actress.mp4": "SSIS-001",
		"SSIS001":                         "SSIS-001",
		"[SSIS-00001] title":              "SSIS-00001",
		"MIDV-00123.1080p.mkv":            "MIDV-00123",
		"abp-123":                         "ABP-123",
		"ABF-342: Falling In Love":        "ABF-342",    // TPDB titles use a trailing colon
		"HEYZO 3877 GIRLS and BOUGA 143":  "HEYZO-3877", // site label split across tokens
		"[grp]STARS-456 actress":          "STARS-456",  // code behind a release-group prefix
		// Code at the end after an English description + studio tag (real tracker form).
		"Double J-cup Slim Big-breasted Exclusive Har MIDA-590 (MOODYZ)": "MIDA-590",
		"Titty Fuck Actor Audition A Big-dick Man Who MIDA-575.mkv":      "MIDA-575",
		// FC2 and uncensored date codes.
		"FC2-PPV-1234567":         "FC2-PPV-1234567",
		"Caribbeancom 010120-001": "010120-001",
		"010120_001 actress":      "010120-001",
		// Release-group / quality suffixes on the code token (Bitsearch-style names).
		"ACHJ-030-C":       "ACHJ-030",
		"ACHJ-030-FHD":     "ACHJ-030",
		"ACHJ-030-C GG5":   "ACHJ-030",
		"SSIS-001-U":       "SSIS-001",
		// Not JAV - codec/quality/Western tokens must yield no code.
		"Blacked 24 01 01 Some Scene XXX 2160p": "",
		"Some.Title.X264.DDP5.1080p":            "",
		"Vixen Scene Name 1080p":                "",
		"":                                      "",
	}
	for in, want := range cases {
		if got := parseJAVCode(in); got != want {
			t.Errorf("parseJAVCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPerformerNameMatches(t *testing.T) {
	yes := [][2]string{
		{"Mio Ishikawa", "Ishikawa Mio"}, // order-independent
		{"Mio Ishikawa", "Mio Ishikawa"},
		{"Yua Mikami", "yua  mikami"},
	}
	for _, c := range yes {
		if !performerNameMatches(c[0], c[1]) {
			t.Errorf("performerNameMatches(%q,%q) = false, want true", c[0], c[1])
		}
	}
	no := [][2]string{
		{"Mio Ishikawa", "Mio"},          // partial must not match
		{"Mio", "Mio Ishikawa"},          // partial must not match
		{"Mio Ishikawa", "Aoi Ishikawa"}, // different given name
		{"", "Mio Ishikawa"},
	}
	for _, c := range no {
		if performerNameMatches(c[0], c[1]) {
			t.Errorf("performerNameMatches(%q,%q) = true, want false", c[0], c[1])
		}
	}
}

func TestCodesMatch(t *testing.T) {
	// Padding variants of the same code match.
	if !CodesMatch("SSIS-001", "SSIS00001") {
		t.Error("SSIS-001 should match SSIS00001")
	}
	if !CodesMatch("MIDV-123", "MIDV-00123") {
		t.Error("MIDV-123 should match MIDV-00123")
	}
	// Different numbers or labels must not match.
	if CodesMatch("SSIS-001", "SSIS-002") {
		t.Error("SSIS-001 must not match SSIS-002")
	}
	if CodesMatch("SSIS-001", "ABP-001") {
		t.Error("SSIS-001 must not match ABP-001")
	}
	// A non-code never matches.
	if CodesMatch("not a code", "SSIS-001") {
		t.Error("non-code must not match")
	}
}

// Regression: a low-numbered code (001) extracted from a release name must still
// confirm against the provider's code field. The stored code must round-trip
// through CodesMatch - the bug was storing a zero-stripped form that re-parsed
// to "" and silently failed for series numbers 1-9.
func TestParsedCodeRoundTrip(t *testing.T) {
	p := ParseRelease("SSIS-001 Some Actress")
	if p.Code == "" {
		t.Fatal("expected a JAV code from SSIS-001 release")
	}
	if !CodesMatch(p.Code, "SSIS-001") {
		t.Errorf("stored code %q should match provider code SSIS-001", p.Code)
	}
	if !CodesMatch(p.Code, "SSIS-00001") {
		t.Errorf("stored code %q should match padded provider code SSIS-00001", p.Code)
	}
}
