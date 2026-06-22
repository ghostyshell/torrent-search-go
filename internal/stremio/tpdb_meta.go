package stremio

import (
	"context"
	"strings"
	"time"

	appconfig "torrent-search-go/internal/config"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
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
	parsed := metadata.ParseRelease(title)
	for _, probe := range metadataTitlesForLookup(title) {
		meta, err := client.SearchMetadataProbe(rctx, parsed, probe)
		if err != nil || meta == nil {
			continue
		}
		shared := normalizedMetaToShared(meta)
		if store != nil && metaID != "" {
			_ = store.setSharedMeta(ctx, "tpdb", metaID, shared)
		}
		return &shared
	}
	if store != nil && metaID != "" {
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
