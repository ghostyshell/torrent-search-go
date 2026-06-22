package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"torrent-search-go/internal/config"
)

// DashboardAuthMiddleware gates monitoring endpoints behind DASHBOARD_PASSWORD.
type DashboardAuthMiddleware struct {
	password string
}

// NewDashboardAuthMiddleware creates dashboard password middleware.
func NewDashboardAuthMiddleware(cfg *config.Config) *DashboardAuthMiddleware {
	return &DashboardAuthMiddleware{password: cfg.Security.DashboardPassword}
}

// RequireDashboardAuth validates the dashboard password when configured.
func (m *DashboardAuthMiddleware) RequireDashboardAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.password == "" {
			next.ServeHTTP(w, r)
			return
		}

		provided := getDashboardPassword(r)
		if provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(m.password)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":"Unauthorized: dashboard password required","code":"DASHBOARD_AUTH_REQUIRED"}`))
	})
}

func getDashboardPassword(r *http.Request) string {
	if header := r.Header.Get("X-Dashboard-Password"); header != "" {
		return header
	}

	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	for _, part := range strings.Split(r.Header.Get("Cookie"), ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "dashboard_auth=") {
			return strings.TrimPrefix(part, "dashboard_auth=")
		}
	}

	return ""
}
