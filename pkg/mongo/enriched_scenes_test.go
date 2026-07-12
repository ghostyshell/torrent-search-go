package mongo

import (
	"testing"
	"time"
)

// TestDateCutoffFrom pins the future-date filter boundary: the cutoff is
// tomorrow (not today) so a same-day scene still surfaces, and it formats as a
// zero-padded YYYY-MM-DD string so the lexicographic `$lt` against the source's
// raw date string matches calendar order. Covers the timezone-sensitive path
// the catalog browse + category catalogs depend on.
func TestDateCutoffFrom(t *testing.T) {
	now := time.Date(2026, 7, 12, 23, 59, 0, 0, time.UTC)
	got := dateCutoffFrom(now)
	want := "2026-07-13"
	if got != want {
		t.Fatalf("dateCutoffFrom = %q, want %q", got, want)
	}
	// Same-day scene date stays below the cutoff (released today surfaces).
	if "2026-07-12" >= want {
		t.Fatalf("today's date not below cutoff: %q >= %q", "2026-07-12", want)
	}
	// A pre-release scene dated tomorrow is dropped (date >= cutoff).
	if "2026-07-13" < want {
		t.Fatalf("tomorrow's date below cutoff: %q < %q", "2026-07-13", want)
	}
	// Month/year rollover: July 31 -> August 1.
	rollover := dateCutoffFrom(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC))
	if rollover != "2026-08-01" {
		t.Fatalf("rollover cutoff = %q, want 2026-08-01", rollover)
	}
}