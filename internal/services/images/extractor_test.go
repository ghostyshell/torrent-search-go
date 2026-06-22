package images

import (
	"context"
	"testing"
)

func TestExtractDirectImageURL_AlreadyDirect(t *testing.T) {
	ctx := context.Background()
	urls := []string{
		"https://example.com/image.jpg",
		"https://trafficimage.club/path/image.png?foo=bar",
	}
	for _, u := range urls {
		got, err := ExtractDirectImageURL(ctx, nil, u)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", u, err)
		}
		if got != u {
			t.Fatalf("expected %q, got %q", u, got)
		}
	}
}

func TestExtractDirectImageURL_UnknownHost(t *testing.T) {
	ctx := context.Background()
	u := "https://unknown.example.com/album/12345"
	got, err := ExtractDirectImageURL(ctx, nil, u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != u {
		t.Fatalf("expected original URL unchanged, got %q", got)
	}
}
