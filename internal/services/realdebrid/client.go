package realdebrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://api.real-debrid.com/rest/1.0"

const (
	maxRefreshRetries = 3
	pollInterval      = 3 * time.Second
	maxPollTime       = 30 * time.Second
)

// Client calls the Real-Debrid REST API.
type Client struct {
	http *http.Client
}

// APIError is a Real-Debrid HTTP error response.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Real-Debrid API error: %d %s", e.StatusCode, e.Body)
}

// NewClient creates a Real-Debrid API client.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 90 * time.Second}}
}

// StreamResult is the outcome of resolving a magnet to a stream URL.
type StreamResult struct {
	StreamURL             string
	Filename              string
	Filesize              int64
	SupportsRangeRequests bool
}

type rdFile struct {
	ID    int    `json:"id"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type torrentInfo struct {
	ID     string   `json:"id"`
	Hash   string   `json:"hash"`
	Files  []rdFile `json:"files"`
	Links  []string `json:"links"`
	Status string   `json:"status"`
}

type torrentListItem struct {
	ID     string `json:"id"`
	Hash   string `json:"hash"`
	Status string `json:"status"`
}

type unrestrictResponse struct {
	Download string `json:"download"`
	Filename string `json:"filename"`
	Filesize int64  `json:"filesize"`
	Host     string `json:"host"`
}

// RefreshStreamURL resolves a magnet to a stream URL with retries.
func (c *Client) RefreshStreamURL(ctx context.Context, apiKey, magnetLink string) (*StreamResult, error) {
	var lastErr error
	var lastTorrentID string

	for attempt := 1; attempt <= maxRefreshRetries; attempt++ {
		result, torrentID, err := c.attemptRefreshStreamURL(ctx, apiKey, magnetLink, attempt, lastTorrentID)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if torrentID != "" {
			lastTorrentID = torrentID
		}
		if !isTransientError(err) || attempt == maxRefreshRetries {
			return nil, err
		}

		backoff := time.Duration(attempt) * 5 * time.Second
		if isRateLimitError(err) {
			backoff = time.Duration(attempt) * 15 * time.Second
		}
		if err := sleepCtx(ctx, backoff); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

func (c *Client) attemptRefreshStreamURL(ctx context.Context, apiKey, magnetLink string, attempt int, previousTorrentID string) (*StreamResult, string, error) {
	if !strings.HasPrefix(magnetLink, "magnet:") {
		return nil, "", fmt.Errorf("invalid magnet link")
	}

	magnetHash := extractMagnetHash(magnetLink)
	if magnetHash == "" {
		return nil, "", fmt.Errorf("invalid magnet link")
	}

	if attempt > 1 {
		if previousTorrentID != "" {
			_ = c.deleteTorrent(ctx, previousTorrentID, apiKey)
		}
		_ = c.deleteExistingTorrentByHash(ctx, magnetHash, apiKey)
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return nil, "", err
		}
	}

	form := url.Values{"magnet": {magnetLink}}
	var addResp struct {
		ID string `json:"id"`
	}
	if err := c.request(ctx, http.MethodPost, "/torrents/addMagnet", apiKey, form, &addResp); err != nil {
		return nil, "", err
	}
	if addResp.ID == "" {
		return nil, "", fmt.Errorf("failed to add magnet to Real-Debrid")
	}
	torrentID := addResp.ID

	processWait := 2*time.Second + time.Duration(attempt-1)*2*time.Second
	if err := sleepCtx(ctx, processWait); err != nil {
		return nil, torrentID, err
	}

	var info torrentInfo
	if err := c.request(ctx, http.MethodGet, "/torrents/info/"+torrentID, apiKey, nil, &info); err != nil {
		return nil, torrentID, err
	}
	if info.Status == "magnet_error" {
		return nil, torrentID, fmt.Errorf("magnet error")
	}
	if len(info.Files) == 0 {
		return nil, torrentID, fmt.Errorf("no files found in torrent")
	}

	video := largestVideoFile(info.Files)
	if video == nil {
		return nil, torrentID, fmt.Errorf("no video files found in torrent")
	}

	selectForm := url.Values{"files": {fmt.Sprintf("%d", video.ID)}}
	if err := c.request(ctx, http.MethodPost, "/torrents/selectFiles/"+torrentID, apiKey, selectForm, nil); err != nil {
		return nil, torrentID, err
	}

	updated, err := c.pollTorrentLinks(ctx, torrentID, apiKey)
	if err != nil {
		return nil, torrentID, err
	}

	unrestrictForm := url.Values{"link": {updated.Links[0]}}
	var unrestricted unrestrictResponse
	if err := c.request(ctx, http.MethodPost, "/unrestrict/link", apiKey, unrestrictForm, &unrestricted); err != nil {
		return nil, torrentID, fmt.Errorf("failed to unrestrict link: %w", err)
	}
	if unrestricted.Download == "" {
		return nil, torrentID, fmt.Errorf("failed to unrestrict link")
	}

	ok, status, err := c.validateStreamURL(ctx, unrestricted.Download)
	if err != nil {
		return nil, torrentID, fmt.Errorf("failed to unrestrict link: %w", err)
	}
	if !ok {
		return nil, torrentID, fmt.Errorf("failed to unrestrict link: validation returned %d", status)
	}

	return &StreamResult{
		StreamURL:             unrestricted.Download,
		Filename:              unrestricted.Filename,
		Filesize:              unrestricted.Filesize,
		SupportsRangeRequests: supportsRangeRequests(unrestricted.Host),
	}, torrentID, nil
}

func (c *Client) pollTorrentLinks(ctx context.Context, torrentID, apiKey string) (*torrentInfo, error) {
	deadline := time.Now().Add(maxPollTime)
	var updated torrentInfo

	for time.Now().Before(deadline) {
		if err := sleepCtx(ctx, pollInterval); err != nil {
			return nil, err
		}
		if err := c.request(ctx, http.MethodGet, "/torrents/info/"+torrentID, apiKey, nil, &updated); err != nil {
			return nil, err
		}
		if len(updated.Links) > 0 {
			return &updated, nil
		}
		if updated.Status == "downloaded" && len(updated.Links) == 0 {
			break
		}
		if updated.Status == "magnet_error" {
			break
		}
	}

	elapsed := int(maxPollTime.Seconds())
	status := updated.Status
	if status == "" {
		status = "unknown"
	}
	return nil, fmt.Errorf("no download links available after %ds. Torrent status: %s", elapsed, status)
}

func (c *Client) deleteTorrent(ctx context.Context, torrentID, apiKey string) error {
	return c.request(ctx, http.MethodDelete, "/torrents/delete/"+torrentID, apiKey, nil, nil)
}

func (c *Client) deleteExistingTorrentByHash(ctx context.Context, magnetHash, apiKey string) error {
	var torrents []torrentListItem
	if err := c.request(ctx, http.MethodGet, "/torrents?limit=100", apiKey, nil, &torrents); err != nil {
		return err
	}
	lowerHash := strings.ToLower(magnetHash)
	for _, t := range torrents {
		if strings.EqualFold(t.Hash, lowerHash) {
			_ = c.deleteTorrent(ctx, t.ID, apiKey)
		}
	}
	return nil
}

func (c *Client) validateStreamURL(ctx context.Context, streamURL string) (bool, int, error) {
	status, err := c.probeRequest(ctx, http.MethodHead, streamURL, nil)
	if err == nil {
		if status == 200 || status == 206 {
			return true, status, nil
		}
		if status != 405 && status != 501 {
			return false, status, nil
		}
	}

	headers := http.Header{"Range": []string{"bytes=0-0"}}
	status, err = c.probeRequest(ctx, http.MethodGet, streamURL, headers)
	if err != nil {
		return false, 0, err
	}
	if status == 200 || status == 206 {
		return true, status, nil
	}
	return false, status, nil
}

func (c *Client) probeRequest(ctx context.Context, method, rawURL string, headers http.Header) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return 0, err
	}
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (c *Client) request(ctx context.Context, method, path, apiKey string, form url.Values, out interface{}) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out == nil || resp.StatusCode == http.StatusNoContent || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("invalid JSON from Real-Debrid: %w", err)
	}
	return nil
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if isRateLimitError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	patterns := []string{
		"real-debrid api error: 5",
		"timeout",
		"deadline",
		"connection reset",
		"connection refused",
		"no download links available",
		"failed to unrestrict link",
		"no files found in torrent",
		"magnet error",
		"no video files found",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func isRateLimitError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429
	}
	return strings.Contains(strings.ToLower(err.Error()), "429")
}

func extractMagnetHash(magnet string) string {
	const prefix = "urn:btih:"
	lower := strings.ToLower(magnet)
	idx := strings.Index(lower, prefix)
	if idx < 0 {
		if match := strings.Index(lower, "btih:"); match >= 0 {
			hash := magnet[match+5:]
			if end := strings.IndexAny(hash, "&?"); end >= 0 {
				hash = hash[:end]
			}
			return strings.ToLower(hash)
		}
		return ""
	}
	hash := magnet[idx+len(prefix):]
	if end := strings.IndexAny(hash, "&?"); end >= 0 {
		hash = hash[:end]
	}
	return strings.ToLower(hash)
}

func largestVideoFile(files []rdFile) *rdFile {
	var best *rdFile
	for i := range files {
		f := &files[i]
		if !isVideoFile(f.Path) {
			continue
		}
		if best == nil || f.Bytes > best.Bytes {
			best = f
		}
	}
	return best
}

func isVideoFile(path string) bool {
	lower := strings.ToLower(path)
	exts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".mpg", ".mpeg", ".ogv", ".ts", ".m2ts"}
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func supportsRangeRequests(host string) bool {
	lower := strings.ToLower(host)
	return strings.Contains(lower, "real-debrid.com") || strings.Contains(lower, "rdeb.io") || strings.Contains(lower, "rdb.io")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ValidateAPIKey checks whether an API key is accepted by Real-Debrid.
func (c *Client) ValidateAPIKey(ctx context.Context, apiKey string) (map[string]interface{}, error) {
	var user map[string]interface{}
	if err := c.request(ctx, http.MethodGet, "/user", apiKey, nil, &user); err != nil {
		return nil, err
	}
	return user, nil
}
