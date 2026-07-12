package hentai

import "testing"

func TestHelpers(t *testing.T) {
	// rating parse: bare, /10, comma, 0-100 normalize, garbage
	cases := []struct{ in string; want float64 }{
		{"8.5", 8.5}, {"8.5/10", 8.5}, {"8,5", 8.5}, {"85", 8.5}, {"", 0}, {"abc", 0}, {"-1", 0},
	}
	for _, c := range cases {
		if got := parseRating(c.in); got != c.want {
			t.Errorf("parseRating(%q)=%v want %v", c.in, got, c.want)
		}
	}

	// episode number from slug / text
	if n := episodeNumFromSlug("foo-episode-3"); n != 3 {
		t.Errorf("episodeNumFromSlug=%d want 3", n)
	}
	if n := episodeNumFromSlug("foo"); n != 0 {
		t.Errorf("episodeNumFromSlug=%d want 0", n)
	}
	if n := parseEpisodeNumber("Episode 7"); n != 7 {
		t.Errorf("parseEpisodeNumber=%d want 7", n)
	}
	if n := parseEpisodeNumber("pilot"); n != 0 {
		t.Errorf("parseEpisodeNumber=%d want 0", n)
	}

	// quality detect + stream dedup/quality sort
	if q := detectQuality("https://x/v/1080p/ep.mp4"); q != "1080P" {
		t.Errorf("detectQuality=%q want 1080P", q)
	}
	got := dedupStreams([]EpisodeStream{
		{URL: "a.mp4", Quality: "360P"},
		{URL: "b.mp4", Quality: "1080P"},
		{URL: "a.mp4", Quality: "360P"},
	})
	if len(got) != 2 || got[0].Quality != "1080P" {
		t.Errorf("dedupStreams order/dedup=%v want [1080P,360P]", got)
	}
}