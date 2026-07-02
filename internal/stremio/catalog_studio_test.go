package stremio

import "testing"

func TestResolveStudioQuery(t *testing.T) {
	tests := []struct {
		slug string
		want string
	}{
		{"brazzersexxtra", "BrazzersExxtra"},
		{"tokyo_hot", "Tokyo Hot"},
		{"groobygirls", "GroobyGirls"},
		{"s1_no_1_style", "S1 No.1 Style"},
		{"men_com", "Men.com"},
		{"unknown_studio", "unknown studio"},
	}
	for _, tc := range tests {
		if got := resolveStudioQuery(tc.slug); got != tc.want {
			t.Fatalf("slug %q: got %q want %q", tc.slug, got, tc.want)
		}
	}
}

func TestGetHbParamsStudioCatalogs(t *testing.T) {
	cases := []struct {
		catalogID string
		query     string
		category  string
		sort      string
	}{
		{"xxx_studio_brazzersexxtra_top", "BrazzersExxtra", "507", "7"},
		{"xxx_studio_tokyo_hot_fhd_top", "Tokyo Hot", "505", "7"},
		{"xxx_studio_groobygirls_recent", "GroobyGirls", "507", "3"},
	}
	for _, tc := range cases {
		p := getHbParams(tc.catalogID)
		if p == nil {
			t.Fatalf("%s: nil params", tc.catalogID)
		}
		if p.Query != tc.query || p.Category != tc.category || p.Sort != tc.sort {
			t.Fatalf("%s: got query=%q cat=%q sort=%q want query=%q cat=%q sort=%q",
				tc.catalogID, p.Query, p.Category, p.Sort, tc.query, tc.category, tc.sort)
		}
	}
}
