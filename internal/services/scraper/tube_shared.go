package scraper

import (
	"errors"
	"html"
	"net/http"
	"regexp"
	"strings"
)

// errPageGone is returned by fetchDoc when the detail page is permanently gone
// (HTTP 410 Gone or 404 Not Found). EnrichEntry treats it as "scraped, nothing to
// add": it sets DetailScraped=true and returns nil, so the enrich sweep does not
// re-fetch a deleted post every tick. Without this, permanently-404'd posts would
// accumulate at the head of the newest-first missing-detail queue and eventually
// livelock the sweep (they can never be enriched, so they never leave the queue).
// Transient failures (5xx, Cloudflare challenge, timeout, parse error) keep
// returning a plain error so the entry is retried next tick - matching the
// pornrips enrich philosophy (retry transient, don't permanently mark on a
// transient blip).
var errPageGone = errors.New("page gone")

// setBrowserHeaders sets a desktop-Chrome UA + standard accept headers on req,
// plus a Referer when referer is non-empty. Both tube sources (perverzija
// xtremestream, freepornvideos.xxx) sit behind Cloudflare and reject bare
// requests; these headers pass the gate. The same UA + Referer are stamped on
// emitted Stremio streams via behaviorHints.proxyHeaders.request so Stremio's
// streaming server re-fetches through the gate too.
func setBrowserHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

// stripTags removes all HTML tags from s. Used to flatten WP REST rendered
// fields (title/excerpt/content) to plain text for Stremio display.
func stripTags(s string) string {
	return htmlTagRe.ReplaceAllString(s, "")
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// decodeEntities unescapes HTML entities (&#8211; &amp; &quot; ...). WP REST
// title/excerpt are entity-encoded; the Stremio meta fields want raw text. The
// unescaped result is dash-normalized so scraped text rendered to Stremio never
// carries an em/en dash (per the repo's no-em-dash hard rule for public-facing
// copy): &#8211; (en dash) / &#8212; (em dash) become an ASCII hyphen.
func decodeEntities(s string) string {
	return normalizeDashes(html.UnescapeString(s))
}

// cleanText collapses runs of whitespace, trims, and dash-normalizes. WP
// excerpts have leading <p>, trailing [&hellip;] and embedded newlines; tube
// titles/categories may carry an en dash that must not reach Stremio UI.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "[&hellip;]", "...")
	s = strings.ReplaceAll(s, "[…]", "...")
	s = wsRe.ReplaceAllString(s, " ")
	return normalizeDashes(strings.TrimSpace(s))
}

// normalizeDashes replaces em dash (U+2014) and en dash (U+2013) with an ASCII
// hyphen. Scraped source text flows into Stremio catalogs/meta which are
// public-facing; the repo forbids em/en dashes there.
func normalizeDashes(s string) string {
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "—", "-")
	return s
}

// StripHeaderUnsafe removes CR, LF, and other C0 control chars (and DEL) from s.
// A compromised upstream card href can carry &#13;&#10; which goquery decodes to
// literal CRLF in the captured slug; that flows into the detail URL and the
// Referer emitted to Stremio via proxyHeaders.request, where unsanitized CRLF
// could split/inject a header on the request Stremio makes to the stream URL.
// Stripping at ingestion (the slug) and at emission (the Referer) closes both
// the stored-data and the fresh-scrape paths. Legit URL slugs never carry these.
func StripHeaderUnsafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// BrowserUA returns the desktop-Chrome User-Agent the scrapers send to pass the
// Cloudflare gate. Exported so the stremio stream handler can stamp the same UA
// on emitted streams via behaviorHints.proxyHeaders.request.
func BrowserUA() string { return browserUA }

// XtremeStreamReferer returns the Referer Stremio's streaming server must send
// when fetching perverzija HLS master/variant URLs (the Cloudflare gate keys on
// it). Exported for the perverzija stream handler.
func XtremeStreamReferer() string { return xtremeStreamPlayerURL }

// FreepornvideosBaseURL returns the freepornvideos.xxx base URL, used as the
// fallback Referer for emitted freepornvideos mp4 streams when an entry has no
// detail URL. Exported for the freepornvideos stream handler.
func FreepornvideosBaseURL() string { return freepornvideosBaseURL }

// YespornBaseURL returns the yesporn.vip base URL, used as the fallback Referer
// for emitted yesporn mp4 streams when an entry has no detail URL. Exported for
// the yesporn stream handler.
func YespornBaseURL() string { return yespornBaseURL }

var wsRe = regexp.MustCompile(`\s+`)
