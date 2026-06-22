package stremio

import "testing"

func TestMatchesTransAdultTitleRejectsNoise(t *testing.T) {
	bad := []string{
		"2600 pages of anti trans hate",
		"Трудности перевода / Lost in Translation [2003, США, Япония, драма",
		"Officially Translated Light Novels (LNs) v24 03",
		"Драйв / Drive [2011, США, триллер, драма, криминал",
		"Transformers 2007 1080p",
		"Money Transfer Guide PDF",
	}
	for _, title := range bad {
		if matchesTransAdultTitle(title) {
			t.Fatalf("expected reject for %q", title)
		}
	}
}

func TestMatchesTransAdultTitleAcceptsPorn(t *testing.T) {
	good := []string{
		"Trans.Active.35.2026.720p",
		"Trans Angels - Scene Name 1080p",
		"Shemale Japan Hardcore 2160p",
		"GroobyGirls - Luna Love 4K",
		"Busty Bareback Trans 2025 1080p",
		"GenderX - Hot Scene XXX 1080p",
	}
	for _, title := range good {
		if !matchesTransAdultTitle(title) {
			t.Fatalf("expected accept for %q", title)
		}
	}
}

func TestFilterTransRelevanceKeepsHiddenbay(t *testing.T) {
	in := []catalogTorrent{
		{Title: "Lost in Translation 2003", Website: "hiddenbay"},
		{Title: "anti trans hate emails", Website: "knaben"},
		{Title: "Trans.Active.35.2026.720p", Website: "bitsearch"},
	}
	got := filterTransRelevance(in)
	if len(got) != 2 {
		t.Fatalf("filterTransRelevance = %#v, want hiddenbay + valid bitsearch", got)
	}
}

func TestFanoutAdultSearchQueryTrans(t *testing.T) {
	if got := fanoutAdultSearchQuery("trans"); got != "shemale transgender transsexual" {
		t.Fatalf("fanoutAdultSearchQuery(trans) = %q", got)
	}
	if got := fanoutAdultSearchQuery("Vixen"); got != "Vixen" {
		t.Fatalf("fanoutAdultSearchQuery passthrough = %q", got)
	}
}
