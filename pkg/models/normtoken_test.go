package models

import "testing"

func TestNormToken(t *testing.T) {
	cases := map[string]string{
		"Adult Time": "adulttime",
		"AdultTime":  "adulttime",
		"adult-time": "adulttime",
		"adult_time": "adulttime",
		"5K Porn":    "5kporn",
		"BANG!":      "bang",
		"MILF AF":    "milfaf",
		"ATKingdom":  "atkingdom",
		"":           "",
		"  ":         "",
		"!!!":        "",
	}
	for in, want := range cases {
		got := NormToken(in)
		if got != want {
			t.Errorf("NormToken(%q) = %q, want %q", in, got, want)
		}
	}
}