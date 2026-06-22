package stremio

import (
	"context"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/metadata"
)

// searchPerformerFanout caps how many of a performer's scenes are turned into TPB
// code searches per query, bounding latency and provider load.
const searchPerformerFanout = 15

// searchTermConcurrency bounds parallel TPB searches when fanning out over codes.
const searchTermConcurrency = 5

// searchResolveTimeout bounds performer resolution so a slow StashDB degrades to
// the raw title search rather than stalling the whole request.
const searchResolveTimeout = 8 * time.Second

// resolveSearchTerms turns a free-text catalog search query into indexer search
// terms. A product code is searched literally; otherwise the query is resolved as
// a performer (StashDB) into scene codes, always alongside the raw query.
func (h *Handler) resolveSearchTerms(ctx context.Context, cfg Config, query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	// A product code is unambiguous - search for it directly.
	if metadata.IsJAVCode(query) {
		return []string{query}
	}

	terms := []string{query}
	stashKey, stashURL := resolveStashdbCredentials(cfg, h.Env)
	if stashKey == "" {
		return terms
	}

	stash := metadata.NewStashDBClient(stashURL, stashKey)
	rctx, cancel := context.WithTimeout(ctx, searchResolveTimeout)
	defer cancel()
	codes, err := stash.SearchPerformerScenes(rctx, query, searchPerformerFanout)
	if err != nil {
		return terms
	}
	seen := map[string]struct{}{strings.ToLower(query): {}}
	for _, c := range codes {
		k := strings.ToLower(c)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		terms = append(terms, c)
	}
	return terms
}

// searchPirateBayTerms runs Scrapers.Search for each term (bounded concurrency)
// and concatenates the results; the caller dedupes by infohash. A code term is
// filtered to torrents whose title actually carries the code - TPB tokenizes
// "MIDA-459" on the hyphen and otherwise returns unrelated titles matching the
// bare number "459".
func (h *Handler) searchPirateBayTerms(ctx context.Context, terms []string, page int, opts models.SearchOptions) ([]models.Torrent, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	if len(terms) == 1 {
		return h.searchOneTerm(ctx, terms[0], page, opts), nil
	}

	sem := make(chan struct{}, searchTermConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []models.Torrent
	for _, term := range terms {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := h.searchOneTerm(ctx, q, page, opts)
			if len(res) == 0 {
				return
			}
			mu.Lock()
			all = append(all, res...)
			mu.Unlock()
		}(term)
	}
	wg.Wait()
	return all, nil
}

func (h *Handler) searchOneTerm(ctx context.Context, term string, page int, opts models.SearchOptions) []models.Torrent {
	res, err := h.Scrapers.Search(ctx, catalogScraper, term, page, opts)
	if err != nil {
		return nil
	}
	if !metadata.IsJAVCode(term) {
		return res
	}
	kept := res[:0]
	for _, t := range res {
		if metadata.CodesMatch(term, t.Name) {
			kept = append(kept, t)
		}
	}
	return kept
}

const sukebeiScraper = "sukebei"

// searchSukebeiTerms runs Scrapers.Search on Sukebei for each resolved term.
func (h *Handler) searchSukebeiTerms(ctx context.Context, terms []string, page int, opts models.SearchOptions) ([]models.Torrent, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	if len(terms) == 1 {
		return h.searchOneSukebeiTerm(ctx, terms[0], page, opts), nil
	}

	sem := make(chan struct{}, searchTermConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []models.Torrent
	for _, term := range terms {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := h.searchOneSukebeiTerm(ctx, q, page, opts)
			if len(res) == 0 {
				return
			}
			mu.Lock()
			all = append(all, res...)
			mu.Unlock()
		}(term)
	}
	wg.Wait()
	return all, nil
}

func (h *Handler) searchOneSukebeiTerm(ctx context.Context, term string, page int, opts models.SearchOptions) []models.Torrent {
	res, err := h.Scrapers.Search(ctx, sukebeiScraper, term, page, opts)
	if err != nil {
		return nil
	}
	if !metadata.IsJAVCode(term) {
		return res
	}
	kept := res[:0]
	for _, t := range res {
		if metadata.CodesMatch(term, t.Name) {
			kept = append(kept, t)
		}
	}
	return kept
}
