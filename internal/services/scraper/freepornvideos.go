package scraper

import (
	"context"
	"encoding/json"
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

const freepornvideosBaseURL = "https://www.freepornvideos.xxx"

// FreepornvideosScraper ingests freepornvideos.xxx card HTML, enriches each
// scene from its detail page (JSON-LD uploadDate + duration, categories,
// network), and resolves multi-quality mp4 streams from the detail <source>
// tags. Like PerverzijaScraper it is not a torrent Scraper and is not
// registered in the scraper Service; it is held by the jobs Runner and exposed
// to the stremio Handler via FreepornvideosStreamResolver.
type FreepornvideosScraper struct {
	client  *http.Client
	baseURL string
}

// NewFreepornvideosScraper builds a scraper. Nil client -> 30s safe client.
func NewFreepornvideosScraper(client *http.Client) *FreepornvideosScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &FreepornvideosScraper{client: client, baseURL: freepornvideosBaseURL}
}

var fpvVideoIDRe = regexp.MustCompile(`/videos/(\d+)/([^/]+)/?`)

// IngestPage walks one /latest-updates/{N}/ page and returns entries with card
// fields filled: video id, slug, title, detail URL, poster, duration, studio
// (thumb_cs channel), performers (thumb_model), rating, views, has4k. Date,
// categories, network, description are left for EnrichEntry.
func (s *FreepornvideosScraper) IngestPage(ctx context.Context, page int) ([]models.FreepornvideosEntry, error) {
	u := fmt.Sprintf("%s/latest-updates/%d/", s.baseURL, page)
	doc, err := s.fetchDoc(ctx, u, s.baseURL)
	if err != nil {
		// A 404/410 on a listing page is the feed tail: pages past the archive
		// end return 404 (not 200-empty), so fetchDoc raises errPageGone. Treat
		// it as end-of-feed (empty, nil) so the ingest loop sets hitEmpty and
		// stops, instead of retrying the gone page forever. errPageGone on a
		// detail page (EnrichEntry) means "deleted scene, stop retrying"; here,
		// in the listing walk, it means "no more pages". A transient mid-feed
		// 404 is rare and the cursor reset on hitEmpty re-walks idempotently.
		if errors.Is(err, errPageGone) {
			return nil, nil
		}
		return nil, err
	}
	var out []models.FreepornvideosEntry
	doc.Find("div.item").Each(func(_ int, item *goquery.Selection) {
		e, ok := s.parseCard(item)
		if !ok {
			return
		}
		out = append(out, e)
	})
	return out, nil
}

// parseCard extracts one card into a FreepornvideosEntry. Returns ok=false for
// non-video cards (ads, sponsor blocks) with no /videos/{id}/ link.
func (s *FreepornvideosScraper) parseCard(item *goquery.Selection) (models.FreepornvideosEntry, bool) {
	href, ok := item.Find(`a.thumb_img`).Attr("href")
	if !ok {
		// Fall back to any /videos/{id}/ link in the card.
		href, _ = item.Find(`a[href*="/videos/"]`).Attr("href")
	}
	m := fpvVideoIDRe.FindStringSubmatch(href)
	if m == nil {
		return models.FreepornvideosEntry{}, false
	}
	title, _ := item.Find(`a.thumb_img`).Attr("title")
	if title == "" {
		title = strings.TrimSpace(item.Find(`strong.title`).Text())
	}
	slug := m[2]
	if clean := StripHeaderUnsafe(slug); clean != slug || clean == "" {
		// CRLF/control chars in a URL slug mean an injected href (goquery decoded
		// &#13;&#10; to literal \r\n) or garbage - never a real slug. Drop the card
		// rather than store a detail URL whose Referer could carry header-splitting
		// bytes. The emission-point strip in tubeStreamsToStremio still guards any
		// already-stored entry from before this fix.
		return models.FreepornvideosEntry{}, false
	}
	e := models.FreepornvideosEntry{
		VideoID:   m[1],
		Slug:      slug,
		Title:     cleanText(title),
		DetailURL: fmt.Sprintf("%s/videos/%s/%s/", s.baseURL, m[1], slug),
	}
	if src, ok := item.Find(`img.thumb`).Attr("src"); ok && src != "" {
		e.Poster = src
	}
	if d := strings.TrimSpace(item.Find(`span.duration`).Text()); d != "" {
		e.Duration = strings.TrimSpace(strings.TrimPrefix(d, "Full Video"))
	}
	// Studio = the channel/site (thumb_cs); performers = thumb_model.
	item.Find(`a.models__item.thumb_cs span`).Each(func(_ int, sp *goquery.Selection) {
		if name := cleanText(sp.Text()); name != "" && e.Studio == "" {
			e.Studio = name
		}
	})
	var performers []string
	item.Find(`a.models__item.thumb_model span`).Each(func(_ int, sp *goquery.Selection) {
		if name := cleanText(sp.Text()); name != "" {
			performers = append(performers, name)
		}
	})
	e.Performers = performers
	if r := cleanText(item.Find(`div.rating`).Text()); r != "" {
		e.Rating = r
	}
	if v := cleanText(item.Find(`div.views`).Text()); v != "" {
		e.Views = v
	}
	if item.Find(`div.k4`).Length() > 0 {
		e.Has4K = true
	}
	return e, true
}

// EnrichEntry fetches the detail page and fills the JSON-LD uploadDate (the
// sort key, only present on detail), the ISO8601 duration, categories
// (a.btn_tag), the network (a.btn_sponsor_group), and a plain-text
// description (og:description). DetailScraped is set true on success, and also
// when the detail page is permanently gone (errPageGone) so a deleted post is
// not retried every tick. Transient fetch failures return the error and leave
// DetailScraped false (retried next tick).
func (s *FreepornvideosScraper) EnrichEntry(ctx context.Context, e *models.FreepornvideosEntry) error {
	if e == nil || e.DetailURL == "" {
		return fmt.Errorf("freepornvideos enrich: empty detail url")
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		if errors.Is(err, errPageGone) {
			e.DetailScraped = true
			return nil
		}
		return err
	}
	if ld := extractVideoObjectJSONLD(doc); ld != nil {
		if ld.UploadDate != "" {
			e.Date = ld.UploadDate
		}
		if ld.Duration != "" {
			e.Duration = iso8601ToReadable(ld.Duration)
		}
	}
	var categories []string
	doc.Find(`a.btn_tag`).Each(func(_ int, a *goquery.Selection) {
		if name := cleanText(a.Text()); name != "" {
			categories = append(categories, name)
		}
	})
	if len(categories) > 0 {
		e.Categories = categories
	}
	doc.Find(`a.btn_sponsor_group span`).Each(func(_ int, sp *goquery.Selection) {
		if name := cleanText(sp.Text()); name != "" && e.Network == "" {
			e.Network = name
		}
	})
	if desc, ok := doc.Find(`meta[property="og:description"]`).Attr("content"); ok && desc != "" {
		e.Description = strings.TrimSpace(desc)
	}
	e.DetailScraped = true
	return nil
}

// ResolveStream re-fetches the detail page (the mp4 token rotates per request)
// and emits one Stream per <source> quality, best quality first. Returns nil
// (no streams) if the fetch fails or no sources are present.
func (s *FreepornvideosScraper) ResolveStream(ctx context.Context, e models.FreepornvideosEntry) ([]Stream, error) {
	if e.DetailURL == "" {
		return nil, nil
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		return nil, nil
	}
	var out []Stream
	doc.Find(`video.video-js source`).Each(func(_ int, src *goquery.Selection) {
		raw, ok := src.Attr("src")
		if !ok || raw == "" {
			return
		}
		safe, ok := ResolveSafeStreamURL(raw, e.DetailURL)
		if !ok {
			// Unparseable, non-http(s), or an internal host a compromised upstream
			// could inject so the user's Stremio server fetches it via proxyHeaders.
			return
		}
		label, _ := src.Attr("label")
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		out = append(out, Stream{URL: safe, Name: "FreePornVideos " + label, Quality: label})
	})
	sort.SliceStable(out, func(i, j int) bool { return fpvQualityRank(out[i].Quality) > fpvQualityRank(out[j].Quality) })
	return out, nil
}

// fpvQualityRank maps a "<source> label" ("480p"/"720p"/"2160p") to a numeric
// rank for best-first sort; unknown -> 0.
func fpvQualityRank(q string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(q, "p"))
	return n
}

// fetchDoc GETs url with browser headers and returns a goquery document.
func (s *FreepornvideosScraper) fetchDoc(ctx context.Context, url, referer string) (*goquery.Document, error) {
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
		return nil, fmt.Errorf("freepornvideos fetch %s status %d", url, resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

// fpvVideoObject is the subset of the detail page JSON-LD VideoObject we read.
type fpvVideoObject struct {
	UploadDate string `json:"uploadDate"`
	Duration   string `json:"duration"`
}

// extractVideoObjectJSONLD finds the first application/ld+json VideoObject in
// the document and returns its uploadDate/duration, or nil if none.
func extractVideoObjectJSONLD(doc *goquery.Document) *fpvVideoObject {
	var v *fpvVideoObject
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, sc *goquery.Selection) bool {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			return true
		}
		var obj fpvVideoObject
		if err := json.Unmarshal([]byte(raw), &obj); err == nil && (obj.UploadDate != "" || obj.Duration != "") {
			v = &obj
			return false
		}
		return true
	})
	return v
}

var iso8601Re = regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)

// iso8601ToReadable converts an ISO8601 duration ("PT0H33M21S") to "HH:MM:SS"
// (zero-padded) for the Stremio runtime field. Returns the input unchanged if it
// does not match.
func iso8601ToReadable(s string) string {
	m := iso8601Re.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	sec, _ := strconv.Atoi(m[3])
	return fmt.Sprintf("%02d:%02d:%02d", h, min, sec)
}
