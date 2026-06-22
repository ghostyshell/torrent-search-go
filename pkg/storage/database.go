package storage

import (
	"context"
	"time"

	"torrent-search-go/pkg/models"
)

// Database is the persistence layer used by StorageProvider.
type Database interface {
	Migrate() error
	Close() error
	HealthCheck() (*models.HealthStatus, error)
	GetStats() *models.Stats
	CleanupExpired(ctx context.Context) error

	GetUserByID(ctx context.Context, id string) (*models.UserRow, error)
	GetUserByEmail(ctx context.Context, email string) (*models.UserRow, error)
	GetUserByGoogleID(ctx context.Context, googleID string) (*models.UserRow, error)
	CreateUser(ctx context.Context, id, email, name, picture, googleID string) error
	UpdateUserLastLogin(ctx context.Context, id string) error
	UpdateUserGoogleTokens(ctx context.Context, id, accessToken, refreshToken string, expiresAt int64) error
	GetRealDebridKey(ctx context.Context, userID string) (string, error)
	SetRealDebridKey(ctx context.Context, userID, apiKey string) error
	DeleteRealDebridKey(ctx context.Context, userID string) error
	GetUsersWithRealDebridKeys(ctx context.Context) ([]models.UserRealDebridKey, error)

	CreateExchangeCode(ctx context.Context, sessionToken string) (string, error)
	ConsumeExchangeCode(ctx context.Context, code string) (string, error)

	CreateSession(ctx context.Context, sessionID, userID, token, userAgent, ipAddress string, expiresAt int64) error
	ValidateSession(ctx context.Context, token string) (*models.SessionRow, error)
	DeleteSession(ctx context.Context, token string) error
	GetSessionsByUserID(ctx context.Context, userID string) ([]*models.SessionRow, error)

	AddFavorite(ctx context.Context, id, userID, torrentKey, torrentName, torrentData, coverImageURL, magnetLink string) error
	GetFavoritesByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.FavoriteRow, error)
	GetFavoritesByUserIDs(ctx context.Context, userIDs []string, limit, offset int) ([]*models.FavoriteRow, error)
	CountFavoritesByUserID(ctx context.Context, userID string) (int, error)
	CountFavoritesByUserIDs(ctx context.Context, userIDs []string) (int, error)
	GetFavoritesForStreamRefresh(ctx context.Context) ([]models.UserFavoritesRefresh, error)
	RemoveFavorite(ctx context.Context, torrentKey, userID string) (bool, error)
	RemoveFavoriteForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (bool, error)
	IsFavorite(ctx context.Context, torrentKey, userID string) (bool, error)
	IsFavoriteForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (bool, error)
	GetFavoriteByKey(ctx context.Context, torrentKey, userID string) (*models.FavoriteRow, error)
	GetFavoriteByKeyForUserIDs(ctx context.Context, torrentKey string, userIDs []string) (*models.FavoriteRow, error)
	UpdateFavoriteEntryCoverImage(ctx context.Context, entryID, coverImageURL string) (bool, error)
	UpdateFavoriteEntryMagnetLink(ctx context.Context, entryID, magnetLink string) (bool, error)
	StoreFavoriteEntry(ctx context.Context, entryID string, data map[string]interface{}) error
	GetFavoriteEntryByID(ctx context.Context, entryID string) (interface{}, error)
	StoreFavoriteDetails(ctx context.Context, favoriteID string, details interface{}) error
	GetFavoriteDetails(ctx context.Context, favoriteID string) (interface{}, error)

	SetCoverImage(ctx context.Context, torrentKey, imageURL string) error
	SetCoverImageWithStorageKey(ctx context.Context, torrentKey, imageURL, originalURL, storageKey string) error
	SetCoverImageMeta(ctx context.Context, torrentKey, imageURL, originalURL, storageKey, source, description, metaID string) error
	GetCoverImageByKey(ctx context.Context, torrentKey string) (*models.ImageRow, error)
	DeleteCoverImage(ctx context.Context, torrentKey string) (bool, error)
	GetCoverImagesByKeys(ctx context.Context, torrentKeys []string) (map[string]*models.ImageRow, error)
	GetObjectStorageCovers(ctx context.Context, limit, offset int) ([]models.ObjectStorageCover, error)
	UpdateCoverPresignedURL(ctx context.Context, torrentKey, presignedURL string) (bool, error)
	DeleteCoverByStorageKey(ctx context.Context, storageKey string) (bool, error)
	UpdateTorrentDetailsCoverImage(ctx context.Context, favoriteID, source, coverImageURL string) (bool, error)
	UpdateCachedLinkCoverImage(ctx context.Context, cachedLinkID, coverImageURL string) (bool, error)
	GetFallbackUrlsByPixhostUrl(pixhostUrl string) ([]string, error)

	SetStreamURL(ctx context.Context, in models.StreamURLInput) error
	GetStreamURLByHash(ctx context.Context, magnetHash string) (*models.StreamURLRow, error)
	GetStreamURLByMagnet(ctx context.Context, magnetLink string) (*models.StreamURLRow, error)

	AddCachedLink(ctx context.Context, id, userID, linkType, originalURL, title string) error
	GetCachedLinks(ctx context.Context, page, limit int, userID string) ([]*models.CachedLinkRow, int, error)
	GetCachedLinkByID(ctx context.Context, id string) (*models.CachedLinkRow, error)
	UpdateCachedLink(ctx context.Context, id, userID string, updates map[string]interface{}) (bool, error)
	RemoveCachedLink(ctx context.Context, id, userID string) (bool, error)

	RecordSearchQuery(ctx context.Context, query string) error
	GetRecentSearchQueries(ctx context.Context, since time.Time) ([]string, error)
	CleanupOldSearchQueries(ctx context.Context, before time.Time) (int64, error)

	KVSet(ctx context.Context, key, value string, ttlSeconds *int64) error
	KVGet(ctx context.Context, key string) (string, bool, error)
	KVDelete(ctx context.Context, key string) error

	SetSharedMeta(ctx context.Context, source, metaID string, p models.SharedMetaPayload) error
	GetSharedMetaPair(ctx context.Context, metaID string) (*models.SharedMetaPayload, *models.SharedMetaPayload, error)
	ExistsSharedMany(ctx context.Context, source string, metaIDs []string) ([]bool, error)

	SetSukebeiCatalog(ctx context.Context, catalogID string, entriesJSON []byte) error
	GetSukebeiCatalog(ctx context.Context, catalogID string) ([]byte, bool, error)

	GetTableStats(ctx context.Context) (*models.DBTableStats, error)
	GetFavoriteStats(ctx context.Context) (map[string]interface{}, error)

	AddBlockedIP(ctx context.Context, ip, reason, notes string, requestCount int64) error
	RemoveBlockedIP(ctx context.Context, ip string) error
	GetBlockedIPs(ctx context.Context) ([]*models.BlockedIP, error)
	IsIPBlocked(ctx context.Context, ip string) (bool, error)
}
