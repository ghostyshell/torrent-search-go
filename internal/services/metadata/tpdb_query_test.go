package metadata

import "testing"

func TestParseCleanQueryBlacked(t *testing.T) {
	p := ParseRelease("Blacked 26 06 12 Hope Heaven Pull Chapter 5 First 2160p MP4-WRB")
	t.Logf("Studio=%q Date=%q Scene=%q CleanQuery=%q", p.Studio, p.Date, p.Scene, p.CleanQuery)
	if p.Studio != "Blacked" {
		t.Errorf("Studio = %q, want Blacked", p.Studio)
	}
	if p.Date != "2026-06-12" {
		t.Errorf("Date = %q, want 2026-06-12", p.Date)
	}
	// Trace the shortening sequence SearchMetadataProbe would use.
	q := p.CleanQuery
	for n := 0; n < 4; n++ {
		t.Logf("  try q=%q", q)
		words := splitFields(q)
		if len(words) <= 3 {
			break
		}
		q = joinWords(words[:len(words)-1])
	}
}

func TestParseCleanQueryEvilAngel(t *testing.T) {
	p := ParseRelease("EvilAngel 26 05 24 TS Mia Bahianinha XXX 2160p MP4")
	t.Logf("Studio=%q Date=%q Scene=%q CleanQuery=%q", p.Studio, p.Date, p.Scene, p.CleanQuery)
}

// VerifyMatch must accept the real TPDB scene for the Blacked favorite once the
// query is clean enough for TPDB to return it.
func TestVerifyMatchBlackedScene(t *testing.T) {
	p := ParseRelease("Blacked 26 06 12 Hope Heaven Pull Chapter 5 First 2160p MP4-WRB")
	cand := MatchCandidate{
		Title:      "Pull Chapter 5: Flight",
		Studio:     "Blacked",
		Performers: []string{"Hope Heaven", "Troy Francisco"},
		Date:       "2026-06-12",
	}
	if !VerifyMatch(p, cand) {
		t.Errorf("VerifyMatch rejected the correct Blacked scene; parsed=%+v cand=%+v", p, cand)
	}
}

// Exact title the MetaEnricher receives for the Blacked Hope Heaven favorite.
func TestParseBlackedHopeHeavenExact(t *testing.T) {
	p := ParseRelease("Blacked 26 06 12 Hope Heaven Pull Chapter 5 Flight XXX 2160p MP4")
	t.Logf("Studio=%q Date=%q Scene=%q CleanQuery=%q Performer=%q", p.Studio, p.Date, p.Scene, p.CleanQuery, p.Performer)
	cand := MatchCandidate{
		Title:      "Pull Chapter 5: Flight",
		Studio:     "Blacked",
		Performers: []string{"Hope Heaven", "Troy Francisco"},
		Date:       "2026-06-12",
	}
	if !VerifyMatch(p, cand) {
		t.Errorf("VerifyMatch rejected Blacked Hope Heaven; parsed=%+v", p)
	}
	// trace shortening
	q := p.CleanQuery
	for n := 0; n < 4; n++ {
		t.Logf("  try q=%q", q)
		words := splitFields(q)
		if len(words) <= 3 {
			break
		}
		q = joinWords(words[:len(words)-1])
	}
}

func joinWords(w []string) string {
	out := ""
	for i, x := range w {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}