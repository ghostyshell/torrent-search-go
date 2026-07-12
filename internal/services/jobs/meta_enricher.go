package jobs

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"torrent-search-go/internal/services/metadata"
)

const (
	defaultMetaInterval    = 60 * time.Second
	defaultMetaInitial     = 5 * time.Second
	defaultMetaMaxPerTick  = 250
	defaultMetaMaxQueue    = 20000
	defaultMetaConcurrency = 2
)

type pendingMeta struct {
	id        string
	title     string
	detailURL string
	website   string
	priority  bool
}

type metaEnrichQueue struct {
	mu    sync.Mutex
	items map[string]pendingMeta
	max   int
}

func newMetaEnrichQueue() *metaEnrichQueue {
	maxQ := defaultMetaMaxQueue
	if v, err := strconv.Atoi(os.Getenv("META_ENRICHER_MAX_QUEUE")); err == nil && v > 0 {
		maxQ = v
	}
	return &metaEnrichQueue{
		items: make(map[string]pendingMeta),
		max:   maxQ,
	}
}

// EnqueueMetaLookups adds torrent records to the pending metadata queue.
func (r *Runner) EnqueueMetaLookups(items []MetaEnqueueItem) int {
	if r.metaQueue == nil {
		return 0
	}
	added := 0
	r.metaQueue.mu.Lock()
	defer r.metaQueue.mu.Unlock()
	for _, it := range items {
		id, title, detailURL, website := NormalizeMetaEnqueueItem(it)
		if id == "" {
			continue
		}
		existing, ok := r.metaQueue.items[id]
		if !ok {
			if len(r.metaQueue.items) >= r.metaQueue.max {
				continue
			}
			r.metaQueue.items[id] = pendingMeta{id: id, title: title, detailURL: detailURL, website: website, priority: it.Priority}
			added++
			continue
		}
		if existing.title == "" && title != "" {
			existing.title = title
		}
		if existing.detailURL == "" && detailURL != "" {
			existing.detailURL = detailURL
		}
		if existing.website == "" && website != "" {
			existing.website = website
		}
		// Upgrade priority on re-enqueue: a torrent first enqueued by browse
		// (non-priority) and later re-enqueued by a favorites load (priority)
		// must drain ahead of the backlog, otherwise user-facing favorites
		// covers stay buried when the queue is chronically backlogged.
		if it.Priority && !existing.priority {
			existing.priority = true
		}
		r.metaQueue.items[id] = existing
	}
	return added
}

// MetaEnricher drains the pending queue and runs TPDB/StashDB lookups.
func (r *Runner) MetaEnricher(ctx context.Context) (map[string]interface{}, error) {
	if r.redis == nil || r.metaQueue == nil {
		return nil, fmt.Errorf("redis not configured")
	}
	if r.cfg == nil {
		return nil, fmt.Errorf("config not available")
	}
	tpdbKey := r.cfg.Metadata.TPDBAPIKey
	stashKey := r.cfg.Metadata.StashDBAPIKey
	if tpdbKey == "" && stashKey == "" {
		return map[string]interface{}{"success": true, "skipped": true}, nil
	}

	maxPerTick := defaultMetaMaxPerTick
	if v, err := strconv.Atoi(os.Getenv("META_ENRICHER_MAX_PER_TICK")); err == nil && v > 0 {
		maxPerTick = v
	}
	concurrency := defaultMetaConcurrency
	if v, err := strconv.Atoi(os.Getenv("META_ENRICHER_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	entries := r.metaQueue.drain(maxPerTick)
	if len(entries) == 0 {
		return map[string]interface{}{"success": true, "queued": 0}, nil
	}

	store := newAddonRedisStore(r.redis)
	tpdb := metadata.NewTPDBClient(r.cfg.Metadata.TPDBAPIURL, tpdbKey)
	stash := metadata.NewStashDBClient(r.cfg.Metadata.StashDBAPIURL, stashKey)

	valid := make([]pendingMeta, 0, len(entries))
	for _, e := range entries {
		if e.title != "" {
			valid = append(valid, e)
		}
	}
	skippedMissing := len(entries) - len(valid)

	ids := make([]string, len(valid))
	for i, v := range valid {
		ids[i] = v.id
	}
	// De-dupe against both the durable Mongo store and the Redis cache. Either
	// having the metadata means the API lookup can be skipped.
	inTPDBRedis, _ := store.ExistsManyShared(ctx, "tpdb", ids)
	inStashRedis, _ := store.ExistsManyShared(ctx, "stashdb", ids)
	inTPDBMongo, _ := r.storage.ExistsSharedMany(ctx, "tpdb", ids)
	inStashMongo, _ := r.storage.ExistsSharedMany(ctx, "stashdb", ids)

	todo := make([]struct {
		item      pendingMeta
		needTPDB  bool
		needStash bool
	}, 0)
	skippedCache := 0
	rehydrated := 0
	for i, item := range valid {
		hasTPDBRedis := i < len(inTPDBRedis) && inTPDBRedis[i]
		hasStashRedis := i < len(inStashRedis) && inStashRedis[i]
		hasTPDBMongo := i < len(inTPDBMongo) && inTPDBMongo[i]
		hasStashMongo := i < len(inStashMongo) && inStashMongo[i]

		// Rehydrate Redis from the durable Mongo store so the addon meta handler
		// (a Redis reader) recovers after a TTL/flush without re-calling the APIs.
		if (hasTPDBMongo && !hasTPDBRedis) || (hasStashMongo && !hasStashRedis) {
			if tp, sp, err := r.storage.GetSharedMetaPair(ctx, item.id); err == nil {
				if tp != nil && !hasTPDBRedis {
					_ = store.SetSharedMeta(ctx, "tpdb", item.id, *payloadToShared(tp))
					rehydrated++
				}
				if sp != nil && !hasStashRedis {
					_ = store.SetSharedMeta(ctx, "stashdb", item.id, *payloadToShared(sp))
					rehydrated++
				}
			}
		}
		// Rehydrate Mongo from Redis when Redis holds the poster but Mongo does
		// not. cover-image reads the durable Mongo store, but the stremio live
		// meta handler caches TPDB/StashDB posters to Redis only, and a partial
		// write that succeeded in Redis but failed in Mongo leaves no durable
		// row. Without this, hasTPDB stays true (Redis) so the item is never
		// re-probed and cover-image returns "not found" despite Redis having the
		// poster. Copy Redis -> Mongo so the store cover-image reads is filled.
		if (hasTPDBRedis && !hasTPDBMongo) || (hasStashRedis && !hasStashMongo) {
			if hasTPDBRedis && !hasTPDBMongo {
				if m, err := store.GetSharedMeta(ctx, "tpdb", item.id); err == nil && m != nil && m.Poster != "" {
					_ = r.storage.SetSharedMeta(ctx, "tpdb", item.id, sharedToPayload(m))
					rehydrated++
				}
			}
			if hasStashRedis && !hasStashMongo {
				if m, err := store.GetSharedMeta(ctx, "stashdb", item.id); err == nil && m != nil && m.Poster != "" {
					_ = r.storage.SetSharedMeta(ctx, "stashdb", item.id, sharedToPayload(m))
					rehydrated++
				}
			}
		}

		hasTPDB := hasTPDBRedis || hasTPDBMongo
		hasStash := hasStashRedis || hasStashMongo
		if hasTPDB && hasStash {
			skippedCache++
			continue
		}
		todo = append(todo, struct {
			item      pendingMeta
			needTPDB  bool
			needStash bool
		}{item: item, needTPDB: !hasTPDB && tpdbKey != "", needStash: !hasStash && stashKey != ""})
	}

	// Incremented from runTPDB/runStash goroutines, so use atomics.
	var tpdbHits, stashHits, failed, requeued atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	runTPDB := func(item pendingMeta) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()
		// SearchMetadataVariants is the shared multi-probe loop the Stremio catalog
		// live path uses, so the enricher resolves the same cover for the same title
		// instead of the former single raw-title probe that mismatched messy TPB names.
		meta, rateLimited := tpdb.SearchMetadataVariants(ctx, item.title)
		if meta == nil {
			if rateLimited {
				if r.metaQueue.requeue(item) {
					requeued.Add(1)
				} else {
					failed.Add(1)
				}
			}
			return
		}
		shared := normalizedToShared(meta)
		_ = store.SetSharedMeta(ctx, "tpdb", item.id, shared)
		_ = r.storage.SetSharedMeta(ctx, "tpdb", item.id, sharedToPayload(&shared))
		tpdbHits.Add(1)
	}
	runStash := func(item pendingMeta) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()
		meta, _ := stash.SearchMetadataVariants(ctx, item.title, item.detailURL)
		if meta == nil {
			return
		}
		shared := normalizedToShared(meta)
		_ = store.SetSharedMeta(ctx, "stashdb", item.id, shared)
		_ = r.storage.SetSharedMeta(ctx, "stashdb", item.id, sharedToPayload(&shared))
		stashHits.Add(1)
	}

	for _, row := range todo {
		if row.needTPDB {
			wg.Add(1)
			go runTPDB(row.item)
		}
		if row.needStash {
			wg.Add(1)
			go runStash(row.item)
		}
	}
	wg.Wait()

	return map[string]interface{}{
		"success":          true,
		"queued":           len(entries),
		"processed":        len(todo),
		"tpdbHits":         tpdbHits.Load(),
		"stashdbHits":      stashHits.Load(),
		"skippedCache":     skippedCache,
		"rehydrated":       rehydrated,
		"skippedMissing":   skippedMissing,
		"failed":           failed.Load(),
		"requeued":         requeued.Load(),
		"remainingInQueue": r.metaQueue.len(),
	}, nil
}

func (q *metaEnrichQueue) drain(max int) []pendingMeta {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	entries := make([]pendingMeta, 0, len(q.items))
	priority := make([]pendingMeta, 0)
	for _, v := range q.items {
		if v.priority {
			priority = append(priority, v)
		} else {
			entries = append(entries, v)
		}
	}
	q.items = make(map[string]pendingMeta)
	// Drain priority items first so favorites (a low-traffic, user-facing path)
	// are processed next tick even when the browse backlog is large.
	drained := make([]pendingMeta, 0, len(q.items))
	take := func(from []pendingMeta) []pendingMeta {
		room := max - len(drained)
		if room <= 0 || len(from) == 0 {
			return from
		}
		n := len(from)
		if n > room {
			n = room
		}
		drained = append(drained, from[:n]...)
		return from[n:]
	}
	leftover := append(take(priority), take(entries)...)
	for _, e := range leftover {
		q.items[e.id] = e
	}
	return drained
}

func (q *metaEnrichQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// requeue puts a drained item back when a TPDB lookup hit a rate limit so it
// can be retried on the next tick instead of being dropped.
func (q *metaEnrichQueue) requeue(item pendingMeta) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.items[item.id]; ok {
		return true
	}
	if len(q.items) >= q.max {
		return false
	}
	q.items[item.id] = item
	return true
}

func normalizedToShared(m *metadata.NormalizedMeta) SharedMeta {
	if m == nil {
		return SharedMeta{}
	}
	return SharedMeta{
		Title:       m.Title,
		Description: m.Description,
		Poster:      m.Poster,
		Background:  m.Background,
		Year:        m.Year,
		Cast:        m.Cast,
		Tags:        m.Tags,
		Genres:      m.Genres,
		Source:      m.Source,
	}
}
