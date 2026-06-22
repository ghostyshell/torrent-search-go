package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"torrent-search-go/internal/services/redis"
)

// Redis key prefixes and TTLs - must match tpb-stremio-addon src/utils/cache.js.
const (
	prefixTorrentStore    = "torrent:v1:"
	prefixTPDBShared      = "tpdb-shared:v1:"
	prefixStashDBShared   = "stashdb-shared:v1:"
	prefixCategoryCache   = "catcat:v1:"
	prefixStashTagCache   = "stashtag:v1:"
	prefixReferenceMeta   = "refmeta:v1:"
	prefixPornripsMagnet  = "prmagnet:v1:"

	ttlTorrentStore   = 6 * time.Hour
	ttlSharedMeta     = 30 * 24 * time.Hour
	ttlCategoryCache  = 6 * time.Hour
	ttlStashTag       = 30 * 24 * time.Hour
	ttlReferenceMeta  = 7 * 24 * time.Hour
	ttlPornripsMagnet = 30 * 24 * time.Hour
	ttlRefNegative    = 6 * time.Hour
)

// StremioMetaPreview is the catalog list entry written by the category warmer.
type StremioMetaPreview struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Poster      string `json:"poster,omitempty"`
	Background  string `json:"background,omitempty"`
	Description string `json:"description"`
	ReleaseInfo string `json:"releaseInfo"`
	PosterShape string `json:"posterShape"`
}

// TorrentRecord is stored in torrent:v1:{jstrmId}.
type TorrentRecord struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	InfoHash   string `json:"infoHash"`
	MagnetLink string `json:"magnetLink"`
	TorrentURL string `json:"torrentUrl"`
	DetailURL  string `json:"detailUrl"`
	Website    string `json:"website"`
	Seeders    int    `json:"seeders"`
}

// SharedMeta is stored in tpdb-shared / stashdb-shared caches.
type SharedMeta struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Poster      string   `json:"poster,omitempty"`
	Background  string   `json:"background,omitempty"`
	Year        string   `json:"year,omitempty"`
	Cast        []string `json:"cast"`
	Tags        []string `json:"tags"`
	Genres      []string `json:"genres,omitempty"`
	Source      string   `json:"source"`
}

type addonRedisStore struct {
	client *redis.Client
}

func newAddonRedisStore(c *redis.Client) *addonRedisStore {
	if c == nil {
		return nil
	}
	return &addonRedisStore{client: c}
}

func (s *addonRedisStore) Exists(ctx context.Context, prefix, key string) (bool, error) {
	return s.client.Exists(ctx, prefix+key)
}

func (s *addonRedisStore) Get(ctx context.Context, prefix, key string) (string, bool, error) {
	return s.client.Get(ctx, prefix+key)
}

func (s *addonRedisStore) Set(ctx context.Context, prefix, key string, value interface{}, ttl time.Duration) error {
	return s.set(ctx, prefix, key, value, ttl)
}

func (s *addonRedisStore) SetCategoryMetas(ctx context.Context, source, slug string, metas []StremioMetaPreview) error {
	return s.set(ctx, prefixCategoryCache, source+":"+slug, metas, ttlCategoryCache)
}

func (s *addonRedisStore) HasSharedMeta(ctx context.Context, source, infoHash string) (bool, error) {
	prefix := prefixTPDBShared
	if source == "stashdb" {
		prefix = prefixStashDBShared
	}
	return s.Exists(ctx, prefix, infoHash)
}

func (s *addonRedisStore) SetSharedMeta(ctx context.Context, source, infoHash string, meta SharedMeta) error {
	prefix := prefixTPDBShared
	if source == "stashdb" {
		prefix = prefixStashDBShared
	}
	return s.set(ctx, prefix, infoHash, meta, ttlSharedMeta)
}

func (s *addonRedisStore) GetSharedMeta(ctx context.Context, source, key string) (*SharedMeta, error) {
	prefix := prefixTPDBShared
	if source == "stashdb" {
		prefix = prefixStashDBShared
	}
	raw, ok, err := s.client.Get(ctx, prefix+key)
	if err != nil || !ok {
		return nil, err
	}
	var meta SharedMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *addonRedisStore) ExistsManyShared(ctx context.Context, source string, keys []string) ([]bool, error) {
	prefix := prefixTPDBShared
	if source == "stashdb" {
		prefix = prefixStashDBShared
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = prefix + k
	}
	return s.client.ExistsMany(ctx, full)
}

func (s *addonRedisStore) ScanTPDBSharedKeys(ctx context.Context, subPrefix string, limit int) ([]string, error) {
	return s.client.ScanKeys(ctx, prefixTPDBShared+subPrefix, limit)
}

func (s *addonRedisStore) GetManyTPDBShared(ctx context.Context, keys []string) ([]*SharedMeta, error) {
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = prefixTPDBShared + k
	}
	raw := make([]interface{}, len(keys))
	if err := s.client.MGetJSON(ctx, full, raw); err != nil {
		return nil, err
	}
	out := make([]*SharedMeta, len(keys))
	for i, v := range raw {
		if v == nil {
			continue
		}
		b, _ := json.Marshal(v)
		var m SharedMeta
		if json.Unmarshal(b, &m) == nil {
			out[i] = &m
		}
	}
	return out, nil
}

func (s *addonRedisStore) SetReferenceMeta(ctx context.Context, slug string, meta interface{}, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = ttlReferenceMeta
	}
	return s.set(ctx, prefixReferenceMeta, slug, meta, ttl)
}

func (s *addonRedisStore) HasPornripsMagnet(ctx context.Context, slug string) (bool, error) {
	return s.Exists(ctx, prefixPornripsMagnet, slug)
}

func (s *addonRedisStore) SetPornripsMagnet(ctx context.Context, slug, url string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = ttlPornripsMagnet
	}
	return s.set(ctx, prefixPornripsMagnet, slug, url, ttl)
}

func (s *addonRedisStore) SetTorrent(ctx context.Context, id string, record TorrentRecord) error {
	return s.set(ctx, prefixTorrentStore, id, record, ttlTorrentStore)
}

func (s *addonRedisStore) set(ctx context.Context, prefix, key string, value interface{}, ttl time.Duration) error {
	return s.client.Set(ctx, prefix+key, value, ttl)
}

func (s *addonRedisStore) stashTagCache() *stashTagRedis {
	return &stashTagRedis{store: s}
}

// stashTagRedis implements metadata.tagCache for StashDB tag resolution.
type stashTagRedis struct {
	store *addonRedisStore
}

func (t *stashTagRedis) Get(ctx context.Context, key string) (string, bool, error) {
	if t.store == nil || t.store.client == nil {
		return "", false, nil
	}
	return t.store.client.Get(ctx, prefixStashTagCache+key)
}

func (t *stashTagRedis) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if t.store == nil || t.store.client == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = ttlStashTag
	}
	return t.store.client.Set(ctx, prefixStashTagCache+key, value, ttl)
}

type itemPayload struct {
	H string `json:"h"`
	T string `json:"t"`
	U string `json:"u"`
	W string `json:"w"`
	D string `json:"d,omitempty"`
}

func encodeItemID(infoHash, title, torrentURL, website, detailURL string) string {
	payload := itemPayload{
		H: infoHash,
		T: title,
		U: torrentURL,
		W: website,
	}
	if detailURL != "" {
		payload.D = detailURL
	}
	b, _ := json.Marshal(payload)
	return "jstrm:" + base64.RawURLEncoding.EncodeToString(b)
}

var magnetHashRE = regexp.MustCompile(`(?i)urn:btih:([a-f0-9]{40})`)

func extractInfoHash(magnet string) string {
	if magnet == "" {
		return ""
	}
	if m := magnetHashRE.FindStringSubmatch(magnet); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

func yearFromDate(dateStr string) string {
	if dateStr == "" {
		return ""
	}
	re := regexp.MustCompile(`\b(19|20)\d{2}\b`)
	if m := re.FindString(dateStr); m != "" {
		return m
	}
	return ""
}
