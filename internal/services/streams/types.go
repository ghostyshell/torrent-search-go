// Package streams provides Magnetio-compatible stream resolution.
// It runs configured providers in parallel, deduplicates results, and returns
// a normalized list of torrent / HTTP fallback records.
package streams

import (
	"context"
	"time"
)

// Stream is a normalized torrent or direct-download result.
// It matches the Magnetio /streams wire format.
type Stream struct {
	ID             string   `json:"id,omitempty"`
	InfoHash       string   `json:"infoHash,omitempty"`
	Title          string   `json:"title"`
	Seeders        int      `json:"seeders,omitempty"`
	Leechers       int      `json:"leechers,omitempty"`
	Size           int64    `json:"size,omitempty"`
	Provider       string   `json:"provider"`
	Quality        string   `json:"quality,omitempty"`
	Languages      []string `json:"languages,omitempty"`
	IMDbID         string   `json:"imdbId,omitempty"`
	Trackers       []string `json:"trackers,omitempty"`
	URL            string   `json:"url,omitempty"` // HTTP-only fallback (AtishMKV)
	FileIdx        int      `json:"fileIdx,omitempty"`
	Source         string   `json:"source,omitempty"`
	Codec          string   `json:"codec,omitempty"`
	HDR            bool     `json:"hdr,omitempty"`
	Bitdepth       string   `json:"bitdepth,omitempty"`
	EpisodeMatched bool     `json:"episodeMatched,omitempty"`
}

// Request holds everything a provider needs to search for streams.
type Request struct {
	Type            string // movie | series | anime
	ID              string // tt1234567 or kitsu:12345
	Name            string
	Year            int
	IMDbID          string
	Season          *int
	Episode         *int
	AbsoluteEpisode int
}

// IsSeries reports whether the request is for series or anime.
func (r Request) IsSeries() bool {
	return r.Type == "series" || r.Type == "anime"
}

// Provider is the interface implemented by every stream source.
type Provider interface {
	ID() string
	Name() string
	Scrape(ctx context.Context, req Request) ([]Stream, error)
}

// Result is the aggregate output of a scrape.
type Result struct {
	Streams []Stream
	Cached  bool
	Took    time.Duration
}

// Default trackers appended to magnet links when a source does not provide them.
var DefaultTrackers = []string{
	"udp://open.demonii.com:1337/announce",
	"udp://tracker.openbittorrent.com:80",
	"udp://tracker.coppersurfer.tk:6969",
	"udp://glotorrents.pw:6969/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://torrent.gresille.org:80/announce",
	"udp://p4p.arenabg.com:1337",
	"udp://tracker.leechers-paradise.org:6969",
}
