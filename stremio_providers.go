package main

import (
	"context"
	"encoding/json"

	"torrent-search-go/internal/handlers"
	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/jobs"
	"torrent-search-go/internal/stremio"
)

const addonStudiosKVKey = "addon:xxx_studios"

type stremioStudioProvider struct {
	storage *handlers.StorageProvider
}

func (p *stremioStudioProvider) ExtraStudios(ctx context.Context) ([]string, error) {
	if p == nil || p.storage == nil {
		return nil, nil
	}
	raw, found, err := p.storage.KVGet(ctx, addonStudiosKVKey)
	if err != nil || !found || raw == "" {
		return nil, err
	}
	var studios []string
	if err := json.Unmarshal([]byte(raw), &studios); err != nil {
		return nil, err
	}
	return studios, nil
}

type stremioCoverProvider struct {
	storage *handlers.StorageProvider
}

func (p *stremioCoverProvider) CoverURL(ctx context.Context, website, title, size string) (string, error) {
	if p == nil || p.storage == nil {
		return "", nil
	}
	key := jobs.TorrentKey(models.Torrent{Name: title, Website: website, Size: size})
	if key == "" {
		return "", nil
	}
	row, err := p.storage.GetCoverImageByKey(ctx, key)
	if err != nil || row == nil || row.PixhostURL == "" {
		return "", err
	}
	return row.PixhostURL, nil
}

func newStremioProviders(storage *handlers.StorageProvider) (stremio.StudioProvider, stremio.CoverProvider) {
	if storage == nil {
		return nil, nil
	}
	return &stremioStudioProvider{storage: storage}, &stremioCoverProvider{storage: storage}
}
