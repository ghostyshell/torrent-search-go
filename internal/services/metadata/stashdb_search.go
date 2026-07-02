package metadata

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const stashMaxResults = 5

// JAV code/title searches return many fuzzy hits; scan deeper for the exact code.
const stashCodeSearchLimit = 20

var trackerHostRE = regexp.MustCompile(`(?i)(^|\.)(thehiddenbay|thepiratebay|piratebay|tpb|torrentgalaxy|magnetdl|limetorrents|1337x|nyaa|rarbg)\.`)

const sceneFields = `
	id title code details release_date production_date duration
	studio { name }
	tags { name category { group name } }
	images { url }
	performers { performer { name } }
`

// SearchMetadata looks up StashDB metadata for a torrent.
func (c *StashDBClient) SearchMetadata(ctx context.Context, title, detailURL string) (*NormalizedMeta, error) {
	parsed := ParseRelease(title)
	return c.SearchMetadataProbe(ctx, parsed, title, detailURL)
}

// SearchMetadataProbe queries StashDB with query but verifies candidates against parsed.
func (c *StashDBClient) SearchMetadataProbe(ctx context.Context, parsed ParsedRelease, query, detailURL string) (*NormalizedMeta, error) {
	if c.APIKey == "" || query == "" {
		return nil, nil
	}

	// JAV: StashDB's searchScene matches the scene `code` column, so the product
	// code is the most reliable lookup. Try the bare code first (precise for
	// labeled DVD codes like SSIS-001), then the full title - date codes such as
	// Caribbeancom's "050926-001" are ambiguous as a bare term (every studio has a
	// "-001"), but the studio name in the title disambiguates the search. Confirm
	// either way with an exact normalized code match.
	if parsed.Code != "" {
		terms := []string{parsed.Code}
		if query != "" && query != parsed.Code {
			terms = append(terms, query)
		}
		searchQ := fmt.Sprintf(`query($term:String!,$limit:Int){ searchScene(term:$term,limit:$limit){ %s } }`, sceneFields)
		for _, term := range terms {
			data, err := c.graphql(ctx, searchQ, map[string]interface{}{
				"term":  term,
				"limit": stashCodeSearchLimit,
			})
			if err != nil {
				continue
			}
			for _, s := range sceneMaps(data, "searchScene") {
				if CodesMatch(parsed.Code, strVal(s["code"])) {
					return normalizeStashScene(s), nil
				}
			}
		}
	}

	if canonical := canonicalizeURL(detailURL); canonical != "" && !isTrackerURL(detailURL) {
		query := fmt.Sprintf(`query($url:String!,$perPage:Int){
			queryScenes(input:{url:$url,per_page:$perPage}){ scenes { %s } }
		}`, sceneFields)
		data, err := c.graphql(ctx, query, map[string]interface{}{
			"url":     canonical,
			"perPage": stashMaxResults,
		})
		if err == nil {
			for _, s := range sceneMaps(data, "queryScenes", "scenes") {
				if VerifyMatch(parsed, stashCandidate(s)) || IsGoodMatch(strVal(s["title"]), parsed.Scene) {
					return normalizeStashScene(s), nil
				}
			}
		}
	}

	term := strings.TrimSpace(query)
	if term == "" {
		term = parsed.Scene
	}
	if term == "" {
		term = parsed.CleanQuery
	}
	searchQ := fmt.Sprintf(`query($term:String!,$limit:Int){ searchScene(term:$term,limit:$limit){ %s } }`, sceneFields)
	data, err := c.graphql(ctx, searchQ, map[string]interface{}{
		"term":  term,
		"limit": stashMaxResults,
	})
	if err == nil {
		for _, s := range sceneMaps(data, "searchScene") {
			if VerifyMatch(parsed, stashCandidate(s)) {
				return normalizeStashScene(s), nil
			}
		}
	}

	if parsed.Date != "" && parsed.Scene != "" {
		if s, err := c.structuredByPerformerDate(ctx, parsed); err == nil && s != nil {
			return normalizeStashScene(s), nil
		}
	}

	return nil, nil
}

// SearchPerformerScenes resolves a performer name to their recent scene codes.
// It returns nil unless a returned performer's name matches the query exactly
// (order-independent token set), so a generic query ("big tits") is not mistaken
// for a performer. Used by catalog search to turn an actress name into JAV codes.
func (c *StashDBClient) SearchPerformerScenes(ctx context.Context, name string, limit int) ([]string, error) {
	if c.APIKey == "" || strings.TrimSpace(name) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 15
	}

	perfQ := `query($term:String!){ searchPerformer(term:$term){ id name } }`
	pdata, err := c.graphql(ctx, perfQ, map[string]interface{}{"term": name})
	if err != nil {
		return nil, err
	}
	var pid string
	for _, p := range nestedSlice(pdata, "searchPerformer") {
		pm, _ := p.(map[string]interface{})
		if strVal(pm["id"]) != "" && performerNameMatches(name, strVal(pm["name"])) {
			pid = strVal(pm["id"])
			break
		}
	}
	if pid == "" {
		return nil, nil
	}

	sceneQ := `query($pid:ID!,$pp:Int){
		queryScenes(input:{performers:{value:[$pid],modifier:INCLUDES}, per_page:$pp, sort:DATE, direction:DESC}){ scenes { code } }
	}`
	sdata, err := c.graphql(ctx, sceneQ, map[string]interface{}{"pid": pid, "pp": limit})
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, limit)
	seen := make(map[string]struct{})
	for _, s := range sceneMaps(sdata, "queryScenes", "scenes") {
		code := strings.TrimSpace(strVal(s["code"]))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes, nil
}

// performerNameMatches reports whether two names share the exact same set of
// lowercased alphanumeric tokens (order-independent: "Mio Ishikawa" == "Ishikawa
// Mio"), guarding performer routing against partial/generic matches.
func performerNameMatches(a, b string) bool {
	at := nameTokenSet(a)
	bt := nameTokenSet(b)
	if len(at) == 0 || len(at) != len(bt) {
		return false
	}
	for t := range at {
		if _, ok := bt[t]; !ok {
			return false
		}
	}
	return true
}

func nameTokenSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range strings.Fields(nonAlnum.ReplaceAllString(strings.ToLower(s), " ")) {
		out[w] = struct{}{}
	}
	return out
}

func (c *StashDBClient) structuredByPerformerDate(ctx context.Context, parsed ParsedRelease) (map[string]interface{}, error) {
	perfQ := `query($term:String!,$limit:Int){ searchPerformer(term:$term,limit:$limit){ id name } }`
	pdata, err := c.graphql(ctx, perfQ, map[string]interface{}{
		"term":  parsed.Scene,
		"limit": 3,
	})
	if err != nil {
		return nil, err
	}
	performers := nestedSlice(pdata, "searchPerformer")
	for _, p := range performers {
		pm, _ := p.(map[string]interface{})
		pid := strVal(pm["id"])
		name := strVal(pm["name"])
		if pid == "" {
			continue
		}
		if !PerformerOverlap(parsed.Tokens, MatchCandidate{Performers: []string{name}}) {
			continue
		}
		sceneQ := fmt.Sprintf(`query($pid:ID!,$date:Date!){
			queryScenes(input:{
				performers:{value:[$pid],modifier:INCLUDES}
				date:{value:$date,modifier:EQUALS}
				per_page:10
			}){ scenes { %s } }
		}`, sceneFields)
		qdata, err := c.graphql(ctx, sceneQ, map[string]interface{}{
			"pid":  pid,
			"date": parsed.Date,
		})
		if err != nil {
			continue
		}
		for _, s := range sceneMaps(qdata, "queryScenes", "scenes") {
			if VerifyMatch(parsed, stashCandidate(s)) {
				return s, nil
			}
		}
	}
	return nil, nil
}

func sceneMaps(data map[string]interface{}, keys ...string) []map[string]interface{} {
	raw := nestedSlice(data, keys...)
	out := make([]map[string]interface{}, 0, len(raw))
	for _, el := range raw {
		if m, ok := el.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func stashCandidate(s map[string]interface{}) MatchCandidate {
	studio := ""
	if st, ok := s["studio"].(map[string]interface{}); ok {
		studio = strVal(st["name"])
	}
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
	date := strVal(s["release_date"])
	if date == "" {
		date = strVal(s["production_date"])
	}
	return MatchCandidate{
		Title:      strVal(s["title"]),
		Studio:     studio,
		Performers: performers,
		Date:       date,
		Code:       strVal(s["code"]),
	}
}

func normalizeStashScene(s map[string]interface{}) *NormalizedMeta {
	poster := ""
	if arr, ok := s["images"].([]interface{}); ok && len(arr) > 0 {
		if im, ok := arr[0].(map[string]interface{}); ok {
			poster = strVal(im["url"])
		}
	}
	tags := make([]string, 0)
	if arr, ok := s["tags"].([]interface{}); ok {
		for _, t := range arr {
			tm, _ := t.(map[string]interface{})
			group := ""
			if cat, ok := tm["category"].(map[string]interface{}); ok {
				group = strVal(cat["group"])
			}
			name := strVal(tm["name"])
			if name != "" && (group == "SCENE" || group == "ACTION" || group == "PEOPLE") {
				tags = append(tags, name)
			}
		}
	}
	cast := make([]string, 0)
	if arr, ok := s["performers"].([]interface{}); ok {
		for _, p := range arr {
			pm, _ := p.(map[string]interface{})
			if perf, ok := pm["performer"].(map[string]interface{}); ok {
				if n := strVal(perf["name"]); n != "" {
					cast = append(cast, n)
				}
			}
		}
	}
	date := strVal(s["release_date"])
	if date == "" {
		date = strVal(s["production_date"])
	}
	return &NormalizedMeta{
		Title:       strVal(s["title"]),
		Description: buildStashDescription(s),
		Poster:      poster,
		Background:  poster,
		Year:        extractYear(date),
		Cast:        cast,
		Tags:        tags,
		Source:      "stashdb",
	}
}

func buildStashDescription(s map[string]interface{}) string {
	parts := make([]string, 0, 4)
	if st, ok := s["studio"].(map[string]interface{}); ok {
		if n := strVal(st["name"]); n != "" {
			parts = append(parts, "Studio: "+n)
		}
	}
	date := strVal(s["release_date"])
	if date == "" {
		date = strVal(s["production_date"])
	}
	if date != "" {
		parts = append(parts, "Released: "+date[:min(len(date), 10)])
	}
	if d := strVal(s["details"]); d != "" {
		if len(d) > 300 {
			d = d[:300]
		}
		parts = append(parts, d)
	}
	return strings.Join(parts, "\n")
}

func canonicalizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(strings.TrimSuffix(raw, "/"))
	}
	u.Fragment = ""
	u.Host = strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
	}
	if len(u.Path) > 1 {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

func isTrackerURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return trackerHostRE.MatchString(u.Hostname())
}
