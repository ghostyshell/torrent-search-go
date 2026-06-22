package stremio

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	qualityPatterns = map[string]*regexp.Regexp{
		"2160p": regexp.MustCompile(`(?i)\b(2160p|4k|uhd|ultra\.?hd)\b`),
		"1080p": regexp.MustCompile(`(?i)\b(1080p|fhd|full\.?hd)\b`),
		"720p":  regexp.MustCompile(`(?i)\b(720p|hd)\b`),
		"480p":  regexp.MustCompile(`(?i)\b(480p|sd)\b`),
	}
	hdrPattern     = regexp.MustCompile(`(?i)\b(hdr10\+?|dolby\.?vision|dv|hlg|hdr)\b`)
	codecPattern   = regexp.MustCompile(`(?i)\b(x265|x264|hevc|avc|av1|xvid|divx)\b`)
	sourcePattern  = regexp.MustCompile(`(?i)\b(bluray|blu-ray|bdrip|brrip|web-dl|webrip|dvdrip|hdtv|remux)\b`)
	infoHashRE     = regexp.MustCompile(`(?i)urn:btih:([a-f0-9]{40}|[a-z2-7]{32})`)
	yearRE         = regexp.MustCompile(`^(.*?)\s+((?:19|20)\d{2})\b`)
	qualStripRE    = regexp.MustCompile(`(?i)\s+(2160p|4k|uhd|1080p|fhd|720p|480p|bluray|blu-ray|bdrip|web-dl|webrip|hdtv|remux|x265|x264|hevc|avc|hdr|dv|dolby).*$`)
	pornripsSlugRE = regexp.MustCompile(`(?i)pornrips\.to/([^/?#]+)`)
)

const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// EncodeItemID builds a Stremio item id: jstrm:<base64url(JSON)>.
func EncodeItemID(record TorrentRecord) string {
	payload := itemPayload{
		H: record.InfoHash,
		T: record.Title,
		U: record.TorrentURL,
		W: record.Website,
		Q: record.Quality,
	}
	if record.DetailURL != "" {
		payload.D = record.DetailURL
	}
	b, _ := json.Marshal(payload)
	return "jstrm:" + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeItemID parses a jstrm: item id back into fields.
func DecodeItemID(id string) *itemPayload {
	if id == "" || !strings.HasPrefix(id, "jstrm:") {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(id[len("jstrm:"):])
	if err != nil {
		return nil
	}
	var payload itemPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return &payload
}

// EncodeGroupID builds a compact-mode group item id that carries several
// torrent records - the 4K and 1080p variants of one scene - so the Node
// stream route can emit one stream per variant. Format:
// jstrg:<base64url(JSON([itemPayload,...]))>. Each member uses the same
// itemPayload shape as EncodeItemID, so the stream route re-encodes each
// member back into a jstrm: id and runs it through the existing per-record
// resolution (debrid + P2P).
func EncodeGroupID(records []TorrentRecord) string {
	payloads := make([]itemPayload, 0, len(records))
	for _, r := range records {
		p := itemPayload{H: r.InfoHash, T: r.Title, U: r.TorrentURL, W: r.Website, Q: r.Quality}
		if r.DetailURL != "" {
			p.D = r.DetailURL
		}
		payloads = append(payloads, p)
	}
	b, _ := json.Marshal(payloads)
	return "jstrg:" + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeGroupID parses a jstrg: group id back into its member payloads. The
// Go backend does not serve streams for jstrg: ids (the Node edge does), but
// ServeMeta uses this to build a detail-page meta from the representative
// (first) member.
func DecodeGroupID(id string) []itemPayload {
	if id == "" || !strings.HasPrefix(id, "jstrg:") {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(id[len("jstrg:"):])
	if err != nil {
		return nil
	}
	var payloads []itemPayload
	if json.Unmarshal(raw, &payloads) != nil {
		return nil
	}
	return payloads
}

// StableMetaID returns the cross-install metadata cache key.
func StableMetaID(website, detailURL, infoHash string) string {
	if website == "pornrips" {
		if m := pornripsSlugRE.FindStringSubmatch(detailURL); len(m) > 1 {
			return "pr:" + m[1]
		}
		return ""
	}
	return strings.ToLower(infoHash)
}

// ParseTorrentTitle extracts a display title and optional year from a torrent name.
func ParseTorrentTitle(name string) (title, year string) {
	cleaned := strings.TrimSpace(regexp.MustCompile(`\s{2,}`).ReplaceAllString(
		strings.NewReplacer(".", " ", "_", " ").Replace(name), " "))
	if cleaned == "" {
		return "", ""
	}
	if m := yearRE.FindStringSubmatch(cleaned); len(m) == 3 {
		return strings.TrimSpace(m[1]), m[2]
	}
	stripped := strings.TrimSpace(qualStripRE.ReplaceAllString(cleaned, ""))
	if stripped == "" {
		stripped = cleaned
	}
	return stripped, ""
}

// QualityTag builds a human-readable quality string from a torrent name.
func QualityTag(name string) string {
	var parts []string
	if q := detectQuality(name); q != "" && q != "unknown" {
		parts = append(parts, q)
	}
	if m := hdrPattern.FindStringSubmatch(name); len(m) > 0 {
		parts = append(parts, strings.ToUpper(m[0]))
	}
	if m := codecPattern.FindStringSubmatch(name); len(m) > 0 {
		parts = append(parts, strings.ToUpper(m[0]))
	}
	if m := sourcePattern.FindStringSubmatch(name); len(m) > 0 {
		parts = append(parts, strings.ToUpper(m[0]))
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	return strings.Join(parts, " ")
}

func detectQuality(name string) string {
	for qual, re := range qualityPatterns {
		if re.MatchString(name) {
			return qual
		}
	}
	return "unknown"
}

// ExtractInfoHash returns a lowercase hex info-hash from a magnet URI.
func ExtractInfoHash(magnet string) string {
	if magnet == "" {
		return ""
	}
	m := infoHashRE.FindStringSubmatch(magnet)
	if len(m) < 2 {
		return ""
	}
	raw := m[1]
	if len(raw) == 32 {
		if hex, err := base32ToHex(raw); err == nil {
			return strings.ToLower(hex)
		}
	}
	return strings.ToLower(raw)
}

func base32ToHex(base32 string) (string, error) {
	var bits, value int
	var hex strings.Builder
	for _, char := range strings.ToUpper(base32) {
		idx := strings.IndexRune(base32Alphabet, char)
		if idx < 0 {
			return "", fmt.Errorf("invalid base32 char: %c", char)
		}
		value = (value << 5) | idx
		bits += 5
		if bits >= 8 {
			hex.WriteByte(byte((value >> (bits - 8)) & 0xff))
			bits -= 8
		}
	}
	return hex.String(), nil
}
