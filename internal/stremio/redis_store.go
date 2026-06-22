package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/redis"
)

// Redis key prefixes - must match tpb-stremio-addon src/utils/cache.js.
const (
	prefixTorrentStore      = "torrent:v1:"
	prefixTPDBShared        = "tpdb-shared:v1:"
	prefixStashDBShared     = "stashdb-shared:v1:"
	prefixTPDBSharedMiss    = "tpdb-miss:v1:"
	prefixStashDBSharedMiss = "stashdb-miss:v1:"
	prefixCategoryCache     = "catcat:v1:"
	prefixCatalogList       = "cat:v1:"
	prefixPornripsCatalog   = "cat:pr:v6:"
	prefixTPDBCatalog       = "cat:tpdb:v3:"
	prefixHentaiCatalog     = "cat:hs:v1:"
	ttlTPDBCatalog          = 1 * time.Hour
	prefixSukebeiCatalog    = "cat:sb:v1:"
	prefixStripchatCatalog  = "cat:sc:v1:"

	// ttlStripchatCatalog bounds the live-cam listing cache. The list churns
	// every few seconds as models go on/offline; 30s keeps the row fresh
	// without hammering the public API.
	ttlStripchatCatalog = 30 * time.Second

	ttlTorrentStore   = 6 * time.Hour
	ttlCatalogList    = 15 * time.Minute
	ttlProxiedCatalog = 15 * time.Minute
	ttlHentaiMeta     = 7 * 24 * time.Hour
	ttlSharedMeta     = 30 * 24 * time.Hour
	// ttlSharedMetaMiss bounds how long a confirmed "no match" is remembered so
	// unmatched items skip live TPDB/StashDB probes on repeat views.
	ttlSharedMetaMiss = 1 * time.Hour

	prefixHentaiMeta = "meta:hs:v1:"
)

type redisStore struct {
	client *redis.Client
}

func newRedisStore(c *redis.Client) *redisStore {
	if c == nil {
		return nil
	}
	return &redisStore{client: c}
}

func (s *redisStore) getCategoryMetas(ctx context.Context, source, slug string) ([]jobs.StremioMetaPreview, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	raw, ok, err := s.client.Get(ctx, prefixCategoryCache+source+":"+slug)
	if err != nil || !ok {
		return nil, err
	}
	var metas []jobs.StremioMetaPreview
	if err := json.Unmarshal([]byte(raw), &metas); err != nil {
		return nil, err
	}
	return metas, nil
}

func (s *redisStore) getCatalogTorrents(ctx context.Context, key string) ([]catalogTorrent, error) {
	return s.getTorrentList(ctx, prefixCatalogList+key)
}

func (s *redisStore) getTorrentList(ctx context.Context, redisKey string) ([]catalogTorrent, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	raw, ok, err := s.client.Get(ctx, redisKey)
	if err != nil || !ok {
		return nil, err
	}
	var torrents []catalogTorrent
	if err := json.Unmarshal([]byte(raw), &torrents); err != nil {
		return nil, err
	}
	return torrents, nil
}

func (s *redisStore) setTorrentList(ctx context.Context, redisKey string, torrents []catalogTorrent, ttl time.Duration) error {
	if s == nil || s.client == nil || len(torrents) == 0 {
		return nil
	}
	return s.client.Set(ctx, redisKey, torrents, ttl)
}

func (s *redisStore) setTorrent(ctx context.Context, id string, rec TorrentRecord) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Set(ctx, prefixTorrentStore+id, rec, ttlTorrentStore)
}

func (s *redisStore) getProxiedMetas(ctx context.Context, key string) ([]MetaPreview, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	raw, ok, err := s.client.Get(ctx, key)
	if err != nil || !ok {
		return nil, err
	}
	var metas []MetaPreview
	if err := json.Unmarshal([]byte(raw), &metas); err != nil {
		return nil, err
	}
	return metas, nil
}

func (s *redisStore) setProxiedMetas(ctx context.Context, key string, metas []MetaPreview) error {
	if s == nil || s.client == nil || len(metas) == 0 {
		return nil
	}
	return s.client.Set(ctx, key, metas, ttlProxiedCatalog)
}

func (s *redisStore) setTPDBMetas(ctx context.Context, key string, metas []MetaPreview) error {
	if s == nil || s.client == nil || len(metas) == 0 {
		return nil
	}
	return s.client.Set(ctx, key, metas, ttlTPDBCatalog)
}

func (s *redisStore) getTorrent(ctx context.Context, id string) (*TorrentRecord, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	raw, ok, err := s.client.Get(ctx, prefixTorrentStore+id)
	if err != nil || !ok {
		return nil, err
	}
	var rec TorrentRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *redisStore) getSharedMeta(ctx context.Context, source, key string) (*jobs.SharedMeta, error) {
	if s == nil || s.client == nil || key == "" {
		return nil, nil
	}
	prefix := prefixTPDBShared
	if source == "stashdb" {
		prefix = prefixStashDBShared
	}
	raw, ok, err := s.client.Get(ctx, prefix+key)
	if err != nil || !ok {
		return nil, err
	}
	var meta jobs.SharedMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *redisStore) setSharedMeta(ctx context.Context, source, key string, meta jobs.SharedMeta) error {
	if s == nil || s.client == nil || key == "" {
		return nil
	}
	prefix := prefixTPDBShared
	if source == "stashdb" {
		prefix = prefixStashDBShared
	}
	return s.client.Set(ctx, prefix+key, meta, ttlSharedMeta)
}

// sharedMetaMissID scopes negative-cache entries to the API credential used for
// the lookup so a miss under the server key does not block a per-install key.
func sharedMetaMissID(metaID, apiKey string) string {
	metaID = strings.TrimSpace(metaID)
	if metaID == "" {
		return ""
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return metaID
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(apiKey))
	return fmt.Sprintf("%s:%x", metaID, h.Sum32())
}

// Shared-meta miss sentinels (Go-internal; not mirrored in the Node addon).
// getSharedMetaMiss reports whether a prior live lookup for this source/key
// found no match and is still within ttlSharedMetaMiss.
func (s *redisStore) getSharedMetaMiss(ctx context.Context, source, metaID, apiKey string) bool {
	if s == nil || s.client == nil {
		return false
	}
	key := sharedMetaMissID(metaID, apiKey)
	if key == "" {
		return false
	}
	prefix := prefixTPDBSharedMiss
	if source == "stashdb" {
		prefix = prefixStashDBSharedMiss
	}
	_, ok, err := s.client.Get(ctx, prefix+key)
	return err == nil && ok
}

// setSharedMetaMiss records a confirmed no-match so repeat views skip live probes.
func (s *redisStore) setSharedMetaMiss(ctx context.Context, source, metaID, apiKey string) error {
	if s == nil || s.client == nil {
		return nil
	}
	key := sharedMetaMissID(metaID, apiKey)
	if key == "" {
		return nil
	}
	prefix := prefixTPDBSharedMiss
	if source == "stashdb" {
		prefix = prefixStashDBSharedMiss
	}
	return s.client.Set(ctx, prefix+key, "1", ttlSharedMetaMiss)
}

func (s *redisStore) getSukebeiCatalogEntries(ctx context.Context, redisKey string) ([]jobs.SukebeiCatalogEntry, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	raw, ok, err := s.client.Get(ctx, redisKey)
	if err != nil || !ok {
		return nil, err
	}
	var entries []jobs.SukebeiCatalogEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *redisStore) setSukebeiCatalogEntries(ctx context.Context, redisKey string, entries []jobs.SukebeiCatalogEntry) error {
	if s == nil || s.client == nil || len(entries) == 0 {
		return nil
	}
	return s.client.Set(ctx, redisKey, entries, ttlCatalogList)
}

func (s *redisStore) getStripchatCatalog(ctx context.Context, redisKey string) ([]MetaPreview, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	raw, ok, err := s.client.Get(ctx, redisKey)
	if err != nil || !ok {
		return nil, err
	}
	var metas []MetaPreview
	if err := json.Unmarshal([]byte(raw), &metas); err != nil {
		return nil, err
	}
	return metas, nil
}

func (s *redisStore) setStripchatCatalog(ctx context.Context, redisKey string, metas []MetaPreview) error {
	if s == nil || s.client == nil || len(metas) == 0 {
		return nil
	}
	return s.client.Set(ctx, redisKey, metas, ttlStripchatCatalog)
}

func (s *redisStore) getHentaiMeta(ctx context.Context, id string) (*Meta, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, prefixHentaiMeta+id)
	if err != nil || !ok {
		return nil, false
	}
	var m Meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, false
	}
	return &m, true
}

func (s *redisStore) setHentaiMeta(ctx context.Context, id string, m *Meta) error {
	if s == nil || s.client == nil || m == nil {
		return nil
	}
	return s.client.Set(ctx, prefixHentaiMeta+id, m, ttlHentaiMeta)
}

func buildCatalogListKey(backendURL, catalogID, contentType, query, genre string, skip int, fanoutKey string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s", backendURL, catalogID, contentType, query, genre, skip, fanoutKey)
}
