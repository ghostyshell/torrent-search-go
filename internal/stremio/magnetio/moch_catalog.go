package magnetio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// mochCatalogResult is a normalized debrid library item.
type mochCatalogResult struct {
	ID          string
	Name        string
	Type        string
	Description string
}

// mochClient describes a single debrid service's catalog fetcher.
type mochClient struct {
	id      string
	hasCatalog bool
	fetch   func(ctx context.Context, apiKey string, contentType string, skip int) ([]mochCatalogResult, error)
}

// mochClients is the canonical list of services that expose library catalogs.
var mochClients = []mochClient{
	{id: "rd", hasCatalog: true, fetch: fetchRealDebridCatalog},
	{id: "pm", hasCatalog: true, fetch: fetchPremiumizeCatalog},
	{id: "dl", hasCatalog: true, fetch: fetchDebridLinkCatalog},
	{id: "tb", hasCatalog: true, fetch: fetchTorBoxCatalog},
	{id: "pu", hasCatalog: true, fetch: fetchPutioCatalog},
}

func mochClientByID(id string) *mochClient {
	for i := range mochClients {
		if mochClients[i].id == id {
			return &mochClients[i]
		}
	}
	return nil
}

// fetchMochCatalog fetches a single debrid library page.
func fetchMochCatalog(ctx context.Context, mochID, apiKey, contentType string, skip int) ([]map[string]interface{}, error) {
	client := mochClientByID(mochID)
	if client == nil || !client.hasCatalog {
		return nil, nil
	}
	if len(apiKey) < minMochKeyLen {
		return nil, nil
	}

	results, err := client.fetch(ctx, apiKey, contentType, skip)
	if err != nil {
		return nil, err
	}

	metas := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		metas = append(metas, map[string]interface{}{
			"id":          r.ID,
			"type":        r.Type,
			"name":        r.Name,
			"description": r.Description,
		})
	}
	return metas, nil
}

// Real-Debrid catalog
// GET https://api.real-debrid.com/rest/1.0/torrents?limit=25&offset=<skip>
func fetchRealDebridCatalog(ctx context.Context, apiKey, contentType string, skip int) ([]mochCatalogResult, error) {
	u, _ := url.Parse("https://api.real-debrid.com/rest/1.0/torrents")
	q := u.Query()
	q.Set("limit", "25")
	q.Set("offset", strconv.Itoa(skip))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var torrents []struct {
		Filename string  `json:"filename"`
		Hash     string  `json:"hash"`
		Bytes    int64   `json:"bytes"`
		Progress float64 `json:"progress"`
	}
	if err := decodeJSON(res.Body, &torrents); err != nil {
		return nil, err
	}

	out := make([]mochCatalogResult, 0, len(torrents))
	for _, t := range torrents {
		if t.Hash == "" {
			continue
		}
		out = append(out, mochCatalogResult{
			ID:          "rd:" + strings.ToLower(t.Hash),
			Type:        contentType,
			Name:        t.Filename,
			Description: fmt.Sprintf("Size: %.1f GB | Progress: %.0f%%", float64(t.Bytes)/(1024*1024*1024), t.Progress),
		})
	}
	return out, nil
}

// Premiumize catalog
// GET https://www.premiumize.me/api/transfer/list
func fetchPremiumizeCatalog(ctx context.Context, apiKey, contentType string, skip int) ([]mochCatalogResult, error) {
	u := "https://www.premiumize.me/api/transfer/list?apikey=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var payload struct {
		Status    string `json:"status"`
		Transfers []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"transfers"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" {
		return nil, nil
	}

	start := skip
	end := skip + 25
	if start > len(payload.Transfers) {
		start = len(payload.Transfers)
	}
	if end > len(payload.Transfers) {
		end = len(payload.Transfers)
	}

	out := make([]mochCatalogResult, 0, end-start)
	for _, t := range payload.Transfers[start:end] {
		out = append(out, mochCatalogResult{
			ID:          "pm:" + t.ID,
			Type:        contentType,
			Name:        t.Name,
			Description: "Status: " + t.Status,
		})
	}
	return out, nil
}

// DebridLink catalog
// GET https://debrid-link.com/api/v2/seedbox/list?perPage=25&page=<skip/25+1>
func fetchDebridLinkCatalog(ctx context.Context, apiKey, contentType string, skip int) ([]mochCatalogResult, error) {
	u, _ := url.Parse("https://debrid-link.com/api/v2/seedbox/list")
	q := u.Query()
	q.Set("perPage", "25")
	q.Set("page", strconv.Itoa(skip/25+1))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var payload struct {
		Success bool `json:"success"`
		Value   []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			TotalSize int64  `json:"totalSize"`
		} `json:"value"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, nil
	}

	out := make([]mochCatalogResult, 0, len(payload.Value))
	for _, t := range payload.Value {
		out = append(out, mochCatalogResult{
			ID:          "dl:" + t.ID,
			Type:        contentType,
			Name:        t.Name,
			Description: fmt.Sprintf("Size: %.1f GB", float64(t.TotalSize)/(1024*1024*1024)),
		})
	}
	return out, nil
}

// TorBox catalog
// GET https://api.torbox.app/v1/api/torrents/mylist?limit=25&offset=<skip>
func fetchTorBoxCatalog(ctx context.Context, apiKey, contentType string, skip int) ([]mochCatalogResult, error) {
	u, _ := url.Parse("https://api.torbox.app/v1/api/torrents/mylist")
	q := u.Query()
	q.Set("limit", "25")
	q.Set("offset", strconv.Itoa(skip))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Size          int64  `json:"size"`
			DownloadState string `json:"download_state"`
		} `json:"data"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		return nil, nil
	}

	out := make([]mochCatalogResult, 0, len(payload.Data))
	for _, t := range payload.Data {
		out = append(out, mochCatalogResult{
			ID:          "tb:" + t.ID,
			Type:        contentType,
			Name:        t.Name,
			Description: fmt.Sprintf("Size: %.1f GB | Status: %s", float64(t.Size)/(1024*1024*1024), t.DownloadState),
		})
	}
	return out, nil
}

// Put.io catalog
// GET https://api.put.io/v2/transfers/list
func fetchPutioCatalog(ctx context.Context, apiKey, contentType string, skip int) ([]mochCatalogResult, error) {
	u := "https://api.put.io/v2/transfers/list?oauth_token=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var payload struct {
		Transfers []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			Status string `json:"status"`
		} `json:"transfers"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return nil, err
	}

	start := skip
	end := skip + 25
	if start > len(payload.Transfers) {
		start = len(payload.Transfers)
	}
	if end > len(payload.Transfers) {
		end = len(payload.Transfers)
	}

	out := make([]mochCatalogResult, 0, end-start)
	for _, t := range payload.Transfers[start:end] {
		out = append(out, mochCatalogResult{
			ID:          "pu:" + strconv.FormatInt(parsePutioID(t.ID), 10),
			Type:        contentType,
			Name:        t.Name,
			Description: fmt.Sprintf("Size: %.1f GB | Status: %s", float64(t.Size)/(1024*1024*1024), t.Status),
		})
	}
	return out, nil
}

func parsePutioID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// mochCatalogEnabled reports whether the named debrid catalog is enabled in config.
func mochCatalogEnabled(cfg Config, mochID string) bool {
	switch mochID {
	case "rd":
		return cfg.RealDebridCatalogEnabled
	case "pm":
		return cfg.PremiumizeCatalogEnabled
	case "dl":
		return cfg.DebridLinkCatalogEnabled
	case "tb":
		return cfg.TorboxCatalogEnabled
	case "pu":
		return cfg.PutioCatalogEnabled
	}
	return false
}

// apiKeyForMoch returns the configured API key for a debrid service short id.
func apiKeyForMoch(cfg Config, mochID string) string {
	switch mochID {
	case "rd":
		return cfg.RealDebridApiKey
	case "pm":
		return cfg.PremiumizeApiKey
	case "dl":
		return cfg.DebridLinkApiKey
	case "tb":
		return cfg.TorboxApiKey
	case "pu":
		return cfg.PutioApiKey
	}
	return ""
}

// decodeJSON reads and unmarshals a JSON body, then closes it.
func decodeJSON(r io.ReadCloser, dest interface{}) error {
	defer r.Close()
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

// minMochKeyLen is the minimum API key length to be considered valid.
const minMochKeyLen = 15

// mochCatalogCache wraps fetchMochCatalog with a short in-memory TTL cache
// to avoid hammering debrid APIs when Stremio paginates through a catalog.
type mochCatalogCache struct {
	mu      sync.RWMutex
	entries map[string]mochCacheEntry
	ttl     time.Duration
}

type mochCacheEntry struct {
	metas []map[string]interface{}
	at    time.Time
}

func newMochCatalogCache() *mochCatalogCache {
	return &mochCatalogCache{
		entries: map[string]mochCacheEntry{},
		ttl:     5 * time.Minute,
	}
}

func (c *mochCatalogCache) key(mochID, apiKey, contentType string, skip int) string {
	return fmt.Sprintf("%s|%s|%s|%d", mochID, apiKey, contentType, skip)
}

func (c *mochCatalogCache) get(ctx context.Context, mochID, apiKey, contentType string, skip int) ([]map[string]interface{}, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[c.key(mochID, apiKey, contentType, skip)]
	if !ok || time.Since(entry.at) > c.ttl {
		return nil, false
	}
	return entry.metas, true
}

func (c *mochCatalogCache) set(mochID, apiKey, contentType string, skip int, metas []map[string]interface{}) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[c.key(mochID, apiKey, contentType, skip)] = mochCacheEntry{metas: metas, at: time.Now()}
}
