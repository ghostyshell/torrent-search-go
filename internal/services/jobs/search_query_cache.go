package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"torrent-search-go/internal/models"
)

const (
	sqcSortSeeders = "7"
	sqcMaxErrors   = 50
)

var sqcCategories = []struct {
	category string
	label    string
	suffix   string
}{
	{category: "507", label: "4K", suffix: ""},
	{category: "505", label: "1080p", suffix: "_fhd"},
}

// sqcTorrent is the normalized cache payload written to Redis.
// It mirrors the shape produced by the Node searchQueryCacheService.
type sqcTorrent struct {
	Title      string `json:"title"`
	Size       string `json:"size"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	InfoHash   string `json:"infoHash"`
	MagnetLink string `json:"magnetLink"`
	TorrentURL string `json:"torrentUrl"`
	CoverImage string `json:"coverImage"`
	Website    string `json:"website"`
	Indexer    string `json:"indexer"`
	Quality    string `json:"quality"`
}

// SearchQueryCache keeps Redis warm with recent search results and cover images.
func (r *Runner) SearchQueryCache(ctx context.Context) (map[string]interface{}, error) {
	results := map[string]interface{}{
		"queriesFound":     0,
		"queriesProcessed": 0,
		"totalTorrents":    0,
		"coversFound":      0,
		"coversCached":     0,
		"redisEntries":     0,
		"cleanedUp":        0,
		"errors":           []map[string]interface{}{},
	}
	errors := results["errors"].([]map[string]interface{})

	if r.scrapers == nil {
		results["success"] = false
		results["error"] = "scraper service not configured"
		return results, nil
	}

	cfg := r.cfg.BackgroundJobs.SearchQueryCacheJobConfig
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 1
	}
	if cfg.RedisTTLSeconds <= 0 {
		cfg.RedisTTLSeconds = 1 * 60 * 60
	}
	if cfg.SleepBetweenCovers <= 0 {
		cfg.SleepBetweenCovers = 300 * time.Millisecond
	}
	if cfg.SleepBetweenQueries <= 0 {
		cfg.SleepBetweenQueries = 1500 * time.Millisecond
	}
	if cfg.SleepBetweenPages <= 0 {
		cfg.SleepBetweenPages = 500 * time.Millisecond
	}

	baseURL := strings.TrimSuffix(os.Getenv("BASE_URL"), "/")

	since := time.Now().AddDate(0, 0, -cfg.RetentionDays)
	queries, err := r.storage.GetRecentSearchQueries(ctx, since)
	if err != nil {
		results["success"] = false
		results["error"] = err.Error()
		return results, err
	}

	results["queriesFound"] = len(queries)
	log.Printf("[SearchQueryCache] %d queries in last %d days (TTL=%ds)", len(queries), cfg.RetentionDays, cfg.RedisTTLSeconds)

	for _, query := range queries {
		for _, tier := range sqcCategories {
			if err := r.processSearchQueryTier(ctx, query, scraperWebsite, tier, baseURL, cfg.RedisTTLSeconds, results, &errors); err != nil {
				if ctx.Err() != nil {
					results["success"] = false
					results["errors"] = errors
					return results, ctx.Err()
				}
				pushJobError(&errors, sqcMaxErrors, map[string]interface{}{
					"query":    query,
					"category": tier.category,
					"error":    err.Error(),
				})
				log.Printf("[SearchQueryCache] %q %s failed: %v", query, tier.label, err)
			}
			if err := sleepCtx(ctx, cfg.SleepBetweenQueries); err != nil {
				results["success"] = false
				results["errors"] = errors
				return results, err
			}
		}

		results["queriesProcessed"] = results["queriesProcessed"].(int) + 1
		if err := sleepCtx(ctx, cfg.SleepBetweenPages); err != nil {
			results["success"] = false
			results["errors"] = errors
			return results, err
		}
	}

	cleaned, err := r.storage.CleanupOldSearchQueries(ctx, since)
	if err != nil {
		results["success"] = false
		results["error"] = err.Error()
		return results, err
	}
	results["cleanedUp"] = cleaned

	results["success"] = true
	results["errors"] = errors
	log.Printf("[SearchQueryCache] Done: queries=%d torrents=%d covers=%d redis=%d cleaned=%d",
		results["queriesProcessed"], results["totalTorrents"], results["coversCached"], results["redisEntries"], cleaned)
	return results, nil
}

func (r *Runner) processSearchQueryTier(ctx context.Context, query, website string, tier struct {
	category string
	label    string
	suffix   string
}, baseURL string, redisTTLSeconds int, results map[string]interface{}, errors *[]map[string]interface{}) error {
	cfg := r.cfg.BackgroundJobs.SearchQueryCacheJobConfig
	if cfg.SleepBetweenCovers <= 0 {
		cfg.SleepBetweenCovers = 300 * time.Millisecond
	}

	log.Printf("[SearchQueryCache] %q %s (cat %s)", query, tier.label, tier.category)

	rawTorrents, err := r.scrapers.Search(ctx, website, query, 1, models.SearchOptions{
		Sort:     sqcSortSeeders,
		Category: tier.category,
	})
	if err != nil {
		return err
	}
	if len(rawTorrents) == 0 {
		log.Printf("[SearchQueryCache] No results for %q %s", query, tier.label)
		return nil
	}

	results["totalTorrents"] = results["totalTorrents"].(int) + len(rawTorrents)

	enriched := make([]sqcTorrent, 0, len(rawTorrents))
	for _, t := range rawTorrents {
		key := TorrentKey(t)
		cover := r.resolveSqcCover(ctx, t, key, results, errors)
		enriched = append(enriched, sqcTorrent{
			Title:      t.Name,
			Size:       t.Size,
			Seeders:    t.Seeders,
			Leechers:   t.Leechers,
			InfoHash:   extractMagnetHash(t.MagnetLink),
			MagnetLink: t.MagnetLink,
			TorrentURL: t.TorrentURL,
			CoverImage: cover,
			Website:    "hiddenbay",
			Indexer:    "hiddenbay",
			Quality:    "",
		})
		if err := sleepCtx(ctx, cfg.SleepBetweenCovers); err != nil {
			return err
		}
	}

	deduped := dedupeSqcTorrents(enriched)
	if err := r.writeSqcRedis(ctx, query, website, tier.category, tier.suffix, deduped, baseURL, redisTTLSeconds, results); err != nil {
		return err
	}
	return nil
}

func (r *Runner) resolveSqcCover(ctx context.Context, t models.Torrent, key string, results map[string]interface{}, errors *[]map[string]interface{}) string {
	existing, err := r.storage.GetCoverImageByKey(ctx, key)
	if err == nil && existing != nil && existing.PixhostURL != "" {
		return existing.PixhostURL
	}

	if t.TorrentURL == "" {
		return ""
	}

	details, err := r.scrapers.GetTorrentDetails(ctx, scraperWebsite, t.TorrentURL)
	if err != nil || details == nil || details.CoverImageURL == "" {
		return ""
	}

	results["coversFound"] = results["coversFound"].(int) + 1
	finalURL := higherResolutionURL(details.CoverImageURL)

	if err := r.storage.SetCoverImage(ctx, key, finalURL, false); err != nil {
		pushJobError(errors, sqcMaxErrors, map[string]interface{}{"torrent": t.Name, "error": err.Error()})
		log.Printf("[SearchQueryCache] Cover store failed for %q: %v", t.Name, err)
		return finalURL
	}

	results["coversCached"] = results["coversCached"].(int) + 1
	return finalURL
}

func dedupeSqcTorrents(torrents []sqcTorrent) []sqcTorrent {
	seen := make(map[string]struct{}, len(torrents))
	out := make([]sqcTorrent, 0, len(torrents))
	for _, t := range torrents {
		key := t.InfoHash
		if key == "" {
			key = t.Title
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}

	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].Seeders < out[j].Seeders {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (r *Runner) writeSqcRedis(ctx context.Context, query, website, category, catalogSuffix string, torrents []sqcTorrent, baseURL string, redisTTLSeconds int, results map[string]interface{}) error {
	if r.redis == nil || len(torrents) == 0 {
		return nil
	}

	payload, err := json.Marshal(torrents)
	if err != nil {
		log.Printf("[SearchQueryCache] Failed to marshal torrents for %q: %v", query, err)
		return nil
	}

	ttl := time.Duration(redisTTLSeconds) * time.Second
	sqcKey := fmt.Sprintf("sqc:v1:%s:%s:%s", website, category, url.QueryEscape(query))
	if err := r.redis.Set(ctx, sqcKey, payload, ttl); err != nil {
		log.Printf("[SearchQueryCache] Redis sqc write failed for %q %s: %v", query, category, err)
	} else {
		results["redisEntries"] = results["redisEntries"].(int) + 1
		log.Printf("[SearchQueryCache] Redis sqc: %q %s -> %d results", query, category, len(torrents))
	}

	if baseURL != "" {
		catalogID := "xxx" + catalogSuffix + "_top"
		catKey := fmt.Sprintf("cat:v1:%s|%s|Porn|%s||0", baseURL, catalogID, query)
		if err := r.redis.Set(ctx, catKey, payload, ttl); err != nil {
			log.Printf("[SearchQueryCache] Redis cat write failed for %q %s: %v", query, catalogID, err)
		} else {
			results["redisEntries"] = results["redisEntries"].(int) + 1
			log.Printf("[SearchQueryCache] Redis cat: %q -> %s", query, catalogID)
		}
	}

	return nil
}
