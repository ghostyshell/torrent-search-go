package stremio

import "context"

// StudioProvider supplies extra per-studio catalog names from KV storage.
type StudioProvider interface {
	ExtraStudios(ctx context.Context) ([]string, error)
}

// CoverProvider resolves poster URLs from the shared cover image cache.
type CoverProvider interface {
	CoverURL(ctx context.Context, website, title, size string) (string, error)
}
