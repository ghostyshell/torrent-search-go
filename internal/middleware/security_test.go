package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"torrent-search-go/internal/config"
)

func newTestRateLimiter(apiMax, authMax int) *RateLimiter {
	rl := NewRateLimiter(config.RateLimitingConfig{
		Enabled:  true,
		WindowMs: 15 * time.Minute,
		Max:      apiMax,
	}, "")
	rl.authMax = authMax
	return rl
}

func TestRateLimiter_APIAndAuthCountersAreSeparate(t *testing.T) {
	rl := newTestRateLimiter(5, 2)
	ip := "203.0.113.10"

	for i := 0; i < 5; i++ {
		if rl.isLimited(rl.apiRequests, ip, rl.max) {
			t.Fatalf("api request %d should not be limited yet", i+1)
		}
	}
	if !rl.isLimited(rl.apiRequests, ip, rl.max) {
		t.Fatal("expected api limiter to trip after max requests")
	}

	for i := 0; i < 2; i++ {
		if rl.isLimited(rl.authRequests, ip, rl.authMax) {
			t.Fatalf("auth request %d should not be limited yet", i+1)
		}
	}
	if !rl.isLimited(rl.authRequests, ip, rl.authMax) {
		t.Fatal("expected auth limiter to trip after max auth requests")
	}
}

func TestRateLimiter_APILimiterSkipsAuthPaths(t *testing.T) {
	rl := newTestRateLimiter(1, 1)
	handler := rl.APILimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/auth/validate", "/api/auth/google"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "203.0.113.10:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected auth path %s to bypass api limiter, got %d", path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring/dashboard", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first monitoring request should pass, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected monitoring request to hit api limiter, got %d", rec.Code)
	}
}

func TestRateLimiter_AuthLimiterDoesNotCountAPIRequests(t *testing.T) {
	rl := newTestRateLimiter(2, 2)
	apiHandler := rl.APILimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	authHandler := rl.AuthLimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/monitoring/dashboard", nil)
		req.RemoteAddr = "203.0.113.42:1234"
		rec := httptest.NewRecorder()
		apiHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("monitoring request %d should pass, got %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/validate", nil)
	req.RemoteAddr = "203.0.113.42:1234"
	rec := httptest.NewRecorder()
	authHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth request should still pass after api traffic, got %d", rec.Code)
	}
}
