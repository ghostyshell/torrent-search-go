package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestParseID(t *testing.T) {
	imdb, season, episode := ParseID("tt1234567:1:2")
	if imdb != "tt1234567" || season == nil || *season != 1 || episode == nil || *episode != 2 {
		t.Fatalf("unexpected parse: %s %v %v", imdb, season, episode)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if got := NormalizeTitle("Thackeray (2019)"); got != "thackeray 2019" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveMetadata(t *testing.T) {
	ctx := context.Background()
	meta, err := ResolveMetadata(ctx, nil, "movie", "tt7286456")
	if err != nil {
		t.Fatalf("cinemeta failed: %v", err)
	}
	if meta.Name == "" {
		t.Fatal("expected name")
	}
	if meta.Year != 2019 {
		t.Fatalf("expected year 2019, got %d", meta.Year)
	}
}

type fakeProvider struct {
	id      string
	name    string
	results []Stream
}

func (f *fakeProvider) ID() string { return f.id }
func (f *fakeProvider) Name() string {
	if f.name != "" {
		return f.name
	}
	return f.id
}
func (f *fakeProvider) Scrape(ctx context.Context, req Request) ([]Stream, error) {
	return f.results, nil
}

func TestServicePreferP2P(t *testing.T) {
	svc := NewService(nil)
	svc.Register(&fakeProvider{id: "p2p", results: []Stream{
		{InfoHash: "abc123", Title: "Joker 2019 1080p", Provider: "P2P"},
	}})
	svc.Register(&fakeProvider{id: "http", results: []Stream{
		{URL: "https://example.com/video", Title: "Joker 2019 480p", Provider: "HTTP"},
	}})

	result, err := svc.Scrape(context.Background(), "movie", "tt7286456", nil)
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}
	if len(result.Streams) != 1 {
		t.Fatalf("expected 1 p2p stream, got %d", len(result.Streams))
	}
	if result.Streams[0].InfoHash == "" {
		t.Fatal("expected p2p result, got http fallback")
	}
}

func TestServiceRetainsCoreHTTPStreamsWhenP2PDominates(t *testing.T) {
	svc := NewService(nil)
	var p2pResults []Stream
	for i := 0; i < 20; i++ {
		p2pResults = append(p2pResults, Stream{InfoHash: fmt.Sprintf("abc%d", i), Title: "Joker 2019 1080p", Provider: "Bitsearch"})
	}
	svc.Register(&fakeProvider{id: "bitsearch", results: p2pResults})
	svc.Register(&fakeProvider{id: "atishmkv", results: []Stream{
		{URL: "https://example.com/video", Title: "Joker 2019 480p", Provider: "AtishMKV"},
	}})
	svc.MarkCore("atishmkv")

	result, err := svc.Scrape(context.Background(), "movie", "tt7286456", []string{"bitsearch", "atishmkv"})
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}
	if len(result.Streams) != 21 {
		t.Fatalf("expected 21 streams (20 p2p + 1 core http), got %d", len(result.Streams))
	}
}

func TestServiceRetainsCoreHTTPStreams(t *testing.T) {
	svc := NewService(nil)
	svc.Register(&fakeProvider{id: "p2p", results: []Stream{
		{InfoHash: "abc123", Title: "Joker 2019 1080p", Provider: "P2P"},
	}})
	svc.Register(&fakeProvider{id: "atishmkv", results: []Stream{
		{URL: "https://example.com/video", Title: "Joker 2019 480p", Provider: "AtishMKV"},
	}})
	svc.MarkCore("atishmkv")

	result, err := svc.Scrape(context.Background(), "movie", "tt7286456", nil)
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}
	if len(result.Streams) != 2 {
		t.Fatalf("expected 2 streams (p2p + core http), got %d", len(result.Streams))
	}
	if result.Streams[0].InfoHash == "" {
		t.Fatal("expected p2p stream first")
	}
	if result.Streams[1].URL == "" {
		t.Fatal("expected core http stream to be retained")
	}
}

func TestServiceFallsBackToHTTP(t *testing.T) {
	svc := NewService(nil)
	svc.Register(&fakeProvider{id: "http", results: []Stream{
		{URL: "https://example.com/video", Title: "Joker 2019 480p", Provider: "HTTP"},
	}})

	result, err := svc.Scrape(context.Background(), "movie", "tt7286456", nil)
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}
	if len(result.Streams) != 1 {
		t.Fatalf("expected 1 http stream, got %d", len(result.Streams))
	}
	if result.Streams[0].URL == "" {
		t.Fatal("expected http result")
	}
}

type fakeCache struct {
	data map[string][]byte
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string][]byte)}
}

func (f *fakeCache) Get(ctx context.Context, key string) (string, bool, error) {
	val, ok := f.data[key]
	if !ok {
		return "", false, nil
	}
	return string(val), true, nil
}

func (f *fakeCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	switch v := value.(type) {
	case string:
		f.data[key] = []byte(v)
	case []byte:
		f.data[key] = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		f.data[key] = data
	}
	return nil
}

func (f *fakeCache) IsConfigured() bool { return true }

func TestServiceListProvidersCoreFlag(t *testing.T) {
	svc := NewService(nil)
	svc.Register(&fakeProvider{id: "knaben", name: "Knaben", results: []Stream{}})
	svc.Register(&fakeProvider{id: "atishmkv", name: "AtishMKV", results: []Stream{}})
	svc.MarkCore("atishmkv")

	providers := svc.ListProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(providers))
	}
	for _, p := range providers {
		want := "false"
		if p["id"] == "atishmkv" {
			want = "true"
		}
		if p["core"] != want {
			t.Fatalf("provider %q core=%q, want %q", p["id"], p["core"], want)
		}
	}
}

func TestServiceCacheHit(t *testing.T) {
	cache := newFakeCache()
	svc := NewService(nil, WithCache(cache, time.Hour))
	svc.Register(&fakeProvider{id: "p2p", results: []Stream{
		{InfoHash: "abc123", Title: "Joker 2019 1080p", Provider: "P2P"},
	}})

	_, err := svc.Scrape(context.Background(), "movie", "tt7286456", nil)
	if err != nil {
		t.Fatalf("first scrape failed: %v", err)
	}

	// Replace provider with empty results; cache should still return the first result.
	svc = NewService(nil, WithCache(cache, time.Hour))
	svc.Register(&fakeProvider{id: "p2p", results: []Stream{}})
	result, err := svc.Scrape(context.Background(), "movie", "tt7286456", nil)
	if err != nil {
		t.Fatalf("second scrape failed: %v", err)
	}
	if !result.Cached {
		t.Fatal("expected cached result")
	}
	if len(result.Streams) != 1 || result.Streams[0].InfoHash != "abc123" {
		t.Fatalf("expected cached stream, got %+v", result.Streams)
	}
}

func TestServiceCacheDisabled(t *testing.T) {
	svc := NewService(nil)
	svc.Register(&fakeProvider{id: "p2p", results: []Stream{
		{InfoHash: "abc123", Title: "Joker 2019 1080p", Provider: "P2P"},
	}})

	result, err := svc.Scrape(context.Background(), "movie", "tt7286456", nil)
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}
	if result.Cached {
		t.Fatal("expected uncached result when no cache configured")
	}
}
