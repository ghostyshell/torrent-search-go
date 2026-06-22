package stremio

import (
	"regexp"
	"strings"
	"testing"
)

// nodeStudioSafeId mirrors adultSections.js studioSafeId for parity checks.
func nodeStudioSafeId(studio string) string {
	s := strings.ToLower(studio)
	s = regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(s, "_")
	s = regexp.MustCompile(`_+`).ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func TestStudioSafeIDMatchesNode(t *testing.T) {
	for _, studio := range StudioPresets {
		got := studioSafeID(studio)
		want := nodeStudioSafeId(studio)
		if got != want {
			t.Fatalf("studio %q: go=%q node=%q", studio, got, want)
		}
	}
}
