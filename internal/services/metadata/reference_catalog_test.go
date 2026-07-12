package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestReferenceClient points a ReferenceClient at a test server so we can
// assert which WP endpoints a studio/tag catalog lookup hits.
func newTestReferenceClient(t *testing.T, handler http.HandlerFunc) (*ReferenceClient, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+"?"+r.URL.RawQuery)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c := &ReferenceClient{
		baseURL:    srv.URL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
	return c, &calls
}

func TestFetchPornripsCatalogUnresolvedStudioReturnsNothing(t *testing.T) {
	// Term search returns a category whose name does NOT match the requested
	// studio, so resolveTermID returns 0. The catalog must return nothing so
	// the caller falls through to the token-scan / WP ?s= filter paths, NOT the
	// unfiltered recent feed.
	c, calls := newTestReferenceClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/categories"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":42,"name":"Other Studio","slug":"other-studio"}]`))
		case strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/posts"):
			// If the bug regresses, this posts call arrives with no categories
			// filter (the recent feed) and we fail the test below.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":1,"slug":"recent-post","title":{"rendered":"Recent"}}]`))
		default:
			http.NotFound(w, r)
		}
	})

	items, err := c.FetchPornripsCatalog(context.Background(), "studio", "5K Porn", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("unresolved studio returned %d items; want 0 (caller must fall through to filter paths)", len(items))
	}
	// Must NOT have fetched posts without a categories filter.
	for _, call := range *calls {
		if strings.HasPrefix(call, "/wp-json/wp/v2/posts") && !strings.Contains(call, "categories=") {
			t.Fatalf("unresolved studio fetched unfiltered posts: %s", call)
		}
	}
}

func TestFetchPornripsCatalogResolvedStudioFilters(t *testing.T) {
	// Term search matches; the posts call must carry the categories filter.
	c, calls := newTestReferenceClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/categories"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":77,"name":"Bang Bros","slug":"bang-bros"}]`))
		case strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/posts"):
			if !strings.Contains(r.URL.RawQuery, "categories=77") {
				t.Fatalf("posts query missing categories=77: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":9,"slug":"bang-bros-scene","title":{"rendered":"Bang Bros Scene"}}]`))
		default:
			http.NotFound(w, r)
		}
	})

	items, err := c.FetchPornripsCatalog(context.Background(), "studio", "Bang Bros", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(items) != 1 || items[0].Slug != "bang-bros-scene" {
		t.Fatalf("items = %+v", items)
	}
	if !hasCall(*calls, "/wp-json/wp/v2/posts") {
		t.Fatalf("expected a posts call; got %v", *calls)
	}
}

func TestResolveTermIDCachesMiss(t *testing.T) {
	// An unresolved name must be cached so pagination doesn't re-hit the term
	// search endpoint on every page.
	var termHits int
	c, calls := newTestReferenceClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/wp-json/wp/v2/tags"):
			termHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":5,"name":"Other","slug":"other"}]`))
		default:
			http.NotFound(w, r)
		}
	})

	for i := 0; i < 3; i++ {
		if _, err := c.resolveTermID(context.Background(), "tags", "Nonexistent Tag"); err != nil {
			t.Fatalf("err = %v", err)
		}
	}
	if termHits != 1 {
		t.Fatalf("term search hit %d times; want 1 (miss should be cached)", termHits)
	}
	if hasCall(*calls, "/wp-json/wp/v2/posts") {
		t.Fatalf("resolveTermID should not fetch posts")
	}
}

func hasCall(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}