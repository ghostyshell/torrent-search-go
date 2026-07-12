package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/middleware"
	"torrent-search-go/internal/services/realdebrid"
	"torrent-search-go/pkg/models"
)

// CacheHandler handles cache/storage endpoints
type CacheHandler struct {
	storage *StorageProvider
	cfg     *config.Config
}

// NewCacheHandler creates a new cache handler
func NewCacheHandler(storage *StorageProvider, cfg *config.Config) *CacheHandler {
	return &CacheHandler{storage: storage, cfg: cfg}
}

// GetStats returns cache statistics
func (h *CacheHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbStats, _ := h.storage.GetTableStats(ctx)
	connStats := h.storage.GetStats()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"stats":     dbStats,
		"db":        connStats,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// StoreCoverImage stores a cover image for a torrent
// POST body: { torrent: { Name, Website } | torrentKey: string, imageUrl: string }
func (h *CacheHandler) StoreCoverImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Torrent    map[string]interface{} `json:"torrent"`
		TorrentKey string                 `json:"torrentKey"`
		ImageData  string                 `json:"imageData"`
		MimeType   string                 `json:"mimeType"`
		ImageURL   string                 `json:"imageUrl"`
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
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: torrent or torrentKey"})
		return
	}

	imageURL := req.ImageURL
	if imageURL == "" && req.ImageData != "" {
		imageURL = req.ImageData
	}
	if imageURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Either imageData or imageUrl is required"})
		return
	}

	if err := h.storage.SetManualCover(r.Context(), torrentKey, imageURL); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store cover image"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Cover image stored successfully"})
}

// GetCoverImage retrieves a cover image by torrent key
func (h *CacheHandler) GetCoverImage(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/cache/cover-image/:torrentKey", r.URL.Path)
	torrentKey := params.Get("torrentKey")
	if torrentKey == "" {
		// Try storage route
		params = middleware.ExtractParams("/api/storage/cover-image/:torrentKey", r.URL.Path)
		torrentKey = params.Get("torrentKey")
	}
	if torrentKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing torrentKey"})
		return
	}

	imgRow, err := h.storage.GetCoverImageByKey(r.Context(), torrentKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get cover image"})
		return
	}
	if imgRow == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Cover image not found"})
		return
	}

	var fallbacks []string
	if imgRow.FallbackURLs != nil && *imgRow.FallbackURLs != "" {
		fallbacks, _ = parseFallbackURLs(*imgRow.FallbackURLs)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"imageUrl":     imgRow.PixhostURL,
		"type":         "url",
		"originalUrl":  derefStr(imgRow.OriginalURL),
		"fallbackUrls": fallbacks,
	})
}

// GetCoverImageForTorrent retrieves a cover image for a torrent object
// POST body: { Name, Source, Size } or torrent object
func (h *CacheHandler) GetCoverImageForTorrent(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}

	torrentKey := torrentKeyFromMap(req)
	if torrentKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Cannot determine torrent key"})
		return
	}

	imgRow, err := h.storage.GetCoverImageByKey(r.Context(), torrentKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get cover image"})
		return
	}

	metaID := metaIDFromTorrentMap(req)
	resolved := resolveCover(r.Context(), h.storage, coverResolveInput{row: imgRow, metaID: metaID})
	if resolved.url != "" {
		var fallbacks []string
		if imgRow != nil && imgRow.FallbackURLs != nil && *imgRow.FallbackURLs != "" {
			fallbacks, _ = parseFallbackURLs(*imgRow.FallbackURLs)
		}
		if resolved.upgraded {
			go h.storage.upgradeCoverFromMeta(context.Background(), torrentKey, resolved.url, resolved.source, metaID)
		}
		resp := map[string]interface{}{
			"success":      true,
			"imageUrl":     resolved.url,
			"type":         "url",
			"originalUrl":  derefStr(imgRowOriginalURL(imgRow)),
			"fallbackUrls": fallbacks,
		}
		if resolved.tpdbURL != "" {
			resp["tpdbUrl"] = resolved.tpdbURL
		}
		if resolved.detailsURL != "" {
			resp["detailsUrl"] = resolved.detailsURL
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if imgRow == nil {
		if coverURL := h.lookupFallbackCoverURL(r, req); coverURL != "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":  true,
				"imageUrl": coverURL,
				"type":     "url",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "found": false, "error": "Cover image not found"})
		return
	}

	var fallbacks []string
	if imgRow.FallbackURLs != nil && *imgRow.FallbackURLs != "" {
		fallbacks, _ = parseFallbackURLs(*imgRow.FallbackURLs)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"imageUrl":     imgRow.PixhostURL,
		"type":         "url",
		"originalUrl":  derefStr(imgRow.OriginalURL),
		"fallbackUrls": fallbacks,
	})
}

// DeleteCoverImage removes a stored cover image for a torrent.
// DELETE body: { torrent }
func (h *CacheHandler) DeleteCoverImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Torrent map[string]interface{} `json:"torrent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}
	if req.Torrent == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: torrent"})
		return
	}

	torrentKey := torrentKeyFromMap(req.Torrent)
	if torrentKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Cannot determine torrent key"})
		return
	}

	deleted, err := h.storage.DeleteCoverImage(r.Context(), torrentKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to remove cover image"})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Cover image not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Cover image removed successfully",
	})
}

// isPresignedS3URL returns true for Amazon S3 presigned URLs, which have a
// limited TTL and may be expired by the time a favorite cover is read back.
func isPresignedS3URL(u string) bool {
	return strings.Contains(u, "X-Amz-Signature") || strings.Contains(u, "x-amz-signature")
}

func (h *CacheHandler) lookupFallbackCoverURL(r *http.Request, req map[string]interface{}) string {
	ctx := r.Context()

	if favoriteID := mapStrField(req, "favoriteEntryId"); favoriteID != "" {
		if doc, err := h.storage.GetFavoriteEntryByID(ctx, favoriteID); err == nil && doc != nil {
			if m, ok := doc.(map[string]interface{}); ok {
				if url := mapStrField(m, "cover_image_url", "coverImageUrl"); url != "" && !isPresignedS3URL(url) {
					return url
				}
			}
		}
	}

	if coverURL := mapStrField(req, "favoriteEntryCoverImageUrl"); coverURL != "" && !isPresignedS3URL(coverURL) {
		return coverURL
	}

	if isTruthy(req["isCachedLink"]) {
		if linkID := mapStrField(req, "cachedLinkId"); linkID != "" {
			if link, err := h.storage.GetCachedLinkByID(ctx, linkID); err == nil && link != nil && link.CoverImageURL != nil && *link.CoverImageURL != "" {
				return *link.CoverImageURL
			}
		}
	}

	return ""
}

// StoreStreamURL stores a stream URL for a magnet link
// POST body: { magnetLink: string, streamData: { streamUrl: string, expiresIn?: int } }
func (h *CacheHandler) StoreStreamURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MagnetLink string                 `json:"magnetLink"`
		StreamData map[string]interface{} `json:"streamData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}

	streamURL, _ := req.StreamData["streamUrl"].(string)
	if req.MagnetLink == "" || streamURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: magnetLink, streamData.streamUrl"})
		return
	}

	// Extract magnet hash
	magnetHash := extractMagnetHashFromLink(req.MagnetLink)

	// Persist the full streamData (filename/filesize/range support/torrent name),
	// matching the JS storage.setStreamUrl behavior.
	filename, _ := req.StreamData["filename"].(string)
	torrentName, _ := req.StreamData["torrentName"].(string)
	supportsRange, _ := req.StreamData["supportsRangeRequests"].(bool)
	var filesize int64
	if v, ok := req.StreamData["filesize"].(float64); ok {
		filesize = int64(v)
	}

	if err := h.storage.SetStreamURL(r.Context(), models.StreamURLInput{
		MagnetHash:            magnetHash,
		MagnetLink:            req.MagnetLink,
		StreamURL:             streamURL,
		Filename:              filename,
		Filesize:              filesize,
		SupportsRangeRequests: supportsRange,
		TorrentName:           torrentName,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store stream URL"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Stream URL stored successfully"})
}

// GetStreamURL retrieves a stream URL by magnet hash
func (h *CacheHandler) GetStreamURL(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/cache/stream-url/:magnetHash", r.URL.Path)
	magnetHash := params.Get("magnetHash")
	if magnetHash == "" {
		params = middleware.ExtractParams("/api/storage/stream-url/:magnetHash", r.URL.Path)
		magnetHash = params.Get("magnetHash")
	}
	if magnetHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing magnetHash"})
		return
	}

	row, err := h.storage.GetStreamURLByHash(r.Context(), magnetHash)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get stream URL"})
		return
	}
	if row == nil {
		// Not an error: a favorite may simply have no cached stream URL yet.
		// Return 200 so per-card cache-status checks don't log noisy 404s.
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "found": false, "error": "Stream URL not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":               true,
		"streamUrl":             row.StreamURL,
		"filename":              row.Filename,
		"filesize":              row.Filesize,
		"supportsRangeRequests": row.SupportsRangeRequests,
		"cachedAt":              row.CreatedAt * 1000,
		"lastAccessed":          row.LastAccessedAt * 1000,
	})
}

// RefreshStreamURL refreshes a stream URL for a magnet link using the user's stored RD key.
// POST body: { magnetLink: string, torrentName?: string }
func (h *CacheHandler) RefreshStreamURL(w http.ResponseWriter, r *http.Request) {
	mwUser := middleware.GetUser(r)
	if mwUser == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Authentication required",
		})
		return
	}

	var req struct {
		MagnetLink  string `json:"magnetLink"`
		TorrentName string `json:"torrentName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !strings.HasPrefix(req.MagnetLink, "magnet:") {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing or invalid magnetLink"})
		return
	}

	apiKey, err := h.storage.GetRealDebridKey(r.Context(), mwUser.UserID)
	if err != nil || apiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Real Debrid API key not configured. Please add it in your account settings.",
		})
		return
	}

	rd := realdebrid.NewClient()
	result, err := rd.RefreshStreamURL(r.Context(), apiKey, req.MagnetLink)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	hash := extractMagnetHashFromLink(req.MagnetLink)
	if err := h.storage.SetStreamURL(r.Context(), models.StreamURLInput{
		MagnetHash:            hash,
		MagnetLink:            req.MagnetLink,
		StreamURL:             result.StreamURL,
		Filename:              result.Filename,
		Filesize:              result.Filesize,
		SupportsRangeRequests: result.SupportsRangeRequests,
		TorrentName:           req.TorrentName,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to cache refreshed stream URL",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":               true,
		"streamUrl":             result.StreamURL,
		"filename":              result.Filename,
		"filesize":              result.Filesize,
		"supportsRangeRequests": result.SupportsRangeRequests,
		"magnetHash":            hash,
		"magnetLink":            req.MagnetLink,
	})
}

// StoreMagnetLink stores a magnet link in the KV cache.
// Node contract: POST { source, url, magnet, torrentName? }
// Legacy Go contract: POST { favoriteId, magnetLink }
func (h *CacheHandler) StoreMagnetLink(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid request body"})
		return
	}

	// Legacy favorite-entry update path.
	if favoriteID, _ := body["favoriteId"].(string); favoriteID != "" {
		magnetLink, _ := body["magnetLink"].(string)
		if magnetLink == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: favoriteId and magnetLink"})
			return
		}
		updated, err := h.storage.UpdateFavoriteEntryMagnetLink(r.Context(), favoriteID, authUserID(r), magnetLink)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store magnet link"})
			return
		}
		if !updated {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Favorite entry not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Magnet link stored successfully"})
		return
	}

	source, _ := body["source"].(string)
	url, _ := body["url"].(string)
	magnet, _ := body["magnet"].(string)
	torrentName, _ := body["torrentName"].(string)
	if source == "" || url == "" || magnet == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: source, url, magnet"})
		return
	}

	magnetData := map[string]interface{}{
		"source": source,
		"url":    url,
		"magnet": magnet,
		"torrentName": func() string {
			if torrentName != "" {
				return torrentName
			}
			return "Unknown"
		}(),
		"cachedAt": time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(magnetData)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store magnet link"})
		return
	}
	if err := h.storage.KVSet(r.Context(), magnetCacheKey(source, url), string(payload), nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to store magnet link"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Magnet link stored successfully"})
}

// GetMagnetLink retrieves a cached magnet link.
// Node contract: GET ?source=&url= → { success, data: { magnet, ... } }
// Legacy Go contract: GET ?magnetHash= or ?magnetLink=
func (h *CacheHandler) GetMagnetLink(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	url := r.URL.Query().Get("url")
	if source != "" && url != "" {
		cached, found, err := h.storage.KVGet(r.Context(), magnetCacheKey(source, url))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to retrieve magnet link"})
			return
		}
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Magnet link not found in cache"})
			return
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(cached), &data); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to retrieve magnet link"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": data})
		return
	}

	magnetLink := r.URL.Query().Get("magnetLink")
	magnetHash := r.URL.Query().Get("magnetHash")
	if magnetHash == "" && magnetLink != "" {
		magnetHash = extractMagnetHashFromLink(magnetLink)
	}
	if magnetHash != "" {
		row, err := h.storage.GetStreamURLByHash(r.Context(), magnetHash)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get magnet link"})
			return
		}
		if row != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "magnetLink": row.MagnetLink})
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Magnet link not found"})
}

func magnetCacheKey(source, rawURL string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(rawURL))
	if len(encoded) > 100 {
		encoded = encoded[:100]
	}
	return "magnet:" + strings.ToLower(source) + ":" + encoded
}

// SetCacheValue sets a generic key-value cache entry
// POST body: { key: string, value: any, ttl?: int64 (seconds) }
func (h *CacheHandler) SetCacheValue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
		TTL   *int64          `json:"ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: key and value"})
		return
	}
	if len(req.Value) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: key and value"})
		return
	}
	// ponytail: hard cap value at 256 KiB. The global 2 MiB MaxBytesReader
	// bounds the whole body; this bounds a single KV entry so one user can't
	// bloat the shared cache collection indefinitely.
	const maxKVValueSize = 256 * 1024
	if len(req.Value) > maxKVValueSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{"success": false, "error": "Value too large"})
		return
	}

	storedValue := string(req.Value)

	if err := h.storage.KVSet(r.Context(), req.Key, storedValue, req.TTL); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to cache value"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Value cached successfully",
		"key":     req.Key,
	})
}

// GetCacheValue gets a generic key-value cache entry
func (h *CacheHandler) GetCacheValue(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/cache/get/:key", r.URL.Path)
	key := params.Get("key")
	if key == "" {
		params = middleware.ExtractParams("/api/storage/get/:key", r.URL.Path)
		key = params.Get("key")
	}
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing key"})
		return
	}

	value, found, err := h.storage.KVGet(r.Context(), key)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get cache value"})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Key not found in cache"})
		return
	}

	var parsed interface{} = value
	var asJSON interface{}
	if json.Unmarshal([]byte(value), &asJSON) == nil {
		parsed = asJSON
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "key": key, "value": parsed})
}

// DeleteCacheValue deletes a generic key-value cache entry
func (h *CacheHandler) DeleteCacheValue(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/cache/delete/:key", r.URL.Path)
	key := params.Get("key")
	if key == "" {
		params = middleware.ExtractParams("/api/storage/delete/:key", r.URL.Path)
		key = params.Get("key")
	}
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing key"})
		return
	}

	if err := h.storage.KVDelete(r.Context(), key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to delete cache value"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Cache value deleted"})
}

// UpdateFavoriteEntryCoverImage updates the cover image URL on a favorite entry.
// PUT body: { coverImageUrl: string }
func (h *CacheHandler) UpdateFavoriteEntryCoverImage(w http.ResponseWriter, r *http.Request) {
	favoriteID := coverImagePathParam(r.URL.Path, "favorite")
	if favoriteID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: favoriteId and coverImageUrl"})
		return
	}
	coverImageURL, ok := decodeCoverImageURL(w, r)
	if !ok {
		return
	}
	userID := authUserID(r)
	updated, err := h.storage.UpdateFavoriteEntryCoverImage(r.Context(), favoriteID, userID, coverImageURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to update favorite entry cover image"})
		return
	}
	if !updated {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Favorite entry not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Favorite entry cover image updated successfully"})
}

// UpdateCachedLinkCoverImage updates the cover image URL on a cached link.
// PUT body: { coverImageUrl: string }
func (h *CacheHandler) UpdateCachedLinkCoverImage(w http.ResponseWriter, r *http.Request) {
	cachedLinkID := coverImagePathParam(r.URL.Path, "cached-link")
	if cachedLinkID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: cachedLinkId and coverImageUrl"})
		return
	}
	coverImageURL, ok := decodeCoverImageURL(w, r)
	if !ok {
		return
	}
	updated, err := h.storage.UpdateCachedLinkCoverImage(r.Context(), cachedLinkID, coverImageURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to update cached link cover image"})
		return
	}
	if !updated {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Cached link not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Cached link cover image updated successfully"})
}

// UpdateTorrentDetailsCoverImage updates the cover image URL on stored torrent details.
// PUT body: { coverImageUrl: string }
func (h *CacheHandler) UpdateTorrentDetailsCoverImage(w http.ResponseWriter, r *http.Request) {
	favoriteID, source := coverImageTorrentDetailsParams(r.URL.Path)
	if favoriteID == "" || source == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required fields: favoriteId, source, and coverImageUrl"})
		return
	}
	coverImageURL, ok := decodeCoverImageURL(w, r)
	if !ok {
		return
	}
	updated, err := h.storage.UpdateTorrentDetailsCoverImage(r.Context(), favoriteID, source, coverImageURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to update torrent details cover image"})
		return
	}
	if !updated {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Torrent details not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Torrent details cover image updated successfully"})
}

func decodeCoverImageURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		CoverImageURL string `json:"coverImageUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CoverImageURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: coverImageUrl"})
		return "", false
	}
	return req.CoverImageURL, true
}

func coverImagePathParam(path, segment string) string {
	prefixes := []string{
		"/api/cache/cover-image/" + segment + "/",
		"/api/storage/cover-image/" + segment + "/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
		}
	}
	return ""
}

func coverImageTorrentDetailsParams(path string) (favoriteID, source string) {
	prefixes := []string{
		"/api/cache/cover-image/torrent-details/",
		"/api/storage/cover-image/torrent-details/",
	}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1]
		}
	}
	return "", ""
}

// UpdateFavoriteEntryMagnetLink updates a favorite entry's magnet link
// PUT body: { magnetLink: string }
func (h *CacheHandler) UpdateFavoriteEntryMagnetLink(w http.ResponseWriter, r *http.Request) {
	params := middleware.ExtractParams("/api/storage/favorites/:favoriteId/magnet", r.URL.Path)
	favoriteID := params.Get("favoriteId")
	if favoriteID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing favoriteId"})
		return
	}

	var req struct {
		MagnetLink string `json:"magnetLink"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MagnetLink == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Missing required field: magnetLink"})
		return
	}

	updated, err := h.storage.UpdateFavoriteEntryMagnetLink(r.Context(), favoriteID, authUserID(r), req.MagnetLink)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to update magnet link"})
		return
	}
	if !updated {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "Favorite entry not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Magnet link updated"})
}

// authUserID returns the authenticated user's ID from the request context,
// or empty string if unauthenticated. Used to scope favorite mutations to
// their owner so one user cannot edit another's favorites by guessing the id.
func authUserID(r *http.Request) string {
	if u := middleware.GetUser(r); u != nil {
		return u.UserID
	}
	return ""
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// extractMagnetHashFromLink extracts the info-hash from a magnet URI
func extractMagnetHashFromLink(magnetLink string) string {
	lower := strings.ToLower(magnetLink)
	const prefix = "urn:btih:"
	idx := strings.Index(lower, prefix)
	if idx == -1 {
		return magnetLink // return as-is if not a magnet link
	}
	hash := magnetLink[idx+len(prefix):]
	if end := strings.IndexByte(hash, '&'); end != -1 {
		hash = hash[:end]
	}
	return strings.ToLower(hash)
}

// parseFallbackURLs parses a JSON string array of fallback URLs
func parseFallbackURLs(s string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return []string{}, err
	}
	return result, nil
}
