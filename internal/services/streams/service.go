package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache is the minimal interface the stream service needs for result caching.
type Cache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	IsConfigured() bool
}

// Service coordinates all stream providers.
type Service struct {
	providers   []Provider
	coreIDs     map[string]struct{}
	client      *http.Client
	concurrency int
	timeout     time.Duration
	minResults  int
	coreWait    time.Duration
	hardTimeout time.Duration
	cache       Cache
	cacheTTL    time.Duration
}

// Option configures the service.
type Option func(*Service)

// WithConcurrency sets the maximum number of providers that run in parallel.
func WithConcurrency(n int) Option {
	return func(s *Service) { s.concurrency = n }
}

// WithTimeouts sets the core and hard deadlines.
func WithTimeouts(core, hard time.Duration) Option {
	return func(s *Service) {
		s.coreWait = core
		s.hardTimeout = hard
	}
}

// WithMinEarlyResults sets how many results are required before early return.
func WithMinEarlyResults(n int) Option {
	return func(s *Service) { s.minResults = n }
}

// WithCache enables result caching with the given TTL.
func WithCache(cache Cache, ttl time.Duration) Option {
	return func(s *Service) {
		s.cache = cache
		s.cacheTTL = ttl
	}
}

// NewService creates an empty stream service.
func NewService(client *http.Client, opts ...Option) *Service {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	s := &Service{
		providers:   make([]Provider, 0),
		coreIDs:     make(map[string]struct{}),
		client:      client,
		concurrency: 6,
		timeout:     15 * time.Second,
		minResults:  10,
		coreWait:    7 * time.Second,
		hardTimeout: 15 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register adds a provider. Providers added first run first when ordering matters.
func (s *Service) Register(p Provider) {
	s.providers = append(s.providers, p)
}

// MarkCore marks a provider ID as core. Early return waits for all core providers.
func (s *Service) MarkCore(id string) {
	s.coreIDs[id] = struct{}{}
}

// ListProviders returns the registered providers.
func (s *Service) ListProviders() []map[string]string {
	out := make([]map[string]string, len(s.providers))
	for i, p := range s.providers {
		out[i] = map[string]string{
			"id":   p.ID(),
			"name": p.Name(),
			"core": strconv.FormatBool(s.isCore(p.ID())),
		}
	}
	return out
}

// Scrape resolves metadata and runs all enabled providers.
func (s *Service) Scrape(ctx context.Context, typ, id string, providerIDs []string) (*Result, error) {
	providerKey := "all"
	if len(providerIDs) > 0 {
		providerKey = strings.Join(providerIDs, ",")
	}
	cacheKey := fmt.Sprintf("streams:v2:%s:%s:%s", typ, id, providerKey)

	if cached := s.loadCache(ctx, cacheKey); cached != nil {
		cached.Cached = true
		return cached, nil
	}

	meta, err := ResolveMetadata(ctx, s.client, typ, id)
	if err != nil {
		return nil, err
	}

	req := Request{
		Type:   typ,
		ID:     id,
		Name:   meta.Name,
		Year:   meta.Year,
		IMDbID: meta.IMDbID,
	}
	if typ == "series" || typ == "anime" {
		_, season, episode := ParseID(id)
		req.Season = season
		req.Episode = episode
		if season != nil && episode != nil {
			req.AbsoluteEpisode = meta.ToAbsolute(*season, *episode)
		}
	}

	providers := s.filterProviders(providerIDs)
	if len(providers) == 0 {
		return &Result{Streams: []Stream{}}, nil
	}

	coreNames := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if s.isCore(p.ID()) {
			coreNames[strings.ToLower(p.Name())] = struct{}{}
		}
	}

	start := time.Now()
	streams := s.runProviders(ctx, providers, req)
	deduped := deduplicate(streams)
	matched := filterByContent(deduped, req)
	result := preferP2P(matched, coreNames)

	res := &Result{
		Streams: result,
		Took:    time.Since(start),
	}
	s.storeCache(ctx, cacheKey, res)
	return res, nil
}

func (s *Service) loadCache(ctx context.Context, key string) *Result {
	if s.cache == nil || !s.cache.IsConfigured() || s.cacheTTL <= 0 {
		return nil
	}
	val, ok, err := s.cache.Get(ctx, key)
	if err != nil || !ok {
		return nil
	}
	var cached Result
	if err := json.Unmarshal([]byte(val), &cached); err != nil {
		return nil
	}
	return &cached
}

func (s *Service) storeCache(ctx context.Context, key string, res *Result) {
	if s.cache == nil || !s.cache.IsConfigured() || s.cacheTTL <= 0 {
		return
	}
	if len(res.Streams) == 0 {
		return
	}
	data, err := json.Marshal(res)
	if err != nil {
		return
	}
	_ = s.cache.Set(ctx, key, data, s.cacheTTL)
}

func (s *Service) filterProviders(ids []string) []Provider {
	if len(ids) == 0 {
		return s.providers
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	out := make([]Provider, 0, len(ids))
	for _, p := range s.providers {
		if _, ok := want[strings.ToLower(p.ID())]; ok {
			out = append(out, p)
		}
	}
	return out
}

func (s *Service) runProviders(ctx context.Context, providers []Provider, req Request) []Stream {
	if len(providers) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.hardTimeout)
	defer cancel()

	collected := make([]Stream, 0)
	var mu sync.Mutex
	state := &runState{}
	for _, p := range providers {
		if s.isCore(p.ID()) {
			state.pendingCore++
		}
	}

	sem := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup

	for _, p := range providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results, err := p.Scrape(ctx, req)
			if err != nil {
				return
			}

			mu.Lock()
			collected = append(collected, results...)
			state.resolved++
			if s.isCore(p.ID()) {
				state.pendingCore--
			}
			mu.Unlock()
		}(p)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	coreTimer := time.NewTimer(s.coreWait)
	defer coreTimer.Stop()

	for {
		select {
		case <-done:
			return collected
		case <-coreTimer.C:
			mu.Lock()
			ready := len(collected) >= s.minResults && state.pendingCore <= 0
			mu.Unlock()
			if ready {
				cancel()
				<-done
				return collected
			}
		case <-ctx.Done():
			<-done
			return collected
		}
	}
}

type runState struct {
	resolved    int
	pendingCore int
}

func (s *Service) isCore(id string) bool {
	_, ok := s.coreIDs[id]
	return ok
}

func deduplicate(streams []Stream) []Stream {
	byHash := make(map[string]Stream)
	byURL := make(map[string]Stream)
	var httpOrder []string

	for _, st := range streams {
		if st.InfoHash != "" {
			existing, ok := byHash[st.InfoHash]
			if !ok || st.Seeders > existing.Seeders {
				byHash[st.InfoHash] = st
			}
			continue
		}
		if st.URL != "" {
			if _, ok := byURL[st.URL]; !ok {
				byURL[st.URL] = st
				httpOrder = append(httpOrder, st.URL)
			}
		}
	}

	out := make([]Stream, 0, len(byHash)+len(byURL))
	for _, st := range byHash {
		out = append(out, st)
	}
	for _, url := range httpOrder {
		out = append(out, byURL[url])
	}
	return out
}

func filterByContent(streams []Stream, req Request) []Stream {
	if req.Name == "" {
		return streams
	}
	nameWords := nameWords(req.Name)
	if len(nameWords) == 0 {
		return streams
	}

	phraseRE := regexp.MustCompile(`\b` + strings.Join(escapeWords(nameWords), `\s+`) + `\b`)

	out := make([]Stream, 0, len(streams))
	for _, st := range streams {
		norm := NormalizeTitle(st.Title)
		if len(nameWords) <= 2 {
			loc := phraseRE.FindStringIndex(norm)
			if loc == nil {
				continue
			}
			after := strings.TrimSpace(norm[loc[1]:])
			if next := strings.Fields(after); len(next) > 0 {
				if !isTorrentMetadata(next[0]) {
					continue
				}
			}
		} else {
			matched := 0
			for _, w := range nameWords {
				if regexp.MustCompile(`\b` + regexp.QuoteMeta(w) + `\b`).MatchString(norm) {
					matched++
				}
			}
			threshold := max(1, (len(nameWords)+1)/2)
			if matched < threshold {
				continue
			}
		}

		if req.IsSeries() && req.Season != nil && !st.EpisodeMatched {
			if !matchesSeasonEpisode(st.Title, *req.Season, req.Episode) {
				continue
			}
		}

		out = append(out, st)
	}
	return out
}

func preferP2P(streams []Stream, coreNames map[string]struct{}) []Stream {
	p2pCount := 0
	for _, st := range streams {
		if st.InfoHash != "" {
			p2pCount++
		}
	}
	if p2pCount == 0 {
		return streams
	}
	out := make([]Stream, 0, len(streams))
	for _, st := range streams {
		if st.InfoHash != "" {
			out = append(out, st)
		}
	}
	for _, st := range streams {
		if st.InfoHash == "" {
			if _, core := coreNames[strings.ToLower(st.Provider)]; core {
				out = append(out, st)
			}
		}
	}
	return out
}

func nameWords(name string) []string {
	words := strings.Fields(NormalizeTitle(name))
	out := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) > 1 {
			out = append(out, w)
		}
	}
	return out
}

func escapeWords(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = regexp.QuoteMeta(w)
	}
	return out
}

func isTorrentMetadata(word string) bool {
	lower := strings.ToLower(word)
	if matched, _ := regexp.MatchString(`^(s\d|season|episode|ep\d|web|hdtv|bluray|bdrip|dvd|hdrip|cam|x26|h26|hevc|avc|xvid|aac|ac3|dts|multi|dual|repack|proper|internal|extended|unrated|complete|full|part|the)`, lower); matched {
		return true
	}
	if _, err := strconv.Atoi(string(lower[0])); err == nil {
		return true
	}
	return false
}

func matchesSeasonEpisode(title string, season int, episode *int) bool {
	s := strconv.Itoa(season)
	if episode != nil {
		e := strconv.Itoa(*episode)
		patterns := []string{
			fmt.Sprintf(`(?i)s0*%s\s*e0*%s\b`, s, e),
			fmt.Sprintf(`(?i)\b%sx0*%s\b`, s, e),
		}
		for _, p := range patterns {
			if regexp.MustCompile(p).MatchString(title) {
				return true
			}
		}
	}
	seasonPack := fmt.Sprintf(`(?i)\bseason\s*0*%s\b|\bcomplete\b.*\bs0*%s\b`, s, s)
	if regexp.MustCompile(seasonPack).MatchString(title) {
		return true
	}
	seasonOnly := fmt.Sprintf(`(?i)s0*%s(e\d|\b)`, s)
	if regexp.MustCompile(seasonOnly).MatchString(title) {
		return true
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SortByQualityAndSeeders sorts streams for stable output.
func SortByQualityAndSeeders(streams []Stream) {
	qualityRank := map[string]int{"8k": 6, "2160p": 5, "4k": 5, "1080p": 4, "720p": 3, "480p": 2, "cam": 1}
	sort.Slice(streams, func(i, j int) bool {
		qi := qualityRank[strings.ToLower(streams[i].Quality)]
		qj := qualityRank[strings.ToLower(streams[j].Quality)]
		if qi != qj {
			return qi > qj
		}
		return streams[i].Seeders > streams[j].Seeders
	})
}
