package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const stashUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

const stashNoTag = "__none__"

// stashDefaultMinGapMS paces StashDB GraphQL requests to stay under its rate
// limit; like TPDB, the enricher fires many lookups per tick. Override with
// STASHDB_MIN_GAP_MS - useful when running fewer keys and needing more per-key
// throughput (the per-key ceiling is 1000/minGap requests/sec).
const stashDefaultMinGapMS = 250

// StashDBClient fetches scenes from StashDB GraphQL.
type StashDBClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	mu         sync.Mutex
	lastReq    time.Time
	minGap     time.Duration
}

// NewStashDBClient creates a StashDB API client.
func NewStashDBClient(baseURL, apiKey string) *StashDBClient {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		baseURL = "https://stashdb.org"
	}
	gapMS := stashDefaultMinGapMS
	if v, err := strconv.Atoi(os.Getenv("STASHDB_MIN_GAP_MS")); err == nil && v >= 0 {
		gapMS = v
	}
	return &StashDBClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 12 * time.Second,
		},
		minGap: time.Duration(gapMS) * time.Millisecond,
	}
}

type tagCache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
}

// ResolveTag resolves a StashDB tag name to its numeric ID.
func (c *StashDBClient) ResolveTag(ctx context.Context, cache tagCache, tagName string) (string, error) {
	if cache != nil {
		if v, ok, err := cache.Get(ctx, tagName); err == nil && ok {
			if v == stashNoTag {
				return "", nil
			}
			return v, nil
		}
	}

	query := `query($name:String!){ queryTags(input:{name:$name, per_page:5}){ tags{ id name } } }`
	data, err := c.graphql(ctx, query, map[string]interface{}{"name": tagName})
	if err != nil {
		return "", err
	}

	tags := nestedSlice(data, "queryTags", "tags")
	var exactID, fuzzyID string
	for _, t := range tags {
		m, _ := t.(map[string]interface{})
		name := strVal(m["name"])
		id := strVal(m["id"])
		if id == "" {
			continue
		}
		if strings.EqualFold(name, tagName) {
			exactID = id
			break
		}
		if fuzzyID == "" {
			fuzzyID = id
		}
	}

	if exactID != "" {
		if cache != nil {
			_ = cache.Set(ctx, tagName, exactID, 30*24*time.Hour)
		}
		return exactID, nil
	}
	if fuzzyID != "" {
		if cache != nil {
			_ = cache.Set(ctx, tagName, fuzzyID, 24*time.Hour)
		}
		return fuzzyID, nil
	}
	if cache != nil {
		_ = cache.Set(ctx, tagName, stashNoTag, 24*time.Hour)
	}
	return "", nil
}

// FetchScenes returns recent StashDB scenes for a category tag.
func (c *StashDBClient) FetchScenes(ctx context.Context, cache tagCache, tagName string, perPage int) ([]Scene, error) {
	if c.APIKey == "" {
		return nil, nil
	}
	tid, err := c.ResolveTag(ctx, cache, tagName)
	if err != nil || tid == "" {
		return nil, err
	}

	query := fmt.Sprintf(`query($tid:ID!){
		queryScenes(input:{
			tags:{value:[$tid],modifier:INCLUDES},
			sort:DATE, direction:DESC, per_page:%d
		}){
			scenes{
				id title release_date
				studio{ name }
				performers{ performer{ name } }
				tags{ name }
				images{ url }
			}
		}
	}`, perPage)

	data, err := c.graphql(ctx, query, map[string]interface{}{"tid": tid})
	if err != nil {
		return nil, err
	}

	raw := nestedSlice(data, "queryScenes", "scenes")
	scenes := make([]Scene, 0, len(raw))
	for _, el := range raw {
		s, ok := el.(map[string]interface{})
		if !ok {
			continue
		}
		studio := ""
		if st, ok := s["studio"].(map[string]interface{}); ok {
			studio = strVal(st["name"])
		}
		date := strVal(s["release_date"])
		performers := make([]string, 0)
		if arr, ok := s["performers"].([]interface{}); ok {
			for _, p := range arr {
				pm, _ := p.(map[string]interface{})
				if perf, ok := pm["performer"].(map[string]interface{}); ok {
					if n := strVal(perf["name"]); n != "" {
						performers = append(performers, n)
					}
				}
			}
		}
		tags := make([]string, 0)
		if arr, ok := s["tags"].([]interface{}); ok {
			for _, t := range arr {
				tm, _ := t.(map[string]interface{})
				if n := strVal(tm["name"]); n != "" {
					tags = append(tags, n)
				}
			}
		}
		poster := ""
		if arr, ok := s["images"].([]interface{}); ok && len(arr) > 0 {
			if im, ok := arr[0].(map[string]interface{}); ok {
				poster = strVal(im["url"])
			}
		}
		descParts := make([]string, 0, 3)
		if studio != "" {
			descParts = append(descParts, "Studio: "+studio)
		}
		if date != "" {
			descParts = append(descParts, "Released: "+date[:min(len(date), 10)])
		}
		if title := strVal(s["title"]); title != "" {
			t := title
			if len(t) > 300 {
				t = t[:300]
			}
			descParts = append(descParts, t)
		}
		scenes = append(scenes, Scene{
			ID:          strVal(s["id"]),
			Title:       strVal(s["title"]),
			Studio:      studio,
			Performers:  performers,
			Tags:        tags,
			Date:        date,
			Poster:      poster,
			Description: strings.Join(descParts, "\n"),
		})
	}
	return scenes, nil
}

func (c *StashDBClient) graphql(ctx context.Context, query string, variables map[string]interface{}) (map[string]interface{}, error) {
	c.mu.Lock()
	if wait := c.minGap - time.Since(c.lastReq); wait > 0 {
		time.Sleep(wait)
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", c.APIKey)
	req.Header.Set("User-Agent", stashUserAgent)

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("stashdb graphql %d: %s", res.StatusCode, truncate(string(raw), 200))
	}

	var payload struct {
		Data   map[string]interface{} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("stashdb graphql: %s", payload.Errors[0].Message)
	}
	return payload.Data, nil
}

func nestedSlice(data map[string]interface{}, keys ...string) []interface{} {
	cur := interface{}(data)
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[k]
	}
	arr, _ := cur.([]interface{})
	return arr
}
