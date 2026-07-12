package scraper

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// MaxScrapeBytes caps how many bytes a scraper will read from any single
// upstream response. Defends against compromised/hos exhaust-the-memory pages.
const MaxScrapeBytes = 8 << 20 // 8 MiB

// NewSafeClient returns an *http.Client hardened against SSRF and unbounded
// responses. It:
//   - refuses to dial private / loopback / link-local / metadata IPs at the
//     transport level (defeats user-supplied internal URLs and DNS rebinding),
//   - follows redirects only to safe hosts (same defense at redirect time),
//   - caps every response body at MaxScrapeBytes.
//
// All outbound scraper traffic should go through a client built here.
func NewSafeClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialContext = safeDialContext(base.DialContext)

	rt := &limitBodyTransport{base: base}

	return &http.Client{
		Timeout:       timeout,
		Transport:     rt,
		CheckRedirect: safeCheckRedirect,
	}
}

// safeCheckRedirect rejects any redirect whose URL resolves to an unsafe host.
func safeCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if err := assertSafeURL(req.URL); err != nil {
		return fmt.Errorf("redirect to unsafe host %q: %w", req.URL.Host, err)
	}
	return nil
}

// safeDialContext wraps a base dialer and aborts any connection whose resolved
// address is private / loopback / link-local / unspecified. This is the
// authoritative SSRF defense: even if a URL passes parsing, the actual TCP
// dial is refused.
func safeDialContext(next func(ctx context.Context, network, addr string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	if next == nil {
		next = (&net.Dialer{Timeout: 15 * time.Second}).DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ip := resolveHost(ctx, host)
		if ip == nil {
			return nil, fmt.Errorf("ssrf: could not resolve %q", host)
		}
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("ssrf: refusing to dial private address %s (%s)", ip, host)
		}
		// ponytail: pin the resolved IP so a rebinding attack between resolve
		// and connect cannot redirect to a private address.
		return next(ctx, network, net.JoinHostPort(ip.String(), port))
	}
}

func resolveHost(ctx context.Context, host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(ips) == 0 {
		return nil
	}
	return ips[0].IP
}

// isPublicIP reports whether ip is safe for outbound scraper traffic.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Block IPv4-mapped IPv6 of private ranges too.
	if v4 := ip.To4(); v4 != nil {
		return isPublicIPv4(v4)
	}
	return true
}

func isPublicIPv4(ip net.IP) bool {
	// CGNAT 100.64.0.0/10
	if ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127 {
		return false
	}
	return true
}

// assertSafeURL validates scheme + host before any dial. Fast-fail for clearly
// internal URLs; the dial-time check is the authoritative gate.
func assertSafeURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("nil url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return fmt.Errorf("host %s is not public", host)
	}
	// Block obvious metadata hostnames used by cloud instances, plus "localhost"
	// (the SafeClient catches localhost at dial time via the loopback IP check,
	// but stream URLs extracted from upstream HTML are never dialed by us - they
	// are emitted to Stremio which fetches them via proxyHeaders - so the hostname
	// must be rejected here, not at dial time).
	lowerHost := strings.ToLower(host)
	for _, bad := range []string{"metadata.google.internal", "metadata", "169.254.169.254", "localhost"} {
		if lowerHost == bad || strings.HasPrefix(lowerHost, bad+".") {
			return fmt.Errorf("host %s blocked", host)
		}
	}
	return nil
}

// limitBodyTransport wraps a base RoundTripper and replaces each response body
// with one capped at MaxScrapeBytes, so a hostile upstream cannot exhaust
// memory regardless of which scraper consumes the body.
type limitBodyTransport struct{ base http.RoundTripper }

func (t *limitBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := assertSafeURL(req.URL); err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &limitedReadCloser{
		Reader:     io.LimitReader(resp.Body, MaxScrapeBytes),
		Closer:     resp.Body,
		exceededAt: MaxScrapeBytes,
	}
	return resp, nil
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
	exceededAt int64
	read       int64
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.Reader.Read(p)
	l.read += int64(n)
	if err == io.EOF && l.read >= l.exceededAt {
		// Differentiate "we truncated" from "clean EOF" only if needed; the
		// underlying LimitReader already returns EOF at the cap.
	}
	return n, err
}

// ResolveSafeStreamURL resolves rawURL against baseURL and validates that the
// result is an http(s) URL pointing at a public (non-private / loopback /
// link-local / metadata) host. Stream URLs are extracted from upstream HTML /
// m3u8 and emitted to Stremio clients, which fetch them through
// behaviorHints.proxyHeaders.request - so a compromised upstream could otherwise
// inject an internal URL (169.254.169.254, localhost, 192.168.x.x, ...) that the
// end-user's Stremio streaming server would then fetch on the user's behalf. The
// backend never dials these URLs itself (the SafeClient does not cover this path),
// so they must be validated before they leave the addon. Returns the resolved
// absolute URL and ok=true, or ""/false if unsafe or unparseable.
//
// For a hostname (not a literal IP) the host is resolved and every resolved
// address must be public. assertSafeURL's hostname check is only a small
// denylist of known metadata names; without this resolve step an attacker who
// controls a DNS A record (evil.example.com -> 169.254.169.254) could pass
// assertSafeURL and emit a stream URL the user's Stremio would fetch against an
// internal/metadata service. Fails closed: a host that will not resolve is
// rejected, matching safeDialContext. Best-effort vs DNS rebinding between this
// resolve and the user's Stremio fetch.
func ResolveSafeStreamURL(rawURL, baseURL string) (string, bool) {
	if rawURL == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if err := assertSafeURL(resolved); err != nil {
		return "", false
	}
	host := resolved.Hostname()
	if net.ParseIP(host) == nil {
		lookupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ips, err := resolveStreamHost(lookupCtx, host)
		if err != nil || len(ips) == 0 {
			return "", false
		}
		for _, ip := range ips {
			if !isPublicIP(ip) {
				return "", false
			}
		}
	}
	return resolved.String(), true
}

// resolveStreamHost resolves a hostname to its IPs for the stream-URL SSRF
// guard. A package var so tests can stub DNS without touching the network.
var resolveStreamHost = func(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(ips))
	for _, a := range ips {
		out = append(out, a.IP)
	}
	return out, nil
}

// keep syscall import referenced on platforms that need it for dialer control.
var _ = syscall.ECONNREFUSED
