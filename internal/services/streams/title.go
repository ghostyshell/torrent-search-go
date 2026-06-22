package streams

import (
	"fmt"
	"regexp"
)

var (
	qualityREs = []struct {
		re      *regexp.Regexp
		quality string
	}{
		{regexp.MustCompile(`(?i)\b(8k|7680x4320)\b`), "8k"},
		{regexp.MustCompile(`(?i)\b(2160p|4k|uhd)\b`), "4k"},
		{regexp.MustCompile(`(?i)\b1080p\b`), "1080p"},
		{regexp.MustCompile(`(?i)\b720p\b`), "720p"},
		{regexp.MustCompile(`(?i)\b480p\b`), "480p"},
		{regexp.MustCompile(`(?i)\b(cam|camrip|ts|telesync|telecine|hdcam)\b`), "cam"},
	}

	codecREs = []struct {
		re    *regexp.Regexp
		codec string
	}{
		{regexp.MustCompile(`(?i)\b(x265|hevc|h\.?265)\b`), "HEVC"},
		{regexp.MustCompile(`(?i)\b(x264|avc|h\.?264)\b`), "AVC"},
		{regexp.MustCompile(`(?i)\bav1\b`), "AV1"},
	}

	sourceREs = []struct {
		re     *regexp.Regexp
		source string
	}{
		{regexp.MustCompile(`(?i)\b(bluray|blu-ray|bdrip|brrip)\b`), "BluRay"},
		{regexp.MustCompile(`(?i)\b(webrip|web-rip)\b`), "WEBRip"},
		{regexp.MustCompile(`(?i)\b(webdl|web-dl|web)\b`), "WEB-DL"},
		{regexp.MustCompile(`(?i)\bhdrip\b`), "HDRip"},
		{regexp.MustCompile(`(?i)\bdvdrip\b`), "DVDRip"},
		{regexp.MustCompile(`(?i)\bhdtv\b`), "HDTV"},
	}

	languageREs = []struct {
		re   *regexp.Regexp
		lang string
	}{
		{regexp.MustCompile(`(?i)\bmulti\b`), "multi"},
		{regexp.MustCompile(`(?i)\bfrench\b`), "fr"},
		{regexp.MustCompile(`(?i)\bspanish\b`), "es"},
		{regexp.MustCompile(`(?i)\bportuguese\b`), "pt"},
		{regexp.MustCompile(`(?i)\bitalian\b`), "it"},
		{regexp.MustCompile(`(?i)\bgerman\b`), "de"},
		{regexp.MustCompile(`(?i)\brussian\b`), "ru"},
		{regexp.MustCompile(`(?i)\bkorean\b`), "ko"},
		{regexp.MustCompile(`(?i)\bjapanese\b`), "ja"},
		{regexp.MustCompile(`(?i)\bchinese\b`), "zh"},
		{regexp.MustCompile(`(?i)\barabic\b`), "ar"},
		{regexp.MustCompile(`(?i)\bturkish\b`), "tr"},
		{regexp.MustCompile(`(?i)\bhindi\b`), "hi"},
		{regexp.MustCompile(`(?i)\b(greek|ellinika|ελληνικά)\b`), "el"},
		{regexp.MustCompile(`(?i)\b(albanian|shqip)\b`), "sq"},
		{regexp.MustCompile(`(?i)\bdubbed\b`), "dubbed"},
		{regexp.MustCompile(`(?i)\bmarathi\b`), "mr"},
	}

	hdrRE = regexp.MustCompile(`(?i)\b(hdr|hdr10|dolby\.?vision|dv)\b`)
)

// ParsedTitle holds metadata extracted from a raw torrent name.
type ParsedTitle struct {
	Quality   string
	Codec     string
	Source    string
	HDR       bool
	Bitdepth  string
	Languages []string
}

// ParseTitle extracts quality, codec, source, HDR and language hints.
func ParseTitle(title string) ParsedTitle {
	var p ParsedTitle
	for _, q := range qualityREs {
		if q.re.MatchString(title) {
			p.Quality = q.quality
			break
		}
	}
	for _, c := range codecREs {
		if c.re.MatchString(title) {
			p.Codec = c.codec
			break
		}
	}
	for _, s := range sourceREs {
		if s.re.MatchString(title) {
			p.Source = s.source
			break
		}
	}
	p.HDR = hdrRE.MatchString(title)
	if p.HDR {
		p.Bitdepth = "10bit"
	} else if regexp.MustCompile(`(?i)\b10\.?bit\b`).MatchString(title) {
		p.Bitdepth = "10bit"
	}

	for _, l := range languageREs {
		if l.re.MatchString(title) {
			p.Languages = append(p.Languages, l.lang)
		}
	}
	if len(p.Languages) == 0 && !regexp.MustCompile(`(?i)\b(dubbed|multi)\b`).MatchString(title) {
		p.Languages = append(p.Languages, "en")
	}
	return p
}

// BuildSearchQuery returns a provider query string for a request.
func BuildSearchQuery(req Request) string {
	if req.IsSeries() && req.Season != nil && req.Episode != nil {
		return fmt.Sprintf("%s S%02dE%02d", req.Name, *req.Season, *req.Episode)
	}
	if req.Year > 0 {
		return fmt.Sprintf("%s %d", req.Name, req.Year)
	}
	return req.Name
}
