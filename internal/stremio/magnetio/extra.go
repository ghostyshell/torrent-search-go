package magnetio

import (
	"net/url"
	"strconv"
	"strings"
)

// parseExtra parses a Stremio catalog extra segment into a key/value map.
// It tolerates both slash-separated and ampersand-separated query strings.
func parseExtra(extra string) map[string]string {
	out := map[string]string{}
	if extra == "" {
		return out
	}
	extra = strings.TrimPrefix(extra, "/")
	decoded, err := url.QueryUnescape(extra)
	if err != nil {
		decoded = extra
	}
	// If the decoded string contains both '/' and '&', prefer the ampersand form.
	if strings.Contains(decoded, "/") && strings.Contains(decoded, "=") {
		for _, pair := range strings.Split(decoded, "&") {
			for _, sub := range strings.Split(pair, "/") {
				if eq := strings.Index(sub, "="); eq >= 0 {
					out[strings.TrimSpace(sub[:eq])] = strings.TrimSpace(sub[eq+1:])
				}
			}
		}
		return out
	}
	vals, err := url.ParseQuery(decoded)
	if err != nil {
		return out
	}
	for k, v := range vals {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// parseSkip extracts the skip value from a catalog extra segment.
func parseSkip(extra string) int {
	params := parseExtra(extra)
	n, _ := strconv.Atoi(params["skip"])
	if n < 0 {
		return 0
	}
	return n
}
