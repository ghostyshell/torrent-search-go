package stremio

import (
	"reflect"
	"testing"
)

func TestNormalizeEnabledSorts(t *testing.T) {
	tests := []struct {
		name           string
		values         []string
		defaultOnEmpty bool
		want           []string
	}{
		{"nil with default", nil, true, []string{"recent", "top"}},
		{"empty with default", []string{}, true, []string{"recent", "top"}},
		{"nil without default", nil, false, []string{}},
		{"empty without default", []string{}, false, []string{}},
		{"both preserved", []string{"recent", "top"}, false, []string{"recent", "top"}},
		{"only recent", []string{"recent"}, false, []string{"recent"}},
		{"only top", []string{"top"}, false, []string{"top"}},
		{"invalid values stripped", []string{"recent", "foo", "top", "bar"}, false, []string{"recent", "top"}},
		{"duplicates removed", []string{"recent", "recent", "top"}, false, []string{"recent", "top"}},
		{"whitespace trimmed", []string{" recent ", "top "}, false, []string{"recent", "top"}},
		{"all invalid without default", []string{"foo", "bar"}, false, []string{}},
		{"all invalid with default", []string{"foo", "bar"}, true, []string{"recent", "top"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeEnabledSorts(tc.values, tc.defaultOnEmpty)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeEnabledSorts(%v, %v) = %v, want %v", tc.values, tc.defaultOnEmpty, got, tc.want)
			}
		})
	}
}

func TestParseConfigEnabledSorts(t *testing.T) {
	cfg := parseConfig(EncodeConfig(Config{EnabledSorts: []string{"top"}}), envDefaults{})
	if !reflect.DeepEqual(cfg.EnabledSorts, []string{"top"}) {
		t.Fatalf("expected EnabledSorts [top], got %v", cfg.EnabledSorts)
	}
}

func TestParseConfigEnabledSortsDefaultsToBoth(t *testing.T) {
	cfg := parseConfig("", envDefaults{})
	if !reflect.DeepEqual(cfg.EnabledSorts, []string{"recent", "top"}) {
		t.Fatalf("expected default EnabledSorts [recent top], got %v", cfg.EnabledSorts)
	}
}

func TestParseConfigEnabledSortsHonoursExplicitEmpty(t *testing.T) {
	cfg := parseConfig(EncodeConfig(Config{EnabledSorts: []string{}}), envDefaults{})
	if len(cfg.EnabledSorts) != 0 {
		t.Fatalf("expected empty EnabledSorts, got %v", cfg.EnabledSorts)
	}
}
