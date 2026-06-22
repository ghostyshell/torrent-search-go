package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"torrent-search-go/internal/crypto"
	"torrent-search-go/internal/services/storage"
	"torrent-search-go/pkg/models"
	store "torrent-search-go/pkg/storage"
)

// StorageProvider coordinates all data operations
type StorageProvider struct {
	dbClient      store.Database
	objectStorage *storage.ObjectStorage
	isInitialized bool
}

// NewStorageProvider creates a new storage provider
func NewStorageProvider(dbClient store.Database) (*StorageProvider, error) {
	provider := &StorageProvider{
		dbClient: dbClient,
	}

	return provider, nil
}

// SetObjectStorage wires the optional S3-compatible object storage backend.
func (p *StorageProvider) SetObjectStorage(os *storage.ObjectStorage) {
	p.objectStorage = os
}

// Initialize initializes the storage provider
func (p *StorageProvider) Initialize(ctx context.Context) error {
	if p.isInitialized {
		return nil
	}

	// Run migrations
	if err := p.dbClient.Migrate(); err != nil {
		return err
	}

	p.isInitialized = true
	return nil
}

// Close closes the storage provider
func (p *StorageProvider) Close() error {
	return p.dbClient.Close()
}

// Cleanup performs periodic cleanup
func (p *StorageProvider) Cleanup(ctx context.Context) error {
	return p.dbClient.CleanupExpired(ctx)
}

// HealthCheck performs a health check
func (p *StorageProvider) HealthCheck() (*models.HealthStatus, error) {
	return p.dbClient.HealthCheck()
}

// GetStats returns storage statistics
func (p *StorageProvider) GetStats() map[string]interface{} {
	stats := p.dbClient.GetStats()
	return map[string]interface{}{
		"isConnected":  stats.IsConnected,
		"databaseType": stats.DatabaseType,
		"lastCheck":    stats.LastCheck,
	}
}

// ─── User delegates ──────────────────────────────────────────────────────────

func (p *StorageProvider) GetUserByID(ctx context.Context, id string) (*models.UserRow, error) {
	return p.dbClient.GetUserByID(ctx, id)
}

func (p *StorageProvider) GetUserByEmail(ctx context.Context, email string) (*models.UserRow, error) {
	return p.dbClient.GetUserByEmail(ctx, email)
}

func (p *StorageProvider) GetUserByGoogleID(ctx context.Context, googleID string) (*models.UserRow, error) {
	return p.dbClient.GetUserByGoogleID(ctx, googleID)
}

func (p *StorageProvider) CreateUser(ctx context.Context, id, email, name, picture, googleID string) error {
	return p.dbClient.CreateUser(ctx, id, email, name, picture, googleID)
}

func (p *StorageProvider) UpdateUserLastLogin(ctx context.Context, id string) error {
	return p.dbClient.UpdateUserLastLogin(ctx, id)
}

func (p *StorageProvider) UpdateUserGoogleTokens(ctx context.Context, id, accessToken, refreshToken string, expiresAt int64) error {
	return p.dbClient.UpdateUserGoogleTokens(ctx, id, accessToken, refreshToken, expiresAt)
}

func (p *StorageProvider) GetRealDebridKey(ctx context.Context, userID string) (string, error) {
	enc, err := p.dbClient.GetRealDebridKey(ctx, userID)
	if err != nil || enc == "" {
		return "", err
	}
	return crypto.DecryptSecret(enc)
}

func (p *StorageProvider) HasRealDebridKey(ctx context.Context, userID string) (bool, error) {
	enc, err := p.dbClient.GetRealDebridKey(ctx, userID)
	if err != nil {
		return false, err
	}
	return enc != "", nil
}

func (p *StorageProvider) SetRealDebridKey(ctx context.Context, userID, apiKey string) error {
	enc, err := crypto.EncryptSecret(apiKey)
	if err != nil {
		return err
	}
	return p.dbClient.SetRealDebridKey(ctx, userID, enc)
}

func (p *StorageProvider) DeleteRealDebridKey(ctx context.Context, userID string) error {
	return p.dbClient.DeleteRealDebridKey(ctx, userID)
}

// ─── Auth exchange delegates ─────────────────────────────────────────────────

func (p *StorageProvider) CreateExchangeCode(ctx context.Context, sessionToken string) (string, error) {
	return p.dbClient.CreateExchangeCode(ctx, sessionToken)
}

func (p *StorageProvider) ConsumeExchangeCode(ctx context.Context, code string) (string, error) {
	return p.dbClient.ConsumeExchangeCode(ctx, code)
}

// ─── Session delegates ───────────────────────────────────────────────────────

func (p *StorageProvider) CreateSession(ctx context.Context, sessionID, userID, token, userAgent, ipAddress string, expiresAt int64) error {
	return p.dbClient.CreateSession(ctx, sessionID, userID, token, userAgent, ipAddress, expiresAt)
}

func (p *StorageProvider) ValidateSession(ctx context.Context, token string) (*models.SessionRow, error) {
	return p.dbClient.ValidateSession(ctx, token)
}

func (p *StorageProvider) DeleteSession(ctx context.Context, token string) error {
	return p.dbClient.DeleteSession(ctx, token)
}

func (p *StorageProvider) GetSessionsByUserID(ctx context.Context, userID string) ([]*models.SessionRow, error) {
	return p.dbClient.GetSessionsByUserID(ctx, userID)
}

// ─── Favorites delegates ─────────────────────────────────────────────────────

func (p *StorageProvider) AddFavorite(ctx context.Context, id, userID, torrentKey, torrentName, torrentData, coverImageURL, magnetLink string) error {
	return p.dbClient.AddFavorite(ctx, id, userID, torrentKey, torrentName, torrentData, coverImageURL, magnetLink)
}

func (p *StorageProvider) GetFavoritesByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.FavoriteRow, error) {
	return p.dbClient.GetFavoritesByUserID(ctx, userID, limit, offset)
}

func (p *StorageProvider) GetFavoritesByUserIDs(ctx context.Context, userIDs []string, limit, offset int) ([]*models.FavoriteRow, error) {
	return p.dbClient.GetFavoritesByUserIDs(ctx, userIDs, limit, offset)
}

func (p *StorageProvider) CountFavoritesByUserID(ctx context.Context, userID string) (int, error) {
	return p.dbClient.CountFavoritesByUserID(ctx, userID)
}

func (p *StorageProvider) CountFavoritesByUserIDs(ctx context.Context, userIDs []string) (int, error) {
	return p.dbClient.CountFavoritesByUserIDs(ctx, userIDs)
}

func (p *StorageProvider) GetFavoritesForStreamRefresh(ctx context.Context) ([]models.UserFavoritesRefresh, error) {
	return p.dbClient.GetFavoritesForStreamRefresh(ctx)
}

func (p *StorageProvider) GetUsersWithRealDebridKeys(ctx context.Context) ([]models.UserRealDebridKey, error) {
	return p.dbClient.GetUsersWithRealDebridKeys(ctx)
}

func (p *StorageProvider) RemoveFavorite(ctx context.Context, torrentKey, userID string) (bool, error) {
	return p.dbClient.RemoveFavorite(ctx, torrentKey, userID)
}

func (p *StorageProvider) RemoveFavoriteForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (bool, error) {
	return p.dbClient.RemoveFavoriteForUserIDs(ctx, torrentKey, userIDs)
}

func (p *StorageProvider) IsFavorite(ctx context.Context, torrentKey, userID string) (bool, error) {
	return p.dbClient.IsFavorite(ctx, torrentKey, userID)
}

func (p *StorageProvider) IsFavoriteForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (bool, error) {
	return p.dbClient.IsFavoriteForUserIDs(ctx, torrentKey, userIDs)
}

func (p *StorageProvider) GetFavoriteByKey(ctx context.Context, torrentKey, userID string) (*models.FavoriteRow, error) {
	return p.dbClient.GetFavoriteByKey(ctx, torrentKey, userID)
}

func (p *StorageProvider) GetFavoriteByKeyForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (*models.FavoriteRow, error) {
	return p.dbClient.GetFavoriteByKeyForUserIDs(ctx, torrentKey, userIDs)
}

func (p *StorageProvider) UpdateFavoriteEntryCoverImage(ctx context.Context, entryID, coverImageURL string) (bool, error) {
	return p.dbClient.UpdateFavoriteEntryCoverImage(ctx, entryID, coverImageURL)
}

func (p *StorageProvider) UpdateTorrentDetailsCoverImage(ctx context.Context, favoriteID, source, coverImageURL string) (bool, error) {
	return p.dbClient.UpdateTorrentDetailsCoverImage(ctx, favoriteID, source, coverImageURL)
}

func (p *StorageProvider) UpdateCachedLinkCoverImage(ctx context.Context, cachedLinkID, coverImageURL string) (bool, error) {
	return p.dbClient.UpdateCachedLinkCoverImage(ctx, cachedLinkID, coverImageURL)
}

func (p *StorageProvider) UpdateFavoriteEntryMagnetLink(ctx context.Context, entryID, magnetLink string) (bool, error) {
	return p.dbClient.UpdateFavoriteEntryMagnetLink(ctx, entryID, magnetLink)
}

func (p *StorageProvider) StoreFavoriteEntry(ctx context.Context, entryID string, data map[string]interface{}) error {
	return p.dbClient.StoreFavoriteEntry(ctx, entryID, data)
}

func (p *StorageProvider) GetFavoriteEntryByID(ctx context.Context, entryID string) (interface{}, error) {
	return p.dbClient.GetFavoriteEntryByID(ctx, entryID)
}

func (p *StorageProvider) StoreFavoriteDetails(ctx context.Context, favoriteID string, details interface{}) error {
	return p.dbClient.StoreFavoriteDetails(ctx, favoriteID, details)
}

func (p *StorageProvider) GetFavoriteDetails(ctx context.Context, favoriteID string) (interface{}, error) {
	return p.dbClient.GetFavoriteDetails(ctx, favoriteID)
}

// ─── Cover image delegates ───────────────────────────────────────────────────

// SetCoverImage stores a cover image. When S3 object storage is enabled, the
// image is fetched from imageURL (or decoded from a data URI) and uploaded to
// the bucket; the resulting presigned URL and S3 object key are persisted.
// isTemp places the object under the temp prefix so it is eligible for cleanup.
func (p *StorageProvider) SetCoverImage(ctx context.Context, torrentKey, imageURL string, isTemp bool) error {
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() {
		return p.dbClient.SetCoverImage(ctx, torrentKey, imageURL)
	}

	imageData, _, err := fetchImageData(ctx, imageURL)
	if err != nil {
		// The source host blocked the server-side fetch (common with hotlink-
		// protected image hosts). Persist the raw URL so the set-cover isn't lost
		// entirely; the browser can usually load it where the server can't.
		return p.dbClient.SetCoverImage(ctx, torrentKey, imageURL)
	}

	objectKey := p.objectStorage.CoverKey(torrentKey, imageData, isTemp)
	presignedURL, err := p.objectStorage.UploadCover(ctx, torrentKey, imageData, isTemp)
	if err != nil {
		// Upload failed; persist the raw URL as above so the cover isn't lost.
		return p.dbClient.SetCoverImage(ctx, torrentKey, imageURL)
	}
	return p.dbClient.SetCoverImageWithStorageKey(ctx, torrentKey, presignedURL, imageURL, objectKey)
}

// SetCoverImageEnriched stores a cover together with its TPDB/StashDB enrichment
// (cover source, description, and the shared_meta link). It mirrors the S3 upload
// path of SetCoverImage so an enriched cover is also backed by object storage when
// enabled, then persists the row via SetCoverImageMeta.
func (p *StorageProvider) SetCoverImageEnriched(ctx context.Context, torrentKey, imageURL string, isTemp bool, source, description, metaID string) error {
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() {
		return p.dbClient.SetCoverImageMeta(ctx, torrentKey, imageURL, imageURL, "", source, description, metaID)
	}

	imageData, _, err := fetchImageData(ctx, imageURL)
	if err != nil {
		return fmt.Errorf("failed to fetch cover image: %w", err)
	}

	objectKey := p.objectStorage.CoverKey(torrentKey, imageData, isTemp)
	presignedURL, err := p.objectStorage.UploadCover(ctx, torrentKey, imageData, isTemp)
	if err != nil {
		return err
	}
	return p.dbClient.SetCoverImageMeta(ctx, torrentKey, presignedURL, imageURL, objectKey, source, description, metaID)
}

func (p *StorageProvider) GetCoverImageByKey(ctx context.Context, torrentKey string) (*models.ImageRow, error) {
	row, err := p.dbClient.GetCoverImageByKey(ctx, torrentKey)
	if err != nil || row == nil {
		return row, err
	}
	p.refreshCoverPresignedURL(ctx, row)
	return row, nil
}

func (p *StorageProvider) GetCoverImagesByKeys(ctx context.Context, torrentKeys []string) (map[string]*models.ImageRow, error) {
	rows, err := p.dbClient.GetCoverImagesByKeys(ctx, torrentKeys)
	if err != nil {
		return rows, err
	}
	for _, row := range rows {
		p.refreshCoverPresignedURL(ctx, row)
	}
	return rows, nil
}

func (p *StorageProvider) refreshCoverPresignedURL(ctx context.Context, row *models.ImageRow) {
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() || row == nil || row.StorageKey == nil || *row.StorageKey == "" {
		return
	}
	if url, err := p.objectStorage.GetPresignedURL(ctx, *row.StorageKey); err == nil {
		row.PixhostURL = url
	}
}

func (p *StorageProvider) GetObjectStorageCovers(ctx context.Context, limit, offset int) ([]models.ObjectStorageCover, error) {
	return p.dbClient.GetObjectStorageCovers(ctx, limit, offset)
}

func (p *StorageProvider) UpdateCoverPresignedURL(ctx context.Context, torrentKey, presignedURL string) (bool, error) {
	return p.dbClient.UpdateCoverPresignedURL(ctx, torrentKey, presignedURL)
}

func (p *StorageProvider) DeleteCoverByStorageKey(ctx context.Context, storageKey string) (bool, error) {
	return p.dbClient.DeleteCoverByStorageKey(ctx, storageKey)
}

func (p *StorageProvider) DeleteCoverImage(ctx context.Context, torrentKey string) (bool, error) {
	return p.dbClient.DeleteCoverImage(ctx, torrentKey)
}

func (p *StorageProvider) GetFallbackUrlsByPixhostUrl(pixhostUrl string) ([]string, error) {
	return p.dbClient.GetFallbackUrlsByPixhostUrl(pixhostUrl)
}

// fetchImageData returns raw image bytes and a content type for an image URL.
// It supports HTTP/HTTPS URLs and base64 data URIs.
func fetchImageData(ctx context.Context, imageURL string) ([]byte, string, error) {
	if strings.HasPrefix(imageURL, "data:") {
		return decodeDataURI(imageURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	return data, resp.Header.Get("Content-Type"), err
}

// decodeDataURI parses a data URI such as data:image/jpeg;base64,... and returns
// the decoded bytes plus the declared content type.
func decodeDataURI(uri string) ([]byte, string, error) {
	const prefix = "data:"
	if !strings.HasPrefix(uri, prefix) {
		return nil, "", fmt.Errorf("invalid data URI")
	}
	body := strings.TrimPrefix(uri, prefix)
	idx := strings.Index(body, ",")
	if idx < 0 {
		return nil, "", fmt.Errorf("invalid data URI")
	}
	meta := body[:idx]
	encoded := body[idx+1:]

	contentType := "image/jpeg"
	if semi := strings.Index(meta, ";"); semi >= 0 {
		contentType = meta[:semi]
	} else if meta != "" {
		contentType = meta
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode data URI: %w", err)
	}
	return data, contentType, nil
}

// ─── Stream URL delegates ────────────────────────────────────────────────────

func (p *StorageProvider) SetStreamURL(ctx context.Context, in models.StreamURLInput) error {
	return p.dbClient.SetStreamURL(ctx, in)
}

func (p *StorageProvider) GetStreamURLByHash(ctx context.Context, magnetHash string) (*models.StreamURLRow, error) {
	return p.dbClient.GetStreamURLByHash(ctx, magnetHash)
}

func (p *StorageProvider) GetStreamURLByMagnet(ctx context.Context, magnetLink string) (*models.StreamURLRow, error) {
	return p.dbClient.GetStreamURLByMagnet(ctx, magnetLink)
}

// ─── Cached links delegates ──────────────────────────────────────────────────

func (p *StorageProvider) AddCachedLink(ctx context.Context, id, userID, linkType, originalURL, title string) error {
	return p.dbClient.AddCachedLink(ctx, id, userID, linkType, originalURL, title)
}

func (p *StorageProvider) GetCachedLinks(ctx context.Context, page, limit int, userID string) ([]*models.CachedLinkRow, int, error) {
	return p.dbClient.GetCachedLinks(ctx, page, limit, userID)
}

func (p *StorageProvider) GetCachedLinkByID(ctx context.Context, id string) (*models.CachedLinkRow, error) {
	return p.dbClient.GetCachedLinkByID(ctx, id)
}

func (p *StorageProvider) UpdateCachedLink(ctx context.Context, id, userID string, updates map[string]interface{}) (bool, error) {
	return p.dbClient.UpdateCachedLink(ctx, id, userID, updates)
}

func (p *StorageProvider) RemoveCachedLink(ctx context.Context, id, userID string) (bool, error) {
	return p.dbClient.RemoveCachedLink(ctx, id, userID)
}

// ─── KV cache delegates ──────────────────────────────────────────────────────

func (p *StorageProvider) KVSet(ctx context.Context, key, value string, ttlSeconds *int64) error {
	return p.dbClient.KVSet(ctx, key, value, ttlSeconds)
}

func (p *StorageProvider) KVGet(ctx context.Context, key string) (string, bool, error) {
	return p.dbClient.KVGet(ctx, key)
}

func (p *StorageProvider) KVDelete(ctx context.Context, key string) error {
	return p.dbClient.KVDelete(ctx, key)
}

// ─── Shared metadata delegates ───────────────────────────────────────────────

func (p *StorageProvider) SetSharedMeta(ctx context.Context, source, metaID string, payload models.SharedMetaPayload) error {
	return p.dbClient.SetSharedMeta(ctx, source, metaID, payload)
}

func (p *StorageProvider) GetSharedMetaPair(ctx context.Context, metaID string) (*models.SharedMetaPayload, *models.SharedMetaPayload, error) {
	return p.dbClient.GetSharedMetaPair(ctx, metaID)
}

func (p *StorageProvider) ExistsSharedMany(ctx context.Context, source string, metaIDs []string) ([]bool, error) {
	return p.dbClient.ExistsSharedMany(ctx, source, metaIDs)
}

func (p *StorageProvider) SetSukebeiCatalog(ctx context.Context, catalogID string, entriesJSON []byte) error {
	return p.dbClient.SetSukebeiCatalog(ctx, catalogID, entriesJSON)
}

func (p *StorageProvider) GetSukebeiCatalog(ctx context.Context, catalogID string) ([]byte, bool, error) {
	return p.dbClient.GetSukebeiCatalog(ctx, catalogID)
}

// ─── Search query cache delegates ────────────────────────────────────────────

func (p *StorageProvider) RecordSearchQuery(ctx context.Context, query string) error {
	return p.dbClient.RecordSearchQuery(ctx, query)
}

func (p *StorageProvider) GetRecentSearchQueries(ctx context.Context, since time.Time) ([]string, error) {
	return p.dbClient.GetRecentSearchQueries(ctx, since)
}

func (p *StorageProvider) CleanupOldSearchQueries(ctx context.Context, before time.Time) (int64, error) {
	return p.dbClient.CleanupOldSearchQueries(ctx, before)
}

// ─── Stats delegates ─────────────────────────────────────────────────────────

func (p *StorageProvider) GetTableStats(ctx context.Context) (*models.DBTableStats, error) {
	return p.dbClient.GetTableStats(ctx)
}

func (p *StorageProvider) GetFavoriteStats(ctx context.Context) (map[string]interface{}, error) {
	return p.dbClient.GetFavoriteStats(ctx)
}

// ─── IP block delegates ───────────────────────────────────────────────────────

func (p *StorageProvider) AddBlockedIP(ctx context.Context, ip, reason, notes string, requestCount int64) error {
	return p.dbClient.AddBlockedIP(ctx, ip, reason, notes, requestCount)
}

func (p *StorageProvider) RemoveBlockedIP(ctx context.Context, ip string) error {
	return p.dbClient.RemoveBlockedIP(ctx, ip)
}

func (p *StorageProvider) GetBlockedIPs(ctx context.Context) ([]*models.BlockedIP, error) {
	return p.dbClient.GetBlockedIPs(ctx)
}

func (p *StorageProvider) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	return p.dbClient.IsIPBlocked(ctx, ip)
}
