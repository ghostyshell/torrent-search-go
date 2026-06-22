package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultHentaiURL = "https://hentaistream-addon.keypop3750.workers.dev"

var hentaiTheirCatalog = map[string]string{
	"hentai_new":     "hentai-monthly",
	"hentai_top":     "hentai-top-rated",
	"hentai_all":     "hentai-all",
	"hentai_studios": "hentai-studios",
	"hentai_years":   "hentai-years",
	"hentai_search":  "hentai-search",
}

// ExternalProxy fetches catalog pages from the reference HentaiStream addon.
type ExternalProxy struct {
	HentaiURL  string
	HTTPClient *http.Client
}

func (h *Handler) externalProxy() *ExternalProxy {
	if h.External == nil {
		return nil
	}
	return h.External
}

func (h *Handler) serveProxiedCatalog(ctx context.Context, catalogID, genre, searchQ string, skip int) (CatalogResponse, error) {
	proxy := h.externalProxy()
	if proxy == nil {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}

	theirID := hentaiTheirCatalog[catalogID]
	if theirID == "" {
		return CatalogResponse{Metas: []MetaPreview{}}, nil
	}
	baseURL := proxy.HentaiURL
	if baseURL == "" {
		baseURL = defaultHentaiURL
	}

	store := newRedisStore(h.Redis)
	cacheKey := prefixHentaiCatalog + catalogID + "|" + genre + "|" + searchQ + "|" + itoa(skip)
	if store != nil {
		if cached, _ := store.getProxiedMetas(ctx, cacheKey); len(cached) > 0 {
			return CatalogResponse{Metas: cached}, nil
		}
	}

	metas, err := proxy.fetchCatalog(ctx, baseURL, "hentai", theirID, genre, searchQ, skip, "hs")
	if err != nil {
		return CatalogResponse{Metas: []MetaPreview{}}, err
	}
	if store != nil && len(metas) > 0 {
		_ = store.setProxiedMetas(ctx, cacheKey, metas)
	}
	return CatalogResponse{Metas: metas}, nil
}

func (p *ExternalProxy) fetchCatalog(ctx context.Context, baseURL, contentType, theirID, genre, searchQ string, skip int, idPrefix string) ([]MetaPreview, error) {
	if p == nil {
		return nil, nil
	}
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	extra := extraSegment(genre, searchQ, skip)
	reqURL := fmt.Sprintf("%s/catalog/%s/%s%s.json",
		strings.TrimSuffix(baseURL, "/"),
		url.PathEscape(contentType),
		url.PathEscape(theirID),
		extra,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return []MetaPreview{}, nil
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Metas []map[string]interface{} `json:"metas"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	out := make([]MetaPreview, 0, len(payload.Metas))
	for _, m := range payload.Metas {
		rawID := strMetaVal(m["id"])
		if rawID == "" {
			continue
		}
		name := strMetaVal(m["name"])
		if name == "" {
			continue
		}
		poster := strMetaVal(m["poster"])
		bg := strMetaVal(m["background"])
		if bg == "" {
			bg = poster
		}
		release := strMetaVal(m["releaseInfo"])
		if release == "" {
			release = strMetaVal(m["year"])
		}
		// Hentai catalog items must be typed "series" to match the meta type.
		// Catalog declared type "hentai" in manifest; items typed "hentai" with
		// "series" meta are rejected by Stremio as "No metadata found".
		// Bare upstream IDs (hse-/hmm-/htv-…) avoid Stremio parsing colons
		// as seriesId:season:episode delimiters.
		out = append(out, MetaPreview{
			ID:          rawID,
			Type:        "series",
			Name:        name,
			Poster:      poster,
			Background:  bg,
			Description: strMetaVal(m["description"]),
			ReleaseInfo: release,
			PosterShape: strMetaVal(m["posterShape"]),
		})
	}
	return out, nil
}

// isHentaiID reports whether an item ID belongs to the proxied HentaiStream
// source. New IDs are bare upstream IDs (hse-/hmm-/htv-/hs-…); "hs:" is the
// legacy colon-prefixed form accepted for backwards compatibility.
func isHentaiID(id string) bool {
	return strings.HasPrefix(id, "hs:") ||
		strings.HasPrefix(id, "hse-") ||
		strings.HasPrefix(id, "hmm-") ||
		strings.HasPrefix(id, "htv-") ||
		strings.HasPrefix(id, "hs-")
}

func (h *Handler) serveProxiedMeta(ctx context.Context, id string) (*Meta, error) {
	proxy := h.externalProxy()
	if proxy == nil {
		return nil, nil
	}

	store := newRedisStore(h.Redis)
	if store != nil {
		if cached, ok := store.getHentaiMeta(ctx, id); ok {
			return cached, nil
		}
	}

	theirID := id
	if strings.HasPrefix(id, "hs:") {
		theirID = strings.TrimPrefix(id, "hs:")
	}
	if theirID == "" {
		return nil, nil
	}

	baseURL := proxy.HentaiURL
	if baseURL == "" {
		baseURL = defaultHentaiURL
	}
	reqURL := fmt.Sprintf("%s/meta/series/%s.json",
		strings.TrimSuffix(baseURL, "/"),
		url.PathEscape(theirID),
	)
	client := proxy.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Meta map[string]interface{} `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	m := payload.Meta
	if m == nil {
		return nil, nil
	}
	name := strMetaVal(m["name"])
	if name == "" {
		name = theirID
	}
	poster := strMetaVal(m["poster"])
	bg := strMetaVal(m["background"])
	if bg == "" {
		bg = poster
	}
	release := strMetaVal(m["releaseInfo"])
	if release == "" {
		release = strMetaVal(m["year"])
	}
	meta := &Meta{
		ID:          id,
		Name:        name,
		Poster:      poster,
		Background:  bg,
		Description: strMetaVal(m["description"]),
		ReleaseInfo: release,
		Genres:      stringSliceMeta(m["genres"]),
		Links:       parseMetaLinks(m["links"]),
		Videos:      parseVideos(m["videos"]),
		Website:     strMetaVal(m["website"]),
	}

	if store != nil {
		_ = store.setHentaiMeta(ctx, id, meta)
	}
	return meta, nil
}

// serveProxiedStream fetches stream results from the upstream HentaiStream
// addon for a hentai episode ID and returns them as a raw JSON-serializable
// slice ready to embed in {"streams": ...}.
func (h *Handler) serveProxiedStream(ctx context.Context, id string) ([]map[string]interface{}, error) {
	proxy := h.externalProxy()
	if proxy == nil {
		return nil, nil
	}

	baseURL := proxy.HentaiURL
	if baseURL == "" {
		baseURL = defaultHentaiURL
	}

	theirID := id
	if strings.HasPrefix(id, "hs:") {
		theirID = strings.TrimPrefix(id, "hs:")
	}

	reqURL := fmt.Sprintf("%s/stream/series/%s.json",
		strings.TrimSuffix(baseURL, "/"),
		url.PathEscape(theirID),
	)
	client := proxy.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Streams []map[string]interface{} `json:"streams"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Streams, nil
}

func stringSliceMeta(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		if s, ok := el.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseMetaLinks(links interface{}) []Link {
	arr, ok := links.([]interface{})
	if !ok {
		return nil
	}
	out := make([]Link, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			continue
		}
		name := strMetaVal(m["name"])
		linkURL := strMetaVal(m["url"])
		if name == "" || linkURL == "" {
			continue
		}
		// category required by stremio-core; fall back to a non-empty default so
		// the link always deserializes.
		category := strMetaVal(m["category"])
		if category == "" {
			category = "Genres"
		}
		out = append(out, Link{Name: name, Category: category, URL: linkURL})
	}
	return out
}

func parseVideos(v interface{}) []Video {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]Video, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			continue
		}
		id := strMetaVal(m["id"])
		if id == "" {
			continue
		}
		video := Video{
			ID:         id,
			Title:      strMetaVal(m["title"]),
			Released:   strMetaVal(m["released"]),
			Thumbnail:  strMetaVal(m["thumbnail"]),
			Overview:   strMetaVal(m["overview"]),
			FirstAired: strMetaVal(m["firstAired"]),
		}
		if n, ok := metaNumber(m["season"]); ok {
			video.Season = int(n)
		}
		if n, ok := metaNumber(m["episode"]); ok {
			video.Episode = int(n)
		}
		out = append(out, video)
	}
	return out
}

func metaNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func extraSegment(genre, searchQ string, skip int) string {
	parts := make([]string, 0, 3)
	if genre != "" && genre != "All" {
		parts = append(parts, "genre="+url.QueryEscape(genre))
	}
	if searchQ != "" {
		parts = append(parts, "search="+url.QueryEscape(searchQ))
	}
	if skip > 0 {
		parts = append(parts, "skip="+itoa(skip))
	}
	if len(parts) == 0 {
		return ""
	}
	return "/" + strings.Join(parts, "&")
}
