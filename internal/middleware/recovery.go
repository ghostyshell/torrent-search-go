package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// Recovery middleware handles panics gracefully
func Recovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic
					stack := debug.Stack()
					log.Printf("Panic recovered: error=%v, path=%s, method=%s, stack=%s",
						err, r.URL.Path, r.Method, string(stack))

					// Return 500 error
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"success":false,"error":"Internal server error"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// WithRecovery returns a router option to add recovery middleware
func WithRecovery() RouterOption {
	return func(r *Router) {
		r.useMiddleware(Recovery())
	}
}
