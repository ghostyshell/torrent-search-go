package stremio

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"torrent-search-go/internal/services/metadata"
)

func TestLiveOnlyFansMetadataProbe(t *testing.T) {
	tpdbKey := os.Getenv("TPDB_KEY")
	if tpdbKey == "" {
		t.Skip("TPDB_KEY required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tpdb := metadata.NewTPDBClient("https://api.theporndb.net", tpdbKey)
	cases := []struct {
		title string
		want  string
	}{
		{"OnlyFans - Madison Ivy - Getting Stretched And Creampied By Girthmasterr rq mp4", "Madison vs. Girthmaster!"},
		{"OnlyFans - Yasmina Khan - Romantic Baby Making Sex With Brady Bud rq mp4", "Yasmina Khan"},
		{"OnlyFans - Anna Ralphs - Family Dinner rq mp4", ""},
	}
	for _, tc := range cases {
		m, _ := tpdb.SearchMetadataVariants(ctx, tc.title)
		var got string
		if m != nil && m.Poster != "" {
			got = m.Title
		}
		if tc.want == "" {
			if got != "" {
				t.Errorf("%s: unexpected match %q", tc.title, got)
			}
			continue
		}
		if got == "" {
			t.Errorf("%s: wanted match containing %q", tc.title, tc.want)
			continue
		}
		if tc.want != "" && !strings.Contains(strings.ToLower(got), strings.ToLower(tc.want)) {
			t.Errorf("%s: got %q want substring %q", tc.title, got, tc.want)
		}
	}
}
