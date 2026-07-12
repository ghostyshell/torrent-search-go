package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"torrent-search-go/internal/services/streams"
)

const knabenEndpoint = "https://api.knaben.org/v1"

// KnabenProvider queries the Knaben aggregator API.
type KnabenProvider struct {
	client  *http.Client
	baseURL string
	timeout time.Duration
}

// NewKnabenProvider creates a Knaben provider.
func NewKnabenProvider(client *http.Client) *KnabenProvider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &KnabenProvider{
		client:  client,
		baseURL: knabenEndpoint,
		timeout: 15 * time.Second,
	}
}

func (k *KnabenProvider) ID() string   { return "knaben" }
func (k *KnabenProvider) Name() string { return "Knaben" }

func (k *KnabenProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	queries := []struct {
		query string
		size  int
		broad bool
	}{
		{query: buildKnabenQuery(req), size: 50, broad: false},
	}
	if req.IsSeries() && req.Episode != nil {
		queries = append(queries, struct {
			query string
			size  int
			broad bool
		}{query: req.Name, size: 300, broad: true})
	}

	results := make(map[string]streams.Stream)
	for _, q := range queries {
		hits, err := k.search(ctx, q.query, q.size)
		if err != nil {
			continue
		}
		for _, h := range hits {
			st := k.normalize(h, req)
			if st.InfoHash == "" && st.URL == "" {
				continue
			}
			if q.broad {
				if !streams.MatchesEpisode(st.Title, req, req.AbsoluteEpisode) {
					continue
				}
				st.EpisodeMatched = true
			}
			key := st.InfoHash
			if key == "" {
				key = st.URL
			}
			existing, ok := results[key]
			if !ok || st.Seeders > existing.Seeders {
				results[key] = st
			}
		}
	}

	out := make([]streams.Stream, 0, len(results))
	for _, st := range results {
		out = append(out, st)
	}
	return out, nil
}

func buildKnabenQuery(req streams.Request) string {
	if req.IsSeries() && req.Season != nil && req.Episode != nil {
		return fmt.Sprintf("%s S%02dE%02d", req.Name, *req.Season, *req.Episode)
	}
	if req.Year > 0 {
		return fmt.Sprintf("%s %d", req.Name, req.Year)
	}
	return req.Name
}

func (k *KnabenProvider) search(ctx context.Context, query string, size int) ([]knabenHit, error) {
	body := map[string]interface{}{
		"query":           query,
		"search_type":     "100%",
		"order_by":        "seeders",
		"order_direction": "desc",
		"size":            size,
		"hide_unsafe":     true,
		"hide_xxx":        true,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("knaben returned %d", resp.StatusCode)
	}

	var data struct {
		Hits []knabenHit `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Hits, nil
}

func (k *KnabenProvider) normalize(h knabenHit, req streams.Request) streams.Stream {
	infoHash := extractKnabenHash(h)
	if infoHash == "" {
		return streams.Stream{}
	}

	title := strings.TrimSpace(h.Title)
	parsed := streams.ParseTitle(title)

	return streams.Stream{
		InfoHash:  infoHash,
		Title:     title,
		Seeders:   parseInt(h.Seeders),
		Leechers:  parseInt(h.Peers),
		Size:      parseInt64(h.Bytes),
		Provider:  "Knaben",
		IMDbID:    req.IMDbID,
		Quality:   parsed.Quality,
		Codec:     parsed.Codec,
		Source:    parsed.Source,
		HDR:       parsed.HDR,
		Bitdepth:  parsed.Bitdepth,
		Languages: parsed.Languages,
		Trackers:  streams.DefaultTrackers,
	}
}

func extractKnabenHash(h knabenHit) string {
	if matched, _ := regexp.MatchString(`^[a-f0-9]{40}$`, strings.ToLower(h.Hash)); matched {
		return strings.ToLower(h.Hash)
	}
	m := regexp.MustCompile(`(?i)urn:btih:([a-fA-F0-9]{40})`).FindStringSubmatch(h.MagnetURL)
	if len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

type knabenHit struct {
	Title     string `json:"title"`
	Hash      string `json:"hash"`
	MagnetURL string `json:"magnetUrl"`
	Seeders   string `json:"seeders"`
	Peers     string `json:"peers"`
	Bytes     string `json:"bytes"`
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
