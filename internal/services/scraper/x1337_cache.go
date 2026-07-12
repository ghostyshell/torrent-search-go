package scraper

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/models"
)

const (
	defaultX1337CacheTTL   = 30 * time.Minute
	x1337WorkerTimeout     = 90 * time.Second
	x1337JobQueueSize      = 256
)

type x1337Backend interface {
	Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error)
	GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error)
}

type x1337JobKind int

const (
	x1337JobSearch x1337JobKind = iota
	x1337JobDetails
)

type x1337Job struct {
	kind  x1337JobKind
	key   string
	query string
	page  int
	sort  string
	url   string
}

type x1337SearchEntry struct {
	torrents []models.Torrent
	at       time.Time
}

type x1337DetailsEntry struct {
	details *models.TorrentDetails
	at      time.Time
}

// X1337Cache serves reads from cache only; misses return empty and enqueue a background refresh.
type X1337Cache struct {
	inner   x1337Backend
	mu      sync.Mutex
	search  map[string]x1337SearchEntry
	details map[string]x1337DetailsEntry
	pending map[string]struct{}
	jobs    chan x1337Job
	ttl     time.Duration
}

// NewX1337Cache wraps the real 1337x scraper with a single-worker refresh queue.
func NewX1337Cache(inner *X1337Scraper) *X1337Cache {
	c := &X1337Cache{
		inner:   inner,
		search:  make(map[string]x1337SearchEntry),
		details: make(map[string]x1337DetailsEntry),
		pending: make(map[string]struct{}),
		jobs:    make(chan x1337Job, x1337JobQueueSize),
		ttl:     x1337CacheTTL(),
	}
	go c.worker()
	return c
}

func x1337CacheTTL() time.Duration {
	if v, err := strconv.Atoi(os.Getenv("X1337_CACHE_TTL_SECONDS")); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return defaultX1337CacheTTL
}

func x1337SearchCacheKey(query string, page int, sort string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if page < 1 {
		page = 1
	}
	if sort == "" {
		sort = "0"
	}
	return fmt.Sprintf("search:%s:%d:%s", q, page, sort)
}

func x1337DetailsCacheKey(url string) string {
	return "details:" + strings.TrimSpace(url)
}

func cloneTorrents(in []models.Torrent) []models.Torrent {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.Torrent, len(in))
	copy(out, in)
	return out
}

// Search returns cached torrents or empty on miss; refresh runs in the background queue.
func (c *X1337Cache) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	key := x1337SearchCacheKey(query, page, options.Sort)
	if t, ok := c.getSearch(key); ok {
		return t, nil
	}
	c.schedule(x1337Job{
		kind:  x1337JobSearch,
		key:   key,
		query: query,
		page:  page,
		sort:  options.Sort,
	})
	return []models.Torrent{}, nil
}

// SearchLive bypasses cache (health checks and ops).
func (c *X1337Cache) SearchLive(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	return c.inner.Search(ctx, query, page, options)
}

// GetDetails returns cached details or a cache-miss placeholder; refresh is queued.
func (c *X1337Cache) GetDetails(ctx context.Context, torrentURL string) (*models.TorrentDetails, error) {
	key := x1337DetailsCacheKey(torrentURL)
	if d, ok := c.getDetails(key); ok {
		return d, nil
	}
	c.schedule(x1337Job{kind: x1337JobDetails, key: key, url: torrentURL})
	return failedLegacyDetails("1337x details not cached yet"), nil
}

func (c *X1337Cache) getSearch(key string) ([]models.Torrent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.search[key]
	if !ok || len(e.torrents) == 0 || time.Since(e.at) > c.ttl {
		return nil, false
	}
	return cloneTorrents(e.torrents), true
}

func (c *X1337Cache) getDetails(key string) (*models.TorrentDetails, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.details[key]
	if !ok || e.details == nil || time.Since(e.at) > c.ttl {
		return nil, false
	}
	return e.details, true
}

// schedule enqueues a refresh job unless the same key is already queued or in flight.
func (c *X1337Cache) schedule(job x1337Job) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.pending[job.key]; ok {
		return
	}
	select {
	case c.jobs <- job:
		c.pending[job.key] = struct{}{}
	default:
	}
}

func (c *X1337Cache) worker() {
	for job := range c.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), x1337WorkerTimeout)
		switch job.kind {
		case x1337JobSearch:
			torrents, err := c.inner.Search(ctx, job.query, job.page, models.SearchOptions{Sort: job.sort})
			if err == nil && len(torrents) > 0 {
				c.mu.Lock()
				c.search[job.key] = x1337SearchEntry{torrents: cloneTorrents(torrents), at: time.Now()}
				c.mu.Unlock()
			}
		case x1337JobDetails:
			details, err := c.inner.GetDetails(ctx, job.url)
			if err == nil && details != nil && details.Error == "" {
				c.mu.Lock()
				c.details[job.key] = x1337DetailsEntry{details: details, at: time.Now()}
				c.mu.Unlock()
			}
		}
		cancel()
		c.mu.Lock()
		delete(c.pending, job.key)
		c.mu.Unlock()
	}
}

var (
	_ Scraper        = (*X1337Cache)(nil)
	_ DetailsScraper = (*X1337Cache)(nil)
)
