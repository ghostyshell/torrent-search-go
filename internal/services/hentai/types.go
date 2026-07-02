// Package hentai self-scrapes HentaiMama (hmm-) for the Stremio hentai
// catalogs, storing series in the hentai_entries Mongo collection and resolving
// episode streams to direct mp4 URLs. It replaces the prior dependency on the
// third-party hentaistream Cloudflare Worker. HentaiTV was removed (its r2 CDN
// 403s from the backend and it was never part of the configured source scope).
// HentaiSea is intentionally not scraped (its streams need a per-playback
// Referer-authed video-proxy; the edge already excluded it).
package hentai

// SeriesListing is a lightweight catalog/listing row used by ingest to enumerate
// series slugs before fetching full details.
type SeriesListing struct {
	Prefix string // "hmm"
	Slug   string // source-local series slug
	Title  string
	Poster string
}

// SeriesDetail is the full per-series scrape result, mapped to a HentaiEntry by
// the ingest job.
type SeriesDetail struct {
	Prefix      string
	Slug        string
	Source      string // "hentaimama"
	Title       string
	Poster      string
	Background  string
	Excerpt     string
	ReleaseYear string
	Studio      string
	Genres      []string
	Rating      float64
	RatingSrc   string
	DetailURL   string
	Episodes    []EpisodeInfo
}

// EpisodeInfo is one episode of a series, stored so the live stream handler can
// resolve a direct mp4 from the episode slug without re-scraping the series page.
type EpisodeInfo struct {
	Number    int
	Title     string
	Slug      string // source episode slug
	SourceURL string // full episode page URL
	Thumbnail string
	Released  string
}

// EpisodeStream is a resolved direct stream for one episode.
type EpisodeStream struct {
	URL     string
	Quality string
	Name    string
}

// ID builds the Stremio item id for a series: "{prefix}-{slug}".
func ID(prefix, slug string) string { return prefix + "-" + slug }