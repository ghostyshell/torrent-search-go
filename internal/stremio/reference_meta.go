package stremio

import (
	"context"
	"encoding/json"
	"time"

	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/services/metadata"
)

const (
	prefixReferenceMeta = "refmeta:v1:"
	ttlReferenceMeta    = 7 * 24 * time.Hour
	ttlRefNegative      = 6 * time.Hour
)

type referenceMetaCacheEntry struct {
	Found bool                   `json:"found"`
	Meta  *metadata.ReferenceMeta `json:"meta,omitempty"`
}

// PornripsSlug extracts the post slug from a PornRips detail URL.
func PornripsSlug(detailURL string) string {
	if m := pornripsSlugRE.FindStringSubmatch(detailURL); len(m) > 1 {
		return m[1]
	}
	return ""
}

func (h *Handler) referenceMetaForSlug(ctx context.Context, slug string, live bool) *metadata.ReferenceMeta {
	if slug == "" {
		return nil
	}
	store := newRedisStore(h.Redis)
	if store != nil {
		if meta, found := store.getReferenceMeta(ctx, slug); found {
			return meta
		}
	}
	if !live || h.Reference == nil || !h.Reference.Enabled() {
		return nil
	}
	meta, err := h.Reference.GetPornripsMeta(ctx, slug)
	if err != nil {
		return nil
	}
	if store != nil {
		entry := referenceMetaCacheEntry{Found: meta != nil, Meta: meta}
		ttl := ttlReferenceMeta
		if meta == nil {
			ttl = ttlRefNegative
		}
		_ = store.setReferenceMeta(ctx, slug, entry, ttl)
	}
	return meta
}

func referenceToMerged(ref *metadata.ReferenceMeta) *jobs.SharedMeta {
	if ref == nil {
		return nil
	}
	shared := ref.ToSharedMeta()
	return &jobs.SharedMeta{
		Title:       shared.Title,
		Poster:      shared.Poster,
		Background:  shared.Background,
		Description: shared.Description,
		Year:        shared.Year,
		Cast:        shared.Cast,
		Genres:      shared.Genres,
		Source:      shared.Source,
	}
}

func (s *redisStore) getReferenceMeta(ctx context.Context, slug string) (*metadata.ReferenceMeta, bool) {
	if s == nil || s.client == nil || slug == "" {
		return nil, false
	}
	raw, ok, err := s.client.Get(ctx, prefixReferenceMeta+slug)
	if err != nil || !ok {
		return nil, false
	}
	var entry referenceMetaCacheEntry
	if err := json.Unmarshal([]byte(raw), &entry); err == nil {
		return entry.Meta, true
	}
	// Legacy: warmer may have stored a bare ReferenceMeta JSON object.
	var meta metadata.ReferenceMeta
	if err := json.Unmarshal([]byte(raw), &meta); err == nil {
		if meta.Name != "" || meta.Poster != "" {
			return &meta, true
		}
		return nil, true
	}
	return nil, false
}

func (s *redisStore) setReferenceMeta(ctx context.Context, slug string, entry referenceMetaCacheEntry, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Set(ctx, prefixReferenceMeta+slug, entry, ttl)
}
