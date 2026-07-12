package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/redis"
	"torrent-search-go/internal/services/streams"
)

// AtishmkvProvider serves HTTP direct URLs for Marathi movies from Redis.
type AtishmkvProvider struct {
	catalog      *AtishmkvCatalog
	resolver     *AtishmkvResolver
	redis        *redis.Client
	liveFallback bool
}

// AtishmkvRedisValue is the cached ephemeral direct link.
type AtishmkvRedisValue struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Size      int64  `json:"size"`
	Quality   string `json:"quality"`
	Host      string `json:"host"`
	ExpiresAt string `json:"expiresAt"`
}

// NewAtishmkvProvider creates an AtishMKV provider.
func NewAtishmkvProvider(httpClient *streams.HTTPClient, redisClient *redis.Client) (*AtishmkvProvider, error) {
	catalog, err := NewAtishmkvCatalog(httpClient)
	if err != nil {
		return nil, err
	}
	return &AtishmkvProvider{
		catalog:      catalog,
		resolver:     NewAtishmkvResolver(httpClient),
		redis:        redisClient,
		liveFallback: os.Getenv("ATISHMKV_LIVE_FALLBACK") == "true",
	}, nil
}

func (a *AtishmkvProvider) ID() string   { return "atishmkv" }
func (a *AtishmkvProvider) Name() string { return "AtishMKV" }

func (a *AtishmkvProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	if req.Type != "movie" || req.Name == "" || a.catalog == nil {
		return []streams.Stream{}, nil
	}

	entries, err := a.catalog.Find(ctx, req.Name, req.Year)
	if err != nil || len(entries) == 0 {
		return []streams.Stream{}, nil
	}

	out := make([]streams.Stream, 0, len(entries))
	for _, e := range entries {
		key := fmt.Sprintf("%s%s:%s", cache.PrefixAtishmkvDirect, e.Slug, e.Quality)
		var cached *AtishmkvRedisValue
		if a.redis != nil && a.redis.IsConfigured() {
			if val, ok, _ := a.redis.Get(ctx, key); ok {
				_ = json.Unmarshal([]byte(val), &cached)
			}
		}

		if cached == nil && a.liveFallback {
			url, host, err := a.resolver.ResolveDirect(ctx, e.LinkobaURL)
			if err == nil && url != "" {
				cached = &AtishmkvRedisValue{
					URL:       url,
					Title:     fmt.Sprintf("%s (%d) (Marathi) %s [%s] [AtishMKV]", e.Name, e.Year, e.Quality, host),
					Size:      e.SizeBytes,
					Quality:   e.Quality,
					Host:      host,
					ExpiresAt: time.Now().Add(5 * time.Hour).Format(time.RFC3339),
				}
			}
		}

		if cached == nil || cached.URL == "" {
			continue
		}

		// Ensure cached values from before the (Marathi) slug change are re-titled.
		cached.Title = fmt.Sprintf("%s (%d) (Marathi) %s [%s] [AtishMKV]", e.Name, e.Year, e.Quality, cached.Host)

		parsed := streams.ParseTitle(e.Title + " " + cached.Quality)
		out = append(out, streams.Stream{
			URL:       cached.URL,
			Title:     cached.Title,
			Provider:  "AtishMKV",
			Size:      cached.Size,
			Quality:   cached.Quality,
			Languages: inferAtishmkvLanguages(e.Title),
			IMDbID:    req.IMDbID,
			Source:    parsed.Source,
			Codec:     parsed.Codec,
			HDR:       parsed.HDR,
			Bitdepth:  parsed.Bitdepth,
		})
	}
	return out, nil
}

// SyncCatalog runs the daily catalog sync.
func (a *AtishmkvProvider) SyncCatalog(ctx context.Context) (map[string]interface{}, error) {
	if a.catalog == nil {
		return nil, fmt.Errorf("catalog not configured")
	}
	return a.catalog.Sync(ctx)
}

// RefreshDirectLinks resolves and caches direct URLs for all catalog entries.
func (a *AtishmkvProvider) RefreshDirectLinks(ctx context.Context) (map[string]interface{}, error) {
	if a.catalog == nil || a.redis == nil || !a.redis.IsConfigured() {
		return nil, fmt.Errorf("catalog and redis required")
	}
	col := a.catalog.db.Collection(atishmkvCollection)
	maxAge := 4*time.Hour - 30*time.Minute
	if v := os.Getenv("ATISHMKV_REFRESH_MAX_AGE_MS"); v != "" {
		if ms, err := time.ParseDuration(v + "ms"); err == nil {
			maxAge = ms
		}
	}
	cutoff := time.Now().Add(-maxAge)
	cursor, err := col.Find(ctx, bson.M{
		"$or": []bson.M{
			{"last_refresh_attempt": bson.M{"$lte": cutoff}},
			{"last_refresh_attempt": bson.M{"$exists": false}},
			{"last_refresh_attempt": nil},
			{"last_refresh_error": bson.M{"$ne": nil}},
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var entries []atishmkvCatalogEntry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}

	resolved, failed := 0, 0
	for _, e := range entries {
		url, host, err := a.resolver.ResolveDirect(ctx, e.LinkobaURL)
		now := time.Now()
		if err != nil || url == "" {
			failed++
			_, _ = col.UpdateOne(ctx, bson.M{"_id": e.ID}, bson.M{"$set": bson.M{
				"last_refresh_attempt": now,
				"last_refresh_error":   err.Error(),
			}})
			continue
		}

		val := AtishmkvRedisValue{
			URL:       url,
			Title:     fmt.Sprintf("%s (%d) (Marathi) %s [%s] [AtishMKV]", e.Name, e.Year, e.Quality, host),
			Size:      e.SizeBytes,
			Quality:   e.Quality,
			Host:      host,
			ExpiresAt: now.Add(5 * time.Hour).Format(time.RFC3339),
		}
		data, _ := json.Marshal(val)
		key := fmt.Sprintf("%s%s:%s", cache.PrefixAtishmkvDirect, e.Slug, e.Quality)
		_ = a.redis.Set(ctx, key, data, 5*time.Hour)

		_, _ = col.UpdateOne(ctx, bson.M{"_id": e.ID}, bson.M{"$set": bson.M{
			"last_refresh_attempt": now,
			"last_refresh_error":   nil,
			"resolved_host_name":   host,
		}})
		resolved++
	}

	return map[string]interface{}{"resolved": resolved, "failed": failed, "total": len(entries)}, nil
}

// RefreshDirectLinksForName resolves and caches direct URLs for catalog entries
// whose normalized name contains the given name (case-insensitive). It ignores
// the usual max-age filter so a single title can be warmed on demand.
func (a *AtishmkvProvider) RefreshDirectLinksForName(ctx context.Context, name string) (map[string]interface{}, error) {
	if a.catalog == nil || a.redis == nil || !a.redis.IsConfigured() {
		return nil, fmt.Errorf("catalog and redis required")
	}
	entries, err := a.catalog.Find(ctx, name, 0)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return map[string]interface{}{"resolved": 0, "failed": 0, "total": 0, "matches": []string{}}, nil
	}

	col := a.catalog.db.Collection(atishmkvCollection)
	resolved, failed := 0, 0
	var matched []string
	for _, e := range entries {
		matched = append(matched, e.Title)
		url, host, err := a.resolver.ResolveDirect(ctx, e.LinkobaURL)
		now := time.Now()
		if err != nil || url == "" {
			failed++
			_, _ = col.UpdateOne(ctx, bson.M{"_id": e.ID}, bson.M{"$set": bson.M{
				"last_refresh_attempt": now,
				"last_refresh_error":   err.Error(),
			}})
			continue
		}

		val := AtishmkvRedisValue{
			URL:       url,
			Title:     fmt.Sprintf("%s (%d) (Marathi) %s [%s] [AtishMKV]", e.Name, e.Year, e.Quality, host),
			Size:      e.SizeBytes,
			Quality:   e.Quality,
			Host:      host,
			ExpiresAt: now.Add(5 * time.Hour).Format(time.RFC3339),
		}
		data, _ := json.Marshal(val)
		key := fmt.Sprintf("%s%s:%s", cache.PrefixAtishmkvDirect, e.Slug, e.Quality)
		_ = a.redis.Set(ctx, key, data, 5*time.Hour)

		_, _ = col.UpdateOne(ctx, bson.M{"_id": e.ID}, bson.M{"$set": bson.M{
			"last_refresh_attempt": now,
			"last_refresh_error":   nil,
			"resolved_host_name":   host,
		}})
		resolved++
	}

	return map[string]interface{}{"resolved": resolved, "failed": failed, "total": len(entries), "matches": matched}, nil
}

// Stats returns catalog + refresh stats.
func (a *AtishmkvProvider) Stats(ctx context.Context) (map[string]interface{}, error) {
	if a.catalog == nil {
		return map[string]interface{}{"enabled": false}, nil
	}
	return a.catalog.Stats(ctx)
}

func inferAtishmkvLanguages(text string) []string {
	lower := strings.ToLower(text)
	langs := []string{}
	if strings.Contains(lower, "marathi") {
		langs = append(langs, "mr")
	}
	if strings.Contains(lower, "hindi") {
		langs = append(langs, "hi")
	}
	if strings.Contains(lower, "english") {
		langs = append(langs, "en")
	}
	if len(langs) == 0 {
		langs = append(langs, "hi")
	}
	return langs
}
