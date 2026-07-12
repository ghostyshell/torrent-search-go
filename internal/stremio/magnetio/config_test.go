package magnetio

import (
	"testing"
)

func TestParseConfigDefaults(t *testing.T) {
	cfg := ParseConfig("")
	if len(cfg.Providers) == 0 {
		t.Fatal("expected default providers")
	}
	foundKnaben := false
	for _, p := range cfg.Providers {
		if p == "knaben" {
			foundKnaben = true
		}
	}
	if !foundKnaben {
		t.Fatal("expected knaben in default providers")
	}
	if cfg.Limit != 10 {
		t.Fatalf("expected limit 10, got %d", cfg.Limit)
	}
}

func TestParseConfigProviders(t *testing.T) {
	cfg := ParseConfig("providers=nyaa,subsplease|limit=25|rd=abc123")
	if len(cfg.Providers) < 3 {
		t.Fatalf("expected at least 3 providers (nyaa + subsplease + knaben core), got %v", cfg.Providers)
	}
	if cfg.Limit != 25 {
		t.Fatalf("expected limit 25, got %d", cfg.Limit)
	}
	if cfg.RealDebridApiKey != "abc123" {
		t.Fatalf("expected rd key, got %q", cfg.RealDebridApiKey)
	}
}

func TestParseConfigAliases(t *testing.T) {
	cfg := ParseConfig("providers=s8,s20")
	seen := map[string]bool{}
	for _, p := range cfg.Providers {
		seen[p] = true
	}
	if !seen["nyaa"] {
		t.Fatal("expected s8 alias to resolve to nyaa")
	}
	if !seen["knaben"] {
		t.Fatal("expected s20 alias to resolve to knaben")
	}
}

func TestClampPrewarmLimit(t *testing.T) {
	if clampPrewarmLimit(-1) != 0 {
		t.Fatal("expected 0")
	}
	if clampPrewarmLimit(15) != 10 {
		t.Fatal("expected 10")
	}
	if clampPrewarmLimit(5) != 5 {
		t.Fatal("expected 5")
	}
}
