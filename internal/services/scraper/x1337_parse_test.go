package scraper

import "testing"

const sample1337xSearchHTML = `<table class="table-list table table-responsive table-striped">
<tbody>
<tr>
<td class="coll-1 name"><a href="/sub/42/0/" class="icon"><i></i></a><a href="/torrent/12345/Ubuntu-22-04-LTS/">Ubuntu 22.04 LTS</a></td>
<td class="coll-2">842</td>
<td class="coll-3">12</td>
<td class="coll-date">Jun. 21st '26</td>
<td class="coll-4"><span class="size">3.2 GB</span></td>
<td class="coll-5"><a href="/user/ubuntu/">ubuntu</a></td>
</tr>
</tbody>
</table>`

func TestParse1337xSearchHTML(t *testing.T) {
	s := &X1337Scraper{baseURL: "https://1337x.to"}
	out := s.parseSearchHTML(sample1337xSearchHTML)
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if out[0].Name != "Ubuntu 22.04 LTS" {
		t.Fatalf("name = %q", out[0].Name)
	}
	if out[0].TorrentURL != "https://1337x.to/torrent/12345/Ubuntu-22-04-LTS/" {
		t.Fatalf("url = %q", out[0].TorrentURL)
	}
	if out[0].Seeders != 842 || out[0].Leechers != 12 {
		t.Fatalf("seeders/leechers = %d/%d", out[0].Seeders, out[0].Leechers)
	}
	if out[0].Time != "Jun. 21st '26" {
		t.Fatalf("time = %q", out[0].Time)
	}
	if out[0].UploadedBy != "ubuntu" {
		t.Fatalf("uploader = %q", out[0].UploadedBy)
	}
}
