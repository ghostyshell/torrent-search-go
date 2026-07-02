package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient wraps an http.Client for stream-provider fetches.
type HTTPClient struct {
	Client *http.Client
}

// NewHTTPClient creates a streams HTTP client.
func NewHTTPClient(client *http.Client) *HTTPClient {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPClient{Client: client}
}

// GetJSON performs a GET and decodes JSON.
func (c *HTTPClient) GetJSON(ctx context.Context, url string, dest interface{}) error {
	data, err := c.GetText(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// GetText fetches a URL as text with browser-like headers (direct only).
func (c *HTTPClient) GetText(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// FollowRedirect returns the final URL after following redirects.
func (c *HTTPClient) FollowRedirect(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Request.URL.String(), nil
}
