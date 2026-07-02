package stremio

import (
	"context"
	"strings"
	"time"

	appconfig "torrent-search-go/internal/config"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
)

const stashMetaLookupTimeout = 8 * time.Second

// resolveStashdbCredentials returns the StashDB API key/URL for this install.
func resolveStashdbCredentials(cfg Config, env *appconfig.Config) (key, url string) {
	key = strings.TrimSpace(cfg.StashdbKey)
	url = strings.TrimSpace(cfg.StashdbURL)
	if env != nil {
		if key == "" {
			key = strings.TrimSpace(env.Metadata.StashDBAPIKey)
		}
		if url == "" {
			url = strings.TrimSpace(env.Metadata.StashDBAPIURL)
		}
	}
	if url == "" {
		url = "https://stashdb.org"
	}
	return key, url
}

func normalizedMetaToShared(m *metadata.NormalizedMeta) jobs.SharedMeta {
	if m == nil {
		return jobs.SharedMeta{}
	}
	return jobs.SharedMeta{
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

func stashBackground(m *jobs.SharedMeta) string {
	if m == nil {
		return ""
	}
	if m.Background != "" {
		return m.Background
	}
	return m.Poster
}

// loadStashMeta returns cached StashDB metadata for a torrent, fetching live on
// miss when a StashDB key is configured. Fresh results are written to Redis so
// catalog rows and detail pages stay in sync.
func (h *Handler) loadStashMeta(
	ctx context.Context,
	cfg Config,
	store *redisStore,
	metaID, title, detailURL string,
) *jobs.SharedMeta {
	stashKey, _ := resolveStashdbCredentials(cfg, h.Env)
	if metaID != "" && store != nil {
		if shared, err := store.getSharedMeta(ctx, "stashdb", metaID); err == nil && shared != nil {
			if shared.Poster != "" || shared.Title != "" {
				return shared
			}
		}
		if store.getSharedMetaMiss(ctx, "stashdb", metaID, stashKey) {
			return nil
		}
	}

	if stashKey == "" || strings.TrimSpace(title) == "" {
		return nil
	}

	rctx, cancel := context.WithTimeout(ctx, stashMetaLookupTimeout)
	defer cancel()

	_, stashURL := resolveStashdbCredentials(cfg, h.Env)
	client := metadata.NewStashDBClient(stashURL, stashKey)
	// SearchMetadataVariants is the shared multi-probe loop used by the
	// MetaEnricher too, so the catalog live path and the background enricher
	// resolve the same cover for the same title.
	meta, _ := client.SearchMetadataVariants(rctx, title, detailURL)
	if meta != nil {
		shared := normalizedMetaToShared(meta)
		if store != nil && metaID != "" {
			_ = store.setSharedMeta(ctx, "stashdb", metaID, shared)
		}
		return &shared
	}
	// A lookup timeout is transient, not a confirmed no-match: StashDB swallows
	// graphql errors and returns nil,nil, so the 8s deadline would otherwise
	// poison the miss cache and block the cover for an hour.
	if store != nil && metaID != "" && rctx.Err() == nil {
		_ = store.setSharedMetaMiss(ctx, "stashdb", metaID, stashKey)
	}
	return nil
}
