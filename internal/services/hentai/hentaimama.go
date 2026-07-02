package hentai

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// MamaScraper scrapes hentaimama.io (WordPress/DooPlay, prefix hmm). Series
// pages live at /tvshows/{slug}/, episodes at /episodes/{epSlug}. Streams are
// direct mp4 (gdvid.info) extracted from the episode page's jwplayer setup /
// sources literal, with a WordPress admin-ajax fallback that returns iframe
// HTML to fetch and parse.
type MamaScraper struct{ hc *httpClient }

func newMamaScraper(hc *httpClient) *MamaScraper { return &MamaScraper{hc: hc} }

const mamaSource = "hentaimama"

// mamaSeriesLinkRe matches a HentaiMama series URL and captures the slug. The
// listing and series pages link series as /tvshows/{slug}/ (DooPlay) and
// occasionally /hentai-series/{slug}/.
var mamaSeriesLinkRe = regexp.MustCompile(`/(?:tvshows|hentai-series)/([^/]+)/?$`)

// mamaEpisodeLinkRe matches /episodes/{slug} and captures the slug.
var mamaEpisodeLinkRe = regexp.MustCompile(`/episodes/([^/]+)/?$`)

// ListSeries walks /hentai-series/page/{N}/ (page 1 = /hentai-series/) and
// returns the series listed on that page. Each article's series link slug is
// deduped; poster/title come from the article's image/heading when present.
func (s *MamaScraper) ListSeries(ctx context.Context, page int) ([]SeriesListing, error) {
	url := mamaBase + "/hentai-series/"
	if page > 1 {
		url = mamaBase + "/hentai-series/page/" + itoa(page) + "/?filter=rating"
	}
	body, err := s.hc.get(ctx, url, "")
	if err != nil {
		if isStatus(err, 404) {
			return nil, nil
		}
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]SeriesListing, 0, 24)
	doc.Find("article").Each(func(_ int, art *goquery.Selection) {
		var slug, title, poster string
		art.Find(`a[href*="/tvshows/"], a[href*="/hentai-series/"]`).Each(func(_ int, a *goquery.Selection) {
			if slug != "" {
				return
			}
			href, _ := a.Attr("href")
			if m := mamaSeriesLinkRe.FindStringSubmatch(href); m != nil && m[1] != "" {
				slug = m[1]
			}
		})
		if slug == "" {
			return
		}
		if _, ok := seen[slug]; ok {
			return
		}
		seen[slug] = struct{}{}
		if t := art.Find("h2, h3, .title, .entry-title").First().Text(); strings.TrimSpace(t) != "" {
			title = strings.TrimSpace(t)
		}
		// Listing imgs are lazy-loaded too: prefer data-src over the 1x1
		// placeholder src, and skip data: URIs entirely.
		img := art.Find("img").First()
		if src, ok := img.Attr("data-src"); ok && src != "" {
			poster = absMama(src)
		} else if src, ok := img.Attr("data-lazy-src"); ok && src != "" {
			poster = absMama(src)
		} else if src, ok := img.Attr("src"); ok && src != "" && !strings.HasPrefix(src, "data:") {
			poster = absMama(src)
		}
		out = append(out, SeriesListing{Prefix: "hmm", Slug: slug, Title: title, Poster: poster})
	})
	return out, nil
}

// FetchSeries scrapes /tvshows/{slug}/ for full series metadata + the episode
// list. Each episode's slug is captured so the stream handler can resolve it
// live; the episode number comes from the slug suffix -episode-N or the link text.
func (s *MamaScraper) FetchSeries(ctx context.Context, slug string) (*SeriesDetail, error) {
	url := mamaBase + "/tvshows/" + slug + "/"
	body, err := s.hc.get(ctx, url, "")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	d := &SeriesDetail{
		Prefix:    "hmm",
		Slug:      slug,
		Source:    mamaSource,
		DetailURL: url,
	}
	// Title: prefer og:title (clean) - the page has an h1 "Hentaimama" site name
	// that a naive h1 selector picks up first.
	if t, _ := doc.Find(`meta[property="og:title"]`).Attr("content"); t != "" {
		d.Title = t
	} else if t := strings.TrimSpace(doc.Find(".single-page h1, .sheader h1").First().Text()); t != "" {
		d.Title = t
	}
	if src, _ := doc.Find(`meta[property="og:image"]`).Attr("content"); src != "" {
		d.Poster = src
	} else {
		// HentaiMama lazy-loads poster images: the <img> src is a 1x1 GIF
		// placeholder and the real URL is in data-src. og:image is absent on
		// series pages, so prefer data-src and skip data: placeholder srcs.
		img := doc.Find(".poster img, .thumbnail img, img.lazyload").First()
		if src, ok := img.Attr("data-src"); ok && src != "" {
			d.Poster = absMama(src)
		} else if src, ok := img.Attr("data-lazy-src"); ok && src != "" {
			d.Poster = absMama(src)
		} else if src, ok := img.Attr("src"); ok && src != "" && !strings.HasPrefix(src, "data:") {
			d.Poster = absMama(src)
		}
	}
	d.Background = d.Poster
	// excerpt: first <p> in the content/sinopsis block, else og:description.
	if p := strings.TrimSpace(doc.Find(".wp-content p, .sinopsis p, .description p, p").First().Text()); p != "" {
		d.Excerpt = p
	} else if desc, _ := doc.Find(`meta[property="og:description"]`).Attr("content"); desc != "" {
		d.Excerpt = desc
	}
	// release year: .date or any 4-digit year in the date element.
	if dt := strings.TrimSpace(doc.Find(".date, .extra .date, .metadata .date").First().Text()); dt != "" {
		if y := mamaYearRe.FindString(dt); y != "" {
			d.ReleaseYear = y
		}
	}
	// rating: .dt_rating_vgs holds the numeric rating value (itemprop=ratingValue).
	// Avoid [class^="dt_rating"] which matches the empty dt_rating_data wrapper first.
	if r := strings.TrimSpace(doc.Find(`.dt_rating_vgs, [itemprop="ratingValue"]`).First().Text()); r != "" {
		d.Rating = parseRating(r)
		d.RatingSrc = "hmm"
	}
	// studio: a[href*="/studio/"] (these also carry rel="tag", so exclude them
	// from genres below).
	if t := strings.TrimSpace(doc.Find(`a[href*="/studio/"]`).First().Text()); t != "" {
		d.Studio = t
	}
	// genres: a[rel="tag"] / a[href*="/genre/"], skipping studio links.
	doc.Find(`a[rel="tag"], a[href*="/genre/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if strings.Contains(href, "/studio/") {
			return
		}
		g := strings.TrimSpace(a.Text())
		if g != "" {
			d.Genres = append(d.Genres, g)
		}
	})
	d.Genres = dedupStrings(d.Genres)
	// episodes: all /episodes/{epSlug} links, deduped by slug, number from suffix.
	seenEp := make(map[string]struct{})
	doc.Find(`a[href*="/episodes/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		m := mamaEpisodeLinkRe.FindStringSubmatch(href)
		if m == nil || m[1] == "" {
			return
		}
		epSlug := m[1]
		if _, ok := seenEp[epSlug]; ok {
			return
		}
		seenEp[epSlug] = struct{}{}
		num := episodeNumFromSlug(epSlug)
		if num == 0 {
			if n := parseEpisodeNumber(a.Text()); n > 0 {
				num = n
			}
		}
		d.Episodes = append(d.Episodes, EpisodeInfo{
			Number:    num,
			Title:     collapseSpaces(a.Text()),
			Slug:      epSlug,
			SourceURL: href,
		})
	})
	sortEpisodes(d.Episodes)
	return d, nil
}

// ResolveEpisodeStream resolves a HentaiMama episode's direct mp4 URLs via the
// DooPlay player AJAX flow. The episode page itself has no inline player: a
// jQuery snippet POSTs `action=get_player_contents&a={postID}` to
// /wp-admin/admin-ajax.php, which returns a JSON array of per-source HTML
// strings, each an <iframe src=".../new2.php?p={base64(filename)}">. Fetching
// that iframe yields a jwplayer setup with `file: "https://gdvid.info/...mp4"`,
// which is directly playable (200, video/mp4, no referer). sourceURL is unused.
// Returns streams sorted highest quality first; empty on any parse miss.
func (s *MamaScraper) ResolveEpisodeStream(ctx context.Context, epSlug, sourceURL string) ([]EpisodeStream, error) {
	_ = sourceURL
	url := mamaBase + "/episodes/" + epSlug
	body, err := s.hc.get(ctx, url, "")
	if err != nil {
		return nil, err
	}
	html := string(body)

	var out []EpisodeStream
	if m := mamaPostIDRe.FindStringSubmatch(html); m != nil {
		out = append(out, s.resolveMamaAJAX(ctx, m[1], url)...)
	}
	// Fallback: a few episodes embed the player inline or in a direct iframe
	// rather than the AJAX flow; harvest those too, deduped.
	out = append(out, extractMamaStreams(html)...)
	for _, src := range mamaIframeRe.FindAllStringSubmatch(html, -1) {
		ib, ferr := s.hc.get(ctx, src[1], url)
		if ferr != nil {
			continue
		}
		out = append(out, extractMamaStreams(string(ib))...)
	}
	return dedupStreams(out), nil
}

// resolveMamaAJAX POSTs get_player_contents for a post id, parses the returned
// JSON array of per-source iframe HTML, fetches each iframe, and harvests the
// jwplayer file URLs. The referer is the episode page.
func (s *MamaScraper) resolveMamaAJAX(ctx context.Context, postID, epURL string) []EpisodeStream {
	form := url.Values{"action": {"get_player_contents"}, "a": {postID}}
	ajaxBody, err := s.hc.postForm(ctx, mamaBase+"/wp-admin/admin-ajax.php", form, epURL)
	if err != nil {
		return nil
	}
	var fields []string
	if err := json.Unmarshal(ajaxBody, &fields); err != nil {
		return nil
	}
	var out []EpisodeStream
	for _, f := range fields {
		for _, src := range mamaIframeRe.FindAllStringSubmatch(f, -1) {
			ib, ierr := s.hc.get(ctx, src[1], epURL)
			if ierr != nil {
				continue
			}
			out = append(out, extractMamaStreams(string(ib))...)
		}
	}
	return out
}

// extractMamaStreams pulls direct file/source mp4 URLs out of a player page's
// scripts. The leading quote on `file` is optional so both `"file":"…"` (JSON)
// and `file: "…"` (JS) match.
func extractMamaStreams(html string) []EpisodeStream {
	var out []EpisodeStream
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || !strings.HasPrefix(u, "http") {
			return
		}
		out = append(out, EpisodeStream{URL: u, Quality: detectQuality(u), Name: "HentaiMama"})
	}
	for _, m := range mamaFileRe.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range mamaSourceRe.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range mamaJWFileRe.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	return dedupStreams(out)
}

var (
	mamaYearRe    = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	mamaPostIDRe  = regexp.MustCompile(`get_player_contents'[^}]*a:'([0-9]+)'`)
	mamaFileRe    = regexp.MustCompile(`"?file"?\s*:\s*"(https?:[^"]+\.(?:mp4|m3u8|mkv|webm)[^"]*)"`)
	mamaSourceRe  = regexp.MustCompile(`source\s*:\s*"(https?:[^"]+\.(?:mp4|m3u8|mkv|webm)[^"]*)"`)
	mamaJWFileRe  = regexp.MustCompile(`file\s*:\s*'(https?:[^']+\.(?:mp4|m3u8|mkv|webm)[^']*)'`)
	mamaIframeRe  = regexp.MustCompile(`<iframe[^>]+src=["'](https?:[^"']+)["']`)
)

func absMama(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return mamaBase + u
	}
	return u
}