package jobs

import (
	"testing"

	"torrent-search-go/internal/services/metadata"
	prmodels "torrent-search-go/pkg/models"
)

// TestPornripsSharedMetaKey pins the contract that the enrich sweep writes
// shared_meta under the same key the catalog reads: e.MetaID ("pr:<slug>") must
// equal StableMetaID("pornrips", e.DetailURL, ""). If this drifts, enriched entries
// would write shared_meta the catalog never finds, so pr_recent would keep showing
// raw release filenames instead of the TPDB/Stash scene name + cover.
func TestPornripsSharedMetaKey(t *testing.T) {
	const slug = "mrluckypov-22-11-04-sophia-locke-prt"
	e := prmodels.PornripsEntry{
		Slug:      slug,
		MetaID:    "pr:" + slug,
		DetailURL: "https://pornrips.to/" + slug + "/",
	}
	if e.MetaID != StableMetaID("pornrips", e.DetailURL, "") {
		t.Fatalf("MetaID %q != StableMetaID %q; shared_meta key misaligned",
			e.MetaID, StableMetaID("pornrips", e.DetailURL, ""))
	}
}

// TestPornripsSharedMetaRoundTrip confirms the TPDB/Stash-resolved scene name and
// cover survive normalizedToShared -> sharedToPayload, so the durable Mongo row
// buildMetas rehydrates carries the resolved Title/Poster (not the raw filename).
func TestPornripsSharedMetaRoundTrip(t *testing.T) {
	m := &metadata.NormalizedMeta{
		Title:  "Richelle Rocks A Bikini",
		Poster: "https://tpdb/scene.jpg",
		Tags:   []string{"blonde", "outdoor"},
		Cast:   []string{"Richelle Ryan"},
		Source: "tpdb",
	}
	shared := normalizedToShared(m)
	if shared.Title != m.Title || shared.Poster != m.Poster {
		t.Fatalf("normalizedToShared dropped Title/Poster: %+v", shared)
	}
	payload := sharedToPayload(&shared)
	if payload.Title != m.Title || payload.Poster != m.Poster {
		t.Fatalf("sharedToPayload dropped Title/Poster: %+v", payload)
	}
	// Round-trip back to SharedMeta (what buildMetas reads from Mongo) must match.
	back := payloadToShared(&payload)
	if back.Title != m.Title || back.Poster != m.Poster {
		t.Fatalf("payloadToShared dropped Title/Poster: %+v", back)
	}
}