package hentai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const mamaBase = "https://hentaimama.io"

// userAgent rotates nothing - a single desktop UA is enough for this
// WordPress/DooPlay site (no Cloudflare JS challenge observed).
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// httpClient wraps a configured *http.Client with the helpers the scrapers share.
type httpClient struct{ c *http.Client }

func newHTTPClient(c *http.Client) *httpClient {
	if c == nil {
		c = &http.Client{Timeout: 20 * time.Second}
	}
	return &httpClient{c: c}
}

// get fetches url with a desktop UA and returns the body bytes. referer, if
// non-empty, sets the Referer header (the episode page for the AJAX fallback).
func (h *httpClient) get(ctx context.Context, url, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json,*/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	res, err := h.c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, errStatus(res.StatusCode)
	}
	return io.ReadAll(res.Body)
}

type statusError struct{ code int }

func (e statusError) Error() string { return "hentai: HTTP " + itoa(e.code) }
func errStatus(code int) error      { return statusError{code: code} }

// isStatus reports whether err is a statusError with one of the given codes.
func isStatus(err error, codes ...int) bool {
	var se statusError
	if !errors.As(err, &se) {
		return false
	}
	for _, c := range codes {
		if se.code == c {
			return true
		}
	}
	return false
}

// postForm POSTs url-encoded form values to url and returns the body bytes.
// referer, if non-empty, sets the Referer header (the episode page for DooPlay's
// get_player_contents AJAX). Used by the HentaiMama stream resolver.
func (h *httpClient) postForm(ctx context.Context, url string, form url.Values, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	res, err := h.c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, errStatus(res.StatusCode)
	}
	return io.ReadAll(res.Body)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}