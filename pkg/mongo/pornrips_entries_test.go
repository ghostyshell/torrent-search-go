package mongo

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// TestPornripsSearchFilter is the one runnable check for the multi-field search
// filter: it must $or over title, resolved_title, and performers (so a performer
// query like "kittyxkum" matches the enriched performers array), and escape regex
// metacharacters in the user query so a "." or "*" in the query is a literal.
// Pure helper, no Mongo harness.
func TestPornripsSearchFilter(t *testing.T) {
	f := pornripsSearchFilter("kitty.xum*")

	or, ok := f["$or"].(bson.A)
	if !ok {
		t.Fatalf("$or is %T, want bson.A", f["$or"])
	}
	if len(or) != 3 {
		t.Fatalf("expected 3 $or clauses, got %d", len(or))
	}

	wantKeys := map[string]bool{"title": false, "resolved_title": false, "performers": false}
	for _, clause := range or {
		m, ok := clause.(bson.M)
		if !ok {
			t.Fatalf("clause is %T, want bson.M", clause)
		}
		if len(m) != 1 {
			t.Fatalf("clause has %d keys, want 1: %v", len(m), m)
		}
		for k := range m {
			wantKeys[k] = true
			regex, ok := m[k].(bson.M)
			if !ok {
				t.Fatalf("clause %q value is %T, want bson.M", k, m[k])
			}
			if got := regex["$options"]; got != "i" {
				t.Errorf("clause %q $options = %v, want \"i\"", k, got)
			}
			pat, _ := regex["$regex"].(string)
			if !strings.Contains(pat, `\.`) || !strings.Contains(pat, `\*`) {
				t.Errorf("clause %q regex = %q, regex metachars not escaped", k, pat)
			}
			if strings.Contains(pat, "kitty.xum*") {
				t.Errorf("clause %q regex = %q, unescaped metachar leaked", k, pat)
			}
		}
	}
	for k, seen := range wantKeys {
		if !seen {
			t.Errorf("missing $or clause for %q", k)
		}
	}
}

// TestStreamablePornripsFilter asserts findPornrips only surfaces entries with a
// resolved info_hash: the merged filter keeps the caller's clauses, adds
// info_hash $nin ["", nil], copies (not mutates) the input, and is idempotent when
// the caller already sets info_hash.
func TestStreamablePornripsFilter(t *testing.T) {
	in := bson.M{"studio_norm": "nubiles"}
	out := streamablePornripsFilter(in)

	// caller clause preserved
	if out["studio_norm"] != "nubiles" {
		t.Errorf("studio_norm = %v, want nubiles", out["studio_norm"])
	}
	// info_hash requirement added
	ih, ok := out["info_hash"].(bson.M)
	if !ok {
		t.Fatalf("info_hash is %T, want bson.M", out["info_hash"])
	}
	nin, _ := ih["$nin"].([]interface{})
	if len(nin) != 2 || nin[0] != "" || nin[1] != nil {
		t.Errorf("info_hash $nin = %v, want [\"\", nil]", nin)
	}
	// input not mutated
	if _, leaked := in["info_hash"]; leaked {
		t.Errorf("streamablePornripsFilter mutated the caller's filter map")
	}
	// idempotent when caller already sets info_hash
	out2 := streamablePornripsFilter(bson.M{"info_hash": bson.M{"$nin": bson.A{"", nil}}})
	if ih2, ok := out2["info_hash"].(bson.M); !ok || ih2["$nin"] == nil {
		t.Errorf("idempotent merge lost info_hash: %v", out2["info_hash"])
	}
}
