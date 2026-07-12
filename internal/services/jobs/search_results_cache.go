package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"torrent-search-go/internal/crypto"
	"torrent-search-go/internal/models"
	storagemodels "torrent-search-go/pkg/models"
)

const (
	filterMaxErrors  = 50
	filterCategory   = "507"
	filterTransQuery = "trans"
	// ponytail: sort 7 (most-seeded) is hot on RD (already downloaded by many), so
	// RefreshStreamURL resolves in seconds. sort 3 (newest uploads) is cold on RD and
	// every magnet timed out within the 60s per-magnet cap -> 0/30 cached, wasting ~90
	// minutes of the 12h job budget. Sort 7 also matches the UI's default search sort.
	filterBrowseSort    = "7"
	filterSearchSort    = "7"
	filterProgressEvery = 50
)

type magnetItem struct {
	magnetLink  string
	magnetHash  string
	torrentName string
}

// SearchResultsCache pre-caches RD stream URLs for browse/filter magnets (Node parity).
func (r *Runner) SearchResultsCache(ctx context.Context) (map[string]interface{}, error) {
	if r.scrapers == nil {
		return map[string]interface{}{"success": false, "error": "scraper service not configured"}, fmt.Errorf("scraper service not configured")
	}

	results := map[string]interface{}{
		"totalSearches":  0,
		"totalTorrents":  0,
		"uniqueMagnets":  0,
		"usersProcessed": 0,
		"usersSkipped":   0,
		"alreadyCached":  0,
		"refreshed":      0,
		"noMagnet":       0,
		"failed":         0,
		"errors":         []map[string]interface{}{},
	}
	jobErrors := results["errors"].([]map[string]interface{})

	start := time.Now()
	log.Printf("[FilterStreamCache] Starting job")
	magnets, err := r.collectFilterMagnets(ctx, results, &jobErrors)
	if err != nil {
		return results, err
	}
	if len(magnets) == 0 {
		log.Printf("[FilterStreamCache] No magnets collected")
		results["success"] = true
		results["errors"] = jobErrors
		return results, nil
	}

	results["uniqueMagnets"] = len(magnets)
	log.Printf("[FilterStreamCache] Collected %d unique magnets", len(magnets))

	users, err := r.storage.GetUsersWithRealDebridKeys(ctx)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, err
	}
	if len(users) == 0 {
		log.Printf("[FilterStreamCache] No users with Real-Debrid API keys")
		results["success"] = true
		results["errors"] = jobErrors
		return results, nil
	}

	// Deduplicate users by decrypted API key so we don't refresh the same RD
	// account multiple times when users share a key.
	apiKeys, skipped := r.collectUniqueRDKeys(users)
	results["usersSkipped"] = results["usersSkipped"].(int) + skipped

	log.Printf("[FilterStreamCache] Processing %d unique RD key(s) from %d user(s)", len(apiKeys), len(users))
	for i, apiKey := range apiKeys {
		shortID := apiKey[:8]
		log.Printf("[FilterStreamCache] Key %d/%d (%s...)", i+1, len(apiKeys), shortID)
		results["usersProcessed"] = results["usersProcessed"].(int) + 1
		if err := r.processFilterUserMagnets(ctx, magnets, apiKey, results, &jobErrors); err != nil {
			// Context cancellation is reported as a partial result, not a fatal error.
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				log.Printf("[FilterStreamCache] Stopping early: %v", err)
				results["stopped"] = true
				results["stopReason"] = err.Error()
				break
			}
			return results, err
		}
	}

	log.Printf("[FilterStreamCache] Job finished in %v - totalSearches=%d uniqueMagnets=%d usersProcessed=%d refreshed=%d failed=%d alreadyCached=%d",
		time.Since(start).Round(time.Second),
		results["totalSearches"].(int),
		results["uniqueMagnets"].(int),
		results["usersProcessed"].(int),
		results["refreshed"].(int),
		results["failed"].(int),
		results["alreadyCached"].(int))

	results["success"] = true
	results["errors"] = jobErrors
	return results, nil
}

func (r *Runner) collectUniqueRDKeys(users []storagemodels.UserRealDebridKey) ([]string, int) {
	seen := make(map[string]struct{})
	var keys []string
	skipped := 0
	for _, user := range users {
		apiKey, err := crypto.DecryptSecret(user.EncryptedKey)
		if err != nil || apiKey == "" {
			skipped++
			continue
		}
		if _, ok := seen[apiKey]; ok {
			continue
		}
		seen[apiKey] = struct{}{}
		keys = append(keys, apiKey)
	}
	return keys, skipped
}

func (r *Runner) collectFilterMagnets(ctx context.Context, results map[string]interface{}, errors *[]map[string]interface{}) ([]magnetItem, error) {
	pages := r.cfg.BackgroundJobs.SearchResultsCachePages
	if pages.PagesBrowseHome <= 0 {
		pages.PagesBrowseHome = 3
	}
	if pages.PagesTrans < 0 {
		pages.PagesTrans = 1
	}
	if pages.PagesPerStudio <= 0 {
		pages.PagesPerStudio = 2
	}

	seen := make(map[string]struct{})
	var magnets []magnetItem

	for page := 1; page <= pages.PagesBrowseHome; page++ {
		if err := r.addFilterBrowsePage(ctx, page, seen, &magnets, results, errors); err != nil {
			return nil, err
		}
	}
	for page := 1; page <= pages.PagesTrans; page++ {
		if err := r.addFilterSearchPage(ctx, filterTransQuery, page, seen, &magnets, results, errors); err != nil {
			return nil, err
		}
	}
	if err := r.addSukebeiFilterMagnets(ctx, seen, &magnets, results, errors); err != nil {
		return nil, err
	}
	studios := StudioSearchTerms()
	log.Printf("[FilterStreamCache] Collecting studio pages: %d studios, %d pages each", len(studios), pages.PagesPerStudio)
	for si, studio := range studios {
		for page := 1; page <= pages.PagesPerStudio; page++ {
			if err := r.addFilterSearchPage(ctx, studio, page, seen, &magnets, results, errors); err != nil {
				return nil, err
			}
		}
		if (si+1)%10 == 0 {
			log.Printf("[FilterStreamCache] Studio collection progress %d/%d, magnets so far %d", si+1, len(studios), len(magnets))
		}
	}
	return magnets, nil
}

func (r *Runner) addFilterBrowsePage(ctx context.Context, page int, seen map[string]struct{}, magnets *[]magnetItem, results map[string]interface{}, errors *[]map[string]interface{}) error {
	results["totalSearches"] = results["totalSearches"].(int) + 1
	torrents, err := r.scrapers.Browse(ctx, scraperWebsite, filterCategory, page, filterBrowseSort, models.SearchOptions{})
	if err != nil {
		results["failed"] = results["failed"].(int) + 1
		pushJobError(errors, filterMaxErrors, map[string]interface{}{"browse": true, "page": page, "error": err.Error()})
		log.Printf("[FilterStreamCache] Browse failed page %d: %v", page, err)
		return nil
	}
	log.Printf("[FilterStreamCache] Browse page %d: %d torrents", page, len(torrents))
	r.ingestFilterTorrents(torrents, seen, magnets, results)
	return sleepCtx(ctx, 500*time.Millisecond)
}

func (r *Runner) addFilterSearchPage(ctx context.Context, query string, page int, seen map[string]struct{}, magnets *[]magnetItem, results map[string]interface{}, errors *[]map[string]interface{}) error {
	results["totalSearches"] = results["totalSearches"].(int) + 1
	torrents, err := r.scrapers.Search(ctx, scraperWebsite, query, page, models.SearchOptions{
		Sort:     filterSearchSort,
		Category: filterCategory,
	})
	if err != nil {
		results["failed"] = results["failed"].(int) + 1
		pushJobError(errors, filterMaxErrors, map[string]interface{}{"query": query, "page": page, "error": err.Error()})
		log.Printf("[FilterStreamCache] Search failed %q page %d: %v", query, page, err)
		return nil
	}
	log.Printf("[FilterStreamCache] Search %q page %d: %d torrents", query, page, len(torrents))
	r.ingestFilterTorrents(torrents, seen, magnets, results)
	return sleepCtx(ctx, 500*time.Millisecond)
}

func (r *Runner) addSukebeiFilterMagnets(
	ctx context.Context,
	seen map[string]struct{},
	magnets *[]magnetItem,
	results map[string]interface{},
	errors *[]map[string]interface{},
) error {
	if r.cfg == nil || r.cfg.Metadata.StashDBAPIKey == "" {
		return nil
	}
	for _, sort := range []string{"7", "3"} {
		for page := 1; page <= sukebeiCatalogPages; page++ {
			results["totalSearches"] = results["totalSearches"].(int) + 1
			torrents, err := r.scrapers.Browse(ctx, "sukebei", "0_0", page, sort, models.SearchOptions{})
			if err != nil {
				results["failed"] = results["failed"].(int) + 1
				pushJobError(errors, filterMaxErrors, map[string]interface{}{
					"sukebei": true, "sort": sort, "page": page, "error": err.Error(),
				})
				log.Printf("[FilterStreamCache] Sukebei browse failed sort=%s page=%d: %v", sort, page, err)
				break
			}
			log.Printf("[FilterStreamCache] Sukebei sort=%s page=%d: %d torrents", sort, page, len(torrents))
			r.ingestFilterTorrents(torrents, seen, magnets, results)
			if len(torrents) == 0 {
				break
			}
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) ingestFilterTorrents(torrents []models.Torrent, seen map[string]struct{}, magnets *[]magnetItem, results map[string]interface{}) {
	if len(torrents) == 0 {
		return
	}
	results["totalTorrents"] = results["totalTorrents"].(int) + len(torrents)
	for _, t := range torrents {
		if t.MagnetLink == "" {
			results["noMagnet"] = results["noMagnet"].(int) + 1
			continue
		}
		hash := extractMagnetHash(t.MagnetLink)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		name := t.Name
		if name == "" {
			name = "Unknown"
		}
		*magnets = append(*magnets, magnetItem{magnetLink: t.MagnetLink, magnetHash: hash, torrentName: name})
	}
}

func (r *Runner) processFilterUserMagnets(ctx context.Context, magnets []magnetItem, apiKey string, results map[string]interface{}, errors *[]map[string]interface{}) error {
	for i, m := range magnets {
		if err := ctx.Err(); err != nil {
			return err
		}

		shortName := m.torrentName
		if len(shortName) > 60 {
			shortName = shortName[:60]
		}

		// Skip magnets that already have a fresh cached stream URL.
		cached, err := r.storage.GetStreamURLByHash(ctx, m.magnetHash)
		if err != nil {
			log.Printf("[FilterStreamCache] Failed to look up cache for %s: %v", shortName, err)
		}
		if cached != nil && cached.StreamURL != "" {
			results["alreadyCached"] = results["alreadyCached"].(int) + 1
			continue
		}

		// Bound each RD refresh so one slow magnet can't stall the whole job.
		magnetCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		stream, err := r.rd.RefreshStreamURL(magnetCtx, apiKey, m.magnetLink)
		cancel()
		if err != nil {
			results["failed"] = results["failed"].(int) + 1
			pushJobError(errors, filterMaxErrors, map[string]interface{}{"torrent": shortName, "error": err.Error()})
			kind := classifyRefreshError(err)
			if kind == "rd_rate_limit" {
				if pauseErr := sleepCtx(ctx, rateLimitJobPause); pauseErr != nil {
					return pauseErr
				}
			}
		} else {
			if err := r.storage.SetStreamURL(ctx, storagemodels.StreamURLInput{
				MagnetHash:            m.magnetHash,
				MagnetLink:            m.magnetLink,
				StreamURL:             stream.StreamURL,
				Filename:              stream.Filename,
				Filesize:              stream.Filesize,
				SupportsRangeRequests: stream.SupportsRangeRequests,
				TorrentName:           m.torrentName,
			}); err != nil {
				results["failed"] = results["failed"].(int) + 1
				pushJobError(errors, filterMaxErrors, map[string]interface{}{"torrent": shortName, "error": err.Error()})
			} else {
				results["refreshed"] = results["refreshed"].(int) + 1
				log.Printf("[FilterStreamCache] Refreshed: %s", shortName)
			}
		}

		if (i+1)%filterProgressEvery == 0 {
			log.Printf("[FilterStreamCache] Magnet progress %d/%d (refreshed=%d, failed=%d, alreadyCached=%d)",
				i+1, len(magnets), results["refreshed"].(int), results["failed"].(int), results["alreadyCached"].(int))
		}

		if err := sleepCtx(ctx, time.Second); err != nil {
			return err
		}
	}
	return nil
}
