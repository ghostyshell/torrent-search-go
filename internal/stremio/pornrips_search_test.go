package stremio

import "testing"

func TestPornripsMatchesSearchMrLuckyPOV(t *testing.T) {
	query := "MrLuckyPOV.2026.Sophia.Locke.Aria.Six.Horny.Redhead.And.Busty.Blonde.Double.Creampie.XXX.720p.HEVC.x265.PRT"
	tokens := pornripsSearchTokens(query)
	if len(tokens) < 5 {
		t.Fatalf("expected several tokens, got %v", tokens)
	}
	item := catalogTorrent{
		Title:     "MrLuckyPOV.2026.Sophia.Locke.Aria.Six.Horny.Redhead.And.Busty.Blonde.Double.Creampie.XXX.720p.HEVC.x265.PRT",
		DetailURL: "https://pornrips.to/mrluckypov-2026-sophia-locke-aria-six-horny-redhead-and-busty-blonde-double-creampie-xxx-720p-hevc-x265-prt/",
	}
	if !pornripsMatchesSearch(item, tokens) {
		t.Fatal("expected release title to match search tokens")
	}
}

func TestPornripsMatchesSearchRejectsUnrelated(t *testing.T) {
	query := "MrLuckyPOV Sophia Locke"
	tokens := pornripsSearchTokens(query)
	item := catalogTorrent{Title: "LegalPorno 26 03 30 Belinha Baracho XXX"}
	if pornripsMatchesSearch(item, tokens) {
		t.Fatal("expected unrelated title not to match")
	}
}
