package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrTPDBRateLimited is returned when TPDB responds 429 even after a Retry-After
// backoff. Callers (e.g. MetaEnricher) should re-queue the lookup for a later tick.
var ErrTPDBRateLimited = errors.New("tpdb rate limited")

const tpdbUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// tpdbDefaultMinGapMS paces TPDB requests to stay under its rate limit; the
// enricher fires many lookups per tick and TPDB 429s on bursts. Override with
// TPDB_MIN_GAP_MS.
const tpdbDefaultMinGapMS = 250

// Scene is a normalized scene record for category warming.
type Scene struct {
	// ID is the source scene id (TPDB numeric id, StashDB uuid). Carried so the
	// EnrichedScenesSync job can build the store _id (the Stremio metaID) without
	// re-deriving it from the raw map. Empty for code paths that don't need it.
	ID          string
	Title       string
	Studio      string
	Performers  []string
	Tags        []string
	Date        string
	Poster      string
	Description string
}

// TPDBClient fetches scenes from ThePornDB.
type TPDBClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	mu         sync.Mutex
	lastReq    time.Time
	minGap     time.Duration
}

// NewTPDBClient creates a TPDB API client.
func NewTPDBClient(baseURL, apiKey string) *TPDBClient {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.theporndb.net"
	}
	gapMS := tpdbDefaultMinGapMS
	if v, err := strconv.Atoi(os.Getenv("TPDB_MIN_GAP_MS")); err == nil && v >= 0 {
		gapMS = v
	}
	return &TPDBClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 12 * time.Second,
		},
		minGap: time.Duration(gapMS) * time.Millisecond,
	}
}

// doGet performs a rate-limited GET against TPDB, honoring a 429 Retry-After with
// a single retry. Returns the status code and body so callers keep their own
// error semantics.
func (c *TPDBClient) doGet(ctx context.Context, rawURL string) (int, []byte, error) {
	for attempt := 0; ; attempt++ {
		c.throttle()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", tpdbUserAgent)

		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		body, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return res.StatusCode, nil, readErr
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			select {
			case <-time.After(tpdbRetryAfter(res.Header.Get("Retry-After"))):
				continue
			case <-ctx.Done():
				return res.StatusCode, body, ctx.Err()
			}
		}
		return res.StatusCode, body, nil
	}
}

// throttle spaces requests by minGap so bursts don't trip TPDB's rate limit.
func (c *TPDBClient) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.minGap - time.Since(c.lastReq); wait > 0 {
		time.Sleep(wait)
	}
	c.lastReq = time.Now()
}

// tpdbRetryAfter parses a 429 Retry-After (delta-seconds), capped, with a default.
func tpdbRetryAfter(h string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && secs > 0 {
		if secs > 30 {
			secs = 30
		}
		return time.Duration(secs) * time.Second
	}
	return time.Second
}

// FetchScenes returns recent scenes for a keyword query.
// BrowseScenes returns the most-recent scenes page without a search query.
func (c *TPDBClient) BrowseScenes(ctx context.Context, page, perPage int) ([]map[string]interface{}, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	qs := url.Values{}
	qs.Set("sort", "-date")
	qs.Set("per_page", strconv.Itoa(perPage))
	qs.Set("page", strconv.Itoa(page))
	return c.scenesRaw(ctx, c.BaseURL+"/scenes?"+qs.Encode())
}

// BrowseScenesDate lists scenes for a date window. The /scenes endpoint hard-caps
// total at 10000 (Laravel paginator), so plain BrowseScenes paging only ever
// reaches the newest ~10000 and clamps out-of-range pages to the last page. TPDB
// stores release dates month-anchored to the 1st, so date=YYYY-MM-01 returns that
// month's scenes as a sub-10000 set with its own total - walking monthly windows
// is the only way to reach the full historical catalog past the newest 10000.
func (c *TPDBClient) BrowseScenesDate(ctx context.Context, date string, page, perPage int) ([]map[string]interface{}, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	qs := url.Values{}
	qs.Set("sort", "-date")
	qs.Set("per_page", strconv.Itoa(perPage))
	qs.Set("page", strconv.Itoa(page))
	qs.Set("date", date)
	return c.scenesRaw(ctx, c.BaseURL+"/scenes?"+qs.Encode())
}

// GetScene returns a single scene by its numeric TPDB ID.
func (c *TPDBClient) GetScene(ctx context.Context, id string) (map[string]interface{}, error) {
	if c.APIKey == "" || id == "" {
		return nil, nil
	}
	status, body, err := c.doGet(ctx, c.BaseURL+"/scenes/"+url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	if status >= 400 {
		return nil, fmt.Errorf("tpdb scene %s: %d", id, status)
	}
	var payload struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

// SearchScenesRaw returns raw scene maps for a keyword query.
func (c *TPDBClient) SearchScenesRaw(ctx context.Context, query string, perPage int) ([]map[string]interface{}, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	qs := url.Values{}
	qs.Set("q", query)
	qs.Set("sort", "-date")
	qs.Set("per_page", strconv.Itoa(perPage))
	return c.scenesRaw(ctx, c.BaseURL+"/scenes?"+qs.Encode())
}

func (c *TPDBClient) scenesRaw(ctx context.Context, reqURL string) ([]map[string]interface{}, error) {
	status, body, err := c.doGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("tpdb scenes %d", status)
	}
	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func (c *TPDBClient) FetchScenes(ctx context.Context, query string, perPage int) ([]Scene, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	qs := url.Values{}
	qs.Set("q", query)
	qs.Set("sort", "-date")
	qs.Set("per_page", fmt.Sprintf("%d", perPage))

	status, body, err := c.doGet(ctx, c.BaseURL+"/scenes?"+qs.Encode())
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("tpdb scenes %d: %s", status, truncate(string(body), 200))
	}

	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	scenes := make([]Scene, 0, len(payload.Data))
	for _, item := range payload.Data {
		scenes = append(scenes, NormalizeTPDBScene(item))
	}
	return scenes, nil
}

// SceneIDFromItem returns the raw TPDB scene id as a string (the numeric id),
// empty when absent. The caller (EnrichedScenesSync) builds the Stremio metaID
// prefix ("porndb:<id>"); metadata stays free of Stremio-protocol concerns.
func SceneIDFromItem(item map[string]interface{}) string {
	switch v := item["id"].(type) {
	case float64:
		return strconv.Itoa(int(v))
	case string:
		return v
	}
	return ""
}

// NormalizeTPDBScene builds a Scene from a raw TPDB scene map. Exported so the
// EnrichedScenesSync discovery pass (which walks BrowseScenes raw maps) reuses the
// same normalization as FetchScenes.
func NormalizeTPDBScene(item map[string]interface{}) Scene {
	studio := ""
	if site, ok := item["site"].(map[string]interface{}); ok {
		studio = strVal(site["name"])
	}
	date := strVal(item["date"])
	performers := stringSliceFromObjects(item["performers"], "name")
	tags := tagNames(item["tags"])
	poster := FindImage(item, "poster")

	descParts := make([]string, 0, 3)
	if studio != "" {
		descParts = append(descParts, "Studio: "+studio)
	}
	if date != "" {
		descParts = append(descParts, "Released: "+date[:min(len(date), 10)])
	}
	summary := strVal(item["description"])
	if summary == "" {
		summary = strVal(item["summary"])
	}
	if summary != "" {
		s := summary
		if len(s) > 300 {
			s = s[:300]
		}
		descParts = append(descParts, s)
	}

	return Scene{
		ID:          SceneIDFromItem(item),
		Title:       strVal(item["title"]),
		Studio:      studio,
		Performers:  performers,
		Tags:        tags,
		Date:        date,
		Poster:      poster,
		Description: strings.Join(descParts, "\n"),
	}
}

// FindImage extracts a poster/background URL from a TPDB item.
func FindImage(item map[string]interface{}, field string) string {
	if item == nil {
		return ""
	}
	var candidates []interface{}
	if field == "background" {
		candidates = []interface{}{
			item["background"], item["background_back"], item[field],
			item["poster"], item["image"], item["cover"],
			item["poster_url"], item["background_url"], item["posters"],
		}
	} else {
		candidates = []interface{}{
			item[field], item["poster"], item["image"], item["cover"],
			item["poster_url"], item["posters"], item["background"],
			item["background_back"], item["background_url"],
		}
	}
	if imgs, ok := item["images"].([]interface{}); ok {
		candidates = append(candidates, imgs...)
	}
	if posters, ok := item["posters"].([]interface{}); ok {
		candidates = append(candidates, posters...)
	}
	if data, ok := item["data"].(map[string]interface{}); ok {
		candidates = append(candidates, data["poster"], data["image"], data["background"])
	}
	for _, c := range candidates {
		if u := resolveImageURL(c); u != "" {
			return u
		}
	}
	return ""
}

func resolveImageURL(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return ""
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return s
		}
		if strings.HasPrefix(s, "//") {
			return "https:" + s
		}
		return ""
	case map[string]interface{}:
		for _, key := range []string{"full", "large", "url", "image"} {
			if u := resolveImageURL(v[key]); u != "" {
				return u
			}
		}
	}
	return ""
}

func strVal(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func stringSliceFromObjects(v interface{}, key string) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			continue
		}
		if s := strVal(m[key]); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func tagNames(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]interface{})
		if !ok {
			continue
		}
		name := strVal(m["tag"])
		if name == "" {
			name = strVal(m["name"])
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
