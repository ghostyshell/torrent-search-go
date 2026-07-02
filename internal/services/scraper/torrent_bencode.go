package scraper

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
)

// InfoHashFromTorrent returns the lowercase hex SHA-1 info-hash from raw
// .torrent bencode data, or "" if the info key cannot be located. Lives in the
// scraper package (which fetches the .torrent bytes) so both the stremio stream
// handler and the jobs backfill sweep can call it without an import cycle
// (internal/stremio imports internal/services/jobs, so jobs cannot import
// stremio).
func InfoHashFromTorrent(data []byte) string {
	key := []byte("4:info")
	idx := bytes.Index(data, key)
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := bencodeSkip(data, start)
	if end <= start {
		return ""
	}
	h := sha1.Sum(data[start:end])
	return hex.EncodeToString(h[:])
}

// bencodeSkip returns the byte index just past the bencoded value at pos.
func bencodeSkip(data []byte, pos int) int {
	if pos >= len(data) {
		return -1
	}
	switch data[pos] {
	case 'd':
		pos++
		for pos < len(data) && data[pos] != 'e' {
			if pos = bencodeSkip(data, pos); pos < 0 {
				return -1
			}
			if pos = bencodeSkip(data, pos); pos < 0 {
				return -1
			}
		}
		if pos >= len(data) {
			return -1
		}
		return pos + 1
	case 'l':
		pos++
		for pos < len(data) && data[pos] != 'e' {
			if pos = bencodeSkip(data, pos); pos < 0 {
				return -1
			}
		}
		if pos >= len(data) {
			return -1
		}
		return pos + 1
	case 'i':
		e := bytes.IndexByte(data[pos:], 'e')
		if e < 0 {
			return -1
		}
		return pos + e + 1
	default: // string: N:...
		col := bytes.IndexByte(data[pos:], ':')
		if col < 0 {
			return -1
		}
		n := 0
		for _, c := range data[pos : pos+col] {
			if c < '0' || c > '9' {
				return -1
			}
			n = n*10 + int(c-'0')
		}
		return pos + col + 1 + n
	}
}