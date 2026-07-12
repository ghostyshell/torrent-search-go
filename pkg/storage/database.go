package storage

import (
	"context"
	"errors"
	"time"

	"torrent-search-go/pkg/models"
)

// ErrAddonStatusNotFound is returned by DeleteAddonStatusReport when no document matched.
var ErrAddonStatusNotFound = errors.New("addon status report not found")

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
	UpdateFavoriteEntryCoverImage(ctx context.Context, entryID, userID, coverImageURL string) (bool, error)
	UpdateFavoriteEntryMagnetLink(ctx context.Context, entryID, userID, magnetLink string) (bool, error)
	StoreFavoriteEntry(ctx context.Context, entryID string, data map[string]interface{}) error
	GetFavoriteEntryByID(ctx context.Context, entryID string) (interface{}, error)
	StoreFavoriteDetails(ctx context.Context, favoriteID string, details interface{}) error
	GetFavoriteDetails(ctx context.Context, favoriteID string) (interface{}, error)

	SetCoverImage(ctx context.Context, torrentKey, imageURL string) error
	SetCoverImageWithStorageKey(ctx context.Context, torrentKey, imageURL, originalURL, storageKey string) error
	SetCoverImageMeta(ctx context.Context, torrentKey, imageURL, originalURL, storageKey, source, description, metaID string) error
	SetManualCover(ctx context.Context, torrentKey, imageURL, originalURL, storageKey string) error
	UpsertTpdbCover(ctx context.Context, torrentKey, imageURL, originalURL, storageKey, source, description, metaID string) error
	UpsertDetailsCover(ctx context.Context, torrentKey, imageURL, storageKey string) error
	GetCoverImageByKey(ctx context.Context, torrentKey string) (*models.ImageRow, error)
	DeleteCoverImage(ctx context.Context, torrentKey string) (bool, error)
	GetCoverImagesByKeys(ctx context.Context, torrentKeys []string) (map[string]*models.ImageRow, error)
	GetObjectStorageCovers(ctx context.Context, limit, offset int) ([]models.ObjectStorageCover, error)
	GetCoverImagesMissingStorageKey(ctx context.Context, limit int, afterKey string) ([]*models.ImageRow, error)
	UpdateCoverPresignedURL(ctx context.Context, torrentKey, presignedURL string) (bool, error)
	UpdateCoverStorageKey(ctx context.Context, torrentKey, storageKey, presignedURL string) (bool, error)
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

	UpsertPornripsEntry(ctx context.Context, entry models.PornripsEntry) error
	UpdatePornripsEnrichment(ctx context.Context, slug, poster, resolvedTitle string, tags, genres, performers []string) error
	GetPornripsRecent(ctx context.Context, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsByTag(ctx context.Context, tags []string, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsEntryBySlug(ctx context.Context, slug string) (*models.PornripsEntry, error)
	GetPornripsEntriesByPerformer(ctx context.Context, performer string, limit int) ([]models.PornripsEntry, error)
	GetPornripsEntriesByPerformers(ctx context.Context, performers []string, limit int) ([]models.PornripsEntry, error)
	PerformersWithTorrent(ctx context.Context, performers []string) (map[string]bool, error)
	SearchPornrips(ctx context.Context, query string, skip, limit int) ([]models.PornripsGroup, error)
	GetPornripsEntriesMissingEnrichment(ctx context.Context, limit int) ([]models.PornripsEntry, error)
	GetPornripsEntriesForReEnrichment(ctx context.Context, since string, enrichedBefore int64, limit int) ([]models.PornripsEntry, error)
	GetPornripsEntriesMissingTorrent(ctx context.Context, limit int) ([]models.PornripsEntry, error)
	SetPornripsTorrent(ctx context.Context, slug, infoHash, torrentURL string) error
	PornripsEntriesCount(ctx context.Context) (int64, error)

	UpsertEnrichedScene(ctx context.Context, scene models.EnrichedScene) error
	GetEnrichedScenesByMatchedSources(ctx context.Context, source string, tags []string, sources []string, skip, limit int) ([]models.EnrichedScene, error)
	GetEnrichedScenesByMatchedSourcesAndIDs(ctx context.Context, source string, ids, sources []string, limit int) ([]models.EnrichedScene, error)
	GetEnrichedSceneByID(ctx context.Context, id string) (*models.EnrichedScene, error)
	GetEnrichedScenesMissingSourceMatch(ctx context.Context, source string, limit int) ([]models.EnrichedScene, error)
	EnrichedScenesCount(ctx context.Context) (int64, error)

	UpsertHentaiEntry(ctx context.Context, entry models.HentaiEntry) error
	GetHentaiRecent(ctx context.Context, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiTop(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiAll(ctx context.Context, genreNorm string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiByYear(ctx context.Context, year string, skip, limit int) ([]models.HentaiEntry, error)
	SearchHentai(ctx context.Context, query string, skip, limit int) ([]models.HentaiEntry, error)
	GetHentaiEntry(ctx context.Context, id string) (*models.HentaiEntry, error)
	HentaiEntriesCount(ctx context.Context) (int64, error)
	GetHentaiTopStudios(ctx context.Context, limit int) ([]string, error)
	GetHentaiTopGenres(ctx context.Context, limit int) ([]string, error)
	GetHentaiYears(ctx context.Context) ([]string, error)

	UpsertPerverzijaEntry(ctx context.Context, entry models.PerverzijaEntry) error
	UpdatePerverzijaEnrichment(ctx context.Context, entry models.PerverzijaEntry) error
	GetPerverzijaRecent(ctx context.Context, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PerverzijaEntry, error)
	SearchPerverzija(ctx context.Context, query string, skip, limit int) ([]models.PerverzijaEntry, error)
	GetPerverzijaEntry(ctx context.Context, slug string) (*models.PerverzijaEntry, error)
	GetPerverzijaMissingDetail(ctx context.Context, limit int) ([]models.PerverzijaEntry, error)
	PerverzijaEntriesCount(ctx context.Context) (int64, error)
	GetPerverzijaTopStudios(ctx context.Context, limit int) ([]string, error)
	GetPerverzijaTopTags(ctx context.Context, limit int) ([]string, error)
	GetPerverzijaTopPerformers(ctx context.Context, limit int) ([]string, error)

	UpsertFreepornvideosEntry(ctx context.Context, entry models.FreepornvideosEntry) error
	UpdateFreepornvideosEnrichment(ctx context.Context, entry models.FreepornvideosEntry) error
	GetFreepornvideosRecent(ctx context.Context, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.FreepornvideosEntry, error)
	SearchFreepornvideos(ctx context.Context, query string, skip, limit int) ([]models.FreepornvideosEntry, error)
	GetFreepornvideosEntry(ctx context.Context, videoID string) (*models.FreepornvideosEntry, error)
	GetFreepornvideosMissingDetail(ctx context.Context, limit int) ([]models.FreepornvideosEntry, error)
	FreepornvideosEntriesCount(ctx context.Context) (int64, error)
	GetFreepornvideosTopStudios(ctx context.Context, limit int) ([]string, error)
	GetFreepornvideosTopTags(ctx context.Context, limit int) ([]string, error)
	GetFreepornvideosTopPerformers(ctx context.Context, limit int) ([]string, error)

	UpsertYespornEntry(ctx context.Context, entry models.YespornEntry) error
	UpsertYespornEntries(ctx context.Context, entries []models.YespornEntry) (int, error)
	UpdateYespornEnrichment(ctx context.Context, entry models.YespornEntry) error
	GetYespornRecent(ctx context.Context, skip, limit int) ([]models.YespornEntry, error)
	GetYespornByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.YespornEntry, error)
	GetYespornByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.YespornEntry, error)
	GetYespornByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.YespornEntry, error)
	SearchYesporn(ctx context.Context, query string, skip, limit int) ([]models.YespornEntry, error)
	GetYespornEntry(ctx context.Context, videoID string) (*models.YespornEntry, error)
	GetYespornMissingDetail(ctx context.Context, limit int) ([]models.YespornEntry, error)
	YespornEntriesCount(ctx context.Context) (int64, error)
	GetYespornTopStudios(ctx context.Context, limit int) ([]string, error)
	GetYespornTopTags(ctx context.Context, limit int) ([]string, error)
	GetYespornTopPerformers(ctx context.Context, limit int) ([]string, error)

	UpsertWatchpornEntry(ctx context.Context, entry models.WatchpornEntry) error
	UpsertWatchpornEntries(ctx context.Context, entries []models.WatchpornEntry) (int, error)
	UpdateWatchpornEnrichment(ctx context.Context, entry models.WatchpornEntry) error
	GetWatchpornRecent(ctx context.Context, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.WatchpornEntry, error)
	SearchWatchporn(ctx context.Context, query string, skip, limit int) ([]models.WatchpornEntry, error)
	GetWatchpornEntry(ctx context.Context, videoID string) (*models.WatchpornEntry, error)
	GetWatchpornMissingDetail(ctx context.Context, limit int) ([]models.WatchpornEntry, error)
	WatchpornEntriesCount(ctx context.Context) (int64, error)
	GetWatchpornTopStudios(ctx context.Context, limit int) ([]string, error)
	GetWatchpornTopTags(ctx context.Context, limit int) ([]string, error)
	GetWatchpornTopPerformers(ctx context.Context, limit int) ([]string, error)

	UpsertPorneecEntry(ctx context.Context, entry models.PorneecEntry) error
	UpdatePorneecEnrichment(ctx context.Context, entry models.PorneecEntry) error
	GetPorneecRecent(ctx context.Context, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecByStudio(ctx context.Context, studioNorm string, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecByTag(ctx context.Context, tagsNorm []string, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecByPerformer(ctx context.Context, performerNorm string, skip, limit int) ([]models.PorneecEntry, error)
	SearchPorneec(ctx context.Context, query string, skip, limit int) ([]models.PorneecEntry, error)
	GetPorneecEntry(ctx context.Context, slug string) (*models.PorneecEntry, error)
	GetPorneecMissingDetail(ctx context.Context, limit int) ([]models.PorneecEntry, error)
	PorneecEntriesCount(ctx context.Context) (int64, error)
	GetPorneecTopStudios(ctx context.Context, limit int) ([]string, error)
	GetPorneecTopTags(ctx context.Context, limit int) ([]string, error)
	GetPorneecTopPerformers(ctx context.Context, limit int) ([]string, error)

	GetTableStats(ctx context.Context) (*models.DBTableStats, error)
	GetFavoriteStats(ctx context.Context) (map[string]interface{}, error)

	AddBlockedIP(ctx context.Context, ip, reason, notes string, requestCount int64) error
	RemoveBlockedIP(ctx context.Context, ip string) error
	GetBlockedIPs(ctx context.Context) ([]*models.BlockedIP, error)
	IsIPBlocked(ctx context.Context, ip string) (bool, error)

	ListAddonStatusReports(ctx context.Context) ([]models.AddonStatusReport, error)
	GetAddonStatusReport(ctx context.Context, id string) (*models.AddonStatusReport, error)
	UpsertAddonStatusReport(ctx context.Context, r models.AddonStatusReport) error
	DeleteAddonStatusReport(ctx context.Context, id string) error
}
