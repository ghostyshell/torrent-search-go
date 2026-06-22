package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const tpdbMaxResults = 5

// SearchMetadata looks up TPDB metadata for a torrent title and returns a normalized match.
func (c *TPDBClient) SearchMetadata(ctx context.Context, title string) (*NormalizedMeta, error) {
	parsed := ParseRelease(title)
	return c.SearchMetadataProbe(ctx, parsed, title)
}

// SearchMetadataProbe queries TPDB with query but verifies candidates against parsed.
func (c *TPDBClient) SearchMetadataProbe(ctx context.Context, parsed ParsedRelease, query string) (*NormalizedMeta, error) {
	if c.APIKey == "" || query == "" {
		return nil, nil
	}

	rateLimited := false

	// JAV resolves on the dedicated /jav endpoint, keyed by product code. TPDB's
	// parse is input-sensitive: labeled DVD codes match best on the bare code
	// (ABF-342), while a full messy title returns nothing; site-prefixed codes
	// (Heyzo) match on the title and not the dashed code. So try both, and require
	// an exact code match - parse also returns fuzzy near-codes (4bf-342, neo-342).
	if parsed.Code != "" {
		probes := []string{parsed.Code}
		if query != "" && query != parsed.Code {
			probes = append(probes, query)
		}
		for _, p := range probes {
			jav, err := c.queryParse(ctx, "jav", p)
			if errors.Is(err, ErrTPDBRateLimited) {
				rateLimited = true
				continue
			}
			if err != nil {
				continue
			}
			for _, item := range jav {
				if CodesMatch(parsed.Code, tpdbCandidate(item).Code) {
					return normalizeTPDBItem(item, "tpdb"), nil
				}
			}
		}
	}

	q := strings.TrimSpace(query)
	if q == "" {
		q = strings.TrimSpace(parsed.CleanQuery)
	}

	if scene, err := c.queryParse(ctx, "scenes", q); err == nil && scene != nil {
		for _, item := range scene {
			if VerifyMatch(parsed, tpdbCandidate(item)) {
				return normalizeTPDBItem(item, "tpdb"), nil
			}
		}
	} else if errors.Is(err, ErrTPDBRateLimited) {
		rateLimited = true
	}

	if movie, err := c.queryParse(ctx, "movies", q); err == nil && movie != nil {
		for _, item := range movie {
			if VerifyMatch(parsed, tpdbCandidate(item)) {
				return normalizeTPDBItem(item, "tpdb"), nil
			}
		}
	} else if errors.Is(err, ErrTPDBRateLimited) {
		rateLimited = true
	}

	if rateLimited {
		return nil, ErrTPDBRateLimited
	}
	return nil, nil
}

func (c *TPDBClient) queryParse(ctx context.Context, endpoint, query string) ([]map[string]interface{}, error) {
	qs := url.Values{}
	qs.Set("parse", query)
	qs.Set("limit", fmt.Sprintf("%d", tpdbMaxResults))

	status, body, err := c.doGet(ctx, c.BaseURL+"/"+endpoint+"?"+qs.Encode())
	if err != nil {
		return nil, err
	}
	if status == 429 {
		return nil, ErrTPDBRateLimited
	}
	if status >= 400 {
		return nil, fmt.Errorf("tpdb %s %d", endpoint, status)
	}

	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func tpdbCandidate(item map[string]interface{}) MatchCandidate {
	performers := stringSliceFromObjects(item["performers"], "name")
	for i, p := range performers {
		if p == "" {
			if arr, ok := item["performers"].([]interface{}); ok && i < len(arr) {
				if m, ok := arr[i].(map[string]interface{}); ok {
					if s := strVal(m["stage_name"]); s != "" {
						performers[i] = s
					}
				}
			}
		}
	}
	studio := ""
	if site, ok := item["site"].(map[string]interface{}); ok {
		studio = strVal(site["name"])
	}
	if studio == "" {
		if st, ok := item["studio"].(map[string]interface{}); ok {
			studio = strVal(st["name"])
		}
	}
	code := parseJAVCode(strVal(item["title"]))
	if code == "" {
		code = strVal(item["external_id"])
	}
	return MatchCandidate{
		Title:      strVal(item["title"]),
		Studio:     studio,
		Performers: performers,
		Date:       strVal(item["date"]),
		Code:       code,
	}
}

func normalizeTPDBItem(item map[string]interface{}, source string) *NormalizedMeta {
	if item == nil {
		return nil
	}
	performers := stringSliceFromObjects(item["performers"], "name")
	date := strVal(item["date"])
	if date == "" {
		date = strVal(item["release_date"])
	}
	year := extractYear(date)
	poster := FindImage(item, "poster")
	bg := FindImage(item, "background")
	if bg == "" {
		bg = poster
	}
	desc := buildTPDBDescription(item)
	return &NormalizedMeta{
		Title:       strVal(item["title"]),
		Description: desc,
		Poster:      poster,
		Background:  bg,
		Year:        year,
		Cast:        performers,
		Tags:        tagNames(item["tags"]),
		Source:      source,
	}
}

func buildTPDBDescription(item map[string]interface{}) string {
	parts := make([]string, 0, 3)
	if site, ok := item["site"].(map[string]interface{}); ok {
		if n := strVal(site["name"]); n != "" {
			parts = append(parts, "Studio: "+n)
		}
	}
	if d := strVal(item["date"]); d != "" {
		parts = append(parts, "Released: "+d[:min(len(d), 10)])
	}
	summary := strVal(item["description"])
	if summary == "" {
		summary = strVal(item["summary"])
	}
	if summary != "" {
		if len(summary) > 300 {
			summary = summary[:300]
		}
		parts = append(parts, summary)
	}
	return strings.Join(parts, "\n")
}

func extractYear(d string) string {
	if d == "" {
		return ""
	}
	re := regexp.MustCompile(`\b(19|20)\d{2}\b`)
	if m := re.FindString(d); m != "" {
		return m
	}
	return ""
}
