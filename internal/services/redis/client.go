package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"torrent-search-go/internal/config"
)

// Client wraps go-redis with application-specific helpers.
type Client struct {
	cfg *config.RedisConfig
	rdb *goredis.Client
}

// NewClient creates a Redis client from configuration without connecting.
func NewClient(cfg config.RedisConfig) *Client {
	return &Client{cfg: &cfg}
}

// NewClientFromConfig is a convenience constructor that reads from the global config.
func NewClientFromConfig(cfg *config.Config) *Client {
	if cfg == nil {
		return &Client{}
	}
	return NewClient(cfg.Redis)
}

// Connect parses the Redis URL and verifies the connection with Ping.
// The configured password, if present, overrides any password in the URL.
func (c *Client) Connect(ctx context.Context) error {
	if c.cfg == nil || c.cfg.URL == "" {
		return fmt.Errorf("redis URL not configured")
	}

	opts, err := goredis.ParseURL(c.cfg.URL)
	if err != nil {
		return fmt.Errorf("invalid redis URL: %w", err)
	}
	if c.cfg.Password != "" {
		opts.Password = c.cfg.Password
	}

	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 5 * time.Second
	opts.WriteTimeout = 5 * time.Second
	opts.MaxRetries = 1
	c.rdb = goredis.NewClient(opts)
	return c.Ping(ctx)
}

// Ping checks whether Redis is reachable.
func (c *Client) Ping(ctx context.Context) error {
	if c.rdb == nil {
		return fmt.Errorf("redis client not connected")
	}
	return c.rdb.Ping(ctx).Err()
}

// SetCatalogJSON marshals value to JSON and stores it under key with the given TTL.
func (c *Client) SetCatalogJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if c.rdb == nil {
		return fmt.Errorf("redis client not connected")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal catalog JSON: %w", err)
	}
	return c.rdb.Set(ctx, key, data, ttl).Err()
}

// Exists reports whether a key is present.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	if c.rdb == nil {
		return false, fmt.Errorf("redis client not connected")
	}
	n, err := c.rdb.Exists(ctx, key).Result()
	return n > 0, err
}

// ExistsMany reports presence for each key (same order). Missing keys are false.
func (c *Client) ExistsMany(ctx context.Context, keys []string) ([]bool, error) {
	if c.rdb == nil {
		return nil, fmt.Errorf("redis client not connected")
	}
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := c.rdb.Pipeline()
	cmds := make([]*goredis.IntCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.Exists(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	out := make([]bool, len(keys))
	for i, cmd := range cmds {
		n, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		out[i] = n > 0
	}
	return out, nil
}

// Get returns the string value for key. The second return is false on miss.
func (c *Client) Get(ctx context.Context, key string) (string, bool, error) {
	if c.rdb == nil {
		return "", false, fmt.Errorf("redis client not connected")
	}
	val, err := c.rdb.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// MGetJSON fetches JSON values for keys. Missing keys yield nil entries.
func (c *Client) MGetJSON(ctx context.Context, keys []string, dest []interface{}) error {
	if c.rdb == nil {
		return fmt.Errorf("redis client not connected")
	}
	if len(keys) == 0 {
		return nil
	}
	if dest == nil {
		dest = make([]interface{}, len(keys))
	}
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return err
	}
	for i, v := range vals {
		if v == nil {
			dest[i] = nil
			continue
		}
		s, ok := v.(string)
		if !ok {
			dest[i] = nil
			continue
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			dest[i] = nil
			continue
		}
		dest[i] = parsed
	}
	return nil
}

// ScanKeys returns up to limit keys matching prefix* (subkeys without prefix).
func (c *Client) ScanKeys(ctx context.Context, prefix string, limit int) ([]string, error) {
	if c.rdb == nil {
		return nil, fmt.Errorf("redis client not connected")
	}
	if limit <= 0 {
		limit = 100
	}
	var cursor uint64
	out := make([]string, 0, limit)
	for len(out) < limit {
		keys, next, err := c.rdb.Scan(ctx, cursor, prefix+"*", int64(limit-len(out))).Result()
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Set stores a raw value under key with the given TTL.
// If value is not a string or []byte it is JSON-marshaled.
func (c *Client) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if c.rdb == nil {
		return fmt.Errorf("redis client not connected")
	}
	switch v := value.(type) {
	case string:
		return c.rdb.Set(ctx, key, v, ttl).Err()
	case []byte:
		return c.rdb.Set(ctx, key, v, ttl).Err()
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal redis value: %w", err)
		}
		return c.rdb.Set(ctx, key, data, ttl).Err()
	}
}

// Close closes the underlying Redis connection.
func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

// IsConfigured reports whether a Redis URL is present in the config.
func (c *Client) IsConfigured() bool {
	return c != nil && c.cfg != nil && c.cfg.URL != ""
}

// CountPrefix returns the number of keys matching prefix*. Safe to call on a
// nil or unconnected client (returns 0, nil). Uses SCAN so it does not block.
func (c *Client) CountPrefix(ctx context.Context, prefix string) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	var n int64
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, prefix+"*", 500).Result()
		if err != nil {
			return 0, err
		}
		n += int64(len(keys))
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return n, nil
}

// DelByPrefix deletes every key matching prefix* and returns the count. Safe
// to call on a nil or unconnected client (returns 0, nil). SCAN + pipelined
// DEL in batches of 500 so it does not block and does not load all keys at once.
// ponytail: SCAN-then-DEL is O(n) in the matched keyspace; fine for cache
// prefixes (thousands). A per-key UNLINK pipeline is the upgrade if a prefix
// ever holds millions of keys.
func (c *Client) DelByPrefix(ctx context.Context, prefix string) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	var deleted int64
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, prefix+"*", 500).Result()
		if err != nil {
			return deleted, err
		}
		if len(keys) > 0 {
			pipe := c.rdb.Pipeline()
			delCmd := pipe.Del(ctx, keys...)
			if _, err := pipe.Exec(ctx); err != nil {
				return deleted, err
			}
			deleted += delCmd.Val()
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return deleted, nil
}
