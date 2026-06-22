package middleware

import (
	"net"
	"net/http"
	"strings"
)

// IPAllowlistMiddleware restricts access based on IP address
type IPAllowlistMiddleware struct {
	allowlist []string
	networks  []*net.IPNet
}

// NewIPAllowlistMiddleware creates a new IP allowlist middleware
func NewIPAllowlistMiddleware(allowlist []string) *IPAllowlistMiddleware {
	middleware := &IPAllowlistMiddleware{
		allowlist: allowlist,
		networks:  make([]*net.IPNet, 0),
	}

	// Parse CIDR ranges
	for _, entry := range allowlist {
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err == nil {
				middleware.networks = append(middleware.networks, network)
			}
		}
	}

	return middleware
}

// RequireIPAllowlist returns middleware that restricts access to allowed IPs
func (m *IPAllowlistMiddleware) RequireIPAllowlist(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If no allowlist configured, allow access (relies on other auth)
		if len(m.allowlist) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := m.getClientIP(r)

		if !m.isIPAllowed(clientIP) {
			http.Error(w, `{"success":false,"error":"Access denied: IP address not allowed","code":"IP_NOT_ALLOWED"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getClientIP extracts the client IP from the request
func (m *IPAllowlistMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		// X-Forwarded-For can contain multiple IPs: client, proxy1, proxy2, ...
		// The first one is the original client
		ips := strings.Split(forwardedFor, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return strings.TrimSpace(realIP)
	}

	// Fall back to direct connection IP
	ip := r.RemoteAddr
	// Remove port if present
	if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
		ip = ip[:colonIdx]
	}
	return ip
}

// isIPAllowed checks if the IP is in the allowlist
func (m *IPAllowlistMiddleware) isIPAllowed(ip string) bool {
	if ip == "" {
		return false
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check exact matches
	for _, allowed := range m.allowlist {
		// Skip CIDR entries (handled separately)
		if strings.Contains(allowed, "/") {
			continue
		}
		if ip == allowed {
			return true
		}
	}

	// Check CIDR ranges
	for _, network := range m.networks {
		if network.Contains(parsedIP) {
			return true
		}
	}

	return false
}

// SetAllowlist updates the allowlist dynamically
func (m *IPAllowlistMiddleware) SetAllowlist(allowlist []string) {
	m.allowlist = allowlist
	// Re-parse networks
	m.networks = make([]*net.IPNet, 0)
	for _, entry := range allowlist {
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err == nil {
				m.networks = append(m.networks, network)
			}
		}
	}
}
