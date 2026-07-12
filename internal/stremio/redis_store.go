package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/redis"
	"torrent-search-go/pkg/models"
)

// Redis key prefixes live in internal/cache (single source of truth, shared
// with the admin cache viewer/buster; values must match tpb-stremio-addon
// src/utils/cache.js). These aliases keep the local call sites unchanged.
var (
	prefixTorrentStore          = cache.PrefixTorrentStore
	prefixTPDBShared            = cache.PrefixTPDBShared
	prefixStashDBShared         = cache.PrefixStashDBShared
	prefixTPDBSharedMiss        = cache.PrefixTPDBSharedMiss
	prefixStashDBSharedMiss     = cache.PrefixStashDBSharedMiss
	prefixCategoryCache         = cache.PrefixCategoryCache
	prefixCatalogList           = cache.PrefixCatalogList
	prefixPornripsCatalog       = cache.PrefixPornripsCatalog
	prefixTPDBCatalog           = cache.PrefixTPDBCatalog
	prefixHentaiCatalog         = cache.PrefixHentaiCatalog
	prefixSukebeiCatalog        = cache.PrefixSukebeiCatalog
	prefixStripchatCatalog      = cache.PrefixStripchatCatalog
	prefixHentaiMeta            = cache.PrefixHentaiMeta
	prefixHentaiStream          = cache.PrefixHentaiStream
)

const (
	// ttlTPDBCatalog bounds the tpdb_new/tpdb_search catalog page cache. Kept
	// short (15min) so the source-resolvable filter re-runs during the bulk-fill
	// and newly-resolved performers surface promptly; the per-performer cache
	// (ttlPerformerTorrent*) absorbs the Mongo cost of those re-runs.
	ttlTPDBCatalog = 15 * time.Minute

	// ttlStripchatCatalog bounds the live-cam listing cache. The list churns
	// every few seconds as models go on/offline; 30s keeps the row fresh
	// without hammering the public API.
	ttlStripchatCatalog = 30 * time.Second

	ttlTorrentStore   = 6 * time.Hour
	ttlCatalogList    = 15 * time.Minute
	ttlProxiedCatalog = 15 * time.Minute
	ttlHentaiMeta     = 7 * 24 * time.Hour
	ttlHentaiStream   = 5 * time.Minute
	ttlSharedMeta     = 30 * 24 * time.Hour
	ttlPornripsMeta   = 30 * time.Minute
	ttlPornStreams    = 30 * time.Minute
	// Tube sources: catalog list 15min (matches hentai/pornrips), meta long
	// (source metadata is stable), stream 5min (the freepornvideos token
	// rotates and the perverzija master can rotate, so keep it short).
	ttlTubeMeta   = 30 * 24 * time.Hour
	ttlTubeStream = 5 * time.Minute
	// ttlPerformerTorrentHit/Miss bound the per-performer "has a resolved
	// pornrips_entries torrent" cache that fronts the tpdb_new/tpdb_search
	// catalog filter. A positive is sticky-correct (a resolved entry never
	// un-resolves) so 60min is safe; a negative can go stale when the bulk-fill
	// backfills a new torrent for that performer, so 30min surfaces newly-
	// streamable performers sooner.
	ttlPerformerTorrentHit  = 60 * time.Minute
	ttlPerformerTorrentMiss = 30 * time.Minute
	// ttlSharedMetaMiss bounds how long a confirmed "no match" is remembered so
	// unmatched items skip live TPDB/StashDB probes on repeat views.
	ttlSharedMetaMiss = 1 * time.Hour
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

func (s *redisStore) getHentaiStream(ctx context.Context, id string) ([]map[string]interface{}, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, prefixHentaiStream+id)
	if err != nil || !ok {
		return nil, false
	}
	var out []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *redisStore) setHentaiStream(ctx context.Context, id string, streams []map[string]interface{}) error {
	if s == nil || s.client == nil || len(streams) == 0 {
		return nil
	}
	return s.client.Set(ctx, prefixHentaiStream+id, streams, ttlHentaiStream)
}

// getHentaiCatalogEntries / setHentaiCatalogEntries cache the durable
// hentai_entries list pages served by serveHentaiCatalog (Mongo-only until now).
// Empty result slices are cached too so empty genres do not stampede Mongo.
func (s *redisStore) getHentaiCatalogEntries(ctx context.Context, key string) ([]models.HentaiEntry, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, prefixHentaiCatalog+key)
	if err != nil || !ok {
		return nil, false
	}
	var entries []models.HentaiEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, false
	}
	return entries, true
}

func (s *redisStore) setHentaiCatalogEntries(ctx context.Context, key string, entries []models.HentaiEntry) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Set(ctx, prefixHentaiCatalog+key, entries, ttlCatalogList)
}

// getPornripsMeta / setPornripsMeta cache the PornRips entry looked up by slug
// as the ServeMeta poster fallback. Only non-nil entries are cached (misses are
// not long-cached); the PornripsSync job is the sole populator so 30m is safe.
func (s *redisStore) getPornripsMeta(ctx context.Context, slug string) (*models.PornripsEntry, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, cache.PrefixPornripsMeta+slug)
	if err != nil || !ok {
		return nil, false
	}
	var entry models.PornripsEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return nil, false
	}
	return &entry, true
}

func (s *redisStore) setPornripsMeta(ctx context.Context, slug string, entry *models.PornripsEntry) error {
	if s == nil || s.client == nil || entry == nil {
		return nil
	}
	return s.client.Set(ctx, cache.PrefixPornripsMeta+slug, entry, ttlPornripsMeta)
}

// getPornStreams / setPornStreams cache the resolved infoHash stream list for a
// porndb:<scene> id (TPDB scene + PornRips performer match). Empty lists are
// cached too so unplayable scenes do not re-probe TPDB + Mongo on every view.
func (s *redisStore) getPornStreams(ctx context.Context, numID string) ([]map[string]interface{}, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, cache.PrefixPornStreams+numID)
	if err != nil || !ok {
		return nil, false
	}
	var out []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *redisStore) setPornStreams(ctx context.Context, numID string, streams []map[string]interface{}) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Set(ctx, cache.PrefixPornStreams+numID, streams, ttlPornStreams)
}

// getPerformerTorrents returns the cached per-performer "has a resolved
// pornrips_entries torrent" flags (keyed pperf:v1:<performer>) and the list of
// performers not yet cached (misses). One batched MGet; on any Redis error the
// whole set is treated as missed so Mongo is queried directly.
func (s *redisStore) getPerformerTorrents(ctx context.Context, performers []string) (map[string]bool, []string) {
	cached := make(map[string]bool)
	if s == nil || s.client == nil || len(performers) == 0 {
		return cached, performers
	}
	keys := make([]string, len(performers))
	for i, p := range performers {
		keys[i] = cache.PrefixPerformerTorrent + p
	}
	dest := make([]interface{}, len(keys))
	if err := s.client.MGetJSON(ctx, keys, dest); err != nil {
		return cached, performers
	}
	misses := make([]string, 0, len(performers))
	for i, p := range performers {
		if b, ok := dest[i].(bool); ok {
			cached[p] = b
		} else {
			misses = append(misses, p)
		}
	}
	return cached, misses
}

// setPerformerTorrent caches a performer's resolved-torrent flag: 60min for a
// positive (sticky-correct), 30min for a negative (may go stale as the bulk-fill
// backfills new torrents).
func (s *redisStore) setPerformerTorrent(ctx context.Context, performer string, has bool) {
	if s == nil || s.client == nil {
		return
	}
	ttl := ttlPerformerTorrentMiss
	if has {
		ttl = ttlPerformerTorrentHit
	}
	_ = s.client.Set(ctx, cache.PrefixPerformerTorrent+performer, has, ttl)
}

func buildCatalogListKey(backendURL, catalogID, contentType, query, genre string, skip int, fanoutKey string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s", backendURL, catalogID, contentType, query, genre, skip, fanoutKey)
}

// ─── Tube source (perverzija / freepornvideos) caches ─────────────────────────
//
// Catalog list pages (entry slices), full meta, and resolved stream lists. The
// entry slices are cached as the raw model types so the catalog handler can
// decode and map to MetaPreview without a Mongo re-read; meta + stream reuse
// the hentai-shaped Meta / []map helpers.

func (s *redisStore) getTubeCatalogEntries(ctx context.Context, prefix, key string, out interface{}) bool {
	if s == nil || s.client == nil {
		return false
	}
	raw, ok, err := s.client.Get(ctx, prefix+key)
	if err != nil || !ok {
		return false
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return false
	}
	return true
}

func (s *redisStore) setTubeCatalogEntries(ctx context.Context, prefix, key string, entries interface{}) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Set(ctx, prefix+key, entries, ttlCatalogList)
}

func (s *redisStore) getTubeMeta(ctx context.Context, prefix, id string) (*Meta, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, prefix+id)
	if err != nil || !ok {
		return nil, false
	}
	var m Meta
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, false
	}
	return &m, true
}

func (s *redisStore) setTubeMeta(ctx context.Context, prefix, id string, m *Meta) error {
	if s == nil || s.client == nil || m == nil {
		return nil
	}
	return s.client.Set(ctx, prefix+id, m, ttlTubeMeta)
}

func (s *redisStore) getTubeStream(ctx context.Context, prefix, id string) ([]map[string]interface{}, bool) {
	if s == nil || s.client == nil {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, prefix+id)
	if err != nil || !ok {
		return nil, false
	}
	var out []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *redisStore) setTubeStream(ctx context.Context, prefix, id string, streams []map[string]interface{}) error {
	if s == nil || s.client == nil || len(streams) == 0 {
		return nil
	}
	return s.client.Set(ctx, prefix+id, streams, ttlTubeStream)
}

// getTubeGenreOptions loads one precomputed tube-source discover option blob
// (top-N studios/tags/performers) from KV. The sync job writes it; the manifest
// path reads it here so it never runs a Mongo aggregation per request. Returns
// ok=false on miss/empty (cold install) so the caller hides Studio/Tag/Performer.
func (s *redisStore) getTubeGenreOptions(ctx context.Context, prefix string) (studios, tags, performers []string, ok bool) {
	if s == nil || s.client == nil {
		return nil, nil, nil, false
	}
	raw, hit, err := s.client.Get(ctx, prefix+"opts")
	if err != nil || !hit {
		return nil, nil, nil, false
	}
	var blob struct {
		Studios    []string `json:"studios"`
		Tags       []string `json:"tags"`
		Performers []string `json:"performers"`
	}
	if err := json.Unmarshal([]byte(raw), &blob); err != nil {
		return nil, nil, nil, false
	}
	return blob.Studios, blob.Tags, blob.Performers, true
}
