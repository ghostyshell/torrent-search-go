package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStremioRoutesRegisterWithoutPanic(t *testing.T) {
	r := NewRouter()
	r.Get("/stremio/:config/manifest.json", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/stremio/:config/catalog/:type/:catalogFile", func(w http.ResponseWriter, req *http.Request) {
		if req.PathValue("catalogFile") != "xxx_top.json" {
			t.Fatalf("catalogFile = %q, want xxx_top.json", req.PathValue("catalogFile"))
		}
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/stremio/:config/catalog/:type/:catalogFile/:extraFile", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/stremio/:config/meta/:type/:metaFile", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/stremio/abc123/catalog/Porn/xxx_top.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, want 200", rec.Code)
	}
	if got := req.PathValue("catalogFile"); got != "xxx_top.json" && got != "" {
		// PathValue is set by ServeMux on the request passed to handler; re-dispatch to verify routing.
		t.Logf("catalogFile path value in outer req: %q", got)
	}
}

func TestToServeMuxPatternsStremioCatalog(t *testing.T) {
	patterns := toServeMuxPatterns("/stremio/:config/catalog/:type/:catalogFile")
	if len(patterns) != 1 {
		t.Fatalf("patterns = %v", patterns)
	}
	want := "/stremio/{config}/catalog/{type}/{catalogFile}"
	if patterns[0] != want {
		t.Fatalf("pattern = %q, want %q", patterns[0], want)
	}
}

// TestRouterSplatWildcardMatchesSubtree locks in that an Express-style
// trailing "*" maps to a ServeMux multi-segment wildcard so POSTs to a
// subtree route reach the handler instead of falling through to the
// static GET / catch-all (which manifests as 405 Allow: GET, HEAD).
func TestRouterSplatWildcardMatchesSubtree(t *testing.T) {
	patterns := toServeMuxPatterns("/api/proxy/real-debrid/*")
	if len(patterns) != 1 || patterns[0] != "/api/proxy/real-debrid/{path...}" {
		t.Fatalf("patterns = %v, want [/api/proxy/real-debrid/{path...}]", patterns)
	}

	r := NewRouter()
	r.All("/api/proxy/real-debrid/*", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/proxy/real-debrid/torrents/addMagnet"},
		{http.MethodGet, "/api/proxy/real-debrid/torrents/info/abc"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s: status = %d (Allow=%q), want 200", c.method, c.path, rec.Code, rec.Header().Get("Allow"))
		}
	}
}
