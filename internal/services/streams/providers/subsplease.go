package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"torrent-search-go/internal/services/streams"
)

const subspleaseBase = "https://subsplease.org/api"

// SubsPleaseProvider queries the SubsPlease JSON API.
type SubsPleaseProvider struct {
	client *http.Client
}

// NewSubsPleaseProvider creates a SubsPlease provider.
func NewSubsPleaseProvider(client *http.Client) *SubsPleaseProvider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &SubsPleaseProvider{client: client}
}

func (s *SubsPleaseProvider) ID() string   { return "subsplease" }
func (s *SubsPleaseProvider) Name() string { return "SubsPlease" }

func (s *SubsPleaseProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	if !req.IsSeries() || req.Name == "" {
		return []streams.Stream{}, nil
	}

	u, _ := url.Parse(subspleaseBase)
	q := u.Query()
	q.Set("f", "search")
	q.Set("tz", "UTC")
	q.Set("s", req.Name)
	u.RawQuery = q.Encode()

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := s.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subsplease returned %d", resp.StatusCode)
	}

	var data map[string]subspleaseEntry
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	out := make([]streams.Stream, 0)
	for _, entry := range data {
		if len(entry.Downloads) == 0 {
			continue
		}
		epStr := strings.TrimSpace(entry.Episode)
		epStr = regexp.MustCompile(`v\d+$`).ReplaceAllString(epStr, "")
		epNum, _ := strconv.Atoi(epStr)

		if req.Episode != nil {
			wanted := []int{*req.Episode, req.AbsoluteEpisode}
			matched := false
			for _, w := range wanted {
				if w > 0 && epNum == w {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		for _, dl := range entry.Downloads {
			infoHash := extractSubspleaseInfoHash(dl.Magnet)
			if infoHash == "" {
				continue
			}
			quality := subspleaseQuality(dl.Res)
			size := parseXL(dl.Magnet)
			title := decodeDN(dl.Magnet)
			if title == "" {
				title = fmt.Sprintf("%s - %s (%sp)", entry.Show, entry.Episode, dl.Res)
			}
			parsed := streams.ParseTitle(title)
			out = append(out, streams.Stream{
				InfoHash:       infoHash,
				Title:          title,
				Seeders:        0,
				Leechers:       0,
				Size:           size,
				Provider:       "SubsPlease",
				IMDbID:         req.IMDbID,
				Quality:        quality,
				Languages:      []string{"ja"},
				Source:         "WEB",
				Codec:          parsed.Codec,
				HDR:            parsed.HDR,
				Bitdepth:       parsed.Bitdepth,
				EpisodeMatched: true,
				Trackers:       streams.DefaultTrackers,
			})
		}
	}
	return out, nil
}

func extractSubspleaseInfoHash(magnet string) string {
	m := regexp.MustCompile(`(?i)xt=urn:btih:([a-fA-F0-9]{40}|[a-z2-7]{32})`).FindStringSubmatch(magnet)
	if len(m) < 2 {
		return ""
	}
	raw := strings.ToLower(m[1])
	if len(raw) == 32 {
		if hex, err := streams.Base32ToHex(raw); err == nil {
			return hex
		}
	}
	return raw
}

func subspleaseQuality(res string) string {
	switch res {
	case "1080":
		return "1080p"
	case "720":
		return "720p"
	case "480":
		return "480p"
	}
	return ""
}

func parseXL(magnet string) int64 {
	m := regexp.MustCompile(`(?i)[?&]xl=(\d+)`).FindStringSubmatch(magnet)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

func decodeDN(magnet string) string {
	m := regexp.MustCompile(`[?&]dn=([^&]+)`).FindStringSubmatch(magnet)
	if len(m) < 2 {
		return ""
	}
	dn, err := url.QueryUnescape(m[1])
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(dn, "+", " ")
}

type subspleaseEntry struct {
	Episode   string               `json:"episode"`
	Show      string               `json:"show"`
	Downloads []subspleaseDownload `json:"downloads"`
}

type subspleaseDownload struct {
	Magnet string `json:"magnet"`
	Res    string `json:"res"`
}
