package mongo

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"torrent-search-go/pkg/models"
)

// TestFilterNonEmpty locks the "All" genre fallback for the pvz_tag / fpv_tag
// catalogs: the catalog sends []string{NormToken("")} = []string{""} when the
// user selects "All", and the ByTag store methods must treat that as "no filter"
// (fall back to Recent) rather than querying $in: [""] (which matches zero docs,
// since normSlice drops empties so no doc has "" in its tags/categories norm
// array). Without this, the "All" tag view returns an empty catalog while every
// other genre works (studio/performer fall back on empty at the store level).
func TestFilterNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"all-empty (All genre)", []string{""}, nil},
		{"mix of empty and real", []string{"", "lesbian", "", "threesome"}, []string{"lesbian", "threesome"}},
		{"all real", []string{"lesbian", "kissing"}, []string{"lesbian", "kissing"}},
		{"nil", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterNonEmpty(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("filterNonEmpty(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("filterNonEmpty(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestPerverzijaUpsertKeepsEnrichmentInSetOnInsert locks the data-loss fix: the
// ingest sweep re-walks from page 1 on every cursor reset, calling
// UpsertPerverzijaEntry with enrichment fields at zero values. If those fields
// were in $set, every re-walked entry would have its stream_hash/performers/poster
// wiped -> "no streams" and no Cast links until the enrich sweep re-scraped it.
// They must be in $setOnInsert (no-op on an existing doc) so a re-walk preserves a
// prior enrichment. The enrich sweep writes them via perverzijaEnrichmentUpdate.
func TestPerverzijaUpsertKeepsEnrichmentInSetOnInsert(t *testing.T) {
	u := perverzijaUpsertUpdate(models.PerverzijaEntry{Slug: "s", Title: "T"})
	set, _ := u["$set"].(bson.M)
	soi, _ := u["$setOnInsert"].(bson.M)
	if set == nil || soi == nil {
		t.Fatal("upsert must have both $set and $setOnInsert")
	}
	for _, k := range []string{"performers", "performers_norm", "poster", "description", "stream_hash", "detail_scraped"} {
		if _, ok := set[k]; ok {
			t.Errorf("%q must NOT be in $set (a re-walk would wipe it to zero)", k)
		}
		if _, ok := soi[k]; !ok {
			t.Errorf("%q must be in $setOnInsert (so re-walks preserve prior enrichment)", k)
		}
	}
	for _, k := range []string{"slug", "title", "detail_url", "date", "excerpt", "wp_poster", "studios", "studios_norm", "tags", "tags_norm"} {
		if _, ok := set[k]; !ok {
			t.Errorf("%q must be in $set (listing fields are refreshed each walk)", k)
		}
	}
	// The enrichment writer $sets the same fields the upsert guards.
	eu := perverzijaEnrichmentUpdate(models.PerverzijaEntry{Slug: "s"})
	eset, _ := eu["$set"].(bson.M)
	for _, k := range []string{"performers", "performers_norm", "poster", "description", "stream_hash", "detail_scraped"} {
		if _, ok := eset[k]; !ok {
			t.Errorf("enrichment writer must $set %q", k)
		}
	}
}

// TestFreepornvideosUpsertKeepsEnrichmentInSetOnInsert locks the data-loss fix
// for fpv: date is the fpv_recent sort key and is only set by enrich (JSON-LD
// uploadDate). If date were in the ingest $set, a re-walk would write "" and sink
// re-walked entries to the bottom of the feed. date/duration/categories/network/
// description/detail_scraped must be $setOnInsert; the enrich sweep writes them
// via freepornvideosEnrichmentUpdate. performers is card-owned (thumb_model) so
// it stays in $set.
func TestFreepornvideosUpsertKeepsEnrichmentInSetOnInsert(t *testing.T) {
	u := freepornvideosUpsertUpdate(models.FreepornvideosEntry{VideoID: "1", Slug: "s", Title: "T"})
	set, _ := u["$set"].(bson.M)
	soi, _ := u["$setOnInsert"].(bson.M)
	if set == nil || soi == nil {
		t.Fatal("upsert must have both $set and $setOnInsert")
	}
	for _, k := range []string{"date", "duration", "categories", "categories_norm", "network", "description", "detail_scraped"} {
		if _, ok := set[k]; ok {
			t.Errorf("%q must NOT be in $set (a re-walk would wipe it; date is the sort key)", k)
		}
		if _, ok := soi[k]; !ok {
			t.Errorf("%q must be in $setOnInsert", k)
		}
	}
	for _, k := range []string{"video_id", "slug", "title", "detail_url", "poster", "studio", "studio_norm", "performers", "performers_norm", "rating", "views", "has_4k"} {
		if _, ok := set[k]; !ok {
			t.Errorf("%q must be in $set (card fields are refreshed each walk)", k)
		}
	}
	eu := freepornvideosEnrichmentUpdate(models.FreepornvideosEntry{VideoID: "1"})
	eset, _ := eu["$set"].(bson.M)
	for _, k := range []string{"date", "duration", "categories", "categories_norm", "network", "description", "detail_scraped"} {
		if _, ok := eset[k]; !ok {
			t.Errorf("enrichment writer must $set %q", k)
		}
	}
}
