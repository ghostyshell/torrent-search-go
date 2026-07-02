package stremio

import (
	"context"
	"strings"
	"time"

	appconfig "torrent-search-go/internal/config"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
	"torrent-search-go/pkg/models"
)

const tpdbMetaLookupTimeout = 8 * time.Second

// resolveTpdbCredentials returns the TPDB API key/URL for this install.
func resolveTpdbCredentials(cfg Config, env *appconfig.Config) (key, url string) {
	key = strings.TrimSpace(cfg.TpdbKey)
	url = strings.TrimSpace(cfg.TpdbURL)
	if env != nil {
		if key == "" {
			key = strings.TrimSpace(env.Metadata.TPDBAPIKey)
		}
		if url == "" {
			url = strings.TrimSpace(env.Metadata.TPDBAPIURL)
		}
	}
	if url == "" {
		url = "https://api.theporndb.net"
	}
	return key, url
}

// loadTpdbMeta returns cached TPDB metadata for a torrent, fetching live on
// miss when a TPDB key is configured.
func (h *Handler) loadTpdbMeta(
	ctx context.Context,
	cfg Config,
	store *redisStore,
	metaID, title string,
) *jobs.SharedMeta {
	tpdbKey, _ := resolveTpdbCredentials(cfg, h.Env)
	if metaID != "" && store != nil {
		if shared, err := store.getSharedMeta(ctx, "tpdb", metaID); err == nil && shared != nil {
			if shared.Poster != "" || shared.Title != "" {
				return shared
			}
		}
		if store.getSharedMetaMiss(ctx, "tpdb", metaID, tpdbKey) {
			return nil
		}
	}

	if tpdbKey == "" || strings.TrimSpace(title) == "" {
		return nil
	}

	rctx, cancel := context.WithTimeout(ctx, tpdbMetaLookupTimeout)
	defer cancel()

	tpdbURL, _ := resolveTpdbCredentials(cfg, h.Env)
	client := metadata.NewTPDBClient(tpdbURL, tpdbKey)
	// SearchMetadataVariants is the shared multi-probe loop used by the
	// MetaEnricher too, so the catalog live path and the background enricher
	// resolve the same cover for the same title.
	meta, rateLimited := client.SearchMetadataVariants(rctx, title)
	if meta != nil {
		shared := normalizedMetaToShared(meta)
		if store != nil && metaID != "" {
			_ = store.setSharedMeta(ctx, "tpdb", metaID, shared)
		}
		return &shared
	}
	// A rate limit or a lookup timeout is transient, not a confirmed no-match:
	// don't poison the miss cache or the next view (and the warmer) skips the
	// live probe for an hour and the cover never resolves. rctx.Err() covers the
	// 8s deadline (SearchMetadataVariants returns nil on context expiry), and
	// rateLimited covers a 429 it surfaced.
	if store != nil && metaID != "" && !rateLimited && rctx.Err() == nil {
		_ = store.setSharedMetaMiss(ctx, "tpdb", metaID, tpdbKey)
	}
	return nil
}

// loadMergedMeta returns TPDB+StashDB metadata from cache, fetching live on miss
// when install keys are configured.
func (h *Handler) loadMergedMeta(
	ctx context.Context,
	cfg Config,
	store *redisStore,
	metaID, title, detailURL string,
) *jobs.SharedMeta {
	var tpdb, stashdb *jobs.SharedMeta
	if store != nil && metaID != "" {
		tpdb, _ = store.getSharedMeta(ctx, "tpdb", metaID)
		stashdb, _ = store.getSharedMeta(ctx, "stashdb", metaID)
	}
	merged := mergeMetadata(tpdb, stashdb)
	if merged != nil && merged.Poster != "" && merged.Title != "" {
		return merged
	}

	if tpdb == nil || tpdb.Poster == "" {
		if live := h.loadTpdbMeta(ctx, cfg, store, metaID, title); live != nil {
			tpdb = live
		}
	}
	if stashdb == nil || stashdb.Poster == "" {
		if live := h.loadStashMeta(ctx, cfg, store, metaID, title, detailURL); live != nil {
			stashdb = live
		}
	}
	return mergeMetadata(tpdb, stashdb)
}

// loadPornripsMeta returns merged TPDB/Stash shared_meta for a PornRips item from
// Redis, rehydrating from the durable Mongo shared_meta store on a Redis miss.
// Mongo-only: no live TPDB/Stash/WP probe on the request path - the background
// PornripsSync job is the sole populator of shared_meta.
func (h *Handler) loadPornripsMeta(ctx context.Context, store *redisStore, metaID string) *jobs.SharedMeta {
	if metaID == "" {
		return nil
	}
	var tpdb, stash *jobs.SharedMeta
	if store != nil {
		tpdb, _ = store.getSharedMeta(ctx, "tpdb", metaID)
		stash, _ = store.getSharedMeta(ctx, "stashdb", metaID)
	}
	if (tpdb == nil || stash == nil) && h.SharedMeta != nil {
		if tp, sp, err := h.SharedMeta.GetSharedMetaPair(ctx, metaID); err == nil {
			if tpdb == nil && tp != nil {
				tpdb = sharedPayloadToShared(tp)
				if store != nil {
					_ = store.setSharedMeta(ctx, "tpdb", metaID, *tpdb)
				}
			}
			if stash == nil && sp != nil {
				stash = sharedPayloadToShared(sp)
				if store != nil {
					_ = store.setSharedMeta(ctx, "stashdb", metaID, *stash)
				}
			}
		}
	}
	return mergeMetadata(tpdb, stash)
}

// sharedPayloadToShared converts a durable Mongo shared_meta payload back into the
// in-process jobs.SharedMeta shape mergeMetadata consumes. Mirrors jobs.payloadToShared,
// which is unexported so the stremio serve path cannot call it directly.
func sharedPayloadToShared(p *models.SharedMetaPayload) *jobs.SharedMeta {
	if p == nil {
		return nil
	}
	return &jobs.SharedMeta{
		Title:       p.Title,
		Description: p.Description,
		Poster:      p.Poster,
		Background:  p.Background,
		Year:        p.Year,
		Cast:        p.Cast,
		Tags:        p.Tags,
		Genres:      p.Genres,
		Source:      p.Source,
	}
}
