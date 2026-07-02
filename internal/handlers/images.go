package handlers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/config"
)

// Pixhost subdomains used to build fallback URLs
var pixhostSubdomains = []string{
	"img1.pixhost.to",
	"img2.pixhost.to",
	"img3.pixhost.to",
	"img4.pixhost.to",
	"img5.pixhost.to",
}

// Image-query cleaning regexes (mirror the JS googleImagesService.cleanQuery).
var (
	imgReExt        = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|mov|wmv|flv|webm)$`)
	imgReQuality    = regexp.MustCompile(`(?i)\b(1080p|720p|480p|4k|hd|bluray|bdrip|hdrip|webrip|dvdrip|brrip)\b`)
	imgReCodec      = regexp.MustCompile(`(?i)\b(x264|x265|hevc|h\.264|h\.265|avc|aac|ac3)\b`)
	imgReGroup      = regexp.MustCompile(`(?i)\b(p2p|rarbg|yts|etrg|wrb)\b`)
	imgReBracket    = regexp.MustCompile(`\[\w+\]`)
	imgReYear       = regexp.MustCompile(`\(\d{4}\)`)
	imgReDate       = regexp.MustCompile(`\b\d{2}\s\d{2}\s\d{2}\b`)
	imgReWhitespace = regexp.MustCompile(`\s+`)
)

// cleanImageQuery strips torrent-release noise from a query to improve image
// search relevance. Mirrors googleImagesService.cleanQuery in the JS backend.
func cleanImageQuery(query string) string {
	q := imgReExt.ReplaceAllString(query, "")
	q = imgReQuality.ReplaceAllString(q, "")
	q = imgReCodec.ReplaceAllString(q, "")
	q = imgReGroup.ReplaceAllString(q, "")
	q = imgReBracket.ReplaceAllString(q, "")
	q = imgReYear.ReplaceAllString(q, "")
	q = imgReDate.ReplaceAllString(q, "")
	q = imgReWhitespace.ReplaceAllString(q, " ")
	return strings.TrimSpace(q)
}

// generateSearchSuggestions builds image-search suggestions from a torrent name.
// Mirrors generateSearchSuggestions in the JS googleImagesService.
func generateSearchSuggestions(torrentName string) []string {
	cleaned := cleanImageQuery(torrentName)
	suggestions := []string{cleaned}

	words := make([]string, 0)
	for _, word := range strings.Fields(cleaned) {
		if word != "" {
			words = append(words, word)
		}
	}

	if len(words) >= 2 {
		suggestions = append(suggestions, strings.Join(words[0:2], " "))
		if len(words) >= 3 {
			suggestions = append(suggestions, words[0]+" "+strings.Join(words[1:3], " "))
			suggestions = append(suggestions, strings.Join(words[len(words)-2:], " "))
		}
		suggestions = append(suggestions, words[1])
		if len(words) >= 3 {
			suggestions = append(suggestions, words[2])
		}
	}

	suggestions = append(suggestions, cleaned+" photo")
	suggestions = append(suggestions, cleaned+" image")
	suggestions = append(suggestions, cleaned+" gallery")

	// De-duplicate while preserving order; drop empties.
	seen := make(map[string]struct{}, len(suggestions))
	out := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ImagesHandler handles image endpoints
type ImagesHandler struct {
	storage    *StorageProvider
	config     *config.Config
	tokenCache *googleTokenCache
}

// NewImagesHandler creates a new images handler
func NewImagesHandler(storage *StorageProvider, cfg *config.Config) *ImagesHandler {
	return &ImagesHandler{
		storage:    storage,
		config:     cfg,
		tokenCache: &googleTokenCache{},
	}
}

// googleTokenCache caches a service-account access token
type googleTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func (c *googleTokenCache) get() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second)) {
		return c.accessToken, true
	}
	return "", false
}

func (c *googleTokenCache) set(token string, expiresIn int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = token
	c.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
}

// GoogleSearch searches for images using Google Custom Search API
// GET /api/images/google-images/search?q=QUERY&limit=N
func (h *ImagesHandler) GoogleSearch(w http.ResponseWriter, r *http.Request) {
	rawQuery := r.URL.Query().Get("q")
	if rawQuery == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Query parameter 'q' is required"})
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}

	query := cleanImageQuery(rawQuery)

	if h.config.Google.ServiceAccountJSON == "" || h.config.Google.CustomSearchEngineID == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "Google API not configured"})
		return
	}

	accessToken, err := h.getGoogleAccessToken(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get Google access token: " + err.Error()})
		return
	}

	// Google Custom Search returns at most 10 results per request, so page
	// through with the `start` index until we have `limit` results.
	results := make([]map[string]interface{}, 0, limit)
	const perRequest = 10
	maxRequests := (limit + perRequest - 1) / perRequest
	for i := 0; i < maxRequests; i++ {
		start := i*perRequest + 1
		items, status, fetchErr := h.fetchGoogleImagePage(r.Context(), accessToken, query, perRequest, start)
		if fetchErr != nil {
			// Fail only if the first page fails; otherwise return what we have.
			if i == 0 {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": fetchErr.Error()})
				return
			}
			break
		}
		if status != http.StatusOK {
			if i == 0 {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": fmt.Sprintf("Google Search error (HTTP %d)", status)})
				return
			}
			break
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			results = append(results, mapGoogleImageItem(item, query))
		}
		if len(results) >= limit {
			break
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"query":   query,
		"results": results,
		"count":   len(results),
	})
}

// fetchGoogleImagePage performs a single Custom Search request for images.
func (h *ImagesHandler) fetchGoogleImagePage(ctx context.Context, accessToken, query string, num, start int) ([]map[string]interface{}, int, error) {
	params := url.Values{
		"cx":         {h.config.Google.CustomSearchEngineID},
		"q":          {query},
		"searchType": {"image"},
		"num":        {strconv.Itoa(num)},
		"start":      {strconv.Itoa(start)},
		"safe":       {"off"},
		"filter":     {"0"},
	}
	apiURL := "https://www.googleapis.com/customsearch/v1?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("Failed to build search request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Google Search request failed: %s", err.Error())
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}

	var searchResp struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("Failed to parse search response")
	}
	return searchResp.Items, resp.StatusCode, nil
}

// mapGoogleImageItem normalizes a CSE image item to the shape the frontend
// expects: {url, title, thumbnail, width, height, source}.
func mapGoogleImageItem(item map[string]interface{}, query string) map[string]interface{} {
	link, _ := item["link"].(string)
	title, _ := item["title"].(string)
	if title == "" {
		title = query
	}

	result := map[string]interface{}{
		"url":       link,
		"title":     title,
		"thumbnail": link,
		"width":     800,
		"height":    600,
		"source":    "google.com",
	}
	if displayLink, ok := item["displayLink"].(string); ok && displayLink != "" {
		result["source"] = displayLink
	}
	if image, ok := item["image"].(map[string]interface{}); ok {
		if thumb, ok := image["thumbnailLink"].(string); ok && thumb != "" {
			result["thumbnail"] = thumb
		}
		if width, ok := image["width"]; ok && width != nil {
			result["width"] = width
		}
		if height, ok := image["height"]; ok && height != nil {
			result["height"] = height
		}
	}
	return result
}

// GoogleSuggestions returns search suggestions derived from the torrent name,
// mirroring the JS generateSearchSuggestions helper (no external call).
// GET /api/images/google-images/suggestions?q=QUERY
func (h *ImagesHandler) GoogleSuggestions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Query parameter 'q' is required"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"query":       query,
		"suggestions": generateSearchSuggestions(query),
	})
}

// PixhostUpload uploads an image to Pixhost
// POST multipart/form-data: file or url
func (h *ImagesHandler) PixhostUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		// Try URL-based upload from JSON body
		var req struct {
			URL string `json:"url"`
		}
		if jsonErr := json.NewDecoder(r.Body).Decode(&req); jsonErr != nil || req.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Provide a file or image URL"})
			return
		}

		// Upload by URL
		result, err := pixhostUploadByURL(r.Context(), req.URL)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Pixhost upload failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "url": result})
		return
	}

	// Form-data upload
	imageURL := r.FormValue("url")
	if imageURL != "" {
		result, err := pixhostUploadByURL(r.Context(), imageURL)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Pixhost upload failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "url": result})
		return
	}

	writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Provide a file or image URL"})
}

// GetPixhostFallbacks returns a list of fallback URLs for a Pixhost image
// GET /api/images/pixhost/fallbacks?url=PIXHOST_URL
func (h *ImagesHandler) GetPixhostFallbacks(w http.ResponseWriter, r *http.Request) {
	imageURL := r.URL.Query().Get("url")
	if imageURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "URL parameter is required"})
		return
	}

	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Invalid URL format"})
		return
	}

	if !isPixhostURL(parsedURL) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "fallbacks": []string{}, "isPixhost": false})
		return
	}

	// Try stored fallbacks from DB
	if h.storage != nil {
		storedFallbacks, err := h.storage.GetFallbackUrlsByPixhostUrl(imageURL)
		if err == nil && len(storedFallbacks) > 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":        true,
				"fallbacks":      storedFallbacks,
				"isPixhost":      true,
				"hasBackupHosts": true,
			})
			return
		}
	}

	// Generate subdomain fallbacks
	fallbacks := generatePixhostFallbacks(imageURL)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"fallbacks": fallbacks,
		"isPixhost": true,
	})
}

// BatchProcess validates a batch of image URLs (mirrors the JS batchProcessImages).
// POST { "images": [{"url": "..."}], "operation": "validate" }
func (h *ImagesHandler) BatchProcess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Images array is required"})
		return
	}
	if req.Images == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Images array is required"})
		return
	}

	operation := req.Operation
	if operation == "" {
		operation = "validate"
	}

	results := make([]map[string]interface{}, 0, len(req.Images))
	for _, img := range req.Images {
		result := map[string]interface{}{"originalUrl": img.URL}
		if operation == "validate" {
			if u, err := url.ParseRequestURI(img.URL); err == nil && u.Scheme != "" && u.Host != "" {
				result["url"] = img.URL
				result["success"] = true
			} else {
				result["success"] = false
				result["error"] = "Invalid URL"
			}
		} else {
			result["success"] = false
			result["error"] = "Unknown operation"
		}
		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"operation":      operation,
		"totalProcessed": len(results),
		"results":        results,
	})
}

// ─── Google service account JWT helpers ──────────────────────────────────────

type googleServiceAccount struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// getGoogleAccessToken returns a cached or freshly-minted service account access token
func (h *ImagesHandler) getGoogleAccessToken(ctx context.Context) (string, error) {
	if token, ok := h.tokenCache.get(); ok {
		return token, nil
	}

	// Parse service account JSON
	var sa googleServiceAccount
	if err := json.Unmarshal([]byte(h.config.Google.ServiceAccountJSON), &sa); err != nil {
		return "", fmt.Errorf("invalid service account JSON: %w", err)
	}

	if sa.PrivateKey == "" || sa.ClientEmail == "" {
		return "", fmt.Errorf("service account JSON missing private_key or client_email")
	}

	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	// Build JWT
	jwt, err := buildServiceAccountJWT(sa.ClientEmail, sa.PrivateKey, "https://www.googleapis.com/auth/cse", tokenURI)
	if err != nil {
		return "", fmt.Errorf("failed to build JWT: %w", err)
	}

	// Exchange JWT for access token
	data := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	h.tokenCache.set(tokenResp.AccessToken, tokenResp.ExpiresIn)
	return tokenResp.AccessToken, nil
}

// buildServiceAccountJWT creates a signed JWT for Google service account auth
func buildServiceAccountJWT(clientEmail, privateKeyPEM, scope, audience string) (string, error) {
	// Decode PEM key
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey
	var err error

	switch block.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if pkcs8Err != nil {
			return "", fmt.Errorf("failed to parse PKCS8 private key: %w", pkcs8Err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("expected RSA private key")
		}
	default:
		return "", fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	now := time.Now().Unix()

	// Build header
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	// Build claims
	claims := map[string]interface{}{
		"iss":   clientEmail,
		"scope": scope,
		"aud":   audience,
		"iat":   now,
		"exp":   now + 3600,
	}
	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	// Sign: build the JWT signing input and sign with RS256
	sigInput := headerB64 + "." + claimsB64
	sig, err := signJWTRS256(privateKey, []byte(sigInput))
	if err != nil {
		return "", err
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return sigInput + "." + sigB64, nil
}

// signJWTRS256 signs data with RS256 (RSA + SHA-256) for JWT
func signJWTRS256(key *rsa.PrivateKey, data []byte) ([]byte, error) {
	h := sha256.New()
	h.Write(data)
	digest := h.Sum(nil)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest)
}

// ─── Pixhost helpers ──────────────────────────────────────────────────────────

// isPixhostURL checks if a URL is a Pixhost URL
func isPixhostURL(u *url.URL) bool {
	return strings.Contains(u.Host, "pixhost.to")
}

// generatePixhostFallbacks generates a list of fallback URLs for a Pixhost image
func generatePixhostFallbacks(originalURL string) []string {
	var path string
	for _, subdomain := range pixhostSubdomains {
		prefix := "https://" + subdomain + "/images/"
		if strings.HasPrefix(originalURL, prefix) {
			path = strings.TrimPrefix(originalURL, prefix)
			break
		}
	}
	if path == "" {
		if strings.HasPrefix(originalURL, "https://pixhost.to/show/") {
			path = strings.TrimPrefix(originalURL, "https://pixhost.to/show/")
		} else {
			return []string{originalURL}
		}
	}

	fallbacks := make([]string, 0, len(pixhostSubdomains))
	for _, subdomain := range pixhostSubdomains {
		fallbacks = append(fallbacks, fmt.Sprintf("https://%s/images/%s", subdomain, path))
	}
	return fallbacks
}

// pixhostUploadByURL uploads an image to Pixhost using a source URL
func pixhostUploadByURL(ctx context.Context, imageURL string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("content_type", "0") // 0 = not adult
	_ = writer.WriteField("url", imageURL)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://pixhost.to/api/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Pixhost upload failed (HTTP %d): %s", resp.StatusCode, respBody)
	}

	var result struct {
		ShowURL  string `json:"show_url"`
		FullSize string `json:"full_size"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse Pixhost response: %w", err)
	}

	if result.ShowURL != "" {
		return result.ShowURL, nil
	}
	return result.FullSize, nil
}
