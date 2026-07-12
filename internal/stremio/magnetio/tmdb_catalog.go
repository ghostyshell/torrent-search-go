package magnetio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tmdbBase       = "https://api.themoviedb.org/3"
	tmdbPosterBase = "https://image.tmdb.org/t/p/w500"
	tmdbBackdrop   = "https://image.tmdb.org/t/p/w780"
)

// streamingService maps a Magnetio service short id to TMDB watch-provider metadata.
type streamingService struct {
	ID           int
	Name         string
	MultiCountry bool
	ProviderIDs  []int
}

var streamingServices = map[string]streamingService{
	"netflix":   {ID: 8, Name: "Netflix", MultiCountry: true},
	"prime":     {ID: 9, Name: "Prime Video", MultiCountry: true, ProviderIDs: []int{9, 119}},
	"disney":    {ID: 337, Name: "Disney+", MultiCountry: true},
	"hulu":      {ID: 15, Name: "Hulu", MultiCountry: false},
	"max":       {ID: 1899, Name: "Max", MultiCountry: true},
	"apple":     {ID: 350, Name: "Apple TV+", MultiCountry: true},
	"peacock":   {ID: 386, Name: "Peacock", MultiCountry: false},
	"paramount": {ID: 2303, Name: "Paramount+", MultiCountry: true},
}

// tmdbClient is a small TMDB helper with short in-memory caching.
type tmdbClient struct {
	apiKey      string
	rpdbKey     string
	omdbKey     string
	httpClient  *http.Client

	mu       sync.RWMutex
	external map[string]tmdbExternalCacheEntry
	idMap    map[string]tmdbIDMapEntry
}

type tmdbExternalCacheEntry struct {
	imdb string
	at   time.Time
}

type tmdbIDMapEntry struct {
	tmdbID int
	at     time.Time
}

func newTMDBClient(apiKey, rpdbKey, omdbKey string) *tmdbClient {
	return &tmdbClient{
		apiKey:     apiKey,
		rpdbKey:    rpdbKey,
		omdbKey:    omdbKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		external:   map[string]tmdbExternalCacheEntry{},
		idMap:      map[string]tmdbIDMapEntry{},
	}
}

func (t *tmdbClient) client() *http.Client {
	if t != nil && t.httpClient != nil {
		return t.httpClient
	}
	return http.DefaultClient
}

// fetchStreamingCatalog returns TMDB discover results for a streaming service catalog id.
func (t *tmdbClient) fetchStreamingCatalog(ctx context.Context, catalogID string) ([]map[string]interface{}, error) {
	parts := strings.Split(catalogID, "_")
	if len(parts) != 4 || parts[0] != "tmdb" {
		return nil, fmt.Errorf("invalid streaming catalog id %q", catalogID)
	}
	svcID := parts[1]
	contentType := parts[2]
	country := parts[3]

	svc, ok := streamingServices[svcID]
	if !ok {
		return nil, nil
	}

	if !svc.MultiCountry {
		country = "us"
	}

	endpoint := "movie"
	if contentType == "series" {
		endpoint = "tv"
	}

	providerIDs := svc.ProviderIDs
	if len(providerIDs) == 0 {
		providerIDs = []int{svc.ID}
	}
	watchProviders := joinInts(providerIDs, "|")

	u, _ := url.Parse(fmt.Sprintf("%s/discover/%s", tmdbBase, endpoint))
	q := u.Query()
	q.Set("api_key", t.apiKey)
	q.Set("page", "1")
	q.Set("with_watch_providers", watchProviders)
	q.Set("watch_region", strings.ToUpper(country))
	q.Set("sort_by", "popularity.desc")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var payload struct {
		Results []tmdbResult `json:"results"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(payload.Results))
	for _, item := range payload.Results {
		var imdbID string
		if t.rpdbKey != "" {
			imdbID, _ = t.externalIDs(ctx, endpoint, item.ID)
			if imdbID == "" && t.omdbKey != "" {
				imdbID, _ = t.omdbByTitle(ctx, item.Title, endpoint)
			}
		}
		meta := streamingToStremioMeta(item, contentType, imdbID, t.rpdbKey)
		if meta != nil {
			out = append(out, meta)
		}
	}
	return out, nil
}

// fetchSimilarCatalog returns TMDB recommendations for an IMDb id.
func (t *tmdbClient) fetchSimilarCatalog(ctx context.Context, imdbID, contentType string) ([]map[string]interface{}, error) {
	if imdbID == "" || t.apiKey == "" {
		return nil, nil
	}
	endpoint := "movie"
	if contentType == "series" {
		endpoint = "tv"
	}

	tmdbID, err := t.imdbToTmdb(ctx, imdbID, endpoint)
	if err != nil || tmdbID == 0 {
		return nil, nil
	}

	u := fmt.Sprintf("%s/%s/%d/recommendations?api_key=%s&page=1", tmdbBase, endpoint, tmdbID, url.QueryEscape(t.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}

	var payload struct {
		Results []tmdbResult `json:"results"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(payload.Results))
	for _, item := range payload.Results {
		imdbID, err := t.externalIDs(ctx, endpoint, item.ID)
		if err != nil || imdbID == "" {
			continue
		}
		meta := similarToStremioMeta(item, contentType, imdbID)
		if meta != nil {
			out = append(out, meta)
		}
	}
	return out, nil
}

func (t *tmdbClient) imdbToTmdb(ctx context.Context, imdbID, endpoint string) (int, error) {
	if t == nil {
		return 0, nil
	}
	t.mu.RLock()
	entry, ok := t.idMap[imdbID]
	t.mu.RUnlock()
	if ok && time.Since(entry.at) < 7*24*time.Hour {
		return entry.tmdbID, nil
	}

	u := fmt.Sprintf("%s/find/%s?api_key=%s&external_source=imdb_id", tmdbBase, url.PathEscape(imdbID), url.QueryEscape(t.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := t.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return 0, nil
	}

	var payload struct {
		MovieResults []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
		TVResults []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return 0, err
	}

	var id int
	if endpoint == "tv" {
		if len(payload.TVResults) > 0 {
			id = payload.TVResults[0].ID
		}
	} else {
		if len(payload.MovieResults) > 0 {
			id = payload.MovieResults[0].ID
		}
	}

	t.mu.Lock()
	t.idMap[imdbID] = tmdbIDMapEntry{tmdbID: id, at: time.Now()}
	t.mu.Unlock()
	return id, nil
}

func (t *tmdbClient) externalIDs(ctx context.Context, endpoint string, tmdbID int) (string, error) {
	if t == nil {
		return "", nil
	}
	key := fmt.Sprintf("%s:%d", endpoint, tmdbID)
	t.mu.RLock()
	entry, ok := t.external[key]
	t.mu.RUnlock()
	if ok && time.Since(entry.at) < 24*time.Hour {
		return entry.imdb, nil
	}

	u := fmt.Sprintf("%s/%s/%d/external_ids?api_key=%s", tmdbBase, endpoint, tmdbID, url.QueryEscape(t.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	res, err := t.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return "", nil
	}

	var payload struct {
		IMDBID string `json:"imdb_id"`
	}
	if err := decodeJSON(res.Body, &payload); err != nil {
		return "", err
	}

	t.mu.Lock()
	t.external[key] = tmdbExternalCacheEntry{imdb: payload.IMDBID, at: time.Now()}
	t.mu.Unlock()
	return payload.IMDBID, nil
}

func (t *tmdbClient) omdbByTitle(ctx context.Context, title, endpoint string) (string, error) {
	if t == nil || t.omdbKey == "" || title == "" {
		return "", nil
	}
	omdbType := "movie"
	if endpoint == "tv" {
		omdbType = "series"
	}
	u := "https://www.omdbapi.com/?t=" + url.QueryEscape(title) + "&type=" + omdbType + "&apikey=" + url.QueryEscape(t.omdbKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	res, err := t.client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return "", nil
	}

	// OMDB returns Response as a string; handle both shapes via a manual decode.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", err
	}
	if strings.EqualFold(fmt.Sprintf("%v", raw["Response"]), "True") {
		return fmt.Sprintf("%v", raw["imdbID"]), nil
	}
	return "", nil
}

type tmdbResult struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Name        string  `json:"name"`
	PosterPath  string  `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	Overview    string  `json:"overview"`
	ReleaseDate string  `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	VoteAverage float64 `json:"vote_average"`
}

func (r tmdbResult) DisplayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

func (r tmdbResult) Year() string {
	d := r.ReleaseDate
	if d == "" {
		d = r.FirstAirDate
	}
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}

func streamingToStremioMeta(item tmdbResult, contentType, imdbID, rpdbKey string) map[string]interface{} {
	meta := map[string]interface{}{
		"id":   imdbOrTmdbID(imdbID, item.ID, contentType),
		"type": contentType,
		"name": item.DisplayTitle(),
	}

	if rpdbKey != "" && imdbID != "" {
		meta["poster"] = fmt.Sprintf("https://api.ratingposterdb.com/%s/imdb/poster-default/%s.jpg", rpdbKey, imdbID)
	} else if item.PosterPath != "" {
		meta["poster"] = tmdbPosterBase + item.PosterPath
	}
	if item.BackdropPath != "" {
		meta["background"] = tmdbBackdrop + item.BackdropPath
	}
	if item.Overview != "" {
		meta["description"] = item.Overview
	}
	if year := item.Year(); year != "" {
		meta["releaseInfo"] = year
	}
	if item.VoteAverage > 0 {
		meta["imdbRating"] = strconv.FormatFloat(item.VoteAverage, 'f', 1, 64)
	}
	return meta
}

func similarToStremioMeta(item tmdbResult, contentType, imdbID string) map[string]interface{} {
	if imdbID == "" {
		return nil
	}
	meta := map[string]interface{}{
		"id":   imdbID,
		"type": contentType,
		"name": item.DisplayTitle(),
	}
	if item.PosterPath != "" {
		meta["poster"] = tmdbPosterBase + item.PosterPath
	}
	if item.BackdropPath != "" {
		meta["background"] = tmdbBackdrop + item.BackdropPath
	}
	if item.Overview != "" {
		meta["description"] = item.Overview
	}
	if year := item.Year(); year != "" {
		meta["releaseInfo"] = year
	}
	if item.VoteAverage > 0 {
		meta["imdbRating"] = strconv.FormatFloat(item.VoteAverage, 'f', 1, 64)
	}
	return meta
}

func imdbOrTmdbID(imdbID string, tmdbID int, contentType string) string {
	if imdbID != "" {
		return imdbID
	}
	return fmt.Sprintf("tmdb:%s:%d", contentType, tmdbID)
}

func joinInts(vals []int, sep string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, sep)
}
