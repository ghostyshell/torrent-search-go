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

	// Prefer the parsed clean query (studio + cleaned scene, with date and
	// resolution junk stripped) over the raw title: TPDB's parse endpoint returns
	// zero results for messy titles like "Blacked 26 06 12 ... 2160p MP4" but
	// resolves the same scene from "Blacked Hope Heaven Pull Chapter 5".
	q := strings.TrimSpace(parsed.CleanQuery)
	if q == "" {
		q = strings.TrimSpace(query)
	}

	// No-date flat performer soups (extra indexers like bitsearch drop the
	// "OnlyFans" prefix and the date): TPDB parse= returns nothing for the alias
	// run and q='s strict AND rejects the full token soup, so the parse/q loop
	// below can't match and only burns the lookup budget. Go straight to pair
	// probes - one sliding 2-token window lands on the canonical pair the scene
	// is indexed under, and VerifyPairDescriptor (pair + descriptor overlap) is
	// the gate the missing date would otherwise provide.
	probes := performerPairProbes(parsed)
	if parsed.Date == "" && len(probes) > 0 {
		// ponytail: fan out <=6 probes concurrently. TPDB q= round-trips are
		// ~1-2s from the deploy region, so a sequential sweep rides the 8s
		// loadTpdbMeta budget too closely (canonical pair is the 3rd probe and
		// a slightly slow call tips the 4th past the deadline, timing out and
		// leaving no cover). The throttle still spaces request starts by minGap,
		// so this is a staggered fan-out, not a burst. Ceiling: maxProbes TPDB
		// calls in flight per lookup; raise per-key limits if 429s appear.
		matched, rl := c.queryKeywordVerifyPairConcurrent(ctx, parsed, probes)
		if matched != nil {
			return matched, nil
		}
		if rl {
			return nil, ErrTPDBRateLimited
		}
		return nil, nil
	}

	// TPDB's parse is fragile to trailing words the torrent has but the scene
	// title does not (e.g. "First"). Try the full clean query first; if TPDB
	// returns nothing, drop the trailing word and retry. VerifyMatch gates every
	// candidate so a looser shorter query can't pick the wrong scene.
	for shortened := 0; ; shortened++ {
		matched, rl := c.queryAndVerify(ctx, parsed, q)
		if matched != nil {
			return matched, nil
		}
		if rl {
			rateLimited = true
		}
		if shortened >= 2 {
			break
		}
		words := strings.Fields(q)
		if len(words) <= 3 {
			break
		}
		q = strings.TrimSpace(strings.Join(words[:len(words)-1], " "))
	}

	// Dated OnlyFans soups: the parse/q loop above may miss the alias run, so
	// fall back to pair probes keyed on the date (VerifyMatch gates on date).
	// No-date soups were handled and returned above; performerPairProbes is nil
	// for normal titles, so this loop only runs for dated OnlyFans soups.
	for _, probe := range probes {
		matched, rl := c.queryKeywordVerify(ctx, parsed, probe)
		if matched != nil {
			return matched, nil
		}
		if rl {
			rateLimited = true
		}
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

// queryAndVerify runs the scenes and movies queries for one query string and
// returns the first VerifyMatch-accepted candidate, plus whether TPDB rate-limited.
func (c *TPDBClient) queryAndVerify(ctx context.Context, parsed ParsedRelease, q string) (*NormalizedMeta, bool) {
	for _, endpoint := range []string{"scenes", "movies"} {
		items, err := c.queryParse(ctx, endpoint, q)
		if err != nil {
			if errors.Is(err, ErrTPDBRateLimited) {
				return nil, true
			}
			continue
		}
		for _, item := range items {
			if VerifyMatch(parsed, tpdbCandidate(item)) {
				return normalizeTPDBItem(item, "tpdb"), false
			}
		}
	}
	return nil, false
}

// keywordScenes runs the q= keyword search (the site search endpoint, strict
// AND) and surfaces 429 as ErrTPDBRateLimited like queryParse.
// ponytail: mirrors queryParse's plumbing because q= uses the "q" param, not
// "parse"; a shared helper would need a param-name switch for the same lines.
func (c *TPDBClient) keywordScenes(ctx context.Context, query string) ([]map[string]interface{}, error) {
	qs := url.Values{}
	qs.Set("q", query)
	qs.Set("per_page", fmt.Sprintf("%d", tpdbMaxResults))
	status, body, err := c.doGet(ctx, c.BaseURL+"/scenes?"+qs.Encode())
	if err != nil {
		return nil, err
	}
	if status == 429 {
		return nil, ErrTPDBRateLimited
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

func (c *TPDBClient) queryKeywordVerify(ctx context.Context, parsed ParsedRelease, q string) (*NormalizedMeta, bool) {
	items, err := c.keywordScenes(ctx, q)
	if err != nil {
		return nil, errors.Is(err, ErrTPDBRateLimited)
	}
	for _, item := range items {
		if VerifyMatch(parsed, tpdbCandidate(item)) {
			return normalizeTPDBItem(item, "tpdb"), false
		}
	}
	return nil, false
}

// queryKeywordVerifyPair is the no-date variant of queryKeywordVerify: it gates
// candidates with VerifyPairDescriptor (both probe performers + a descriptor
// overlap) instead of VerifyMatch, which needs a parsed date to disambiguate.
func (c *TPDBClient) queryKeywordVerifyPair(ctx context.Context, parsed ParsedRelease, q string) (*NormalizedMeta, bool) {
	items, err := c.keywordScenes(ctx, q)
	if err != nil {
		return nil, errors.Is(err, ErrTPDBRateLimited)
	}
	primary, partner := splitProbe(q)
	for _, item := range items {
		if VerifyPairDescriptor(parsed, primary, partner, tpdbCandidate(item)) {
			return normalizeTPDBItem(item, "tpdb"), false
		}
	}
	return nil, false
}

// queryKeywordVerifyPairConcurrent runs the no-date pair probes in parallel and
// returns the first VerifyPairDescriptor-accepted match. Cancelling on the
// first match abandons the remaining in-flight q= requests. The buffered channel
// lets cancelled goroutines send and exit without blocking.
func (c *TPDBClient) queryKeywordVerifyPairConcurrent(ctx context.Context, parsed ParsedRelease, probes []string) (*NormalizedMeta, bool) {
	type result struct {
		meta *NormalizedMeta
		rl   bool
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan result, len(probes))
	for _, probe := range probes {
		go func(q string) {
			meta, rl := c.queryKeywordVerifyPair(ctx, parsed, q)
			ch <- result{meta: meta, rl: rl}
		}(probe)
	}
	rateLimited := false
	for range probes {
		r := <-ch
		if r.meta != nil {
			cancel()
			return r.meta, false
		}
		if r.rl {
			rateLimited = true
		}
	}
	return nil, rateLimited
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
		Studio:      tpdbStudioName(item),
		Source:      source,
	}
}

// tpdbStudioName extracts the scene's site/studio name, the key the PornRips
// enrich sweep indexes pr_studio under.
func tpdbStudioName(item map[string]interface{}) string {
	if site, ok := item["site"].(map[string]interface{}); ok {
		if n := strVal(site["name"]); n != "" {
			return n
		}
	}
	if st, ok := item["studio"].(map[string]interface{}); ok {
		if n := strVal(st["name"]); n != "" {
			return n
		}
	}
	return ""
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
