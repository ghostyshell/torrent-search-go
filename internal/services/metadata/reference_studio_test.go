package metadata

import "testing"

func TestPostTagStudio(t *testing.T) {
	p := wpPost{
		Embedded: struct {
			FeaturedMedia []struct {
				SourceURL string `json:"source_url"`
			} `json:"wp:featuredmedia"`
			Terms [][]wpTerm `json:"wp:term"`
		}{
			Terms: [][]wpTerm{
				// WP returns one slice per taxonomy; categories (720p) come first,
			// then post_tag. Only the post_tag name is the studio.
			{{ID: 3, Name: "720p", Slug: "720p", Taxonomy: "category"}},
			{{ID: 11, Name: "BrazzersExxtra", Slug: "brazzersexxtra", Taxonomy: "post_tag"}},
		},
		},
	}
	if got := postTagStudio(p); got != "BrazzersExxtra" {
		t.Fatalf("postTagStudio = %q, want BrazzersExxtra (post_tag, not the 720p category)", got)
	}
}

func TestPostTagStudioEmptyWhenNoPostTag(t *testing.T) {
	p := wpPost{
		Embedded: struct {
			FeaturedMedia []struct {
				SourceURL string `json:"source_url"`
			} `json:"wp:featuredmedia"`
			Terms [][]wpTerm `json:"wp:term"`
		}{
			Terms: [][]wpTerm{
			{{ID: 3, Name: "1080p", Slug: "1080p", Taxonomy: "category"}},
		},
		},
	}
	if got := postTagStudio(p); got != "" {
		t.Fatalf("postTagStudio = %q, want empty when no post_tag term", got)
	}
}