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

func (p *StorageProvider) UpdateFavoriteEntryCoverImage(ctx context.Context, entryID, userID, coverImageURL string) (bool, error) {
	return p.dbClient.UpdateFavoriteEntryCoverImage(ctx, entryID, userID, coverImageURL)
}

func (p *StorageProvider) UpdateTorrentDetailsCoverImage(ctx context.Context, favoriteID, source, coverImageURL string) (bool, error) {
	return p.dbClient.UpdateTorrentDetailsCoverImage(ctx, favoriteID, source, coverImageURL)
}

func (p *StorageProvider) UpdateCachedLinkCoverImage(ctx context.Context, cachedLinkID, coverImageURL string) (bool, error) {
	return p.dbClient.UpdateCachedLinkCoverImage(ctx, cachedLinkID, coverImageURL)
}

func (p *StorageProvider) UpdateFavoriteEntryMagnetLink(ctx context.Context, entryID, userID, magnetLink string) (bool, error) {
	return p.dbClient.UpdateFavoriteEntryMagnetLink(ctx, entryID, userID, magnetLink)
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

// SetCoverImageEnriched stores a TPDB/StashDB or description cover in its
// dedicated slot (tpdb_url or details_url) and conditionally updates the
// primary cover. Source routing: "tpdb"/"stashdb" → UpsertTpdbCover;
// anything else → UpsertDetailsCover.
func (p *StorageProvider) SetCoverImageEnriched(ctx context.Context, torrentKey, imageURL string, isTemp bool, source, description, metaID string) error {
	isTpdb := source == "tpdb" || source == "stashdb"

	if p.objectStorage == nil || !p.objectStorage.IsEnabled() {
		if isTpdb {
			return p.dbClient.UpsertTpdbCover(ctx, torrentKey, imageURL, imageURL, "", source, description, metaID)
		}
		return p.dbClient.UpsertDetailsCover(ctx, torrentKey, imageURL, "")
	}

	imageData, _, err := fetchImageData(ctx, imageURL)
	if err != nil {
		return fmt.Errorf("failed to fetch cover image: %w", err)
	}

	objectKey := p.objectStorage.CoverKey(torrentKey, imageData, false) // covers are permanent
	presignedURL, err := p.objectStorage.UploadCover(ctx, torrentKey, imageData, false)
	if err != nil {
		return err
	}
	if isTpdb {
		return p.dbClient.UpsertTpdbCover(ctx, torrentKey, presignedURL, imageURL, objectKey, source, description, metaID)
	}
	return p.dbClient.UpsertDetailsCover(ctx, torrentKey, presignedURL, objectKey)
}

// SetCoverImageDetails stores a description/NFO scrape cover in its dedicated
// details_url slot. It mirrors the S3 upload path of SetCoverImage but uses
// the permanent prefix and writes only to the details slot.
func (p *StorageProvider) SetCoverImageDetails(ctx context.Context, torrentKey, imageURL string) error {
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() {
		return p.dbClient.UpsertDetailsCover(ctx, torrentKey, imageURL, "")
	}

	imageData, _, err := fetchImageData(ctx, imageURL)
	if err != nil {
		return p.dbClient.UpsertDetailsCover(ctx, torrentKey, imageURL, "")
	}

	objectKey := p.objectStorage.CoverKey(torrentKey, imageData, false)
	presignedURL, err := p.objectStorage.UploadCover(ctx, torrentKey, imageData, false)
	if err != nil {
		return p.dbClient.UpsertDetailsCover(ctx, torrentKey, imageURL, "")
	}
	return p.dbClient.UpsertDetailsCover(ctx, torrentKey, presignedURL, objectKey)
}

// SetManualCover stores a user-selected cover in the primary slot with
// cover_source="manual" so automated jobs never overwrite it.
func (p *StorageProvider) SetManualCover(ctx context.Context, torrentKey, imageURL string) error {
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() {
		return p.dbClient.SetManualCover(ctx, torrentKey, imageURL, imageURL, "")
	}

	imageData, _, err := fetchImageData(ctx, imageURL)
	if err != nil {
		return p.dbClient.SetManualCover(ctx, torrentKey, imageURL, imageURL, "")
	}

	objectKey := p.objectStorage.CoverKey(torrentKey, imageData, false)
	presignedURL, err := p.objectStorage.UploadCover(ctx, torrentKey, imageData, false)
	if err != nil {
		return p.dbClient.SetManualCover(ctx, torrentKey, imageURL, imageURL, "")
	}
	return p.dbClient.SetManualCover(ctx, torrentKey, presignedURL, imageURL, objectKey)
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
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() || row == nil {
		return
	}
	if row.StorageKey != nil && *row.StorageKey != "" {
		if url, err := p.objectStorage.GetPresignedURL(ctx, *row.StorageKey); err == nil {
			row.PixhostURL = url
		}
	} else if key := s3KeyFromPresignedURL(row.PixhostURL); key != "" {
		// Row was stored before storage_key tracking; extract the key from the URL so
		// we can still re-sign it (common for temp covers written by older code).
		if url, err := p.objectStorage.GetPresignedURL(ctx, key); err == nil {
			row.PixhostURL = url
		}
	}
	if row.TpdbStorageKey != nil && *row.TpdbStorageKey != "" {
		if url, err := p.objectStorage.GetPresignedURL(ctx, *row.TpdbStorageKey); err == nil {
			row.TpdbURL = &url
		}
	}
	if row.DetailsStorageKey != nil && *row.DetailsStorageKey != "" {
		if url, err := p.objectStorage.GetPresignedURL(ctx, *row.DetailsStorageKey); err == nil {
			row.DetailsURL = &url
		}
	}
}

// s3KeyFromPresignedURL extracts the object key (e.g. "covers/temp/foo.jpg") from
// an S3 presigned URL when the storage_key column was not recorded.
// Returns "" for any non-S3 or unrecognised URL.
func s3KeyFromPresignedURL(rawURL string) string {
	if !strings.Contains(rawURL, "X-Amz-Signature") {
		return ""
	}
	const marker = "/torrent-cache/"
	idx := strings.Index(rawURL, marker)
	if idx < 0 {
		return ""
	}
	key := rawURL[idx+len(marker):]
	if q := strings.IndexByte(key, '?'); q >= 0 {
		key = key[:q]
	}
	return key
}

// BackfillCoverStorageKey uploads an existing cover's raw external URL to object
// storage (keep/) and records the storage_key so the row becomes re-signable on
// read. Used by the cover-storage maintenance backfill for rows persisted without
// S3 (set-cover fallback path when the server-side fetch was blocked). Returns
// (modified, error); an unreachable source URL or a missing row yields an error
// so the caller can skip and retry. No-ops (returns false, nil) when S3 is off.
func (p *StorageProvider) BackfillCoverStorageKey(ctx context.Context, torrentKey, imageURL string) (bool, error) {
	if p.objectStorage == nil || !p.objectStorage.IsEnabled() {
		return false, nil
	}
	imageData, _, err := fetchImageData(ctx, imageURL)
	if err != nil {
		return false, err
	}
	objectKey := p.objectStorage.CoverKey(torrentKey, imageData, false)
	presignedURL, err := p.objectStorage.UploadCover(ctx, torrentKey, imageData, false)
	if err != nil {
		return false, err
	}
	modified, err := p.UpdateCoverStorageKey(ctx, torrentKey, objectKey, presignedURL)
	return modified, err
}

func (p *StorageProvider) GetObjectStorageCovers(ctx context.Context, limit, offset int) ([]models.ObjectStorageCover, error) {
	return p.dbClient.GetObjectStorageCovers(ctx, limit, offset)
}

func (p *StorageProvider) GetCoverImagesMissingStorageKey(ctx context.Context, limit int, afterKey string) ([]*models.ImageRow, error) {
	return p.dbClient.GetCoverImagesMissingStorageKey(ctx, limit, afterKey)
}

func (p *StorageProvider) UpdateCoverPresignedURL(ctx context.Context, torrentKey, presignedURL string) (bool, error) {
	return p.dbClient.UpdateCoverPresignedURL(ctx, torrentKey, presignedURL)
}

func (p *StorageProvider) UpdateCoverStorageKey(ctx context.Context, torrentKey, storageKey, presignedURL string) (bool, error) {
	return p.dbClient.UpdateCoverStorageKey(ctx, torrentKey, storageKey, presignedURL)
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

// ─── Pornrips entries delegates ──────────────────────────────────────────────

func (p *StorageProvider) UpsertPornripsEntry(ctx context.Context, entry models.PornripsEntry) error {
	return p.dbClient.UpsertPornripsEntry(ctx, entry)
}

func (p *StorageProvider) UpdatePornripsEnrichment(ctx context.Context, slug, poster, resolvedTitle string, tags, genres, performers []string) error {
	return p.dbClient.UpdatePornripsEnrichment(ctx, slug, poster, resolvedTitle, tags, genres, performers)
}

func (p *StorageProvider) GetPornripsRecent(ctx context.Context, skip, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsRecent(ctx, skip, limit)
}

func (p *StorageProvider) GetPornripsByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsByStudio(ctx, studioNorm, skip, limit)
}

func (p *StorageProvider) GetPornripsByTag(ctx context.Context, tags []string, skip, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsByTag(ctx, tags, skip, limit)
}

func (p *StorageProvider) SearchPornrips(ctx context.Context, query string, skip, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.SearchPornrips(ctx, query, skip, limit)
}

func (p *StorageProvider) GetPornripsEntryBySlug(ctx context.Context, slug string) (*models.PornripsEntry, error) {
	return p.dbClient.GetPornripsEntryBySlug(ctx, slug)
}

func (p *StorageProvider) GetPornripsEntriesByPerformer(ctx context.Context, performer string, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsEntriesByPerformer(ctx, performer, limit)
}

func (p *StorageProvider) GetPornripsEntriesByPerformers(ctx context.Context, performers []string, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsEntriesByPerformers(ctx, performers, limit)
}

func (p *StorageProvider) PerformersWithTorrent(ctx context.Context, performers []string) (map[string]bool, error) {
	return p.dbClient.PerformersWithTorrent(ctx, performers)
}

func (p *StorageProvider) GetPornripsEntriesMissingEnrichment(ctx context.Context, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsEntriesMissingEnrichment(ctx, limit)
}

func (p *StorageProvider) GetPornripsEntriesMissingTorrent(ctx context.Context, limit int) ([]models.PornripsEntry, error) {
	return p.dbClient.GetPornripsEntriesMissingTorrent(ctx, limit)
}

func (p *StorageProvider) SetPornripsTorrent(ctx context.Context, slug, infoHash, torrentURL string) error {
	return p.dbClient.SetPornripsTorrent(ctx, slug, infoHash, torrentURL)
}

func (p *StorageProvider) PornripsEntriesCount(ctx context.Context) (int64, error) {
	return p.dbClient.PornripsEntriesCount(ctx)
}

// ─── Enriched scenes delegates ───────────────────────────────────────────────

func (p *StorageProvider) UpsertEnrichedScene(ctx context.Context, scene models.EnrichedScene) error {
	return p.dbClient.UpsertEnrichedScene(ctx, scene)
}

func (p *StorageProvider) GetEnrichedScenesByMatchedSources(ctx context.Context, source string, tags []string, sources []string, skip, limit int) ([]models.EnrichedScene, error) {
	return p.dbClient.GetEnrichedScenesByMatchedSources(ctx, source, tags, sources, skip, limit)
}

func (p *StorageProvider) GetEnrichedSceneByID(ctx context.Context, id string) (*models.EnrichedScene, error) {
	return p.dbClient.GetEnrichedSceneByID(ctx, id)
}

func (p *StorageProvider) GetEnrichedScenesMissingSourceMatch(ctx context.Context, source string, limit int) ([]models.EnrichedScene, error) {
	return p.dbClient.GetEnrichedScenesMissingSourceMatch(ctx, source, limit)
}

func (p *StorageProvider) EnrichedScenesCount(ctx context.Context) (int64, error) {
	return p.dbClient.EnrichedScenesCount(ctx)
}

// ─── Hentai entries delegates ────────────────────────────────────────────────

func (p *StorageProvider) UpsertHentaiEntry(ctx context.Context, entry models.HentaiEntry) error {
	return p.dbClient.UpsertHentaiEntry(ctx, entry)
}

func (p *StorageProvider) GetHentaiRecent(ctx context.Context, skip, limit int) ([]models.HentaiEntry, error) {
	return p.dbClient.GetHentaiRecent(ctx, skip, limit)
}

func (p *StorageProvider) GetHentaiTop(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error) {
	return p.dbClient.GetHentaiTop(ctx, genreNorm, skip, limit)
}

func (p *StorageProvider) GetHentaiAll(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error) {
	return p.dbClient.GetHentaiAll(ctx, genreNorm, skip, limit)
}

func (p *StorageProvider) GetHentaiByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.HentaiEntry, error) {
	return p.dbClient.GetHentaiByStudio(ctx, studioNorm, skip, limit)
}

func (p *StorageProvider) GetHentaiByYear(ctx context.Context, year string, skip, limit int) ([]models.HentaiEntry, error) {
	return p.dbClient.GetHentaiByYear(ctx, year, skip, limit)
}

func (p *StorageProvider) SearchHentai(ctx context.Context, query string, skip, limit int) ([]models.HentaiEntry, error) {
	return p.dbClient.SearchHentai(ctx, query, skip, limit)
}

func (p *StorageProvider) GetHentaiEntry(ctx context.Context, id string) (*models.HentaiEntry, error) {
	return p.dbClient.GetHentaiEntry(ctx, id)
}

func (p *StorageProvider) HentaiEntriesCount(ctx context.Context) (int64, error) {
	return p.dbClient.HentaiEntriesCount(ctx)
}

func (p *StorageProvider) GetHentaiTopStudios(ctx context.Context, limit int) ([]string, error) {
	return p.dbClient.GetHentaiTopStudios(ctx, limit)
}

func (p *StorageProvider) GetHentaiTopGenres(ctx context.Context, limit int) ([]string, error) {
	return p.dbClient.GetHentaiTopGenres(ctx, limit)
}

func (p *StorageProvider) GetHentaiYears(ctx context.Context) ([]string, error) {
	return p.dbClient.GetHentaiYears(ctx)
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

func (p *StorageProvider) ListAddonStatusReports(ctx context.Context) ([]models.AddonStatusReport, error) {
	return p.dbClient.ListAddonStatusReports(ctx)
}

func (p *StorageProvider) GetAddonStatusReport(ctx context.Context, id string) (*models.AddonStatusReport, error) {
	return p.dbClient.GetAddonStatusReport(ctx, id)
}

func (p *StorageProvider) UpsertAddonStatusReport(ctx context.Context, r models.AddonStatusReport) error {
	return p.dbClient.UpsertAddonStatusReport(ctx, r)
}

func (p *StorageProvider) DeleteAddonStatusReport(ctx context.Context, id string) error {
	return p.dbClient.DeleteAddonStatusReport(ctx, id)
}
