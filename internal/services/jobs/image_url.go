package jobs

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var reMdExt = regexp.MustCompile(`(?i)\.md(\.[^.]+)$`)

// higherResolutionURL mirrors Node descriptionImageCacheService.getHigherResolutionUrl.
func higherResolutionURL(directURL string) string {
	switch {
	case strings.Contains(directURL, "trafficimage.club"):
		return reMdExt.ReplaceAllString(directURL, "$1")
	case strings.Contains(directURL, "postimg.cc"):
		u := directURL
		u = regexp.MustCompile(`/([st]\d+x\d+)/`).ReplaceAllString(u, "/")
		u = strings.ReplaceAll(u, "_thumb.", ".")
		u = regexp.MustCompile(`\?[^=]*thumb[^=]*=[^&]*(&|$)`).ReplaceAllString(u, "")
		return u
	case strings.Contains(directURL, "ibb.co"):
		return regexp.MustCompile(`/([st]\d+x\d+)/`).ReplaceAllString(directURL, "/")
	case strings.Contains(directURL, "imgur.com"):
		u := regexp.MustCompile(`(?i)[sbtlmh]\.jpg$`).ReplaceAllString(directURL, ".jpg")
		return strings.Replace(u, ".jpg", "h.jpg", 1)
	case strings.Contains(directURL, "fastpic.org"):
		return strings.Replace(directURL, "/thumbs/", "/big/", 1)
	default:
		u := directURL
		u = reMdExt.ReplaceAllString(u, "$1")
		u = regexp.MustCompile(`(?i)_thumb(\.[^.]+)$`).ReplaceAllString(u, "$1")
		u = regexp.MustCompile(`(?i)_small(\.[^.]+)$`).ReplaceAllString(u, "$1")
		u = regexp.MustCompile(`(?i)_medium(\.[^.]+)$`).ReplaceAllString(u, "$1")
		u = regexp.MustCompile(`(?i)\.thumb(\.[^.]+)$`).ReplaceAllString(u, "$1")
		return u
	}
}

func validateImageURL(ctx context.Context, client *http.Client, url string) bool {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
