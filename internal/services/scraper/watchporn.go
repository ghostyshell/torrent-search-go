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

const watchpornBaseURL = "https://watchporn.to"

// WatchpornScraper ingests watchporn.to card HTML, enriches each scene from its
// detail page (og: release_date/duration/image/description, and the single__info
// block's HTML links: /categories/ -> Studios, /models/ -> Performers, /tags/ ->
// Tags), and resolves multi-quality mp4 streams from the player-config
// video_url/video_alt_url[N] (NO function/0/ prefix on this site; the v-acctoken
// query param rotates per request). watchporn.to runs the same KernelTeam tube
// script family as yesporn.vip / freepornvideos.xxx, so the card parsing is a
// near-clone of YespornScraper; the detail page renders richer HTML links than
// yesporn's JS-only config, so EnrichEntry reads those instead. Like the other
// tube scrapers it is not a torrent Scraper and is not registered in the scraper
// Service; it is held by the jobs Runner (for the Mac-cron ingest/enrich) and
// exposed to the stremio Handler via WatchpornStreamResolver.
type WatchpornScraper struct {
	client  *http.Client
	baseURL string
}

// NewWatchpornScraper builds a scraper. Nil client -> 30s safe client.
func NewWatchpornScraper(client *http.Client) *WatchpornScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &WatchpornScraper{client: client, baseURL: watchpornBaseURL}
}

// WatchpornBaseURL exposes the site origin for the stremio StreamReferer fallback.
func WatchpornBaseURL() string { return watchpornBaseURL }

// wptVideoIDRe matches a watchporn card/detail href /video/{id}/{slug}/.
var wptVideoIDRe = regexp.MustCompile(`/video/(\d+)/([^/?#]+)`)

// IngestPage walks one /latest-updates/{N}/ page (1-indexed; page 1 is the
// homepage) and returns entries with card fields filled: video id, slug, title,
// detail URL, poster (data-original; the src is a 1x1 gif placeholder), duration.
// Studios/tags/performers/date/description are left for EnrichEntry.
func (s *WatchpornScraper) IngestPage(ctx context.Context, page int) ([]models.WatchpornEntry, error) {
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
	var out []models.WatchpornEntry
	doc.Find("div.thumb.item").Each(func(_ int, item *goquery.Selection) {
		e, ok := s.parseCard(item)
		if !ok {
			return
		}
		out = append(out, e)
	})
	return out, nil
}

// parseCard extracts one card into a WatchpornEntry. Returns ok=false for
// non-video cards / ads with no /video/{id}/ link, or a slug carrying CRLF
// (an injected href; goquery decodes &#13;&#10; to literal \r\n) - never store a
// detail URL whose Referer could carry header-splitting bytes.
func (s *WatchpornScraper) parseCard(item *goquery.Selection) (models.WatchpornEntry, bool) {
	href, ok := item.Find(`a[href*="/video/"]`).Attr("href")
	if !ok {
		return models.WatchpornEntry{}, false
	}
	m := wptVideoIDRe.FindStringSubmatch(href)
	if m == nil {
		return models.WatchpornEntry{}, false
	}
	title, _ := item.Find(`a[href*="/video/"]`).Attr("title")
	if title == "" {
		title = strings.TrimSpace(item.Find(`strong.title`).Text())
	}
	slug := strings.TrimSuffix(m[2], "/")
	if clean := StripHeaderUnsafe(slug); clean != slug || clean == "" {
		return models.WatchpornEntry{}, false
	}
	e := models.WatchpornEntry{
		VideoID:   m[1],
		Slug:      slug,
		Title:     cleanText(title),
		DetailURL: fmt.Sprintf("%s/video/%s/%s/", s.baseURL, m[1], slug),
	}
	// Poster lives in data-original (src is a 1x1 gif placeholder).
	if src, ok := item.Find(`img.lazy-load`).Attr("data-original"); ok && src != "" {
		e.Poster = src
	}
	// watchporn renders the card runtime in span.thumb__info-item (not div.time).
	if d := strings.TrimSpace(item.Find(`span.thumb__info-item`).First().Text()); d != "" {
		e.Duration = d // card shows MM:SS; enrich overwrites with detail seconds
	}
	// watchporn caps at 1080p so Has4K stays false; no 4K badge is read.
	return e, true
}

// EnrichEntry fetches the detail page and fills the og: release_date (sort key)
// + duration (seconds -> HH:MM:SS) + description + full poster (og:image), and
// the single__info block's HTML links: /categories/ -> Studios (the site/network,
// multi-key), /models/ -> Performers, /tags/ -> Tags. DetailScraped is set true
// on success and on a permanently-gone page (errPageGone) so a deleted post is
// not retried every tick. Transient fetch failures return the error and leave
// DetailScraped false (retried next tick).
func (s *WatchpornScraper) EnrichEntry(ctx context.Context, e *models.WatchpornEntry) error {
	if e == nil || e.DetailURL == "" {
		return fmt.Errorf("watchporn enrich: empty detail url")
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
	// Studios = /categories/ link text (the site/network, e.g. "ManyVids").
	// Multi-key (a scene can list several); the link text is the display name.
	// Scoped to a.single__info-tag (the info-block tag chips) so a sidebar/related
	// widget that later adds /categories/ links does not pollute Studios. Verified
	// against the live detail page: every info-block link carries single__info-tag.
	var studios []string
	doc.Find(`a.single__info-tag[href*="/categories/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if href == "" || strings.HasSuffix(href, "/categories/") {
			return
		}
		if name := cleanText(a.Text()); name != "" {
			studios = append(studios, name)
		}
	})
	if len(studios) > 0 {
		e.Studios = studios
	}
	// Performers = /models/ link text (e.g. "Jasmine Jae"). Scoped to single__info-tag.
	var performers []string
	doc.Find(`a.single__info-tag[href*="/models/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if href == "" || strings.HasSuffix(href, "/models/") {
			return
		}
		if name := cleanText(a.Text()); name != "" {
			performers = append(performers, name)
		}
	})
	if len(performers) > 0 {
		e.Performers = performers
	}
	// Tags = /tags/ link text (the content tags, e.g. "milf", "brunette", "pov").
	// Scoped to single__info-tag so a future tag-cloud widget does not pollute Tags.
	var tags []string
	doc.Find(`a.single__info-tag[href*="/tags/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if href == "" || strings.HasSuffix(href, "/tags/") {
			return
		}
		if name := cleanText(a.Text()); name != "" {
			tags = append(tags, name)
		}
	})
	if len(tags) > 0 {
		e.Tags = tags
	}
	e.DetailScraped = true
	return nil
}

// ResolveStream re-fetches the detail page (the /get_file v-acctoken rotates per
// request) and emits one Stream per player-config video_url / video_alt_url[N]
// quality (NO function/0/ prefix on this site), best quality first. Returns nil
// (no streams) if the fetch fails or no URLs are present.
func (s *WatchpornScraper) ResolveStream(ctx context.Context, e models.WatchpornEntry) ([]Stream, error) {
	if e.DetailURL == "" {
		return nil, nil
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		return nil, nil
	}
	pairs := extractWatchpornPlayerConfig(doc).Streams
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
		out = append(out, Stream{URL: safe, Name: "WatchPorn " + label, Quality: label})
	}
	sort.SliceStable(out, func(i, j int) bool { return wptQualityRank(out[i].Quality) > wptQualityRank(out[j].Quality) })
	return out, nil
}

// wptQualityRank maps a player-config label ("480p"/"720p"/"1080p") to a numeric
// rank for best-first sort; unknown -> 0.
func wptQualityRank(q string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(q, "p"))
	return n
}

// fetchDoc GETs url with browser headers and returns a goquery document.
func (s *WatchpornScraper) fetchDoc(ctx context.Context, url, referer string) (*goquery.Document, error) {
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
		return nil, fmt.Errorf("watchporn fetch %s status %d", url, resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

// wptPlayerConfig is the subset of the detail-page player-config JS object we
// read: the stream URL/label pairs. Categories/models come from HTML links, not
// the JS config, so they are not read here.
type wptPlayerConfig struct {
	Streams []wptStreamPair
}

type wptStreamPair struct {
	Key   string // video_url / video_alt_url / video_alt_url2 ...
	URL   string
	Label string
}

// extractWatchpornPlayerConfig parses the detail-page inline JS config object for
// the stream URL+label pairs. The config is a sequence of `key: 'value',` fields
// inside a <script>; we scan the full script text with regexes rather than
// JSON-decode (it is a JS object literal, not valid JSON - trailing commas,
// unquoted keys, single quotes). Unlike yesporn, watchporn's video_url has no
// function/0/ prefix, so the URL regex captures the full value verbatim.
func extractWatchpornPlayerConfig(doc *goquery.Document) wptPlayerConfig {
	var raw string
	doc.Find(`script`).EachWithBreak(func(_ int, sc *goquery.Selection) bool {
		t := sc.Text()
		if strings.Contains(t, "video_url:") {
			raw = t
			return false
		}
		return true
	})
	if raw == "" {
		return wptPlayerConfig{}
	}
	return wptPlayerConfig{Streams: wptExtractStreams(raw)}
}

// wptStreamURLRe captures the identifier (video_url / video_alt_url / video_alt_url2)
// and the full URL value (no function/0/ prefix on this site). The URL ends at the
// closing quote; the rotating v-acctoken is part of the URL and is never stored
// outside the in-memory Stream emitted at resolve time.
var wptStreamURLRe = regexp.MustCompile(`(video(?:_alt)?_url\d*):\s*'([^']+)'`)
var wptStreamLabelRe = regexp.MustCompile(`(video(?:_alt)?_url\d*)_text:\s*'([^']+)'`)

// wptExtractStreams pairs each video_url[N] with its _text label by identifier.
// Stable left-to-right order (video_url, then video_alt_url, video_alt_url2, ...);
// duplicate keys keep only the first occurrence. The _text fields use the same
// identifier group so the label regex captures video_url_text / video_alt_url_text
// (the digit is part of the captured identifier for the _url2 variants).
func wptExtractStreams(raw string) []wptStreamPair {
	labels := map[string]string{}
	for _, m := range wptStreamLabelRe.FindAllStringSubmatch(raw, -1) {
		labels[m[1]] = m[2]
	}
	var out []wptStreamPair
	seen := map[string]bool{}
	for _, m := range wptStreamURLRe.FindAllStringSubmatch(raw, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, wptStreamPair{Key: key, URL: m[2], Label: labels[key]})
	}
	return out
}