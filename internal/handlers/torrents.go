package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/scraper"
	pkgmodels "torrent-search-go/pkg/models"
)

// coverEnrichConcurrency caps simultaneous cover lookups/scrapes.
const coverEnrichConcurrency = 6

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// generateTorrentKey builds the cover-cache key using a normalized format:
// lowercase(normalize("Name_Source_Size")), hashing if the result exceeds 200 chars.
// This ensures consistent key generation for the image cache.
func generateTorrentKey(name, source, size string) string {
	key := strings.ToLower(nonAlnum.ReplaceAllString(name+"_"+source+"_"+size, "_"))
	if len(key) > 200 {
		key = key[:150] + "_" + simpleHash(key)
	}
	return key
}

// simpleHash mirrors the JS 32-bit string hash rendered in base36.
func simpleHash(s string) string {
	var hash int32
	for _, c := range s {
		hash = (hash << 5) - hash + int32(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return strconv.FormatInt(int64(hash), 36)
}

// enrichCoverImages fills CoverImage on each result when includeCoverImages is
// requested. It prefers TPDB/StashDB metadata (same priority as the Stremio
// addon), then the stored cover cache, and only then scrapes the detail page.
func (h *TorrentHandler) enrichCoverImages(ctx context.Context, fallbackWebsite string, results []models.Torrent) {
	if h.scrapers == nil || len(results) == 0 {
		return
	}
	sem := make(chan struct{}, coverEnrichConcurrency)
	var wg sync.WaitGroup
	for i := range results {
		t := &results[i]
		website := t.Website
		if website == "" {
			website = fallbackWebsite
		}
		// Cover key uses the same normalization (Name_Source_Size),
		// so lookups hit the shared image cache.
		key := generateTorrentKey(t.Name, website, t.Size)
		metaID := metaIDForTorrent(t, website)

		wg.Add(1)
		sem <- struct{}{}
		go func(t *models.Torrent, website, key, metaID string) {
			defer wg.Done()
			defer func() { <-sem }()

			var row *pkgmodels.ImageRow
			if h.storage != nil && key != "" {
				row, _ = h.storage.GetCoverImageByKey(ctx, key)
			}

			resolved := resolveCover(ctx, h.storage, coverResolveInput{row: row, metaID: metaID})
			if resolved.url != "" {
				t.CoverImage = &models.CoverImage{
					Type:       "url",
					URL:        resolved.url,
					TpdbURL:    resolved.tpdbURL,
					DetailsURL: resolved.detailsURL,
				}
				if resolved.upgraded && h.storage != nil && key != "" {
					go h.storage.upgradeCoverFromMeta(context.Background(), key, resolved.url, resolved.source, metaID)
				}
				return
			}

			// No shared_meta cover — enqueue async TPDB/StashDB lookup so the
			// next browse request finds the cover without scraping.
			if h.MetaEnqueuer != nil && metaID != "" {
				h.MetaEnqueuer(t.Name, t.TorrentURL, website, metaID)
			}

			// Scrape the detail page (NFO / description fallback).
			if t.TorrentURL == "" {
				return
			}
			details, err := h.scrapers.GetTorrentDetails(ctx, website, t.TorrentURL)
			if err != nil || details == nil {
				return
			}
			// Prefer explicit cover URL, fall back to first description image.
			coverURL := details.CoverImageURL
			if coverURL == "" && len(details.Images) > 0 {
				coverURL = details.Images[0].DirectURL
			}
			if coverURL == "" {
				return
			}
			t.CoverImage = &models.CoverImage{Type: "url", URL: coverURL, DetailsURL: coverURL}

			if h.storage != nil && key != "" {
				_ = h.storage.SetCoverImageDetails(ctx, key, coverURL)
			}
		}(t, website, key, metaID)
	}
	wg.Wait()
}

// TorrentHandler handles torrent search endpoints
type TorrentHandler struct {
	storage      *StorageProvider
	scrapers     *scraper.Service
	MetaEnqueuer func(title, detailURL, website, infoHash string)
}

// NewTorrentHandler creates a new torrent handler
func NewTorrentHandler(storage *StorageProvider, scrapers *scraper.Service) *TorrentHandler {
	return &TorrentHandler{
		storage:  storage,
		scrapers: scrapers,
	}
}

// GetWebsites returns available torrent websites
func (h *TorrentHandler) GetWebsites(w http.ResponseWriter, r *http.Request) {
	var websites []string
	if h.scrapers != nil {
		websites = h.scrapers.GetAvailableScrapers()
	} else {
		websites = []string{
			"piratebay",
			"limetorrent",
			"nyaasi",
			"yts",
			"torrentproject",
			"1337x",
		}
	}
	writeJSON(w, http.StatusOK, websites)
}

// parsePageParam parses a page path segment, defaulting to 1.
func parsePageParam(s string) int {
	if s == "" {
		return 1
	}
	page, err := strconv.Atoi(s)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// parseSearchOptions reads filtering options from the query string.
func parseSearchOptions(r *http.Request) models.SearchOptions {
	options := models.SearchOptions{
		Sort:     r.URL.Query().Get("sort"),
		Category: r.URL.Query().Get("category"),
	}
	if v := r.URL.Query().Get("minSeeders"); v != "" {
		options.MinSeeders, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("maxResults"); v != "" {
		options.MaxResults, _ = strconv.Atoi(v)
	}
	options.IncludeCoverImages = r.URL.Query().Get("includeCoverImages") == "true"
	return options
}

// runSearch executes a single- or all-site search, tags Source, and enriches
// cover images when requested. Shared by the wrapped and bare-array handlers.
func (h *TorrentHandler) runSearch(ctx context.Context, website, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	var results []models.Torrent
	var err error

	if h.scrapers != nil {
		if website == "all" {
			results, err = h.scrapers.SearchAll(ctx, query, page, options)
		} else {
			results, err = h.scrapers.Search(ctx, website, query, page, options)
		}
		if err != nil {
			return nil, err
		}
	}

	if results == nil {
		results = []models.Torrent{}
	}

	// Tag the source with the requested website (matches the JS backend's
	// Source field and the cover-cache key derivation).
	if website != "all" {
		for i := range results {
			results[i].Website = website
		}
	}

	if options.IncludeCoverImages {
		h.enrichCoverImages(ctx, website, results)
	}

	return results, nil
}

// recordSearchQuery upserts the query into search_queries for background cache
// warming (mirrors the JS searchQueries.upsert call). Fire-and-forget so it
// never blocks the response; skips "all" searches and blank queries.
func (h *TorrentHandler) recordSearchQuery(website, query string) {
	if h.storage == nil {
		return
	}
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" || website == "all" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.storage.RecordSearchQuery(ctx, normalized)
	}()
}

// Search searches torrents on a specific website and returns the wrapped
// {success, website, query, page, results} shape used by /api/torrents/search/*.
func (h *TorrentHandler) Search(w http.ResponseWriter, r *http.Request) {
	website := strings.ToLower(r.PathValue("website"))
	query := r.PathValue("query")
	page := parsePageParam(r.PathValue("page"))
	options := parseSearchOptions(r)

	results, err := h.runSearch(r.Context(), website, query, page, options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to search torrents",
			"message": err.Error(),
			"website": website,
			"query":   query,
		})
		return
	}

	h.recordSearchQuery(website, query)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"website": website,
		"query":   query,
		"page":    page,
		"results": results,
	})
}

// SearchLegacy serves the backward-compatible routes (/api/:website/:query/:page?
// and /api/torrents/:website/:query/:page?) which return a bare array of
// torrents, matching the JS searchTorrents handler the frontend depends on.
func (h *TorrentHandler) SearchLegacy(w http.ResponseWriter, r *http.Request) {
	website := strings.ToLower(r.PathValue("website"))
	h.serveLegacySearch(w, r, website)
}

// SearchLegacyFor returns a handler for a per-scraper legacy route
// (/api/{scraper}/:query/:page?) where the website is fixed by registration.
func (h *TorrentHandler) SearchLegacyFor(website string) http.HandlerFunc {
	website = strings.ToLower(website)
	return func(w http.ResponseWriter, r *http.Request) {
		h.serveLegacySearch(w, r, website)
	}
}

// serveLegacySearch runs a search and writes a bare JSON array.
func (h *TorrentHandler) serveLegacySearch(w http.ResponseWriter, r *http.Request, website string) {
	query := r.PathValue("query")
	page := parsePageParam(r.PathValue("page"))
	options := parseSearchOptions(r)

	results, err := h.runSearch(r.Context(), website, query, page, options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error":   "Search failed",
			"message": err.Error(),
		})
		return
	}

	h.recordSearchQuery(website, query)

	writeJSON(w, http.StatusOK, results)
}

// Browse browses torrents by category (piratebay)
func (h *TorrentHandler) Browse(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	if category == "" {
		category = "507"
	}
	page := parsePageParam(r.PathValue("page"))
	var err error
	sortStr := r.URL.Query().Get("sort")
	if sortStr == "" {
		sortStr = "3"
	}

	options := models.SearchOptions{}
	if v := r.URL.Query().Get("minSeeders"); v != "" {
		options.MinSeeders, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("maxResults"); v != "" {
		options.MaxResults, _ = strconv.Atoi(v)
	}
	// Cover images are part of the normal browse response for homepage/category
	// grids (Node parity). Default to true; callers can opt out with
	// ?includeCoverImages=false.
	options.IncludeCoverImages = r.URL.Query().Get("includeCoverImages") != "false"

	// Allow caller to specify ?website=hiddenbay (etc.); default to piratebay
	website := r.URL.Query().Get("website")
	if website == "" {
		website = "piratebay"
	}

	var results []models.Torrent
	if h.scrapers != nil {
		results, err = h.scrapers.Browse(r.Context(), website, category, page, sortStr, options)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Browse failed",
				"message": err.Error(),
			})
			return
		}
	}
	if results == nil {
		results = []models.Torrent{}
	}

	// Tag the source with the requested website (matches JS Source + cover key).
	for i := range results {
		results[i].Website = website
	}

	if options.IncludeCoverImages {
		h.enrichCoverImages(r.Context(), website, results)
	}

	writeJSON(w, http.StatusOK, results)
}

// AdvancedSearch performs advanced multi-site search
func (h *TorrentHandler) AdvancedSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"success": false,
			"error":   "Method not allowed",
		})
		return
	}

	var req struct {
		Query              string   `json:"query"`
		Websites           []string `json:"websites"`
		MinSeeders         int      `json:"minSeeders"`
		MaxResults         int      `json:"maxResults"`
		SortBy             string   `json:"sortBy"`
		SortOrder          string   `json:"sortOrder"`
		IncludeCoverImages bool     `json:"includeCoverImages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Query is required",
		})
		return
	}

	if req.MaxResults == 0 {
		req.MaxResults = 50
	}

	options := models.SearchOptions{
		MinSeeders:         req.MinSeeders,
		MaxResults:         req.MaxResults,
		IncludeCoverImages: req.IncludeCoverImages,
		Sort:               req.SortBy,
	}

	ctx := r.Context()
	var results []models.Torrent
	var searchErr error

	if h.scrapers != nil {
		searchAll := len(req.Websites) == 0 || containsString(req.Websites, "all")

		if searchAll {
			results, searchErr = h.scrapers.SearchAll(ctx, req.Query, 1, options)
		} else {
			// Search each requested website in parallel via channels
			type scraperResult struct {
				torrents []models.Torrent
			}
			ch := make(chan scraperResult, len(req.Websites))
			for _, site := range req.Websites {
				go func(site string) {
					t, _ := h.scrapers.Search(ctx, strings.ToLower(site), req.Query, 1, options)
					ch <- scraperResult{torrents: t}
				}(site)
			}
			for range req.Websites {
				r := <-ch
				results = append(results, r.torrents...)
			}
		}

		if searchErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Search failed",
				"message": searchErr.Error(),
			})
			return
		}
	}

	if results == nil {
		results = []models.Torrent{}
	}

	if options.IncludeCoverImages {
		h.enrichCoverImages(ctx, "", results)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"query":        req.Query,
		"websites":     req.Websites,
		"filters":      options,
		"totalResults": len(results),
		"results":      results,
	})
}

// Details gets torrent details in the Node-compatible wire format used by the
// browse UI (/api/torrent-details and /api/torrents/torrent-details).
func (h *TorrentHandler) Details(w http.ResponseWriter, r *http.Request) {
	h.serveLegacyDetails(w, r)
}

// DetailsPrefetch returns torrent details for magnet prefetch (/api/torrents/details).
// Uses the same legacy wire shape; 1337x responses include a top-level magnet field.
func (h *TorrentHandler) DetailsPrefetch(w http.ResponseWriter, r *http.Request) {
	h.serveLegacyDetails(w, r)
}

func (h *TorrentHandler) serveLegacyDetails(w http.ResponseWriter, r *http.Request) {
	website := strings.ToLower(r.PathValue("website"))
	torrentURL := r.PathValue("torrentUrl")

	if strings.TrimSpace(torrentURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Torrent URL is required",
		})
		return
	}

	if h.scrapers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"success": false,
			"error":   "Scraper service not available",
		})
		return
	}

	details, err := h.scrapers.GetTorrentDetails(r.Context(), website, torrentURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch torrent details",
			"message": err.Error(),
			"website": website,
		})
		return
	}

	writeJSON(w, http.StatusOK, details.LegacyWire())
}

// containsString checks if a string slice contains a value
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, val) {
			return true
		}
	}
	return false
}
