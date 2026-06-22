package streams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"torrent-search-go/internal/services/flaresolverr"
)

// HTTPClient wraps an http.Client with FlareSolverr fallback.
type HTTPClient struct {
	Client          *http.Client
	FlareSolverrURL string
}

// NewHTTPClient creates a streams HTTP client.
func NewHTTPClient(client *http.Client, flareSolverrURL string) *HTTPClient {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPClient{Client: client, FlareSolverrURL: flareSolverrURL}
}

// GetJSON performs a GET and decodes JSON.
func (c *HTTPClient) GetJSON(ctx context.Context, url string, dest interface{}) error {
	data, err := c.GetText(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// GetText fetches a URL as text, falling back to FlareSolverr on block.
func (c *HTTPClient) GetText(ctx context.Context, url string) ([]byte, error) {
	if c.FlareSolverrURL != "" {
		data, err := c.getViaFlareSolverr(ctx, url)
		if err == nil {
			return data, nil
		}
	}
	return c.getDirect(ctx, url)
}

func (c *HTTPClient) getDirect(ctx context.Context, url string) ([]byte, error) {
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

type flareSolverrRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout,omitempty"`
}

type flareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		Response string `json:"response"`
	} `json:"solution"`
}

func (c *HTTPClient) getViaFlareSolverr(ctx context.Context, url string) ([]byte, error) {
	if err := flaresolverr.Acquire(ctx); err != nil {
		return nil, err
	}
	defer flaresolverr.Release()

	body, err := json.Marshal(flareSolverrRequest{Cmd: "request.get", URL: url, MaxTimeout: 60000})
	if err != nil {
		return nil, err
	}
	fsURL := c.FlareSolverrURL
	if !bytes.HasSuffix([]byte(fsURL), []byte("/v1")) {
		fsURL += "/v1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fsURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flaresolverr returned %d", resp.StatusCode)
	}
	var fsResp flareSolverrResponse
	if err := json.NewDecoder(resp.Body).Decode(&fsResp); err != nil {
		return nil, err
	}
	if fsResp.Status != "ok" {
		return nil, fmt.Errorf("flaresolverr error: %s", fsResp.Message)
	}
	return []byte(fsResp.Solution.Response), nil
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
