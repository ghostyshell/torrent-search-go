package stremio

import "testing"

func TestCatalogFanoutKey(t *testing.T) {
	if got := catalogFanoutKey(Config{}); got != "hb" {
		t.Fatalf("default = %q, want hb", got)
	}
	if got := catalogFanoutKey(Config{ExtraIndexers: true}); got != "hb,kn,bs,xc" {
		t.Fatalf("extra indexers = %q", got)
	}
	if got := catalogFanoutKey(Config{ExtraIndexers: true, Enable1337x: true}); got != "hb,kn,bs,xc,1337" {
		t.Fatalf("all fanout = %q", got)
	}
}

func TestFilterByIndexerConfigDropsKnabenWhenDisabled(t *testing.T) {
	in := []catalogTorrent{
		{Title: "HB", Website: "hiddenbay"},
		{Title: "Knaben", Website: "knaben"},
		{Title: "BS", Website: "bitsearch"},
	}
	got := filterByIndexerConfig(in, Config{})
	if len(got) != 1 || got[0].Website != "hiddenbay" {
		t.Fatalf("filterByIndexerConfig = %#v, want hiddenbay only", got)
	}
}

func TestFilterByIndexerConfigKeepsKnabenWhenEnabled(t *testing.T) {
	in := []catalogTorrent{{Title: "Knaben", Website: "knaben"}}
	got := filterByIndexerConfig(in, Config{ExtraIndexers: true})
	if len(got) != 1 {
		t.Fatalf("expected knaben kept, got %#v", got)
	}
}

func TestBuildCatalogListKeyIncludesFanout(t *testing.T) {
	got := buildCatalogListKey("https://api.test", "xxx_trans_recent", "Porn", "", "", 0, "hb")
	want := "https://api.test|xxx_trans_recent|Porn|||0|hb"
	if got != want {
		t.Fatalf("buildCatalogListKey = %q, want %q", got, want)
	}
}
