package providers

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/internal/services/streams"
)

const bitsearchBase = "https://bitsearch.eu"

// BitsearchProvider scrapes Bitsearch search result cards.
type BitsearchProvider struct {
	client *streams.HTTPClient
	base   string
}

// NewBitsearchProvider creates a Bitsearch provider.
func NewBitsearchProvider(client *streams.HTTPClient) *BitsearchProvider {
	return &BitsearchProvider{client: client, base: bitsearchBase}
}

func (b *BitsearchProvider) ID() string   { return "bitsearch" }
func (b *BitsearchProvider) Name() string { return "Bitsearch" }

func (b *BitsearchProvider) Scrape(ctx context.Context, req streams.Request) ([]streams.Stream, error) {
	query := streams.BuildSearchQuery(req)
	searchURL := fmt.Sprintf("%s/search?q=%s&sort=seeders", b.base, url.QueryEscape(query))

	data, err := b.client.GetText(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	out := make([]streams.Stream, 0, 30)

	doc.Find(`a[href^="magnet:"]`).Each(func(i int, s *goquery.Selection) {
		if len(out) >= 30 {
			return
		}
		card := s.Closest("div.bg-white")
		if card.Length() == 0 {
			return
		}

		magnet, _ := s.Attr("href")
		infoHash := extractBitsearchInfoHash(magnet)
		if infoHash == "" || seen[infoHash] {
			return
		}

		title := strings.TrimSpace(card.Find(`a[href^="/torrent/"]`).First().Text())
		if title == "" {
			return
		}
		title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")

		text := strings.TrimSpace(card.Text())
		text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

		parsed := streams.ParseTitle(title)
		seen[infoHash] = true
		out = append(out, streams.Stream{
			InfoHash:  infoHash,
			Title:     title,
			Seeders:   parseLabeledInteger(text, "seeders"),
			Leechers:  parseLabeledInteger(text, "leechers"),
			Size:      parseBitsearchSize(text),
			Provider:  "Bitsearch",
			IMDbID:    req.IMDbID,
			Quality:   parsed.Quality,
			Codec:     parsed.Codec,
			Source:    parsed.Source,
			HDR:       parsed.HDR,
			Bitdepth:  parsed.Bitdepth,
			Languages: parsed.Languages,
			Trackers:  streams.DefaultTrackers,
		})
	})

	return out, nil
}

func extractBitsearchInfoHash(magnet string) string {
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

func parseLabeledInteger(text, label string) int {
	re := regexp.MustCompile(`(?i)([\d,]+)\s+` + regexp.QuoteMeta(label))
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	return n
}

func parseBitsearchSize(text string) int64 {
	matches := regexp.MustCompile(`(?i)([\d.]+)\s*(KB|MB|GB|TB|B)\b`).FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return 0
	}
	m := matches[len(matches)-1]
	value, _ := strconv.ParseFloat(m[1], 64)
	units := map[string]int64{"b": 1, "kb": 1024, "mb": 1024 * 1024, "gb": 1024 * 1024 * 1024, "tb": 1024 * 1024 * 1024 * 1024}
	return int64(value * float64(units[strings.ToLower(m[2])]))
}
