package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"torrent-search-go/internal/models"
)

const knabenAdultEndpoint = "https://api.knaben.org/v1"

type KnabenAdultScraper struct {
	client  *http.Client
	baseURL string
}

func NewKnabenAdultScraper(client *http.Client) *KnabenAdultScraper {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &KnabenAdultScraper{client: client, baseURL: knabenAdultEndpoint}
}

func (s *KnabenAdultScraper) Search(ctx context.Context, query string, page int, options models.SearchOptions) ([]models.Torrent, error) {
	size := 50
	if page > 1 {
		size = 50
	}
	hits, err := s.searchAPI(ctx, query, size, options.Sort)
	if err != nil {
		return nil, &ScraperError{Message: "knaben adult search failed", Err: err}
	}
	out := make([]models.Torrent, 0, len(hits))
	for _, h := range hits {
		t := h.toTorrent()
		if t.Name != "" && t.MagnetLink != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *KnabenAdultScraper) searchAPI(ctx context.Context, query string, size int, sort string) ([]knabenAdultHit, error) {
	order := "seeders"
	if sort == "3" {
		order = "date"
	}
	body := map[string]interface{}{
		"query":           query,
		"search_type":     "100%",
		"order_by":        order,
		"order_direction": "desc",
		"size":            size,
		"hide_unsafe":     true,
		"hide_xxx":        false,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("knaben returned %d", resp.StatusCode)
	}
	var data struct {
		Hits []knabenAdultHit `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Hits, nil
}

type knabenAdultHit struct {
	Title     string      `json:"title"`
	Hash      string      `json:"hash"`
	MagnetURL string      `json:"magnetUrl"`
	Seeders   json.Number `json:"seeders"`
	Peers     json.Number `json:"peers"`
	Bytes     json.Number `json:"bytes"`
}

func (h knabenAdultHit) toTorrent() models.Torrent {
	infoHash := extractKnabenAdultHash(h)
	if infoHash == "" {
		return models.Torrent{}
	}
	title := strings.TrimSpace(h.Title)
	magnet := h.MagnetURL
	if magnet == "" {
		magnet = buildMagnetLink(infoHash, title)
	}
	seeders, _ := h.Seeders.Int64()
	leechers, _ := h.Peers.Int64()
	bytesVal, _ := h.Bytes.Int64()
	return models.Torrent{
		Name:       title,
		MagnetLink: magnet,
		Seeders:    int(seeders),
		Leechers:   int(leechers),
		Size:       formatBytes(bytesVal),
		Website:    "knaben",
		TorrentURL: magnet,
	}
}

func extractKnabenAdultHash(h knabenAdultHit) string {
	direct := strings.ToLower(strings.TrimSpace(h.Hash))
	if matched, _ := regexp.MatchString(`^[a-f0-9]{40}$`, direct); matched {
		return direct
	}
	m := regexp.MustCompile(`(?i)urn:btih:([a-fA-F0-9]{40})`).FindStringSubmatch(h.MagnetURL)
	if len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

var defaultTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.tiny-vps.com:6969/announce",
}

func buildMagnetLink(infoHash, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "magnet:?xt=urn:btih:%s", infoHash)
	if name != "" {
		fmt.Fprintf(&b, "&dn=%s", urlEncode(name))
	}
	for _, t := range defaultTrackers {
		fmt.Fprintf(&b, "&tr=%s", urlEncode(t))
	}
	return b.String()
}

func urlEncode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteByte('+')
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' || r == ':' || r == '/' || r == '?' ||
			r == '&' || r == '=' || r == '(' || r == ')':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}

var _ Scraper = (*KnabenAdultScraper)(nil)
