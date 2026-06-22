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

const nekobtBase = "https://nekobt.to/api/torznab/api"

// NekobtProvider queries the nekoBT Torznab API.
type NekobtProvider struct {
	client *http.Client
}

// NewNekobtProvider creates a nekoBT provider.
func NewNekobtProvider(client *http.Client) *NekobtProvider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &NekobtProvider{client: client}
}

func (n *NekobtProvider) ID() string   { return "nekobt" }
func (n *NekobtProvider) Name() string { return "nekoBT" }

func (n *NekobtProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	if !req.IsSeries() || req.Name == "" {
		return []streams.Stream{}, nil
	}

	query := streams.BuildSearchQuery(req)
	u, _ := url.Parse(nekobtBase)
	q := u.Query()
	q.Set("t", "search")
	q.Set("q", query)
	q.Set("cat", "5070")
	u.RawQuery = q.Encode()

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept", "application/rss+xml")
	r.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := n.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nekobt returned %d", resp.StatusCode)
	}

	var feed nekobtFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}

	out := make([]streams.Stream, 0, len(feed.Items))
	for _, item := range feed.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		infoHash := item.getAttr("infohash")
		if infoHash == "" {
			magnet := item.getAttr("magneturl")
			if magnet == "" {
				magnet = item.Link
			}
			infoHash = extractNekobtInfoHash(magnet)
		}
		if infoHash == "" {
			continue
		}

		seeders := parseIntNekobt(item.getAttr("seeders"))
		peers := parseIntNekobt(item.getAttr("peers"))
		leechers := peers - seeders
		if leechers < 0 {
			leechers = 0
		}
		size := int64(0)
		if item.Size != "" {
			size, _ = strconv.ParseInt(item.Size, 10, 64)
		}
		if size == 0 {
			size, _ = strconv.ParseInt(item.getAttr("size"), 10, 64)
		}

		parsed := streams.ParseTitle(title)
		out = append(out, streams.Stream{
			InfoHash:  strings.ToLower(infoHash),
			Title:     title,
			Seeders:   seeders,
			Leechers:  leechers,
			Size:      size,
			Provider:  "nekoBT",
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

func extractNekobtInfoHash(magnet string) string {
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

func parseIntNekobt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

type nekobtFeed struct {
	Items []nekobtItem `xml:"channel>item"`
}

type nekobtItem struct {
	Title        string        `xml:"title"`
	Link         string        `xml:"link"`
	Size         string        `xml:"size"`
	Attrs        []torznabAttr `xml:"attr"`
	TorznabAttrs []torznabAttr `xml:"torznab:attr"`
}

func (i nekobtItem) getAttr(name string) string {
	for _, a := range i.Attrs {
		if a.Name == name {
			return a.Value
		}
	}
	for _, a := range i.TorznabAttrs {
		if a.Name == name {
			return a.Value
		}
	}
	return ""
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
