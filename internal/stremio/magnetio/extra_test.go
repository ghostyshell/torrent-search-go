package magnetio

import "testing"

func TestParseExtraEmpty(t *testing.T) {
	params := parseExtra("")
	if len(params) != 0 {
		t.Fatalf("expected empty params")
	}
}

func TestParseExtraGenre(t *testing.T) {
	params := parseExtra("genre=tt1234567")
	if params["genre"] != "tt1234567" {
		t.Fatalf("expected genre tt1234567, got %q", params["genre"])
	}
}

func TestParseExtraSlash(t *testing.T) {
	params := parseExtra("/genre=tt1234567&skip=25")
	if params["genre"] != "tt1234567" {
		t.Fatalf("expected genre tt1234567, got %q", params["genre"])
	}
	if params["skip"] != "25" {
		t.Fatalf("expected skip 25, got %q", params["skip"])
	}
}

func TestParseSkip(t *testing.T) {
	if parseSkip("") != 0 {
		t.Fatal("expected 0")
	}
	if parseSkip("skip=50") != 50 {
		t.Fatal("expected 50")
	}
	if parseSkip("skip=-1") != 0 {
		t.Fatal("expected 0 for negative skip")
	}
}
