package scraper

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"torrent-search-go/pkg/models"
)

const porneecBaseURL = "https://porneec.com"

// PorneecScraper ingests porneec.com listing cards, enriches each scene from
// its detail page (article:published_time, og:description, and the stream mp4
// pulled from the clean-tube-player WP-plugin iframe's base64-encoded q param),
// and resolves a single tokenless Bunny CDN mp4 stream. porneec.com is a
// WordPress tube site whose WP REST API is 401-gated, so IngestPage walks the
// HTML listing /page/{N}/. The stream mp4 is stored at enrich time and emitted
// directly by ResolveStream (no re-fetch) - it is a stable, tokenless
// {sub}.b-cdn.net mp4, unlike the rotating-token yesporn/freepornvideos
// sources. Not a torrent Scraper; held by the jobs Runner and exposed to the
// stremio Handler via PorneecStreamResolver.
type PorneecScraper struct {
	client  *http.Client
	baseURL string
}

// NewPorneecScraper builds a scraper. Nil client -> 30s safe client.
func NewPorneecScraper(client *http.Client) *PorneecScraper {
	if client == nil {
		client = NewSafeClient(30 * time.Second)
	}
	return &PorneecScraper{client: client, baseURL: porneecBaseURL}
}

// PorneecBaseURL returns the porneec base URL, used as the stream Referer
// fallback when an entry has no detail URL.
func PorneecBaseURL() string { return porneecBaseURL }

// pecPostSlugRe extracts the slug from a porneec post href
// https://porneec.com/{slug}/. Slugs carry no numeric id; the doc id is "pec:"+slug.
// The trailing $ anchors the match to a single path segment so a multi-segment
// taxonomy URL (/category/{x}/, /tag/{x}/, /actor/{x}/) cannot match and be
// mistaken for a post slug (which would collide cards under one _id).
var pecPostSlugRe = regexp.MustCompile(`^https?://porneec\.com/([^/?#]+)/?$`)

// pecClassTermRe splits an <article> class attribute into its individual
// taxonomy classes so we can pick out the category-{slug} (studios) and
// actors-{slug} (performers) terms. The class list is whitespace-separated.
var pecClassTermRe = regexp.MustCompile(`\b(category|actors)-([a-z0-9-]+)\b`)

// IngestPage walks one /page/{N}/ page (1-indexed; page 1 is the homepage) and
// returns entries with card fields filled: WP post id, slug, title, detail URL,
// poster (data-main-thumb), duration, studios (category- classes, humanized),
// performers (actors- classes, humanized). Tags are left empty (porneec
// obfuscates tag slugs and exposes no post-owned tag display names).
// Date/description/stream mp4 are left for EnrichEntry. A 404/410 listing page
// (past the archive end, which 404s like other WP sites) is the feed tail.
func (s *PorneecScraper) IngestPage(ctx context.Context, page int) ([]models.PorneecEntry, error) {
	u := fmt.Sprintf("%s/page/%d/", s.baseURL, page)
	doc, err := s.fetchDoc(ctx, u, s.baseURL)
	if err != nil {
		// A 404/410 listing page is the feed tail (WP 404s past the archive end,
		// not 200-empty): treat errPageGone as end-of-feed so the ingest loop sets
		// hitEmpty and stops instead of retrying the gone page forever.
		if errors.Is(err, errPageGone) {
			return nil, nil
		}
		return nil, err
	}
	var out []models.PorneecEntry
	doc.Find("article.thumb-block").Each(func(_ int, item *goquery.Selection) {
		e, ok := s.parseCard(item)
		if !ok {
			return
		}
		out = append(out, e)
	})
	return out, nil
}

// parseCard extracts one article.thumb-block into a PorneecEntry. Returns
// ok=false for cards with no post href or a slug carrying CRLF (an injected
// href; goquery decodes &#13;&#10; to literal \r\n) - never store a detail URL
// whose Referer could carry header-splitting bytes.
func (s *PorneecScraper) parseCard(item *goquery.Selection) (models.PorneecEntry, bool) {
	// The post link is the card's single <a> wrapping the thumbnail + title.
	// Taxonomy (category/actor/tag) lives on the <article> class, not separate
	// anchors, so today there is one anchor per card. Iterate and pick the first
	// anchor whose href is a single-segment post URL (pecPostSlugRe is $-anchored),
	// so a future layout that adds a taxonomy badge link before the post link is
	// skipped instead of mistaken for the post slug and colliding cards.
	var slug, title string
	item.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, ok := a.Attr("href")
		if !ok {
			return true
		}
		m := pecPostSlugRe.FindStringSubmatch(href)
		if m == nil {
			return true // not a post link; keep scanning
		}
		if clean := StripHeaderUnsafe(m[1]); clean != m[1] || clean == "" {
			return true // injected CRLF; skip, do not store as a Referer
		}
		slug = m[1]
		if t, ok := a.Attr("title"); ok {
			title = t
		}
		return false // found the post link; stop
	})
	if slug == "" {
		return models.PorneecEntry{}, false
	}
	if title == "" {
		title = strings.TrimSpace(item.Find(`span.title`).Text())
	}
	e := models.PorneecEntry{
		VideoID:   strings.TrimSpace(item.AttrOr("data-post-id", "")),
		Slug:      slug,
		Title:     normalizeDashes(strings.TrimSpace(title)),
		DetailURL: fmt.Sprintf("%s/%s/", s.baseURL, slug),
	}
	if src, ok := item.Attr("data-main-thumb"); ok && src != "" {
		e.Poster = src
	}
	if d := strings.TrimSpace(item.Find(`span.duration`).Text()); d != "" {
		e.Duration = d
	}
	// Studios + Performers come from the article class list: category-{slug}
	// (the WP category, humanized to the studio display name) and actors-{slug}
	// (the WP actor taxonomy, humanized to the performer display name). Tags are
	// skipped: porneec obfuscates tag slugs (tag-big-ass-porn-v565h4) so humanizing
	// them yields junk, and the detail page exposes no post-owned tag names.
	classAttr, _ := item.Attr("class")
	for _, term := range pecClassTermRe.FindAllStringSubmatch(classAttr, -1) {
		name := pecHumanize(term[2])
		if name == "" {
			continue
		}
		switch term[1] {
		case "category":
			e.Studios = append(e.Studios, name)
		case "actors":
			e.Performers = append(e.Performers, name)
		}
	}
	return e, true
}

// EnrichEntry fetches the detail page and fills the article:published_time
// (sort key), og:description, and the stream mp4 decoded from the
// clean-tube-player iframe's base64 q param (a tokenless Bunny CDN mp4, stored
// on the doc so ResolveStream emits it without a re-fetch). DetailScraped is
// set true on success and on a permanently-gone page (errPageGone) so a
// deleted post is not retried every tick. Transient fetch failures return the
// error and leave DetailScraped false (retried next tick).
func (s *PorneecScraper) EnrichEntry(ctx context.Context, e *models.PorneecEntry) error {
	if e == nil || e.DetailURL == "" {
		return fmt.Errorf("porneec enrich: empty detail url")
	}
	doc, err := s.fetchDoc(ctx, e.DetailURL, s.baseURL)
	if err != nil {
		if errors.Is(err, errPageGone) {
			e.DetailScraped = true
			return nil
		}
		return err
	}
	if v, ok := doc.Find(`meta[property="article:published_time"]`).Attr("content"); ok && v != "" {
		// Keep the ISO 8601 timestamp as-is; releaseYear reads the leading 4 chars
		// and the date:-1 sort is lexicographic on the fixed YYYY-MM-DDThh:mm:ss prefix.
		e.Date = strings.TrimSpace(v)
	}
	if v, ok := doc.Find(`meta[property="og:description"]`).Attr("content"); ok && v != "" {
		e.Description = normalizeDashes(strings.TrimSpace(v))
	}
	if u := pecExtractStreamURL(doc); u != "" {
		if safe, ok := ResolveSafeStreamURL(u, e.DetailURL); ok {
			e.StreamURL = safe
		}
	}
	e.DetailScraped = true
	return nil
}

// ResolveStream emits the stored tokenless Bunny CDN mp4 directly - no re-fetch
// (the URL is stable and has no rotating token, unlike yesporn/fpv). Returns
// nil if the entry was never enriched (no stored URL).
func (s *PorneecScraper) ResolveStream(ctx context.Context, e models.PorneecEntry) ([]Stream, error) {
	if e.StreamURL == "" {
		return nil, nil
	}
	safe, ok := ResolveSafeStreamURL(e.StreamURL, e.DetailURL)
	if !ok {
		return nil, nil
	}
	return []Stream{{URL: safe, Name: "Porneec", Quality: "HD"}}, nil
}

// pecIframeSrcRe captures the player-x.php?q={base64} value from the
// clean-tube-player iframe's data-litespeed-src (and src) attribute. The base64
// decodes to a URL-encoded <video><source src="https://{sub}.b-cdn.net/{file}.mp4">.
var pecIframeSrcRe = regexp.MustCompile(`player-x\.php\?q=([A-Za-z0-9+/=]+)`)

// pecMp4Re extracts the mp4 source URL from the decoded <video> tag. Single
// quality; the first match wins. The optional trailing (\?[^"]*)? tolerates a
// query string after .mp4 so a future site change that appends ?token= does not
// silently blank stream_url (the URL today is tokenless, but cheap to harden).
var pecMp4Re = regexp.MustCompile(`src="(https?://[^"]+\.mp4(?:\?[^"]*)?)"`)

// pecExtractStreamURL finds the clean-tube-player iframe on the detail page,
// base64-decodes its q param, URL-decodes the result, and returns the mp4
// source URL. Returns "" if the iframe is absent or no mp4 is embedded.
func pecExtractStreamURL(doc *goquery.Document) string {
	var raw string
	doc.Find(`iframe`).EachWithBreak(func(_ int, f *goquery.Selection) bool {
		for _, attr := range []string{"data-litespeed-src", "src"} {
			if v, ok := f.Attr(attr); ok && strings.Contains(v, "player-x.php?q=") {
				raw = v
				return false
			}
		}
		return true
	})
	m := pecIframeSrcRe.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	dec, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return ""
	}
	tag, err := url.QueryUnescape(string(dec))
	if err != nil {
		return ""
	}
	if mm := pecMp4Re.FindStringSubmatch(tag); mm != nil {
		return mm[1]
	}
	return ""
}

// pecHumanize turns a WP taxonomy slug ("brook-logan" / "brazzers") into a
// display name ("Brook Logan" / "Brazzers"): split on "-", title-case each token,
// join with spaces. Empty result for an empty slug.
func pecHumanize(slug string) string {
	var parts []string
	for _, tok := range strings.Split(slug, "-") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(tok[:1])+tok[1:])
	}
	return strings.Join(parts, " ")
}

// fetchDoc GETs url with browser headers and returns a goquery document.
func (s *PorneecScraper) fetchDoc(ctx context.Context, url, referer string) (*goquery.Document, error) {
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
		return nil, fmt.Errorf("porneec fetch %s status %d", url, resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}
