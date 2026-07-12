package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/pkg/models"
)

const yespornBaseURL = "https://yesporn.vip"

// YespornScraper ingests yesporn.vip card HTML, enriches each scene from its
// detail page (og: release_date/duration/image/description, channel links, and
// the player-config JS object carrying categories + models), and resolves
// multi-quality mp4 streams from the player-config video_url/video_alt_url[N]
// (strip the `function/0/` prefix; the token rotates per request). yesporn.vip
// runs the same KernelTeam tube script as freepornvideos.xxx, so this is a
// near-clone of FreepornvideosScraper in shape but with yesporn's selectors. Like
// FreepornvideosScraper it is not a torrent Scraper and is not registered in the
// scraper Service; it is held by the jobs Runner and exposed to the stremio
// Handler via YespornStreamResolver.
type YespornScraper struct {
	client  *http.Client
	baseURL string
}

// NewYespornScraper builds a scraper. Nil client -> 30s safe client.
func NewYespornScraper(client *http.Client) *YespornScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &YespornScraper{client: client, baseURL: yespornBaseURL}
}

// ypvVideoIDRe matches a yesporn card/detail href /video/{id}/{slug}/. The slug
// carries the literal -bjy9kc suffix on every card; store it whole.
var ypvVideoIDRe = regexp.MustCompile(`/video/(\d+)/([^/?#]+)`)

// IngestPage walks one /latest-updates/{N}/ page (1-indexed; page 1 is the
// homepage) and returns entries with card fields filled: video id, slug, title,
// detail URL, poster (data-original; the src is a 1x1 gif placeholder), duration,
// HD badge. Studios/tags/performers/date/description are left for EnrichEntry.
func (s *YespornScraper) IngestPage(ctx context.Context, page int) ([]models.YespornEntry, error) {
	u := fmt.Sprintf("%s/latest-updates/%d/", s.baseURL, page)
	doc, err := s.fetchDoc(ctx, u, s.baseURL)
	if err != nil {
		// A 404/410 listing page is the feed tail (pages past the archive end 404,
		// not 200-empty): treat errPageGone as end-of-feed so the ingest loop sets
		// hitEmpty and stops instead of retrying the gone page forever.
		if errors.Is(err, errPageGone) {
			return nil, nil
		}
		return nil, err
	}
	var out []models.YespornEntry
	doc.Find("div.thumb.item").Each(func(_ int, item *goquery.Selection) {
		e, ok := s.parseCard(item)
		if !ok {
			return
		}
		out = append(out, e)
	})
	return out, nil
}

// parseCard extracts one card into a YespornEntry. Returns ok=false for
// non-video cards / ads with no /video/{id}/ link, or a slug carrying CRLF
// (an injected href; goquery decodes &#13;&#10; to literal \r\n) - never store a
// detail URL whose Referer could carry header-splitting bytes.
func (s *YespornScraper) parseCard(item *goquery.Selection) (models.YespornEntry, bool) {
	href, ok := item.Find(`a[href*="/video/"]`).Attr("href")
	if !ok {
		return models.YespornEntry{}, false
	}
	m := ypvVideoIDRe.FindStringSubmatch(href)
	if m == nil {
		return models.YespornEntry{}, false
	}
	title, _ := item.Find(`a[href*="/video/"]`).Attr("title")
	if title == "" {
		title = strings.TrimSpace(item.Find(`strong.title`).Text())
	}
	slug := strings.TrimSuffix(m[2], "/")
	if clean := StripHeaderUnsafe(slug); clean != slug || clean == "" {
		return models.YespornEntry{}, false
	}
	e := models.YespornEntry{
		VideoID:   m[1],
		Slug:      slug,
		Title:     cleanText(title),
		DetailURL: fmt.Sprintf("%s/video/%s/%s/", s.baseURL, m[1], slug),
	}
	// Poster lives in data-original (src is a 1x1 gif placeholder); data-webp is
	// the higher-res variant but data-original is the stable jpg.
	if src, ok := item.Find(`img.lazy-load`).Attr("data-original"); ok && src != "" {
		e.Poster = src
	}
	if d := strings.TrimSpace(item.Find(`div.time`).Text()); d != "" {
		e.Duration = d // card shows MM:SS; enrich overwrites with detail seconds
	}
	// yesporn caps at 1080p so Has4K stays false; the HD badge (div.qualtiy, sic)
	// carries no 4K signal and is not read.
	return e, true
}

// EnrichEntry fetches the detail page and fills the og: release_date (sort key)
// + duration (seconds -> HH:MM:SS) + description + full poster (og:image), the
// channel links (Studios, multi-key), and the player-config JS object's
// video_categories (Tags) + video_models (Performers). DetailScraped is set true
// on success and on a permanently-gone page (errPageGone) so a deleted post is
// not retried every tick. Transient fetch failures return the error and leave
// DetailScraped false (retried next tick).
func (s *YespornScraper) EnrichEntry(ctx context.Context, e *models.YespornEntry) error {
	if e == nil || e.DetailURL == "" {
		return fmt.Errorf("yesporn enrich: empty detail url")
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		if errors.Is(err, errPageGone) {
			e.DetailScraped = true
			return nil
		}
		return err
	}
	if v, ok := doc.Find(`meta[property="video:release_date"]`).Attr("content"); ok && v != "" {
		e.Date = strings.TrimSpace(v)
	}
	if v, ok := doc.Find(`meta[property="video:duration"]`).Attr("content"); ok && v != "" {
		if d := secondsToReadable(v); d != "" {
			e.Duration = d
		}
	}
	if v, ok := doc.Find(`meta[property="og:image"]`).Attr("content"); ok && v != "" {
		e.Poster = strings.TrimSpace(v)
	}
	if v, ok := doc.Find(`meta[property="og:description"]`).Attr("content"); ok && v != "" {
		e.Description = normalizeDashes(strings.TrimSpace(v))
	}
	// Studios = channel link text (/channels/{slug}/). Multi-key (a scene can
	// list several channels); the link text is the display name (e.g. "TEAM SKEET").
	var studios []string
	doc.Find(`a[href*="/channels/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if href == "" || strings.HasSuffix(href, "/channels/") {
			return
		}
		if name := cleanText(a.Text()); name != "" {
			studios = append(studios, name)
		}
	})
	if len(studios) > 0 {
		e.Studios = studios
	}
	// Tags + Performers come from the player-config JS object (video_categories /
	// video_models), not from HTML links: the detail page renders them as JS
	// string fields, not /categories/{slug}/ anchors. Categories are comma-
	// separated display names; models likewise.
	cfg := extractYespornPlayerConfig(doc)
	if len(cfg.Categories) > 0 {
		e.Tags = cfg.Categories
	}
	if len(cfg.Models) > 0 {
		e.Performers = cfg.Models
	}
	e.DetailScraped = true
	return nil
}

// ResolveStream re-fetches the detail page (the /get_file token rotates per
// request) and emits one Stream per player-config video_url / video_alt_url[N]
// quality (strip the `function/0/` prefix), best quality first. Returns nil
// (no streams) if the fetch fails or no URLs are present.
func (s *YespornScraper) ResolveStream(ctx context.Context, e models.YespornEntry) ([]Stream, error) {
	if e.DetailURL == "" {
		return nil, nil
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		return nil, nil
	}
	pairs := extractYespornPlayerConfig(doc).Streams
	var out []Stream
	for _, p := range pairs {
		safe, ok := ResolveSafeStreamURL(p.URL, e.DetailURL)
		if !ok {
			// Unparseable, non-http(s), or an internal host a compromised upstream
			// could inject so the user's Stremio server fetches it via proxyHeaders.
			continue
		}
		label := strings.TrimSpace(p.Label)
		if label == "" {
			continue
		}
		out = append(out, Stream{URL: safe, Name: "YesPorn " + label, Quality: label})
	}
	sort.SliceStable(out, func(i, j int) bool { return ypvQualityRank(out[i].Quality) > ypvQualityRank(out[j].Quality) })
	return out, nil
}

// ypvQualityRank maps a player-config label ("480p"/"720p"/"1080p") to a numeric
// rank for best-first sort; unknown -> 0.
func ypvQualityRank(q string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(q, "p"))
	return n
}

// fetchDoc GETs url with browser headers and returns a goquery document.
func (s *YespornScraper) fetchDoc(ctx context.Context, url, referer string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req, referer)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
		return nil, errPageGone
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yesporn fetch %s status %d", url, resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

// ypvPlayerConfig is the subset of the detail-page player-config JS object we
// read: the stream URL/label pairs, and the comma-separated categories + models.
type ypvPlayerConfig struct {
	Streams    []ypvStreamPair
	Categories []string
	Models     []string
}

type ypvStreamPair struct {
	Key   string // video_url / video_alt_url / video_alt_url2 ...
	URL   string
	Label string
}

// extractYespornPlayerConfig parses the detail-page inline JS config object for
// the stream URL+label pairs and the categories/models lists. The config is a
// sequence of `key: 'value',` fields inside a <script>; we scan the full script
// text with regexes rather than JSON-decode (it is a JS object literal, not
// valid JSON - trailing commas, unquoted keys, single quotes).
func extractYespornPlayerConfig(doc *goquery.Document) ypvPlayerConfig {
	var raw string
	doc.Find(`script`).EachWithBreak(func(_ int, sc *goquery.Selection) bool {
		t := sc.Text()
		if strings.Contains(t, "video_url:") && strings.Contains(t, "function/0/") {
			raw = t
			return false
		}
		return true
	})
	if raw == "" {
		return ypvPlayerConfig{}
	}
	cfg := ypvPlayerConfig{Streams: ypvExtractStreams(raw)}
	if v := ypvStringField(raw, "video_categories"); v != "" {
		cfg.Categories = splitComma(v)
	}
	if v := ypvStringField(raw, "video_models"); v != "" {
		cfg.Models = splitComma(v)
	}
	return cfg
}

// ypvStreamURLRe captures the identifier (video_url / video_alt_url / video_alt_url2)
// and the URL after the `function/0/` prefix. The URL ends at the closing quote.
var ypvStreamURLRe = regexp.MustCompile(`(video(?:_alt)?_url\d*):\s*'function/0/([^']+)'`)
var ypvStreamLabelRe = regexp.MustCompile(`(video(?:_alt)?_url\d*)_text:\s*'([^']+)'`)

// ypvExtractStreams pairs each video_url[N] with its _text label by identifier.
// Stable left-to-right order (video_url, then video_alt_url, video_alt_url2, ...);
// duplicate keys keep only the first occurrence.
func ypvExtractStreams(raw string) []ypvStreamPair {
	labels := map[string]string{}
	for _, m := range ypvStreamLabelRe.FindAllStringSubmatch(raw, -1) {
		labels[m[1]] = m[2]
	}
	var out []ypvStreamPair
	seen := map[string]bool{}
	for _, m := range ypvStreamURLRe.FindAllStringSubmatch(raw, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ypvStreamPair{Key: key, URL: m[2], Label: labels[key]})
	}
	return out
}

// ypvStringFieldRe captures a single-quoted JS string field value for the given
// key. Used for video_categories / video_models (comma-separated display names).
var ypvStringFieldRe = regexp.MustCompile(`\b([A-Za-z_]+):\s*'([^']*)'`)

func ypvStringField(raw, key string) string {
	for _, m := range ypvStringFieldRe.FindAllStringSubmatch(raw, -1) {
		if m[1] == key {
			return m[2]
		}
	}
	return ""
}

// splitComma splits a comma-separated JS string field into trimmed non-empty
// display names (video_categories: 'Brunette, Outdoor' -> ["Brunette","Outdoor"]).
func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, normalizeDashes(p))
		}
	}
	return out
}

// secondsToReadable converts a whole-second count ("1855") to "HH:MM:SS" for the
// Stremio runtime field. Returns "" for a non-numeric input.
func secondsToReadable(s string) string {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return ""
	}
	h := n / 3600
	m := (n % 3600) / 60
	sec := n % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, sec)
}