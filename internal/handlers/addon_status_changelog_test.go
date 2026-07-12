package handlers

import (
	"testing"

	"torrent-search-go/pkg/models"
)

func TestParseChangelogMarkdown(t *testing.T) {
	md := `# Changelog

Some preamble text.

## [Unreleased]

### Changed
- **Refactor** - extract X into Y.
- **Drop-in** - no behaviour change.

### Fixed
- Stream bug fixed.

## [1.9.40] - 2026-06-22

### Added
- Stripchat HLS proxy.

## [1.9.21] - 2026-06-22
- Account save/restore.
`
	got := parseChangelogMarkdown(md)
	want := []models.AddonChangelog{
		{Version: "Unreleased", Highlights: []string{
			"[Changed] Refactor - extract X into Y.",
			"[Changed] Drop-in - no behaviour change.",
			"[Fixed] Stream bug fixed.",
		}},
		{Version: "1.9.40", Date: "2026-06-22", Highlights: []string{"[Added] Stripchat HLS proxy."}},
		{Version: "1.9.21", Date: "2026-06-22", Highlights: []string{"Account save/restore."}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Version != want[i].Version || got[i].Date != want[i].Date || !sliceEq(got[i].Highlights, want[i].Highlights) {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestValidateChangelogURL(t *testing.T) {
	cases := []struct {
		url  string
		ok   bool
	}{
		{"", true},
		{"https://raw.githubusercontent.com/u/r/main/CHANGELOG.md", true},
		{"http://example.com/CHANGELOG.md", true},
		{"ftp://example.com/c.md", false},
		{"https://localhost/c.md", false},
		{"https://127.0.0.1/c.md", false},
		{"https://10.0.0.1/c.md", false},
		{"https://192.168.1.1/c.md", false},
		{"https://172.16.0.1/c.md", false},
		{"https://[::1]/c.md", false},
		{"https://[fc00::1]/c.md", false},
		{"https://[fe80::1]/c.md", false},
		{"not a url", false},
	}
	for _, c := range cases {
		err := validateChangelogURL(c.url)
		if (err == nil) != c.ok {
			t.Errorf("url %q: got err=%v, ok=%v", c.url, err, c.ok)
		}
	}
}

// TestIsPrivateHost pins the SSRF IP guard at save/redirect time. denyPrivateDial
// uses the same net.IP predicates at the socket layer; this is the runnable check
// for that path (a real dial test would need a private-bound listener).
func TestIsPrivateHost(t *testing.T) {
	bad := []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.1.1", "::1", "fc00::1", "fd00::1", "fe80::1", "0.0.0.0", "localhost"}
	for _, h := range bad {
		if !isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = false, want true", h)
		}
	}
	good := []string{"8.8.8.8", "1.1.1.1", "203.0.113.5", "2001:4860:4860::8888", "github.com"}
	for _, h := range good {
		if isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = true, want false", h)
		}
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNormalizeChangelogURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/ghostyshell/tpb-adult-stremio-addon/blob/main/CHANGELOG.md",
			"https://raw.githubusercontent.com/ghostyshell/tpb-adult-stremio-addon/main/CHANGELOG.md"},
		{"https://raw.githubusercontent.com/ghostyshell/tpb-adult-stremio-addon/main/CHANGELOG.md",
			"https://raw.githubusercontent.com/ghostyshell/tpb-adult-stremio-addon/main/CHANGELOG.md"},
		{"https://github.com/ghostyshell/tpb-adult-stremio-addon/blob/v1.9.40/docs/x.md",
			"https://raw.githubusercontent.com/ghostyshell/tpb-adult-stremio-addon/v1.9.40/docs/x.md"},
		{"https://example.com/CHANGELOG.md", "https://example.com/CHANGELOG.md"},
		{"https://github.com/ghostyshell/tpb-adult-stremio-addon", "https://github.com/ghostyshell/tpb-adult-stremio-addon"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeChangelogURL(c.in); got != c.want {
			t.Errorf("in=%q got=%q want=%q", c.in, got, c.want)
		}
	}
}