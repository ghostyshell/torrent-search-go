package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Router is a simple HTTP router with middleware support
type Router struct {
	mux        *http.ServeMux
	middleware []func(http.Handler) http.Handler
	logger     *Logger
	apiTracker func(method, path string, statusCode int, duration time.Duration, userAgent string)
	mu         sync.RWMutex
}

// RouterOption is a function that configures a Router
type RouterOption func(*Router)

// NewRouter creates a new router with the given options
func NewRouter(opts ...RouterOption) *Router {
	r := &Router{
		mux:        http.NewServeMux(),
		middleware: make([]func(http.Handler) http.Handler, 0),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// WithLogger sets the logger for the router
func WithLogger(logger *Logger) RouterOption {
	return func(r *Router) {
		r.logger = logger
		r.useMiddleware(r.requestLogger())
	}
}

// WithAPITracker records API usage for the monitoring dashboard.
func WithAPITracker(tracker func(method, path string, statusCode int, duration time.Duration, userAgent string)) RouterOption {
	return func(r *Router) {
		r.apiTracker = tracker
	}
}

// WithTrustProxy configures the router to trust proxy headers. When true,
// clientIP / isHTTPS honor X-Forwarded-For / X-Forwarded-Proto; when false
// (default) direct clients cannot spoof their IP or scheme.
func WithTrustProxy(trustProxy bool) RouterOption {
	return func(r *Router) {
		SetTrustProxy(trustProxy)
	}
}

// WithSecurityHeaders adds security-header middleware.
func WithSecurityHeaders() RouterOption {
	return func(r *Router) {
		r.useMiddleware(SecurityHeaders())
	}
}

// WithRequestID adds request-ID middleware.
func WithRequestID() RouterOption {
	return func(r *Router) {
		r.useMiddleware(RequestID())
	}
}

// WithDDoSGuard adds DDoS detection and IP-blocking middleware.
func WithDDoSGuard(guard *DDoSGuard) RouterOption {
	return func(r *Router) {
		if guard == nil {
			return
		}
		r.useMiddleware(guard.Middleware())
	}
}

// WithRateLimiter adds API and auth rate-limiting middleware.
func WithRateLimiter(limiter *RateLimiter) RouterOption {
	return func(r *Router) {
		if limiter == nil {
			return
		}
		r.useMiddleware(limiter.APILimiter())
	}
}

// WithMaxBodySize adds a global request-body cap (MaxBytesReader) to defend
// against unbounded upload / JSON-bomb memory exhaustion.
func WithMaxBodySize(maxBytes int64) RouterOption {
	return func(r *Router) {
		if maxBytes <= 0 {
			return
		}
		r.useMiddleware(MaxBodySize(maxBytes))
	}
}

// useMiddleware adds middleware to the chain
func (r *Router) useMiddleware(mw func(http.Handler) http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, mw)
}

// ServeHTTP implements http.Handler
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	middleware := make([]func(http.Handler) http.Handler, len(r.middleware))
	copy(middleware, r.middleware)
	r.mu.RUnlock()

	// Build middleware chain
	var handler http.Handler = r.mux
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}

	handler.ServeHTTP(w, req)
}

// redactPath replaces the :config segment (which carries debrid API keys) in
// Stremio/Magnetio addon URLs with a placeholder before logging or tracking.
// Without this, every catalog/meta hit writes the user's Real-Debrid / TPDB /
// StashDB keys into app.log, all.log and the api_usage collection.
func redactPath(p string) string {
	for _, prefix := range []string{"/stremio/", "/magnetio/"} {
		if strings.HasPrefix(p, prefix) {
			rest := strings.TrimPrefix(p, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				if len(parts) == 1 {
					return prefix + ":redacted"
				}
				return prefix + ":redacted/" + parts[1]
			}
		}
	}
	return p
}

// requestLogger creates middleware for logging requests
func (r *Router) requestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			start := time.Now()
			rw := &ResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rw, req)

			duration := time.Since(start)
			if r.logger != nil {
				r.logger.LogRequest(req.Method, redactPath(req.URL.Path), rw.statusCode, duration, slog.String("requestId", GetRequestID(req)))
			}
			if r.apiTracker != nil {
				r.apiTracker(req.Method, redactPath(req.URL.RequestURI()), rw.statusCode, duration, req.UserAgent())
			}
		})
	}
}

// Get registers a GET handler
func (r *Router) Get(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	r.Handle(pattern, handlerFunc, http.MethodGet, middlewares...)
}

// Post registers a POST handler
func (r *Router) Post(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	r.Handle(pattern, handlerFunc, http.MethodPost, middlewares...)
}

// Put registers a PUT handler
func (r *Router) Put(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	r.Handle(pattern, handlerFunc, http.MethodPut, middlewares...)
}

// Delete registers a DELETE handler
func (r *Router) Delete(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	r.Handle(pattern, handlerFunc, http.MethodDelete, middlewares...)
}

// Patch registers a PATCH handler
func (r *Router) Patch(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	r.Handle(pattern, handlerFunc, http.MethodPatch, middlewares...)
}

// Options registers an OPTIONS handler
func (r *Router) Options(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	r.Handle(pattern, handlerFunc, http.MethodOptions, middlewares...)
}

// All registers a handler for all HTTP methods
func (r *Router) All(pattern string, handlerFunc http.HandlerFunc, middlewares ...func(http.Handler) http.Handler) {
	// Register for all common HTTP methods to support Go 1.21+ ServeMux
	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodHead,
		http.MethodOptions,
	}
	for _, method := range methods {
		r.Handle(pattern, handlerFunc, method, middlewares...)
	}
}

// Handle registers a handler for the given pattern and method
func (r *Router) Handle(pattern string, handlerFunc http.HandlerFunc, method string, middlewares ...func(http.Handler) http.Handler) {
	// Apply route-specific middleware first
	var handler http.Handler = handlerFunc
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}

	// Wrap with method check
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method {
			handler.ServeHTTP(w, r)
		} else if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"success":false,"error":"Method not allowed"}`))
		}
	})

	// Translate Express-style patterns (":param", trailing ":param?") into the
	// Go 1.22 ServeMux wildcard syntax ("{param}") and register each resulting
	// pattern with the HTTP method prefix. Handlers still parse params from the
	// path via middleware.ExtractParams, so wildcard names only need to route.
	for _, muxPattern := range toServeMuxPatterns(pattern) {
		r.mux.HandleFunc(method+" "+muxPattern, finalHandler.ServeHTTP)
	}
}

// toServeMuxPatterns converts an Express-style route pattern to one or more
// Go ServeMux patterns. A trailing optional segment (":page?") expands into
// two patterns: with and without that segment.
func toServeMuxPatterns(pattern string) []string {
	segs := strings.Split(pattern, "/")
	optionalTail := false
	for i, s := range segs {
		if !strings.HasPrefix(s, ":") {
			continue
		}
		name := strings.TrimPrefix(s, ":")
		if strings.HasSuffix(name, "?") {
			name = strings.TrimSuffix(name, "?")
			if i == len(segs)-1 {
				optionalTail = true
			}
		}
		segs[i] = "{" + name + "}"
	}
	full := strings.Join(segs, "/")
	if optionalTail {
		base := strings.Join(segs[:len(segs)-1], "/")
		return []string{full, base}
	}
	return []string{full}
}

// NotFoundHandler returns a 404 handler
func (r *Router) NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"success":false,"error":"Not found"}`))
	}
}

// ErrorHandler returns an error handler
func (r *Router) ErrorHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false,"error":"Internal server error"}`))
	}
}

// Static serves static files from a directory
func (r *Router) Static(pattern string, directory string) {
	fs := http.StripPrefix(pattern, http.FileServer(http.Dir(directory)))
	r.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	})
}

// URLParams extracts URL parameters from a pattern
type URLParams struct {
	params map[string]string
}

// Get retrieves a URL parameter by name
func (p *URLParams) Get(name string) string {
	if p.params == nil {
		return ""
	}
	return p.params[name]
}

// ExtractParams extracts parameters from a URL path based on a pattern
func ExtractParams(pattern, path string) *URLParams {
	params := make(map[string]string)

	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return &URLParams{params: params}
	}

	for i, part := range patternParts {
		if strings.HasPrefix(part, ":") {
			// Strip the ":" prefix and any trailing "?" used to mark an
			// optional segment (e.g. ":page?") so lookups use the bare name.
			key := strings.TrimSuffix(strings.TrimPrefix(part, ":"), "?")
			if i < len(pathParts) {
				params[key] = pathParts[i]
			}
		}
	}

	return &URLParams{params: params}
}

// ContextKey is a type for context keys
type ContextKey string

const (
	// ParamsKey is the context key for URL parameters
	ParamsKey ContextKey = "params"
)
