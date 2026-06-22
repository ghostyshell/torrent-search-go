package models

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// TestTorrentWireShape asserts the JSON output matches the documented wire shape
// (field names + string Seeders/Leechers + lowercase coverImage).
func TestTorrentWireShape(t *testing.T) {
	tor := Torrent{
		Name:       "Blacked 26 05 23 Eve Sweet",
		Size:       "10.1 GiB",
		Seeders:    108,
		Leechers:   31,
		Time:       "05-23 21:06",
		UploadedBy: "Mesoglea",
		Category:   "Porn",
		MagnetLink: "magnet:?xt=urn:btih:ABC",
		TorrentURL: "https://thehiddenbay.com/torrent/1/x",
		Website:    "piratebay",
		CoverImage: &CoverImage{Type: "url", URL: "https://img/x.jpg"},
	}

	b, err := json.Marshal(tor)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	wantKeys := []string{"Category", "DateUploaded", "Leechers", "Magnet", "Name", "Seeders", "Size", "Source", "UploadedBy", "Url", "coverImage"}
	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("keys mismatch:\n got  %v\n want %v", gotKeys, wantKeys)
	}

	// Seeders/Leechers must be JSON strings (matching the JS scraper).
	if got["Seeders"] != "108" || got["Leechers"] != "31" {
		t.Errorf("Seeders/Leechers not string: %v / %v", got["Seeders"], got["Leechers"])
	}
	if got["DateUploaded"] != "05-23 21:06" {
		t.Errorf("DateUploaded mismatch: %v", got["DateUploaded"])
	}
	if got["Url"] != tor.TorrentURL || got["Magnet"] != tor.MagnetLink {
		t.Errorf("Url/Magnet mismatch")
	}
	cover, ok := got["coverImage"].(map[string]any)
	if !ok || cover["url"] != "https://img/x.jpg" {
		t.Errorf("coverImage mismatch: %v", got["coverImage"])
	}
}

// TestTorrentNoCoverOmitted ensures coverImage is omitted when absent.
func TestTorrentNoCoverOmitted(t *testing.T) {
	b, _ := json.Marshal(Torrent{Name: "x", Seeders: 1})
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if _, present := got["coverImage"]; present {
		t.Errorf("coverImage should be omitted when nil")
	}
}

func TestTorrentLegacyWire(t *testing.T) {
	d := &TorrentDetails{
		Description: "test description",
		Files:       []File{{Name: "a.mkv", Size: "1 GB"}},
		Comments:    []TorrentComment{{Author: "user", Comment: "nice", Date: "today"}},
		Images:      []TorrentImageLink{{OriginalURL: "http://x/img.html", DirectURL: "http://x/img.jpg"}},
		MagnetLink:  "magnet:?xt=urn:btih:abc",
		InfoHash:    "abc",
	}
	wire := d.LegacyWire()
	if wire["description"] != "test description" {
		t.Fatalf("description mismatch")
	}
	if wire["magnet"] != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("magnet mismatch")
	}
	if wire["hash"] != "abc" {
		t.Fatalf("hash mismatch")
	}
	files, ok := wire["files"].([]File)
	if !ok || len(files) != 1 {
		t.Fatalf("files mismatch")
	}
}
