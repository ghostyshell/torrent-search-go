package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const cinemetaBase = "https://v3-cinemeta.strem.io/meta"

// Meta holds resolved metadata for a piece of content.
type Meta struct {
	Name              string
	Year              int
	IMDbID            string
	Type              string
	EpisodesPerSeason map[int]int
}

// ToAbsolute maps a (season, episode) pair to absolute episode numbering.
func (m *Meta) ToAbsolute(season, episode int) int {
	if m.EpisodesPerSeason == nil {
		return 0
	}
	before := 0
	for s, count := range m.EpisodesPerSeason {
		if s > 0 && s < season {
			before += count
		}
	}
	return before + episode
}

// ResolveMetadata fetches Cinemeta metadata for an ID, with movie<->series fallback.
func ResolveMetadata(ctx context.Context, client *http.Client, typ, id string) (*Meta, error) {
	parts := strings.SplitN(id, ":", 3)
	imdbID := parts[0]

	m, err := fetchCinemeta(ctx, client, typ, imdbID)
	if err == nil && m != nil && m.Name != "" {
		return m, nil
	}

	// Fallback: try the opposite type.
	altType := "movie"
	if typ == "movie" {
		altType = "series"
	}
	m, err = fetchCinemeta(ctx, client, altType, imdbID)
	if err == nil && m != nil && m.Name != "" {
		m.Type = typ
		return m, nil
	}

	return nil, fmt.Errorf("no metadata found for %s/%s", typ, id)
}

func fetchCinemeta(ctx context.Context, client *http.Client, typ, imdbID string) (*Meta, error) {
	if client == nil {
		client = http.DefaultClient
	}
	url := fmt.Sprintf("%s/%s/%s.json", cinemetaBase, typ, imdbID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cinemeta returned %d", resp.StatusCode)
	}

	var body struct {
		Meta cinemetaMeta `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Meta.Name == "" {
		return nil, nil
	}

	m := &Meta{
		Name:   body.Meta.Name,
		IMDbID: imdbID,
		Type:   typ,
	}
	if body.Meta.Year != "" {
		m.Year, _ = strconv.Atoi(body.Meta.Year[:4])
	}
	m.EpisodesPerSeason = countEpisodes(body.Meta.Videos)
	return m, nil
}

type cinemetaMeta struct {
	Name   string          `json:"name"`
	Year   string          `json:"year"`
	Videos []cinemetaVideo `json:"videos"`
}

type cinemetaVideo struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

func countEpisodes(videos []cinemetaVideo) map[int]int {
	if len(videos) == 0 {
		return nil
	}
	counts := make(map[int]int)
	for _, v := range videos {
		if v.Season > 0 && v.Episode > 0 {
			counts[v.Season]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

// ParseID splits a Stremio ID into its components.
// movie:  tt1234567
// series: tt1234567:1:2
// anime:  kitsu:12345:1:2 (treated as series)
func ParseID(id string) (imdbID string, season, episode *int) {
	parts := strings.Split(id, ":")
	if len(parts) == 0 {
		return "", nil, nil
	}
	imdbID = parts[0]
	if len(parts) >= 2 {
		if s, err := strconv.Atoi(parts[1]); err == nil {
			season = &s
		}
	}
	if len(parts) >= 3 {
		if e, err := strconv.Atoi(parts[2]); err == nil {
			episode = &e
		}
	}
	return
}

var nonAlnumRE = regexp.MustCompile(`[^a-z0-9\s]`)

// NormalizeTitle lowercases, strips punctuation, and collapses spaces.
func NormalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = nonAlnumRE.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// HTTPClientWithTimeout returns a clean HTTP client for metadata calls.
func HTTPClientWithTimeout(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}
