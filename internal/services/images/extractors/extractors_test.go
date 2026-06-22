package extractors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtract_AlreadyDirect(t *testing.T) {
	ctx := context.Background()
	urls := []string{
		"https://example.com/image.jpg",
		"https://trafficimage.club/path/image.png?foo=bar",
		"https://imgur.com/direct.jpeg",
	}
	for _, u := range urls {
		got, err := Extract(ctx, nil, u)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", u, err)
		}
		if got != u {
			t.Fatalf("expected %q, got %q", u, got)
		}
	}
}

func TestExtract_UnknownHost(t *testing.T) {
	ctx := context.Background()
	u := "https://unknown.example.com/album/12345"
	got, err := Extract(ctx, nil, u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != u {
		t.Fatalf("expected original URL unchanged, got %q", got)
	}
}

func TestTrafficImageExtractor(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		want     string
	}{
		{
			name:     "og:image meta",
			selector: `<meta property="og:image" content="https://trafficimage.club/i/image.jpg">`,
			want:     "https://trafficimage.club/i/image.jpg",
		},
		{
			name:     "img#image",
			selector: `<img id="image" src="https://trafficimage.club/uploads/image.png">`,
			want:     "https://trafficimage.club/uploads/image.png",
		},
		{
			name:     "first plausible img",
			selector: `<div><img src="https://other.example.com/icon.gif"><img src="https://trafficimage.club/i/main.webp"></div>`,
			want:     "https://trafficimage.club/i/main.webp",
		},
	}

	extractor := &trafficimageExtractor{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := serveHTML(fmt.Sprintf(`<!doctype html><html><head>%s</head><body></body></html>`, tt.selector))
			defer hs.Close()

			got, err := extractor.Extract(context.Background(), nil, hs.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestImgtrafficExtractor_DirectPath(t *testing.T) {
	ctx := context.Background()
	extractor := &imgtrafficExtractor{}
	u := "https://imgtraffic.com/1/2025/07/24/688278ecbe629.jpeg"
	got, err := extractor.Extract(ctx, nil, u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != u {
		t.Fatalf("expected %q, got %q", u, got)
	}
}

func TestImgtrafficExtractor_Page(t *testing.T) {
	hs := serveHTML(`<!doctype html><html><body><img id="image" src="https://imgtraffic.com/1/2025/07/24/688278ecbe629.jpeg"></body></html>`)
	defer hs.Close()

	extractor := &imgtrafficExtractor{}
	pageURL := hs.URL + "/i-1/2025/07/24/688278ecbe629.jpeg"
	got, err := extractor.Extract(context.Background(), nil, pageURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://imgtraffic.com/1/2025/07/24/688278ecbe629.jpeg"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestImgBBExtractor(t *testing.T) {
	hs := serveHTML(`<!doctype html><html><body><img class="image" src="https://i.ibb.co/abc123/image.jpg"></body></html>`)
	defer hs.Close()

	extractor := &imgbbExtractor{}
	got, err := extractor.Extract(context.Background(), nil, hs.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://i.ibb.co/abc123/image.jpg"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPostimgExtractor(t *testing.T) {
	hs := serveHTML(`<!doctype html><html><body><img id="main-image" src="https://i.postimg.cc/abc/image.png"></body></html>`)
	defer hs.Close()

	extractor := &postimgExtractor{}
	got, err := extractor.Extract(context.Background(), nil, hs.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://i.postimg.cc/abc/image.png"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestImgurExtractor(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ABC.jpg":
			w.WriteHeader(http.StatusOK)
		case "/ABC.png", "/ABC.webp", "/ABC.gif":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected imgur HEAD path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	extractor := &imgurExtractor{}
	pageURL := server.URL + "/ABC"
	got, err := extractor.Extract(ctx, server.Client(), pageURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "/ABC.jpg") {
		t.Fatalf("expected .jpg URL for ABC, got %q", got)
	}
}

func TestFastpicExtractor_Page(t *testing.T) {
	hs := serveHTML(`<!doctype html><html><body><img id="image" src="https://i125.fastpic.org/big/2025/0630/8f/8f8065ead21a577bc534c04d996be983.jpg?md5=abc&expires=12345"></body></html>`)
	defer hs.Close()

	extractor := &fastpicExtractor{}
	pageURL := hs.URL + "/view/125/2025/0630/_8f8065ead21a577bc534c04d996be983.jpg.html"
	got, err := extractor.Extract(context.Background(), nil, pageURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://i125.fastpic.org/big/2025/0630/8f/8f8065ead21a577bc534c04d996be983.jpg?md5=abc&expires=12345"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFastpicExtractor_FallbackConversion(t *testing.T) {
	extractor := &fastpicExtractor{}
	pageURL := "https://fastpic.org/view/125/2025/0630/_8f8065ead21a577bc534c04d996be983.jpg.html"

	m := fastpicViewRE.FindStringSubmatch(pageURL)
	if m == nil {
		t.Fatalf("view URL did not match conversion regex")
	}
	server, year, monthDay, hash, ext := m[1], m[2], m[3], m[4], m[5]
	want := "https://i" + server + ".fastpic.org/big/" + year + "/" + monthDay + "/" + hash[:2] + "/" + hash + "." + ext

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network disabled for test")
		}),
	}

	ctx := context.Background()
	got, err := extractor.Extract(ctx, client, pageURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestXxxwebdlxxxExtractor(t *testing.T) {
	hs := serveHTML(`<!doctype html><html><body><img src="https://xxxwebdlxxx.org/uploads/image.jpg" width="200" height="300"></body></html>`)
	defer hs.Close()

	extractor := &xxxwebdlxxxExtractor{}
	pageURL := hs.URL + "/img-abc123.html"
	got, err := extractor.Extract(context.Background(), nil, pageURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://xxxwebdlxxx.org/uploads/image.jpg"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		pageURL string
		src     string
		want    string
	}{
		{"https://example.com/page", "https://cdn.example.com/image.jpg", "https://cdn.example.com/image.jpg"},
		{"https://example.com/page", "//cdn.example.com/image.png", "https://cdn.example.com/image.png"},
		{"https://example.com/dir/page", "/image.gif", "https://example.com/image.gif"},
		{"https://example.com/dir/page", "image.webp", "https://example.com/dir/image.webp"},
		{"https://example.com/dir/page", "", ""},
	}
	for _, tt := range tests {
		got := normalizeURL(tt.src, tt.pageURL)
		if got != tt.want {
			t.Fatalf("normalizeURL(%q, %q) = %q, want %q", tt.src, tt.pageURL, got, tt.want)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func serveHTML(html string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
}
