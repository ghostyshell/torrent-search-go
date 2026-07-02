package providers

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"torrent-search-go/internal/services/streams"
)

const animetoshoRSS = "https://feed.animetosho.org/rss2"

// AnimeToshoProvider queries the AnimeTosho RSS feed.
type AnimeToshoProvider struct {
	client *http.Client
}

// NewAnimeToshoProvider creates an AnimeTosho provider.
func NewAnimeToshoProvider(client *http.Client) *AnimeToshoProvider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &AnimeToshoProvider{client: client}
}

func (a *AnimeToshoProvider) ID() string   { return "animetosho" }
func (a *AnimeToshoProvider) Name() string { return "AnimeTosho" }

func (a *AnimeToshoProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	if !req.IsSeries() || req.Name == "" {
		return []streams.Stream{}, nil
	}

	query := streams.BuildSearchQuery(req)
	u, _ := url.Parse(animetoshoRSS)
	q := u.Query()
	q.Set("q", query)
	q.Set("qx", "1")
	q.Set("only_tor", "1")
	u.RawQuery = q.Encode()

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept", "application/rss+xml")
	r.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := a.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("animetosho returned %d", resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}

	out := make([]streams.Stream, 0, len(feed.Items))
	for _, item := range feed.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		desc := item.Description
		link := strings.TrimSpace(item.Link)

		infoHash := ""
		magnetMatch := regexp.MustCompile(`(?i)magnet:\?[^\s"<]+`).FindString(desc)
		if magnetMatch == "" {
			magnetMatch = regexp.MustCompile(`(?i)magnet:\?[^\s"<]+`).FindString(link)
		}
		if magnetMatch != "" {
			infoHash = extractAnimetoshoInfoHash(magnetMatch)
		}
		if infoHash == "" {
			if m := regexp.MustCompile(`(?i)/storage/torrent/([a-fA-F0-9]{40})/`).FindStringSubmatch(desc); len(m) > 1 {
				infoHash = strings.ToLower(m[1])
			}
		}
		if infoHash == "" {
			continue
		}

		seeders, leechers := 0, 0
		if m := regexp.MustCompile(`\[(\d+)[^/]*/(\d+)`).FindStringSubmatch(desc); len(m) > 2 {
			seeders, _ = strconv.Atoi(m[1])
			leechers, _ = strconv.Atoi(m[2])
		}

		size := int64(0)
		if item.Enclosure.Length != "" {
			size, _ = strconv.ParseInt(item.Enclosure.Length, 10, 64)
		}
		if size == 0 {
			if m := regexp.MustCompile(`(?i)([\d.]+)\s*(GB|MB|TB|KB)`).FindStringSubmatch(desc); len(m) > 2 {
				size = parseUnitSize(m[1], m[2])
			}
		}

		parsed := streams.ParseTitle(title)
		out = append(out, streams.Stream{
			InfoHash:  infoHash,
			Title:     title,
			Seeders:   seeders,
			Leechers:  leechers,
			Size:      size,
			Provider:  "AnimeTosho",
			IMDbID:    req.IMDbID,
			Languages: []string{"ja"},
			Quality:   parsed.Quality,
			Codec:     parsed.Codec,
			Source:    parsed.Source,
			HDR:       parsed.HDR,
			Bitdepth:  parsed.Bitdepth,
			Trackers:  streams.DefaultTrackers,
		})
	}
	return out, nil
}

func extractAnimetoshoInfoHash(magnet string) string {
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

func parseUnitSize(value, unit string) int64 {
	v, _ := strconv.ParseFloat(value, 64)
	units := map[string]int64{"b": 1, "kb": 1024, "mb": 1024 * 1024, "gb": 1024 * 1024 * 1024, "tb": 1024 * 1024 * 1024 * 1024}
	return int64(v * float64(units[strings.ToLower(unit)]))
}

type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title       string       `xml:"title"`
	Link        string       `xml:"link"`
	Description string       `xml:"description"`
	Enclosure   rssEnclosure `xml:"enclosure"`
}

type rssEnclosure struct {
	Length string `xml:"length,attr"`
}
