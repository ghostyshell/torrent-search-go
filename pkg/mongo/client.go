package mongo

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"torrent-search-go/pkg/models"
	"torrent-search-go/pkg/storage"
)

var _ storage.Database = (*Client)(nil)

// Client is a MongoDB-backed storage client compatible with pkg/storage.Database.
type Client struct {
	client       *mongo.Client
	db           *mongo.Database
	dbName       string
	streamURLTTL int64
	// isConnected / lastCheck are read by GetStats from concurrent HTTP
	// handlers and written by HealthCheck from background jobs; atomic to
	// avoid the data race `go test -race` flags.
	isConnected atomic.Bool
	lastCheck   atomic.Pointer[time.Time]
}

// NewClient connects to MongoDB.
func NewClient(uri, dbName string, streamURLTTL int64) (*Client, error) {
	if uri == "" {
		return nil, fmt.Errorf("mongodb URI is required")
	}
	if dbName == "" {
		dbName = "torrent_search"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(8*time.Second).
		SetMaxPoolSize(10).
		// Retire idle pool connections before a WAN/LB path reaps them: an idle
		// conn silently dropped mid-flight is handed back out without a socket
		// probe and the next read blocks. 30s sits below common LB idle timeouts
		// and the ~5min NAT reap, so pooled conns are fresh on checkout.
		SetMaxConnIdleTime(30*time.Second))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	c := &Client{
		client:       client,
		db:           client.Database(dbName),
		dbName:       dbName,
		streamURLTTL: streamURLTTL,
	}
	c.isConnected.Store(true)
	now := time.Now().UTC()
	c.lastCheck.Store(&now)
	return c, nil
}

// mongoOpTimeout caps a single bulk-fill Mongo operation. SetMaxConnIdleTime
// only retires IDLE connections; a connection that goes silent mid-read still
// blocks forever absent a deadline. The bulk-fill tick ctx has a long one, so
// wrap each op in this cap: on deadline the driver abandons the conn and the
// next op checks out a fresh one. 20s is ~40x a healthy WAN round-trip; a
// healthy op never approaches it. Applied only to the bulk-fill-critical ops
// (UpsertEnrichedScene, GetEnrichedScenesMissingSourceMatch,
// GetPornripsEntriesByPerformers) - the deployed read path uses request-scoped
// ctxs that are already bounded.
const mongoOpTimeout = 20 * time.Second

// opTimeoutCtx derives a per-op context capped at mongoOpTimeout. Callers must
// defer cancel(). A timed-out op returns context.DeadlineExceeded, which the
// bulk-fill callers tolerate (discovery ignores upsert errors; matching skips
// the source/treats it as transient and retries next tick) - no data loss, no
// crash, just a skipped op retried idempotently next tick.
func opTimeoutCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, mongoOpTimeout)
}

// BuildURI builds a MongoDB URI from env vars (matches Node buildMongoUri).
func BuildURI() string {
	base := os.Getenv("MONGODB_URI")
	if base == "" {
		base = os.Getenv("MONGO_URL")
	}
	if base == "" {
		return ""
	}
	user := os.Getenv("MONGO_USERNAME")
	if user == "" {
		user = os.Getenv("MONGO_USER")
	}
	pass := os.Getenv("MONGO_PASSWORD")
	if pass == "" {
		pass = os.Getenv("MONGO_PASS")
	}
	if user == "" || pass == "" {
		return base
	}
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "mongodb://") && !strings.HasPrefix(lower, "mongodb+srv://") {
		return base
	}
	if strings.Contains(base, "@") {
		return base
	}
	schemeEnd := strings.Index(base, "://")
	if schemeEnd < 0 {
		return base
	}
	scheme := base[:schemeEnd+3]
	rest := base[schemeEnd+3:]
	return scheme + url.QueryEscape(user) + ":" + url.QueryEscape(pass) + "@" + rest
}

func (c *Client) coll(name string) *mongo.Collection {
	return c.db.Collection(name)
}

// Migrate ensures indexes exist.
func (c *Client) Migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	indexes := []struct {
		coll string
		keys bson.D
		opts *options.IndexOptions
	}{
		{"cache", bson.D{{Key: "expires_at", Value: 1}}, nil},
		{"images", bson.D{{Key: "torrent_key", Value: 1}, {Key: "image_type", Value: 1}}, options.Index().SetUnique(true)},
		{"cached_links", bson.D{{Key: "user_id", Value: 1}}, nil},
		{"favorite_entries", bson.D{{Key: "torrent_key", Value: 1}, {Key: "user_id", Value: 1}}, options.Index().SetUnique(true)},
		{"favorite_entries", bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}, nil},
		{"torrent_details", bson.D{{Key: "favorite_entry_id", Value: 1}, {Key: "source", Value: 1}}, options.Index().SetUnique(true)},
		{"users", bson.D{{Key: "email", Value: 1}}, options.Index().SetUnique(true)},
		{"users", bson.D{{Key: "google_id", Value: 1}}, options.Index().SetUnique(true)},
		{"user_sessions", bson.D{{Key: "session_token", Value: 1}}, options.Index().SetUnique(true)},
		{"user_sessions", bson.D{{Key: "expires_at", Value: 1}}, nil},
		{"search_queries", bson.D{{Key: "updated_at", Value: -1}}, nil},
		{"shared_meta", bson.D{{Key: "meta_id", Value: 1}}, nil},
		{"shared_meta", bson.D{{Key: "updated_at", Value: -1}}, nil},
		{"blocked_ips", bson.D{{Key: "blocked_at", Value: -1}}, nil},
		// pornrips_entries: durable (no expires_at, exempt from CleanupExpired).
		// studio_norm backs pr_studio; date -1 backs pr_recent + all listing
		// sorts; tags_norm multikey backs pr_tag.
		{"pornrips_entries", bson.D{{Key: "studio_norm", Value: 1}}, nil},
		{"pornrips_entries", bson.D{{Key: "date", Value: -1}}, nil},
		{"pornrips_entries", bson.D{{Key: "tags_norm", Value: 1}}, nil},
		// performers multikey backs the tpdb_cat Mongo match (TPDB scene performer -> entries).
		{"pornrips_entries", bson.D{{Key: "performers", Value: 1}}, nil},
		// performers+info_hash backs the tpdb_new/tpdb_search catalog filter
		// (PerformersWithTorrent distinct - index-covered) and the all-performers
		// stream resolver (GetPornripsEntriesByPerformers - filter covered; the
		// date:-1 sort is in-memory but bounded by limit 3). Both filter
		// performers $in + info_hash $nin.
		{"pornrips_entries", bson.D{{Key: "performers", Value: 1}, {Key: "info_hash", Value: 1}}, options.Index().SetName("performers_torrent")},
		// enriched_tpdb+enriched_stash+date backs GetPornripsEntriesMissingEnrichment
		// (the enrich sweep, newest-first so the deployed job prioritizes newly-ingested
		// posts over the old-archive backlog the local one-off clears); info_hash+date
		// backs GetPornripsEntriesMissingTorrent (the backfill sweep, filter + newest-first
		// sort). Both sort/limit are index-covered. Named to match the prod one-off
		// (cmd/prindex) so a post-one-off Migrate no-ops on prod; on envs that never ran
		// the one-off, CreateOne is best-effort (errors swallowed) and a same-key index
		// keeps its auto-name - keys are identical so coverage is unaffected either way.
		{"pornrips_entries", bson.D{{Key: "enriched_tpdb", Value: 1}, {Key: "enriched_stash", Value: 1}, {Key: "date", Value: -1}}, options.Index().SetName("missing_enrich")},
		{"pornrips_entries", bson.D{{Key: "info_hash", Value: 1}, {Key: "date", Value: -1}}, options.Index().SetName("missing_torrent")},
		// hentai_entries: durable (no expires_at). source+updated_at backs
		// hentai_new; studio_norm backs hentai_studios; genres_norm multikey
		// backs hentai_top/hentai_all genre filter; release_year backs
		// hentai_years; rating -1 backs hentai_top sort.
		{"hentai_entries", bson.D{{Key: "source", Value: 1}, {Key: "updated_at", Value: -1}}, nil},
		{"hentai_entries", bson.D{{Key: "studio_norm", Value: 1}}, nil},
		{"hentai_entries", bson.D{{Key: "genres_norm", Value: 1}}, nil},
		{"hentai_entries", bson.D{{Key: "release_year", Value: 1}}, nil},
		{"hentai_entries", bson.D{{Key: "rating", Value: -1}}, nil},
		// enriched_scenes: durable (no expires_at). source+matched_sources+date
		// backs GetEnrichedScenesByMatchedSources (the store-backed catalog browse:
		// {source, matched_sources $in, date:-1} - the source-gate fix). tags_norm
		// multikey backs the category-catalog tag filter. attempted_sources+date
		// backs GetEnrichedScenesMissingSourceMatch (the torrent-match sweep, $ne
		// on attempted_sources + newest-first sort); the $ne is index-unfriendly but
		// the collection is bounded to discovered scenes - see the ponytail note on
		// that method.
		{"enriched_scenes", bson.D{{Key: "source", Value: 1}, {Key: "matched_sources", Value: 1}, {Key: "date", Value: -1}}, nil},
		{"enriched_scenes", bson.D{{Key: "tags_norm", Value: 1}}, nil},
		{"enriched_scenes", bson.D{{Key: "attempted_sources", Value: 1}, {Key: "date", Value: -1}}, nil},
		// date_desc makes GetEnrichedScenesMissingSourceMatch fast: its
		// `find({attempted_sources:{$ne:src}}).sort({date:-1}).limit(N)` can't seek the
		// attempted_sources multikey index on a $ne, so without a leading-date index it
		// scans all 252k docs + in-memory sorts over the WAN (the match phase freezes).
		// date_desc lets Mongo walk newest-first, fetch each doc, apply $ne on the full
		// array, stop at N - O(N) over the WAN instead of O(total).
		{"enriched_scenes", bson.D{{Key: "date", Value: -1}}, nil},
	}

	for _, idx := range indexes {
		model := mongo.IndexModel{Keys: idx.keys}
		if idx.opts != nil {
			model.Options = idx.opts
		}
		_, _ = c.coll(idx.coll).Indexes().CreateOne(ctx, model)
	}

	// Seed the addon status reports with the initial TPB 4K Porn data when empty.
	if err := c.seedAddonStatusReports(ctx); err != nil {
		return err
	}
	return nil
}

// Close disconnects MongoDB.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.isConnected.Store(false)
	return c.client.Disconnect(ctx)
}

// HealthCheck pings MongoDB.
func (c *Client) HealthCheck() (*models.HealthStatus, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.client.Ping(ctx, readpref.Primary())
	now := time.Now().UTC()
	c.lastCheck.Store(&now)
	responseTime := time.Since(start).Milliseconds()
	if err != nil {
		c.isConnected.Store(false)
		return &models.HealthStatus{
			Status:       "unhealthy",
			Type:         "mongodb",
			ResponseTime: responseTime,
			Timestamp:    now.Format(time.RFC3339),
		}, err
	}
	c.isConnected.Store(true)
	return &models.HealthStatus{
		Status:       "healthy",
		Type:         "mongodb",
		ResponseTime: responseTime,
		Timestamp:    now.Format(time.RFC3339),
	}, nil
}

// GetStats returns connection stats.
func (c *Client) GetStats() *models.Stats {
	var last time.Time
	if p := c.lastCheck.Load(); p != nil {
		last = *p
	}
	return &models.Stats{
		IsConnected:  c.isConnected.Load(),
		DatabaseType: "MongoDB",
		LastCheck:    last,
	}
}
func (c *Client) CleanupExpired(ctx context.Context) error {
	now := nowSec()
	_, _ = c.coll("user_sessions").DeleteMany(ctx, bson.M{"expires_at": bson.M{"$lte": now}})
	_, _ = c.coll("auth_exchange_codes").DeleteMany(ctx, bson.M{
		"$or": []bson.M{
			{"expires_at": bson.M{"$lte": now}},
			{"used": true, "used_at": bson.M{"$lte": now - 3600}},
		},
	})
	_, _ = c.coll("cache").DeleteMany(ctx, bson.M{
		"expires_at": bson.M{"$ne": nil, "$lte": now},
	})
	return nil
}

// GetTableStats returns collection counts.
func (c *Client) GetTableStats(ctx context.Context) (*models.DBTableStats, error) {
	count := func(name string) int {
		n, _ := c.coll(name).EstimatedDocumentCount(ctx)
		return int(n)
	}
	return &models.DBTableStats{
		Users:           count("users"),
		Sessions:        count("user_sessions"),
		FavoriteEntries: count("favorite_entries"),
		Images:          count("images"),
		StreamURLs:      count("stream_urls"),
		CachedLinks:     count("cached_links"),
		TorrentDetails:  count("torrent_details"),
		KVCache:         count("cache"),
	}, nil
}

// GetFavoriteStats returns favorite debug stats.
func (c *Client) GetFavoriteStats(ctx context.Context) (map[string]interface{}, error) {
	total, _ := c.coll("favorite_entries").CountDocuments(ctx, bson.M{})
	withMagnet, _ := c.coll("favorite_entries").CountDocuments(ctx, bson.M{
		"magnet_link": bson.M{"$nin": []interface{}{nil, ""}},
	})
	return map[string]interface{}{
		"totalFavorites":       total,
		"favoritesWithMagnets": withMagnet,
	}, nil
}

// GetFallbackUrlsByPixhostUrl is a no-op.
func (c *Client) GetFallbackUrlsByPixhostUrl(_ string) ([]string, error) {
	return []string{}, nil
}
