package mongo

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"torrent-search-go/pkg/storage"
	"torrent-search-go/pkg/models"
)

var _ storage.Database = (*Client)(nil)

// Client is a MongoDB-backed storage client compatible with pkg/storage.Database.
type Client struct {
	client       *mongo.Client
	db           *mongo.Database
	dbName       string
	streamURLTTL int64
	isConnected  bool
	lastCheck    time.Time
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
		SetMaxPoolSize(10))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	return &Client{
		client:       client,
		db:           client.Database(dbName),
		dbName:       dbName,
		streamURLTTL: streamURLTTL,
		isConnected:  true,
		lastCheck:    time.Now().UTC(),
	}, nil
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
	}

	for _, idx := range indexes {
		model := mongo.IndexModel{Keys: idx.keys}
		if idx.opts != nil {
			model.Options = idx.opts
		}
		_, _ = c.coll(idx.coll).Indexes().CreateOne(ctx, model)
	}
	return nil
}

// Close disconnects MongoDB.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.isConnected = false
	return c.client.Disconnect(ctx)
}

// HealthCheck pings MongoDB.
func (c *Client) HealthCheck() (*models.HealthStatus, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.client.Ping(ctx, readpref.Primary())
	c.lastCheck = time.Now().UTC()
	responseTime := time.Since(start).Milliseconds()
	if err != nil {
		c.isConnected = false
		return &models.HealthStatus{
			Status:       "unhealthy",
			Type:         "mongodb",
			ResponseTime: responseTime,
			Timestamp:    c.lastCheck.Format(time.RFC3339),
		}, err
	}
	c.isConnected = true
	return &models.HealthStatus{
		Status:       "healthy",
		Type:         "mongodb",
		ResponseTime: responseTime,
		Timestamp:    c.lastCheck.Format(time.RFC3339),
	}, nil
}

// GetStats returns connection stats.
func (c *Client) GetStats() *models.Stats {
	return &models.Stats{
		IsConnected:  c.isConnected,
		DatabaseType: "MongoDB",
		LastCheck:    c.lastCheck,
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
