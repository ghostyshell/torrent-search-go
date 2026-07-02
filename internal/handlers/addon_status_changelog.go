package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/singleflight"

	"torrent-search-go/pkg/models"
)

// addon_status_changelog.go: parse a Keep-a-Changelog markdown file fetched from a
// public URL (set on the report as ChangelogSourceURL by the dashboard). The public
// GET path calls fetchChangelogFromURL when a report has a source URL set, and falls
// back to the stored manual Changelog on any fetch/parse error.
//
// SSRF defense: the URL is admin-set (dashboard-password-gated), but the public GET
// fetches it unauthenticated, so the http client refuses to dial private/loopback/
// link-local IPs at the socket layer (denyPrivateDial) and re-validates each redirect
// hop (CheckRedirect). denyPrivateDial is authoritative: it runs on the resolved
// address for every connection, so DNS-rebinding, alternate IP encodings, and
// redirects to internal hosts all fail there.

const (
	changelogCacheTTL = 5 * time.Minute
	changelogMaxBody  = 2 << 20 // 2 MiB
)

var changelogHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return validateChangelogURL(req.URL.String())
	},
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: denyPrivateDial,
		}).DialContext,
	},
}

// denyPrivateDial is the Control hook for the changelog HTTP client: it refuses to
// establish a connection to any private/loopback/link-local/unspecified IP. It sees
// the resolved address, so it defeats DNS-rebinding and IP-encoding tricks that a
// hostname-string check would miss.
func denyPrivateDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return errors.New("dial to private IP refused")
		}
	}
	return nil
}

type changelogCacheEntry struct {
	at      time.Time
	entries []models.AddonChangelog
	err     error
}

var (
	changelogCache sync.Map // url -> changelogCacheEntry
	changelogSF    singleflight.Group
)

var changelogVersionRe = regexp.MustCompile(`^##\s+\[([^\]]+)\]\s*(?:-\s*(.*))?$`)

// fetchChangelogFromURL fetches the markdown at url and parses it into changelog
// entries. Results (success or error) are cached for changelogCacheTTL so the
// unauthenticated read path can't be amplified into many outbound fetches; a
// singleflight collapses concurrent misses for the same URL into one fetch.
func fetchChangelogFromURL(ctx context.Context, url string) ([]models.AddonChangelog, error) {
	url = normalizeChangelogURL(url)
	if v, ok := changelogCache.Load(url); ok {
		if e, ok := v.(changelogCacheEntry); ok && time.Since(e.at) < changelogCacheTTL {
			return e.entries, e.err
		}
	}
	v, err, _ := changelogSF.Do(url, func() (interface{}, error) {
		// Re-check inside the single flight: another caller may have just populated it.
		if v, ok := changelogCache.Load(url); ok {
			if e, ok := v.(changelogCacheEntry); ok && time.Since(e.at) < changelogCacheTTL {
				return e, nil
			}
		}
		return fetchChangelogOnce(ctx, url)
	})
	if err != nil {
		return nil, err
	}
	e := v.(changelogCacheEntry)
	return e.entries, e.err
}

// normalizeChangelogURL accepts both the raw and the github.com blob/ web view of a
// file and returns the raw form the parser needs. github.com/{owner}/{repo}/blob/
// {ref}/{path} -> raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}. Non-GitHub
// and already-raw URLs pass through unchanged. Lets an admin paste the blob URL
// straight from the browser address bar.
func normalizeChangelogURL(url string) string {
	u, err := neturl.Parse(url)
	if err != nil || u.Host != "github.com" {
		return url
	}
	// blob URLs are /{owner}/{repo}/blob/{ref}/{path...}
	rest := strings.TrimPrefix(u.Path, "/")
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 || parts[2] != "blob" {
		return url
	}
	u.Host = "raw.githubusercontent.com"
	u.Path = "/" + parts[0] + "/" + parts[1] + "/" + parts[3]
	return u.String()
}

func fetchChangelogOnce(ctx context.Context, url string) (changelogCacheEntry, error) {
	storeErr := func(err error) (changelogCacheEntry, error) {
		e := changelogCacheEntry{at: time.Now(), err: err}
		changelogCache.Store(url, e)
		return e, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return storeErr(err)
	}
	req.Header.Set("User-Agent", "torrent-search-go-addon-status")
	resp, err := changelogHTTPClient.Do(req)
	if err != nil {
		return storeErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return storeErr(fmt.Errorf("changelog fetch: status %d", resp.StatusCode))
	}
	// Read one extra byte so we can detect (and reject) a body that exceeds the cap
	// instead of silently serving a truncated changelog.
	body, err := io.ReadAll(io.LimitReader(resp.Body, changelogMaxBody+1))
	if err != nil {
		return storeErr(err)
	}
	if len(body) > changelogMaxBody {
		return storeErr(errors.New("changelog fetch: body exceeds 2 MiB"))
	}
	entries := parseChangelogMarkdown(string(body))
	e := changelogCacheEntry{at: time.Now(), entries: entries}
	changelogCache.Store(url, e)
	return e, nil
}

// parseChangelogMarkdown parses Keep-a-Changelog markdown into versioned entries.
// It collects every "- "/"* " bullet under a "## [version] - date" heading (until
// the next such heading) as that version's highlights, prefixing each with its
// "### Subsection" label when present. Bold markers (**) are stripped so the site
// renders clean text.
func parseChangelogMarkdown(md string) []models.AddonChangelog {
	var entries []models.AddonChangelog
	var cur *models.AddonChangelog
	var subsection string

	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
			cur = nil
		}
	}

	for _, raw := range strings.Split(md, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "## "):
			if m := changelogVersionRe.FindStringSubmatch(line); m != nil {
				flush()
				cur = &models.AddonChangelog{Version: strings.TrimSpace(m[1])}
				if len(m) > 2 {
					cur.Date = strings.TrimSpace(m[2])
				}
				subsection = ""
				continue
			}
			subsection = "" // a non-version ## heading ends the current subsection
		case strings.HasPrefix(line, "### "):
			subsection = strings.TrimSpace(strings.TrimPrefix(line, "### "))
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			if cur == nil {
				continue
			}
			bullet := strings.ReplaceAll(strings.TrimSpace(line[2:]), "**", "")
			if bullet == "" {
				continue
			}
			if subsection != "" {
				bullet = "[" + subsection + "] " + bullet
			}
			cur.Highlights = append(cur.Highlights, bullet)
		}
	}
	flush()
	return entries
}

// validateChangelogURL rejects non-http(s) and obvious internal/private hosts at
// save time so a misconfigured URL fails fast with a clear error. denyPrivateDial is
// the authoritative check at fetch time; this is a fast pre-check + redirect guard.
func validateChangelogURL(url string) error {
	if url == "" {
		return nil
	}
	u, err := neturl.Parse(url)
	if err != nil {
		return errors.New("invalid changelog URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("changelog URL must be http or https")
	}
	if isPrivateHost(strings.ToLower(u.Hostname())) {
		return errors.New("changelog URL host is not allowed")
	}
	return nil
}

func isPrivateHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	return host == "localhost"
}