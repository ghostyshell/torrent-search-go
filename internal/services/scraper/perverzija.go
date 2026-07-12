package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/pkg/models"
)

// Stream is one resolved direct playable URL for a tube scene, plus a display
// name and quality label. Perverzija emits one per HLS master variant; freeporn
// videos emits one per <source> mp4 quality. The stremio Handler wraps each in
// behaviorHints.proxyHeaders.request so Stremio's streaming server re-fetches
// through the Cloudflare gate (browser UA + Referer).
type Stream struct {
	URL     string
	Name    string
	Quality string
}

const perverzijaBaseURL = "https://tube.perverzija.com"

// xtremeStreamPlayerURL is the Referer Stremio's streaming server must send for
// the Cloudflare-gated xs1.php master + variant media playlists.
const xtremeStreamPlayerURL = "https://j2.xtremestream.xyz/player/index.php"

// browserUA is a desktop Chrome UA that passes the Cloudflare gate on both
// sources' stream endpoints. Sent on every scraper fetch and on every emitted
// Stremio stream via proxyHeaders.request.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// PerverzijaScraper ingests tube.perverzija.com (a WordPress site) via the WP
// REST API, enriches each scene from its detail HTML (performers, full poster,
// xtremestream stream hash), and resolves multi-quality HLS streams from the
// xtremestream master m3u8. Not a torrent Scraper: it does not implement Search
// (it is not registered in the scraper Service); it is held by the jobs Runner
// and exposed to the stremio Handler via the PerverzijaStreamResolver
// interface, mirroring how hentai is wired.
type PerverzijaScraper struct {
	client  *http.Client
	baseURL string
}

// NewPerverzijaScraper builds a scraper. Nil client -> 30s safe client.
func NewPerverzijaScraper(client *http.Client) *PerverzijaScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &PerverzijaScraper{client: client, baseURL: perverzijaBaseURL}
}

// IngestPage walks one WP REST page (per_page=100) and returns entries with the
// listing fields filled: slug, title, detail URL, date_gmt, excerpt, wp poster,
// studios (every WP category display name), tags (post_tag taxonomy). Performers,
// full poster, description, stream hash are left for EnrichEntry.
func (s *PerverzijaScraper) IngestPage(ctx context.Context, page int) ([]models.PerverzijaEntry, error) {
	u := fmt.Sprintf("%s/wp-json/wp/v2/posts?per_page=100&page=%d&_embed=wp:featuredmedia,wp:term", s.baseURL, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req, s.baseURL)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		// WP end-of-feed signal: page beyond the last. Empty, not an error.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("perverzija ingest status %d", resp.StatusCode)
	}
	var posts []wpPost
	if err := json.NewDecoder(resp.Body).Decode(&posts); err != nil {
		return nil, err
	}
	out := make([]models.PerverzijaEntry, 0, len(posts))
	for _, p := range posts {
		e := p.toEntry()
		if e.Slug == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// EnrichEntry fetches the detail HTML and fills performers (Stars: rel=tag
// anchors), the full poster, a plain-text description, and the xtremestream
// stream hash. DetailScraped is set true on success, and also when the detail
// page is permanently gone (errPageGone) so a deleted post is not retried every
// tick. Transient fetch failures return the error and leave DetailScraped false
// (retried next tick). Best-effort: a missing block leaves the prior value.
func (s *PerverzijaScraper) EnrichEntry(ctx context.Context, e *models.PerverzijaEntry) error {
	if e == nil || e.DetailURL == "" {
		return fmt.Errorf("perverzija enrich: empty detail url")
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		if errors.Is(err, errPageGone) {
			e.DetailScraped = true
			return nil
		}
		return err
	}
	// Performers: <a href=".../stars/{slug}/" rel="tag">{Display Name}</a>.
	var performers []string
	doc.Find(`a[href*="/stars/"][rel="tag"]`).Each(func(_ int, a *goquery.Selection) {
		name := strings.TrimSpace(a.Text())
		if name != "" {
			performers = append(performers, name)
		}
	})
	// Stream hash: first xtremestream data= hash on the page.
	if hash, ok := doc.Find(`iframe[src*="xtremestream.xyz/player/index.php"]`).Attr("src"); ok {
		if m := xtremeHashRe.FindStringSubmatch(hash); m != nil {
			e.StreamHash = m[1]
		}
	}
	if e.StreamHash == "" {
		// Fallback: scan raw HTML (some embeds are JS-string, not an iframe attr).
		if html, err := doc.Html(); err == nil {
			if m := xtremeHashRe.FindStringSubmatch(html); m != nil {
				e.StreamHash = m[1]
			}
		}
	}
	// Full poster: og:image or the featured img. Falls back to wp_poster.
	if og, ok := doc.Find(`meta[property="og:image"]`).Attr("content"); ok && og != "" {
		e.Poster = og
	} else if src, ok := doc.Find(`#featured-img-id img, img.wp-post-image`).Attr("src"); ok && src != "" {
		e.Poster = src
	}
	if e.Poster == "" {
		e.Poster = e.WpPoster
	}
	// Description: og:description (plain text) beats the WP excerpt.
	if desc, ok := doc.Find(`meta[property="og:description"]`).Attr("content"); ok && desc != "" {
		e.Description = strings.TrimSpace(desc)
	}
	e.Performers = performers
	e.DetailScraped = true
	return nil
}

// ResolveStream fetches the xtremestream master m3u8 for the scene's hash and
// emits one Stream per #EXT-X-STREAM-INF variant, best quality first. Returns
// nil (no streams) if the hash is empty or the master fetch is Cloudflare-blocked.
func (s *PerverzijaScraper) ResolveStream(ctx context.Context, e models.PerverzijaEntry) ([]Stream, error) {
	if e.StreamHash == "" {
		return nil, nil
	}
	u := fmt.Sprintf("https://j2.xtremestream.xyz/player/xs1.php?data=%s", e.StreamHash)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req, xtremeStreamPlayerURL)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil // Cloudflare-blocked or gone -> clean "no streams"
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	return parseHLSVariants(body, "Perverzija", u), nil
}

// parseHLSVariants extracts one Stream per #EXT-X-STREAM-INF variant from a
// master m3u8 body, sorted best quality (tallest) first. masterURL is the m3u8
// URL, used to resolve relative variant lines and to validate each resolved URL
// is an http(s) URL on a public host before it is emitted to Stremio (a
// compromised upstream could otherwise inject an internal URL that the user's
// Stremio server would fetch via proxyHeaders). sourceName is the display-name
// prefix ("Perverzija" / "FreePornVideos").
func parseHLSVariants(body []byte, sourceName, masterURL string) []Stream {
	lines := strings.Split(string(body), "\n")
	var out []Stream
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		if i+1 >= len(lines) {
			break
		}
		raw := strings.TrimSpace(lines[i+1])
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		safe, ok := ResolveSafeStreamURL(raw, masterURL)
		if !ok {
			continue
		}
		height := hlsResolutionHeight(line)
		quality := height + "p"
		out = append(out, Stream{URL: safe, Name: sourceName + " " + quality, Quality: quality})
	}
	sort.SliceStable(out, func(i, j int) bool { return hlsQualityRank(out[i].Quality) > hlsQualityRank(out[j].Quality) })
	return out
}

// hlsResolutionHeight pulls the RESOLUTION={W}x{H} height from an EXT-X-STREAM-INF line.
func hlsResolutionHeight(line string) string {
	m := hlsResRe.FindStringSubmatch(line)
	if m != nil {
		return m[1]
	}
	return ""
}

// hlsQualityRank maps a "480p"/"720p"/"1080p" quality to a numeric rank for
// best-first sort; unknown -> 0.
func hlsQualityRank(q string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(q, "p"))
	return n
}

var (
	// ponytail: {20,200} caps the hash length so a compromised detail page cannot
	// store a megabyte-scale hex string that ResolveStream would later ship to
	// xtremestream as a huge query string. Real hashes are fixed-length (<64).
	xtremeHashRe = regexp.MustCompile(`data=([a-f0-9]{20,200})`)
	hlsResRe     = regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
)

// wpPost is the subset of the WP REST /wp/v2/posts response we ingest.
type wpPost struct {
	Slug  string `json:"slug"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	DateGmt string `json:"date_gmt"`
	Excerpt struct {
		Rendered string `json:"rendered"`
	} `json:"excerpt"`
	Link  string `json:"link"`
	Embed struct {
		FeaturedMedia []struct {
			SourceURL string `json:"source_url"`
		} `json:"wp:featuredmedia"`
		Term [][]wpTerm `json:"wp:term"`
	} `json:"_embedded"`
}

type wpTerm struct {
	Taxonomy string `json:"taxonomy"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
}

// toEntry maps a WP REST post to a PerverzijaEntry with listing fields filled.
func (p wpPost) toEntry() models.PerverzijaEntry {
	e := models.PerverzijaEntry{
		Slug:      p.Slug,
		Title:     decodeEntities(stripTags(p.Title.Rendered)),
		DetailURL: p.Link,
		Date:      p.DateGmt,
		Excerpt:   cleanText(stripTags(p.Excerpt.Rendered)),
	}
	for _, media := range p.Embed.FeaturedMedia {
		if media.SourceURL != "" {
			e.WpPoster = media.SourceURL
			break
		}
	}
	for _, group := range p.Embed.Term {
		for _, t := range group {
			name := decodeEntities(t.Name)
			if name == "" {
				continue
			}
			switch t.Taxonomy {
			case "category":
				e.Studios = append(e.Studios, name)
			case "post_tag":
				e.Tags = append(e.Tags, name)
			}
		}
	}
	return e
}

// fetchDoc GETs url with browser headers and returns a goquery document.
func (s *PerverzijaScraper) fetchDoc(ctx context.Context, url, referer string) (*goquery.Document, error) {
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
		return nil, fmt.Errorf("perverzija fetch %s status %d", url, resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}
