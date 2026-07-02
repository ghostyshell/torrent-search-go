package scraper

import (
	"context"
	"testing"
	"time"

	"torrent-search-go/internal/models"
)

type stubX1337Backend struct {
	searchCalls  int
	detailsCalls int
	searchOut    []models.Torrent
}

func (s *stubX1337Backend) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	s.searchCalls++
	return cloneTorrents(s.searchOut), nil
}

func (s *stubX1337Backend) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	s.detailsCalls++
	return &models.TorrentDetails{Website: "1337x", TorrentURL: torrentURL, MagnetLink: "magnet:?xt=urn:btih:abc"}, nil
}

func newTestX1337Cache(inner x1337Backend) *X1337Cache {
	return &X1337Cache{
		inner:   inner,
		search:  make(map[string]x1337SearchEntry),
		details: make(map[string]x1337DetailsEntry),
		pending: make(map[string]struct{}),
		jobs:    make(chan x1337Job, x1337JobQueueSize),
		ttl:     time.Hour,
	}
}

func TestX1337CacheMissReturnsEmptyAndQueuesRefresh(t *testing.T) {
	stub := &stubX1337Backend{searchOut: []models.Torrent{{Name: "ubuntu iso", Website: "1337x", Seeders: 10}}}
	c := newTestX1337Cache(stub)
	go c.worker()

	out, err := c.Search(context.Background(), "ubuntu", 1, models.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("miss should return empty slice, got %d", len(out))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stub.searchCalls > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stub.searchCalls != 1 {
		t.Fatalf("worker search calls = %d, want 1", stub.searchCalls)
	}

	out, err = c.Search(context.Background(), "ubuntu", 1, models.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "ubuntu iso" {
		t.Fatalf("hit = %+v", out)
	}
	if stub.searchCalls != 1 {
		t.Fatalf("cached read should not scrape again, calls = %d", stub.searchCalls)
	}
}

func TestX1337CacheSkipsEmptyResults(t *testing.T) {
	stub := &stubX1337Backend{}
	c := newTestX1337Cache(stub)
	go c.worker()

	_, _ = c.Search(context.Background(), "empty", 1, models.SearchOptions{})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stub.searchCalls > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stub.searchCalls != 1 {
		t.Fatalf("worker calls = %d, want 1", stub.searchCalls)
	}

	c.mu.Lock()
	_, cached := c.search[x1337SearchCacheKey("empty", 1, "0")]
	c.mu.Unlock()
	if cached {
		t.Fatal("empty search results should not be cached")
	}
}

func TestX1337CacheDedupesPendingJobs(t *testing.T) {
	c := newTestX1337Cache(&stubX1337Backend{})
	key := x1337SearchCacheKey("dup", 1, "")
	c.schedule(x1337Job{kind: x1337JobSearch, key: key, query: "dup", page: 1})
	c.schedule(x1337Job{kind: x1337JobSearch, key: key, query: "dup", page: 1})
	c.mu.Lock()
	n := len(c.pending)
	q := len(c.jobs)
	c.mu.Unlock()
	if n != 1 || q != 1 {
		t.Fatalf("pending=%d queue=%d, want 1/1", n, q)
	}
}

func TestX1337CacheSearchSkipsDuplicateQueueEntries(t *testing.T) {
	stub := &stubX1337Backend{searchOut: []models.Torrent{{Name: "ubuntu iso", Website: "1337x", Seeders: 10}}}
	c := newTestX1337Cache(stub)

	for i := 0; i < 5; i++ {
		out, err := c.Search(context.Background(), "ubuntu", 1, models.SearchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 0 {
			t.Fatalf("miss %d should return empty slice, got %d", i, len(out))
		}
	}

	c.mu.Lock()
	pending := len(c.pending)
	queued := len(c.jobs)
	c.mu.Unlock()
	if pending != 1 || queued != 1 {
		t.Fatalf("pending=%d queue=%d, want 1/1", pending, queued)
	}

	go c.worker()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stub.searchCalls > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stub.searchCalls != 1 {
		t.Fatalf("worker search calls = %d, want 1", stub.searchCalls)
	}
}
