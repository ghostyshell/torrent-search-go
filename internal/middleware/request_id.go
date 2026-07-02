package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader is the header used for request correlation.
const RequestIDHeader = "X-Request-Id"

// requestIDKey is the context key for the request ID.
type requestIDKey struct{}

// RequestID returns middleware that assigns a unique request ID to each request.
// If the client sends an X-Request-Id header, that value is reused; otherwise
// a random 16-byte hex ID is generated. The ID is added to the response header
// and to the request context so downstream loggers can include it.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if id == "" {
				id = generateRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetRequestID returns the request ID stored in the request context, or "" if absent.
func GetRequestID(r *http.Request) string {
	if id, ok := r.Context().Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GetRequestIDFromContext returns the request ID stored in the context, or "" if absent.
func GetRequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
