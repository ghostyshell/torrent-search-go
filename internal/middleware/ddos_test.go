package middleware

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestDDoSSkipRequest(t *testing.T) {
	const token = "addon-secret"
	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"health", httptestGet("/health"), true},
		{"stremio catalog", httptestGet("/stremio/default/catalog/Porn/xxx_top.json"), true},
		{"magnetio manifest", httptestGet("/magnetio/abc/manifest.json"), true},
		{"addon token header", httptestGetWithHeader("/api/cache/get/foo", "X-Addon-Token", token), true},
		{"public api", httptestGet("/api/monitoring/dashboard"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ddosSkipRequest(tc.req, token); got != tc.want {
				t.Fatalf("ddosSkipRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckDDoSLimits(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	limits := []DDoSLimit{
		{Name: "1m", Window: time.Minute, Threshold: 2},
		{Name: "1h", Window: time.Hour, Threshold: 4},
	}
	times := []time.Time{
		now.Add(-30 * time.Second),
		now.Add(-20 * time.Second),
		now.Add(-10 * time.Second),
	}

	lim, count, tripped := checkDDoSLimits(times, now, limits)
	if !tripped || lim.Name != "1m" || count != 3 {
		t.Fatalf("expected 1m trip at 3, got tripped=%v lim=%q count=%d", tripped, lim.Name, count)
	}

	slowSpam := []time.Time{
		now.Add(-50 * time.Minute),
		now.Add(-51 * time.Minute),
		now.Add(-52 * time.Minute),
		now.Add(-53 * time.Minute),
		now.Add(-54 * time.Minute),
	}
	lim, count, tripped = checkDDoSLimits(slowSpam, now, limits)
	if !tripped || lim.Name != "1h" || count != 5 {
		t.Fatalf("expected 1h trip at 5, got tripped=%v lim=%q count=%d", tripped, lim.Name, count)
	}

	counts := ipTrafficCounts(slowSpam, now, limits)
	if counts["1m"] != 0 || counts["1h"] != 5 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
}

func httptestGet(path string) *http.Request {
	return httptestGetWithHeader(path, "", "")
}

func httptestGetWithHeader(path, key, val string) *http.Request {
	req := &http.Request{Method: http.MethodGet, Header: make(http.Header), URL: mustParseURL(path)}
	if key != "" {
		req.Header.Set(key, val)
	}
	return req
}

func mustParseURL(path string) *url.URL {
	u, err := url.Parse(path)
	if err != nil {
		panic(err)
	}
	return u
}
