package middleware

import (
	"net/http"
	"strings"

	"torrent-search-go/internal/config"
)

// CORS handles Cross-Origin Resource Sharing
type CORS struct {
	origins        []string
	credentials    bool
	methods        []string
	allowedHeaders []string
	exposedHeaders []string
}

// NewCORS creates a new CORS middleware
func NewCORS(cfg config.CORSConfig) *CORS {
	return &CORS{
		origins:        cfg.Origins,
		credentials:    cfg.Credentials,
		methods:        cfg.Methods,
		allowedHeaders: cfg.AllowedHeaders,
		exposedHeaders: cfg.ExposedHeaders,
	}
}

// Middleware returns the CORS handler
func (c *CORS) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowedOrigin := c.isOriginAllowed(origin)

		if allowedOrigin {
			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", origin)

			if c.credentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Set exposed headers
			if len(c.exposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(c.exposedHeaders, ", "))
			}
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			// Set allowed headers
			if len(c.allowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(c.allowedHeaders, ", "))
			}

			// Set allowed methods
			if len(c.methods) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(c.methods, ", "))
			}

			// Set max age
			w.Header().Set("Access-Control-Max-Age", "86400")

			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if the origin is in the allowed list.
// When credentials are enabled, "*" is never treated as a wildcard: reflecting
// an arbitrary origin alongside Access-Control-Allow-Credentials: true lets
// any website issue authenticated cross-origin requests. Require an explicit
// origin match instead.
func (c *CORS) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for _, o := range c.origins {
		if c.credentials && o == "*" {
			continue // wildcard unsafe with credentials; require explicit match
		}
		if o == "*" {
			return true
		}
		if o == origin {
			return true
		}
	}

	return false
}

// WithCORS returns a router option to add CORS middleware
func WithCORS(cfg config.CORSConfig) RouterOption {
	return func(r *Router) {
		cors := NewCORS(cfg)
		r.useMiddleware(cors.Middleware)
	}
}
