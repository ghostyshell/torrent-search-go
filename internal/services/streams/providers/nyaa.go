package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"torrent-search-go/internal/services/streams"
)

const animetoshoFeed = "https://feed.animetosho.org/json"

// NyaaProvider searches anime via the AnimeTosho feed (mirrors Nyaa).
type NyaaProvider struct {
	client  *http.Client
	maxAIDs int
}

// NewNyaaProvider creates a Nyaa/AnimeTosho provider.
func NewNyaaProvider(client *http.Client) *NyaaProvider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &NyaaProvider{client: client, maxAIDs: 2}
}

func (n *NyaaProvider) ID() string   { return "nyaa" }
func (n *NyaaProvider) Name() string { return "Nyaa" }

func (n *NyaaProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	if !req.IsSeries() || req.Name == "" {
		return []streams.Stream{}, nil
	}

	absEp := req.AbsoluteEpisode
	if req.Episode != nil {
		absEp = *req.Episode
	}

	entries := make(map[string]animetoshoEntry)
	if _, err := n.fetch(ctx, map[string]string{"q": req.Name}, entries); err != nil {
		return nil, err
	}

	// Per-anime listings for top AniDB IDs.
	aidCounts := make(map[string]int)
	for _, e := range entries {
		if e.AnidbAID != "" {
			aidCounts[e.AnidbAID]++
		}
	}
	type aidCount struct {
		id    string
		count int
	}
	var aids []aidCount
	for id, count := range aidCounts {
		aids = append(aids, aidCount{id, count})
	}
	sort.Slice(aids, func(i, j int) bool { return aids[i].count > aids[j].count })
	if len(aids) > n.maxAIDs {
		aids = aids[:n.maxAIDs]
	}
	for _, a := range aids {
		_, _ = n.fetch(ctx, map[string]string{"aid": a.id}, entries)
	}

	// Page 2 for best-matching aid.
	if req.Episode != nil && len(aids) > 0 {
		matchedPerAid := make(map[string]int)
		for _, e := range entries {
			if e.AnidbAID != "" && n.entryMatches(e, req, absEp) {
				matchedPerAid[e.AnidbAID]++
			}
		}
		best := ""
		bestCount := 0
		for id, c := range matchedPerAid {
			if c > bestCount {
				best = id
				bestCount = c
			}
		}
		if best != "" {
			_, _ = n.fetch(ctx, map[string]string{"aid": best, "page": "2"}, entries)
		}
	}

	out := make([]streams.Stream, 0, len(entries))
	for _, e := range entries {
		title := e.Title
		if title == "" {
			title = e.TorrentName
		}
		if title == "" {
			continue
		}
		if req.Episode != nil && !n.entryMatches(e, req, absEp) {
			continue
		}

		parsed := streams.ParseTitle(title)
		out = append(out, streams.Stream{
			InfoHash:       e.InfoHash,
			Title:          title,
			Seeders:        e.Seeders,
			Leechers:       e.Leechers,
			Size:           e.TotalSize,
			Provider:       "Nyaa",
			IMDbID:         req.IMDbID,
			Languages:      []string{"ja"},
			Quality:        parsed.Quality,
			Codec:          parsed.Codec,
			Source:         parsed.Source,
			HDR:            parsed.HDR,
			Bitdepth:       parsed.Bitdepth,
			EpisodeMatched: true,
			Trackers:       streams.DefaultTrackers,
		})
	}
	return out, nil
}

func (n *NyaaProvider) fetch(ctx context.Context, params map[string]string, acc map[string]animetoshoEntry) (bool, error) {
	u, err := url.Parse(animetoshoFeed)
	if err != nil {
		return false, err
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := n.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("animetosho feed returned %d", resp.StatusCode)
	}

	var entries []animetoshoEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.InfoHash == "" {
			continue
		}
		if _, ok := acc[e.InfoHash]; !ok {
			acc[e.InfoHash] = e
		}
	}
	return true, nil
}

func (n *NyaaProvider) entryMatches(e animetoshoEntry, req streams.Request, absEp int) bool {
	for _, name := range []string{e.Title, e.TorrentName} {
		if name == "" {
			continue
		}
		if streams.MatchesEpisode(name, req, absEp) {
			return true
		}
	}
	return false
}

type animetoshoEntry struct {
	InfoHash    string `json:"info_hash"`
	Title       string `json:"title"`
	TorrentName string `json:"torrent_name"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	TotalSize   int64  `json:"total_size"`
	AnidbAID    string `json:"anidb_aid"`
}
