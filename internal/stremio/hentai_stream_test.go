package stremio

import (
	"context"
	"testing"

	"torrent-search-go/internal/services/hentai"
	hmodels "torrent-search-go/pkg/models"
)

// fakeHentaiResolver records the args it was called with and returns canned
// streams, so we can assert serveHentaiStream dispatches the right
// prefix/slug/sourceURL to the resolver.
type fakeHentaiResolver struct {
	prefix    string
	epSlug    string
	sourceURL string
	streams   []hentai.EpisodeStream
	err       error
}

func (f *fakeHentaiResolver) ResolveEpisodeStream(ctx context.Context, prefix, epSlug, sourceURL string) ([]hentai.EpisodeStream, error) {
	f.prefix, f.epSlug, f.sourceURL = prefix, epSlug, sourceURL
	return f.streams, f.err
}

func hentaiStreamEntry(id, prefix string, eps ...hmodels.HentaiEpisode) *hmodels.HentaiEntry {
	return &hmodels.HentaiEntry{ID: id, Prefix: prefix, Slug: "s", Episodes: eps}
}

func TestParseHentaiStreamID(t *testing.T) {
	cases := []struct {
		in           string
		wantSeries   string
		wantEp       int
		wantHentaiOK bool
	}{
		{"hmm-toshi-ie:1:3", "hmm-toshi-ie", 3, true},
		{"hmm-toshi-ie", "hmm-toshi-ie", 1, true},
		{"hmm-x:1:12", "hmm-x", 12, true},
		{"porndb:123", "", 0, false},
		{"htv-kuroinu:1:1", "", 0, false}, // htv- no longer routed as hentai
	}
	for _, c := range cases {
		gotSeries, gotEp := parseHentaiStreamID(c.in)
		isHentai := gotSeries != ""
		if gotSeries != c.wantSeries || gotEp != c.wantEp || isHentai != c.wantHentaiOK {
			t.Errorf("parseHentaiStreamID(%q) = (%q,%d,hentai=%v), want (%q,%d,%v)",
				c.in, gotSeries, gotEp, isHentai, c.wantSeries, c.wantEp, c.wantHentaiOK)
		}
	}
}

func TestServeHentaiStreamResolvesAndMaps(t *testing.T) {
	entry := hentaiStreamEntry("hmm-toshi-ie", "hmm",
		hmodels.HentaiEpisode{Number: 1, Slug: "toshi-ie-episode-1", SourceURL: "https://hm/ep/1"},
		hmodels.HentaiEpisode{Number: 3, Slug: "toshi-ie-episode-3", SourceURL: "https://hm/ep/3"},
	)
	store := &fakeHentaiStore{byID: entry}
	resolver := &fakeHentaiResolver{streams: []hentai.EpisodeStream{
		{URL: "https://x/v.mp4", Quality: "1080P", Name: "HentaiMama"},
		{URL: "https://x/v720.mp4", Quality: "", Name: ""},
	}}
	h := &Handler{Hentai: store, HentaiResolver: resolver}

	streams := h.serveHentaiStream(context.Background(), "hmm-toshi-ie:1:3")
	if len(streams) != 2 {
		t.Fatalf("streams = %d, want 2: %+v", len(streams), streams)
	}
	if resolver.prefix != "hmm" || resolver.epSlug != "toshi-ie-episode-3" || resolver.sourceURL != "https://hm/ep/3" {
		t.Fatalf("resolver dispatched (%q,%q,%q), want (hmm,toshi-ie-episode-3,https://hm/ep/3)",
			resolver.prefix, resolver.epSlug, resolver.sourceURL)
	}
	if streams[0]["url"] != "https://x/v.mp4" {
		t.Fatalf("stream[0] url = %v", streams[0]["url"])
	}
	if streams[0]["name"] != "HentaiMama E3 1080P" {
		t.Fatalf("stream[0] name = %q, want HentaiMama E3 1080P", streams[0]["name"])
	}
	if streams[1]["name"] != "Hentai E3" {
		t.Fatalf("stream[1] name = %q, want Hentai E3 (empty source+quality fallback)", streams[1]["name"])
	}
	bh, ok := streams[0]["behaviorHints"].(map[string]interface{})
	if !ok || bh["notWebReady"] != true {
		t.Fatalf("stream[0] behaviorHints = %+v, want notWebReady:true", streams[0]["behaviorHints"])
	}
}

func TestServeHentaiStreamBareIDDefaultsEp1(t *testing.T) {
	entry := hentaiStreamEntry("hmm-kuroinu", "hmm",
		hmodels.HentaiEpisode{Number: 1, Slug: "kuroinu-episode-1", SourceURL: "https://hm/ep/1"},
	)
	store := &fakeHentaiStore{byID: entry}
	resolver := &fakeHentaiResolver{streams: []hentai.EpisodeStream{{URL: "https://gd/v.mp4", Quality: "", Name: "HentaiMama"}}}
	h := &Handler{Hentai: store, HentaiResolver: resolver}

	streams := h.serveHentaiStream(context.Background(), "hmm-kuroinu")
	if len(streams) != 1 || resolver.epSlug != "kuroinu-episode-1" {
		t.Fatalf("bare id should default to ep1: streams=%d slug=%q", len(streams), resolver.epSlug)
	}
	if resolver.prefix != "hmm" || resolver.sourceURL != "https://hm/ep/1" {
		t.Fatalf("hmm dispatch wrong: prefix=%q sourceURL=%q", resolver.prefix, resolver.sourceURL)
	}
}

func TestServeHentaiStreamNilDepsReturnsNil(t *testing.T) {
	h := &Handler{}
	if s := h.serveHentaiStream(context.Background(), "hmm-x:1:1"); s != nil {
		t.Fatalf("nil resolver should return nil, got %+v", s)
	}
}

func TestServeHentaiStreamMissingEpisodeReturnsNil(t *testing.T) {
	entry := hentaiStreamEntry("hmm-x", "hmm",
		hmodels.HentaiEpisode{Number: 1, Slug: "x-1"},
	)
	store := &fakeHentaiStore{byID: entry}
	resolver := &fakeHentaiResolver{streams: []hentai.EpisodeStream{{URL: "https://x"}}}
	h := &Handler{Hentai: store, HentaiResolver: resolver}
	if s := h.serveHentaiStream(context.Background(), "hmm-x:1:99"); s != nil {
		t.Fatalf("missing episode 99 should return nil, got %+v", s)
	}
	if resolver.prefix != "" {
		t.Fatalf("resolver should not have been called for missing episode, got prefix=%q", resolver.prefix)
	}
}

func TestServeHentaiStreamEmptyResolveReturnsNil(t *testing.T) {
	entry := hentaiStreamEntry("hmm-x", "hmm", hmodels.HentaiEpisode{Number: 1, Slug: "x-1"})
	store := &fakeHentaiStore{byID: entry}
	resolver := &fakeHentaiResolver{streams: nil}
	h := &Handler{Hentai: store, HentaiResolver: resolver}
	if s := h.serveHentaiStream(context.Background(), "hmm-x:1:1"); s != nil {
		t.Fatalf("empty resolve should return nil, got %+v", s)
	}
}