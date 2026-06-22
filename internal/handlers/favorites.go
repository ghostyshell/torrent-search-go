package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"torrent-search-go/internal/middleware"
	"torrent-search-go/pkg/models"
)

// FavoritesHandler handles favorites endpoints
type FavoritesHandler struct {
	storage *StorageProvider
}

// resolveFavoriteUserIDs returns user ids that may own favorites (uuid, google_id, legacy null).
func (h *FavoritesHandler) resolveFavoriteUserIDs(ctx context.Context, userID string) []string {
	ids := []string{userID, ""}
	if row, err := h.storage.GetUserByID(ctx, userID); err == nil && row != nil && row.GoogleID != nil && *row.GoogleID != "" {
		ids = append(ids, *row.GoogleID)
	}
	return ids
}

// NewFavoritesHandler creates a new favorites handler
func NewFavoritesHandler(storage *StorageProvider) *FavoritesHandler {
	return &FavoritesHandler{storage: storage}
}

// AddFavorite adds a torrent to the user's favorites
// POST body: { torrent: { Name, Website, ... }, coverImageUrl?: string }
func (h *FavoritesHandler) AddFavorite(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	if mwUser == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}

	var req struct {
		Torrent       map[string]interface{} `json:"torrent"`
		CoverImageURL string                 `json:"coverImageUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Torrent == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: torrent"})
		return
	}

	torrentName := mapStrField(req.Torrent, "Name", "name")
	magnetLink := mapStrField(req.Torrent, "Magnet", "magnetLink", "magnet_link")
	torrentKey := torrentKeyFromMap(req.Torrent)

	// Store the full torrent JSON in torrent_data (matches the JS backend).
	torrentDataBytes, _ := json.Marshal(req.Torrent)

	ctx := r.Context()
	favoriteID := generateTokenID()

	if err := h.storage.AddFavorite(ctx, favoriteID, mwUser.UserID, torrentKey, torrentName, string(torrentDataBytes), req.CoverImageURL, magnetLink); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to add favorite"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Favorite added successfully"})
}

// GetFavorites returns the user's favorites with pagination
func (h *FavoritesHandler) GetFavorites(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	if mwUser == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}

	page, limit := parsePagination(r, 1, 20)
	offset := (page - 1) * limit

	ctx := r.Context()
	favoriteUserIDs := h.resolveFavoriteUserIDs(ctx, mwUser.UserID)
	favorites, err := h.storage.GetFavoritesByUserIDs(ctx, favoriteUserIDs, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get favorites"})
		return
	}
	totalCount, _ := h.storage.CountFavoritesByUserIDs(ctx, favoriteUserIDs)

	// Batch-load all cover images for this page in a single query to avoid the
	// N+1 pattern (previously one query per favorite).
	torrentKeys := make([]string, 0, len(favorites))
	for _, fav := range favorites {
		torrentKeys = append(torrentKeys, fav.TorrentKey)
	}
	coverImages, err := h.storage.GetCoverImagesByKeys(ctx, torrentKeys)
	if err != nil {
		coverImages = map[string]*models.ImageRow{}
	}

	// Enrich with cover images and merge stored torrent_data (matches Node getMergedFavorites).
	enriched := make([]map[string]interface{}, 0, len(favorites))
	for _, fav := range favorites {
		item := mergeFavoriteItem(fav, coverImages)
		enriched = append(enriched, item)
	}

	totalPages := 0
	if totalCount > 0 && limit > 0 {
		totalPages = (totalCount + limit - 1) / limit
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"favorites": enriched,
		"pagination": map[string]interface{}{
			"currentPage": page,
			"totalPages":  totalPages,
			"totalCount":  totalCount,
			"limit":       limit,
			"hasNextPage": page < totalPages,
			"hasPrevPage": page > 1,
		},
	})
}

// RemoveFavorite removes a torrent from the user's favorites
func (h *FavoritesHandler) RemoveFavorite(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	if mwUser == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}

	var req struct {
		Torrent    map[string]interface{} `json:"torrent"`
		TorrentKey string                 `json:"torrentKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}

	torrentKey := req.TorrentKey
	if torrentKey == "" && req.Torrent != nil {
		torrentKey = torrentKeyFromMap(req.Torrent)
	}
	if torrentKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: torrent"})
		return
	}

	removed, err := h.storage.RemoveFavoriteForUserIDs(r.Context(), torrentKey, h.resolveFavoriteUserIDs(r.Context(), mwUser.UserID))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to remove favorite"})
		return
	}
	if !removed {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Favorite not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Favorite removed successfully"})
}

// GetFavoriteDetails returns stored details for a favorite
func (h *FavoritesHandler) GetFavoriteDetails(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/favorites/:favoriteId/details", r.URL.Path)
	favoriteID := params.Get("favoriteId")
	if favoriteID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing favoriteId parameter"})
		return
	}
	details, err := h.storage.GetFavoriteDetails(r.Context(), favoriteID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get favorite details"})
		return
	}
	if details == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Favorite details not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "details": details})
}

// StoreFavoriteDetails stores details for a favorite
func (h *FavoritesHandler) StoreFavoriteDetails(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/favorites/:favoriteId/details", r.URL.Path)
	favoriteID := params.Get("favoriteId")

	var req struct {
		Details interface{} `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || favoriteID == "" || req.Details == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: favoriteId and details"})
		return
	}
	if err := h.storage.StoreFavoriteDetails(r.Context(), favoriteID, req.Details); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store favorite details"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Favorite details stored successfully"})
}

// CheckFavorite checks if a torrent is in the user's favorites
func (h *FavoritesHandler) CheckFavorite(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	if mwUser == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}
	var req struct {
		Torrent    map[string]interface{} `json:"torrent"`
		TorrentKey string                 `json:"torrentKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}

	torrentKey := req.TorrentKey
	if torrentKey == "" && req.Torrent != nil {
		torrentKey = torrentKeyFromMap(req.Torrent)
	}
	if torrentKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: torrent"})
		return
	}

	ctx := r.Context()
	favoriteUserIDs := h.resolveFavoriteUserIDs(ctx, mwUser.UserID)
	isFav, err := h.storage.IsFavoriteForUserIDs(ctx, torrentKey, favoriteUserIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to check favorite status"})
		return
	}

	resp := map[string]interface{}{
		"success":    true,
		"isFavorite": isFav,
	}
	if isFav {
		if entry, err := h.storage.GetFavoriteByKeyForUserIDs(ctx, torrentKey, favoriteUserIDs); err == nil && entry != nil {
			resp["favoriteEntry"] = favoriteEntryToMap(entry)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// StoreFavoriteEntry stores entry data for a favorite
func (h *FavoritesHandler) StoreFavoriteEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FavoriteID string                 `json:"favoriteId"`
		EntryData  map[string]interface{} `json:"entryData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FavoriteID == "" || req.EntryData == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: favoriteId and entryData"})
		return
	}
	if err := h.storage.StoreFavoriteEntry(r.Context(), req.FavoriteID, req.EntryData); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store favorite entry"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Favorite entry stored successfully"})
}

// ─── Cached links ─────────────────────────────────────────────────────────────

// GetCachedLinks returns stored links
func (h *FavoritesHandler) GetCachedLinks(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	userID := ""
	if mwUser != nil {
		userID = mwUser.UserID
	}

	page, limit := parsePagination(r, 1, 20)
	links, total, err := h.storage.GetCachedLinks(r.Context(), page, limit, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get stored links"})
		return
	}
	if links == nil {
		links = []*models.CachedLinkRow{}
	}

	totalPages := 0
	if total > 0 && limit > 0 {
		totalPages = (total + limit - 1) / limit
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"links":        links,
		"storedLinks":  links,
		"pagination": map[string]interface{}{
			"currentPage": page,
			"totalPages":  totalPages,
			"totalCount":  total,
			"limit":       limit,
		},
	})
}

// AddCachedLink adds a new stored link
func (h *FavoritesHandler) AddCachedLink(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	userID := ""
	if mwUser != nil {
		userID = mwUser.UserID
	}

	var req struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: url"})
		return
	}
	if req.Title == "" {
		req.Title = req.URL
	}

	id := generateTokenID()
	if err := h.storage.AddCachedLink(r.Context(), id, userID, "url", req.URL, req.Title); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store link"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Link stored successfully",
		"storedLink": map[string]interface{}{
			"id":    id,
			"url":   req.URL,
			"title": req.Title,
		},
	})
}

// UpdateCachedLink updates a stored link
func (h *FavoritesHandler) UpdateCachedLink(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	userID := ""
	if mwUser != nil {
		userID = mwUser.UserID
	}

	// Try both route patterns
	linkID := extractIDParam(r.URL.Path, "/api/storage/stored-links/", "/api/cache/cached-links/")
	if linkID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing link ID"})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}

	updated, err := h.storage.UpdateCachedLink(r.Context(), linkID, userID, updates)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to update link"})
		return
	}
	if !updated {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Link not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Link updated successfully"})
}

// RemoveCachedLink removes a stored link
func (h *FavoritesHandler) RemoveCachedLink(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	userID := ""
	if mwUser != nil {
		userID = mwUser.UserID
	}

	linkID := extractIDParam(r.URL.Path, "/api/storage/stored-links/", "/api/cache/cached-links/")
	if linkID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing link ID"})
		return
	}

	removed, err := h.storage.RemoveCachedLink(r.Context(), linkID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to remove link"})
		return
	}
	if !removed {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Link not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Link removed successfully"})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func torrentSourceFromMap(m map[string]interface{}) string {
	if s := mapStrField(m, "Source", "source"); s != "" {
		return s
	}
	return mapStrField(m, "Website", "website")
}

func torrentKeyFromMap(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	if k := mapStrField(m, "torrentKey"); k != "" {
		return k
	}
	if isTruthy(m["isCachedLink"]) {
		if id := mapStrField(m, "cachedLinkId"); id != "" {
			return "cached_link_" + id
		}
	}
	name := mapStrField(m, "Name", "name")
	source := torrentSourceFromMap(m)
	size := mapStrField(m, "Size", "size")
	return generateTorrentKey(name, source, size)
}

func isTruthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	default:
		return false
	}
}

func mergeFavoriteItem(fav *models.FavoriteRow, coverImages map[string]*models.ImageRow) map[string]interface{} {
	item := map[string]interface{}{}
	if fav.TorrentData != "" {
		var torrentData map[string]interface{}
		if err := json.Unmarshal([]byte(fav.TorrentData), &torrentData); err == nil {
			for k, v := range torrentData {
				item[k] = v
			}
		}
	}

	item["favoriteEntryId"] = fav.ID
	if fav.CoverImageURL != "" {
		item["favoriteEntryCoverImageUrl"] = fav.CoverImageURL
	}
	item["dateAdded"] = time.Unix(fav.CreatedAt, 0).UTC().Format(time.RFC3339)

	if fav.MagnetLink != "" {
		if _, ok := item["Magnet"]; !ok || item["Magnet"] == "" {
			item["Magnet"] = fav.MagnetLink
		}
	}

	if imgRow := coverImages[fav.TorrentKey]; imgRow != nil {
		item["coverImage"] = map[string]interface{}{
			"type": imgRow.ImageType,
			"url":  imgRow.PixhostURL,
		}
	}

	return item
}

func favoriteEntryToMap(fav *models.FavoriteRow) map[string]interface{} {
	entry := map[string]interface{}{
		"id":          fav.ID,
		"torrentKey":  fav.TorrentKey,
		"torrentName": fav.TorrentName,
		"magnetLink":  fav.MagnetLink,
		"createdAt":   fav.CreatedAt,
		"updatedAt":   fav.UpdatedAt,
	}
	if fav.CoverImageURL != "" {
		entry["coverImageUrl"] = fav.CoverImageURL
	}
	if fav.TorrentData != "" {
		var torrentData map[string]interface{}
		if err := json.Unmarshal([]byte(fav.TorrentData), &torrentData); err == nil {
			entry["torrentData"] = torrentData
		}
	}
	return entry
}

// mapStrField extracts a string from a map by trying multiple keys
func mapStrField(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// parsePagination parses page/limit from query params with defaults
func parsePagination(r *http.Request, defaultPage, defaultLimit int) (int, int) {
	page := defaultPage
	limit := defaultLimit
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	return page, limit
}

// extractIDParam extracts the last path segment after any of the given prefixes
func extractIDParam(path string, prefixes ...string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			id := strings.TrimPrefix(path, prefix)
			// Remove any trailing slash or query string
			if idx := strings.IndexAny(id, "?/"); idx != -1 {
				id = id[:idx]
			}
			if id != "" {
				return id
			}
		}
	}
	return ""
}
