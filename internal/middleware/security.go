package middleware

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/config"
)

// trustProxyHeaders gates whether clientIP / isHTTPS honor X-Forwarded-*
// headers. Set once at startup from cfg.Security.TrustProxy. Default false
// so direct-to-origin clients cannot spoof their IP past rate limiting or
// the DDoS guard.
var trustProxyHeaders bool

// SetTrustProxy configures proxy-header trust for the package. Call once at
// startup; not safe to flip at runtime.
func SetTrustProxy(trust bool) { trustProxyHeaders = trust }

// SecurityHeaders returns middleware that sets HTTP security headers.
// CSP is disabled so the monitoring dashboard inline scripts keep working.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := w.Header()
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-Frame-Options", "SAMEORIGIN")
			headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			headers.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			if r.TLS != nil || isHTTPS(r) {
				headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isHTTPS(r *http.Request) bool {
	if trustProxyHeaders && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	if r.URL.Scheme == "https" {
		return true
	}
	return false
}

// RateLimiter is an IP-based sliding-window rate limiter with separate
// counters for general API traffic and auth endpoints.
type RateLimiter struct {
	cfg          config.RateLimitingConfig
	skipToken    string
	apiRequests  map[string][]time.Time
	authRequests map[string][]time.Time
	mu           sync.Mutex
	window       time.Duration
	max          int
	authMax      int
	msg          map[string]interface{}
	authMsg      map[string]interface{}
}

// NewRateLimiter creates a production-ready rate limiter.
// If rate limiting is disabled in config, the returned limiter is a no-op.
func NewRateLimiter(cfg config.RateLimitingConfig, addonAPIToken string) *RateLimiter {
	window := cfg.WindowMs
	if window <= 0 {
		window = 15 * time.Minute
	}
	max := cfg.Max
	if max <= 0 {
		max = 1000
	}
	authMax := 100
	if authMax > max {
		authMax = max
	}
	return &RateLimiter{
		cfg:          cfg,
		skipToken:    addonAPIToken,
		apiRequests:  make(map[string][]time.Time),
		authRequests: make(map[string][]time.Time),
		window:       window,
		max:          max,
		authMax:      authMax,
		msg: map[string]interface{}{
			"success": false,
			"error":   "Too many requests, please try again later.",
			"code":    "RATE_LIMITED",
		},
		authMsg: map[string]interface{}{
			"success": false,
			"error":   "Too many auth requests, please try again later.",
			"code":    "RATE_LIMITED",
		},
	}
}

// APILimiter returns middleware that rate-limits /api/ routes except health and auth.
func (rl *RateLimiter) APILimiter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.cfg.Enabled || rl.allow(r) || !isAPIPath(r.URL.Path) || isAuthPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			ip := clientIP(r)
			if rl.isLimited(rl.apiRequests, ip, rl.max) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(rl.msg)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthLimiter returns middleware that rate-limits /api/auth/ routes.
func (rl *RateLimiter) AuthLimiter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.cfg.Enabled || rl.allow(r) {
				next.ServeHTTP(w, r)
				return
			}
			ip := clientIP(r)
			if rl.isLimited(rl.authRequests, ip, rl.authMax) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(rl.authMsg)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// allow returns true when the request should bypass rate limiting.
func (rl *RateLimiter) allow(r *http.Request) bool {
	path := r.URL.Path
	if path == "/health" || strings.HasPrefix(path, "/health/") {
		return true
	}
	// Trust authenticated addon traffic (Bearer or X-Addon-Token), matching Node.
	if MatchesAddonToken(r, rl.skipToken) {
		return true
	}
	return false
}

func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func isAuthPath(path string) bool {
	return path == "/api/auth" || strings.HasPrefix(path, "/api/auth/")
}

// isLimited records a request and returns true if the IP is over its budget.
func (rl *RateLimiter) isLimited(requests map[string][]time.Time, ip string, max int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)
	var recent []time.Time
	for _, t := range requests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	requests[ip] = recent
	return len(recent) > max
}

// clientIP returns the client IP. X-Forwarded-For / X-Real-Ip are only
// honored when trustProxyHeaders is true (set from cfg.Security.TrustProxy),
// otherwise a direct client cannot spoof its IP past rate limiting / DDoS.
func clientIP(r *http.Request) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				ip := strings.TrimSpace(parts[0])
				if net.ParseIP(ip) != nil {
					return ip
				}
			}
		}
		if xri := r.Header.Get("X-Real-Ip"); xri != "" {
			if net.ParseIP(xri) != nil {
				return xri
			}
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "" {
		return host
	}
	return r.RemoteAddr
}

// Cleanup periodically prunes stale rate-limit entries.
func (rl *RateLimiter) Cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			rl.mu.Lock()
			cutoff := time.Now().Add(-rl.window)
			pruneRateLimitEntries(rl.apiRequests, cutoff)
			pruneRateLimitEntries(rl.authRequests, cutoff)
			rl.mu.Unlock()
		}
	}()
}

func pruneRateLimitEntries(requests map[string][]time.Time, cutoff time.Time) {
	for ip, times := range requests {
		var recent []time.Time
		for _, t := range times {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(requests, ip)
		} else {
			requests[ip] = recent
		}
	}
}

// NoContentJSON writes a JSON 405 response used by the router when a method is not allowed.
func NoContentJSON(w http.ResponseWriter, status int, body map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// SetRateRateLimitHeaders sets the standard RateLimit headers on a response.
func SetRateLimitHeaders(w http.ResponseWriter, limit, remaining, reset int) {
	header := w.Header()
	header.Set("RateLimit-Limit", fmt.Sprintf("%d", limit))
	header.Set("RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	header.Set("RateLimit-Reset", fmt.Sprintf("%d", reset))
}

// MaxBodySize returns middleware that caps every request body at maxBytes,
// defending against JSON-bomb / unbounded-upload memory exhaustion. The cap
// applies to all routes; handlers that genuinely need larger bodies can wrap
// r.Body back up, but the default is bounded.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
