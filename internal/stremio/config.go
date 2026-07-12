package stremio

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"

	"torrent-search-go/internal/services/jobs"
)

// Config is the parsed per-install Stremio addon configuration.
type Config struct {
	RdKey                string   `json:"rdKey"`
	TbKey                string   `json:"tbKey"`
	PmKey                string   `json:"pmKey"`
	EdKey                string   `json:"edKey"`
	DlKey                string   `json:"dlKey"`
	OcKey                string   `json:"ocKey"`
	PuKey                string   `json:"puKey"`
	DpKey                string   `json:"dpKey"`
	LsKey                string   `json:"lsKey"`
	MgKey                string   `json:"mgKey"`
	DrKey                string   `json:"drKey"`
	SrKey                string   `json:"srKey"`
	Sources              []string `json:"sources"`
	DisabledCatalogs     []string `json:"disabledCatalogs"`
	DisabledPrStudios    []string `json:"disabledPrStudios"`
	EnabledCatalogs      []string `json:"enabledCatalogs"`
	EnabledSorts         []string `json:"enabledSorts"`
	Group                int      `json:"group"`
	GroupTotal           int      `json:"groupTotal"`
	ProviderTotal        int      `json:"providerTotal"`
	BackendURL           string   `json:"backendUrl"`
	BackendToken         string   `json:"backendToken"`
	MaxResults           int      `json:"maxResults"`
	MinSeeders           int      `json:"minSeeders"`
	HideP2P              bool     `json:"hideP2P"`
	HideFromHome         bool     `json:"hideFromHome"`
	MediaFlowProxyURL    string   `json:"mediaFlowProxyUrl"`
	MediaFlowAPIPassword string   `json:"mediaFlowApiPassword"`
	ProxyDebridStreams   bool     `json:"proxyDebridStreams"`
	NamePostfix          string   `json:"namePostfix"`
	TpdbKey              string   `json:"tpdbKey"`
	TpdbURL              string   `json:"tpdbUrl"`
	StashdbKey           string   `json:"stashdbKey"`
	StashdbURL           string   `json:"stashdbUrl"`
	TpdbCategories       []string `json:"tpdbCategories"`
	StashdbCategories    []string `json:"stashdbCategories"`
	ExtraIndexers        bool     `json:"extraIndexers"`
	Enable1337x          bool     `json:"enable1337x"`
	CompactStudios       bool     `json:"compactStudios"`
}

var metadataAPIHostRE = regexp.MustCompile(`(?i)(?:api\.)?metadataapi\.net`)

// DefaultConfig returns hardcoded defaults (before env / user merges).
func DefaultConfig() Config {
	return Config{
		Sources:           []string{"piratebay"},
		DisabledCatalogs:  []string{},
		DisabledPrStudios: []string{},
		EnabledCatalogs:   nil,
		EnabledSorts:      []string{"recent", "top"},
		MaxResults:        20,
		MinSeeders:        3,
		TpdbCategories:    []string{},
		StashdbCategories: []string{},
	}
}

type envDefaults struct {
	BackendURL       string
	BackendToken     string
	TpdbKey          string
	TpdbURL          string
	StashdbKey       string
	StashdbURL       string
	AllowUserBackend bool
}

func envFromOS() envDefaults {
	tpdbURL := metadataAPIHostRE.ReplaceAllString(
		envOr("TPDB_API_URL", "https://api.theporndb.net"),
		"api.theporndb.net",
	)
	return envDefaults{
		BackendURL:       os.Getenv("BACKEND_URL"),
		BackendToken:     os.Getenv("ADDON_API_TOKEN"),
		TpdbKey:          os.Getenv("TPDB_API_KEY"),
		TpdbURL:          strings.TrimSuffix(tpdbURL, "/"),
		StashdbKey:       os.Getenv("STASHDB_API_KEY"),
		StashdbURL:       strings.TrimSuffix(envOr("STASHDB_API_URL", "https://stashdb.org"), "/"),
		AllowUserBackend: os.Getenv("ALLOW_USER_BACKEND") != "",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ParseConfig decodes config from a base64url path segment and merges env defaults.
func ParseConfig(encoded string) Config {
	return parseConfig(encoded, envFromOS())
}

func parseConfig(encoded string, env envDefaults) Config {
	cfg := DefaultConfig()
	cfg.BackendURL = env.BackendURL
	cfg.BackendToken = env.BackendToken
	cfg.TpdbKey = env.TpdbKey
	cfg.TpdbURL = env.TpdbURL
	cfg.StashdbKey = env.StashdbKey
	cfg.StashdbURL = env.StashdbURL

	rawUser := map[string]json.RawMessage{}
	if encoded != "" && encoded != "default" {
		b64 := strings.NewReplacer("-", "+", "_", "/").Replace(encoded)
		if pad := len(b64) % 4; pad != 0 {
			b64 += strings.Repeat("=", 4-pad)
		}
		if data, err := base64.StdEncoding.DecodeString(b64); err == nil {
			_ = json.Unmarshal(data, &rawUser)
			var userMap map[string]interface{}
			if err := json.Unmarshal(data, &userMap); err == nil {
				baseMap := configToMap(cfg)
				merged := mergeConfigMaps(baseMap, userMap)
				if mergedCfg, err := mapToConfig(merged); err == nil {
					cfg = mergedCfg
				}
			}
		}
	}

	enforceDebridExclusion(&cfg)
	normalizeSources(&cfg)
	cfg.DisabledCatalogs = filterStrings(cfg.DisabledCatalogs)
	cfg.DisabledPrStudios = filterStrings(cfg.DisabledPrStudios)
	cfg.EnabledCatalogs = normalizeEnabledCatalogs(cfg.EnabledCatalogs)
	if _, supplied := rawUser["enabledSorts"]; supplied {
		cfg.EnabledSorts = normalizeEnabledSorts(cfg.EnabledSorts, false)
	} else {
		cfg.EnabledSorts = []string{"recent", "top"}
	}

	cfg.Group = maxInt(cfg.Group, 0)
	cfg.GroupTotal = maxInt(cfg.GroupTotal, 0)
	cfg.ProviderTotal = maxInt(cfg.ProviderTotal, 0)

	if cfg.MaxResults == 0 {
		cfg.MaxResults = 20
	}
	cfg.MaxResults = minInt(maxInt(cfg.MaxResults, 1), 100)
	cfg.MinSeeders = maxInt(cfg.MinSeeders, 0)

	cfg.NamePostfix = trimNamePostfix(cfg.NamePostfix)

	cfg.TpdbKey = strings.TrimSpace(cfg.TpdbKey)
	cfg.TpdbURL = strings.TrimSuffix(strings.TrimSpace(cfg.TpdbURL), "/")
	if cfg.TpdbURL == "" {
		cfg.TpdbURL = env.TpdbURL
	}

	cfg.StashdbKey = strings.TrimSpace(cfg.StashdbKey)
	cfg.StashdbURL = strings.TrimSuffix(strings.TrimSpace(cfg.StashdbURL), "/")
	if cfg.StashdbURL == "" {
		cfg.StashdbURL = env.StashdbURL
	}

	cfg.TpdbCategories = resolveCategorySlugs(rawUser, "tpdbCategories", cfg.TpdbKey != "", cfg.TpdbCategories)
	cfg.StashdbCategories = resolveCategorySlugs(rawUser, "stashdbCategories", cfg.StashdbKey != "", cfg.StashdbCategories)

	if env.BackendURL != "" {
		if !env.AllowUserBackend {
			cfg.BackendURL = env.BackendURL
			cfg.BackendToken = env.BackendToken
		} else if cfg.BackendURL != "" && !isSafeURL(cfg.BackendURL) {
			cfg.BackendURL = env.BackendURL
			cfg.BackendToken = env.BackendToken
		}
	} else if cfg.BackendURL != "" && !isSafeURL(cfg.BackendURL) {
		cfg.BackendURL = ""
		cfg.BackendToken = ""
	}

	if cfg.TpdbURL != "" && !isSafeURL(cfg.TpdbURL) {
		cfg.TpdbURL = env.TpdbURL
	}
	if cfg.StashdbURL != "" && !isSafeURL(cfg.StashdbURL) {
		cfg.StashdbURL = env.StashdbURL
	}

	return cfg
}

// EncodeConfig serializes config to a base64url string for embedding in install URLs.
func EncodeConfig(cfg Config) string {
	data, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	s := base64.StdEncoding.EncodeToString(data)
	s = strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(s), "=")
	return s
}

func configToMap(cfg Config) map[string]interface{} {
	data, _ := json.Marshal(cfg)
	out := map[string]interface{}{}
	_ = json.Unmarshal(data, &out)
	return out
}

func mapToConfig(m map[string]interface{}) (Config, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func mergeConfigMaps(base, user map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(user))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range user {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func enforceDebridExclusion(cfg *Config) {
	var winner string
	for _, field := range DebridKeyFields {
		if cfg.debridKey(field) != "" {
			winner = field
			break
		}
	}
	for _, field := range DebridKeyFields {
		if field != winner {
			cfg.setDebridKey(field, "")
		}
	}
}

func normalizeSources(cfg *Config) {
	valid := map[string]struct{}{
		"piratebay": {}, "pornrips": {}, "hentai": {}, "sukebei": {}, "stripchat": {},
	}
	// Tube sources: each registered source's Key() is valid, so adding a source
	// = registering it (no edit here).
	if defaultTubeRegistry != nil {
		for _, src := range defaultTubeRegistry.All() {
			valid[src.Key()] = struct{}{}
		}
	}
	out := make([]string, 0, len(cfg.Sources))
	seen := make(map[string]struct{})
	for _, s := range cfg.Sources {
		if _, ok := valid[s]; !ok {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		cfg.Sources = []string{"piratebay"}
		return
	}
	cfg.Sources = out
}

func filterStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func normalizeEnabledCatalogs(values []string) []string {
	if values == nil {
		return nil
	}
	out := filterStrings(values)
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeEnabledSorts(values []string, defaultOnEmpty bool) []string {
	valid := map[string]struct{}{"recent": {}, "top": {}}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := valid[v]; !ok {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 && defaultOnEmpty {
		return []string{"recent", "top"}
	}
	return out
}

func resolveCategorySlugs(rawUser map[string]json.RawMessage, field string, keyPresent bool, current []string) []string {
	known := categorySlugSet()
	if _, supplied := rawUser[field]; supplied {
		seen := make(map[string]struct{})
		out := make([]string, 0, len(current))
		for _, slug := range current {
			if slug == "" {
				continue
			}
			if _, ok := known[slug]; !ok {
				continue
			}
			if _, dup := seen[slug]; dup {
				continue
			}
			seen[slug] = struct{}{}
			out = append(out, slug)
		}
		return out
	}
	if keyPresent {
		return defaultCategorySlugs()
	}
	return []string{}
}

func categorySlugSet() map[string]struct{} {
	set := make(map[string]struct{}, len(jobs.AllCategories))
	for _, c := range jobs.AllCategories {
		set[c.Slug] = struct{}{}
	}
	return set
}

func defaultCategorySlugs() []string {
	out := make([]string, 0, 20)
	for _, c := range jobs.AllCategories {
		if c.Default {
			out = append(out, c.Slug)
		}
	}
	return out
}

func categoryNames(slugs []string) []string {
	bySlug := make(map[string]string, len(jobs.AllCategories))
	for _, c := range jobs.AllCategories {
		bySlug[c.Slug] = c.Name
	}
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		if name, ok := bySlug[slug]; ok {
			out = append(out, name)
		}
	}
	return out
}

// prTagOptions returns the pr_tag genre option list, reusing the TPDB/StashDB
// category taxonomy so the options line up with the tags the PornRips enrich sweep
// writes to pornrips_entries (from TPDB/Stash scene tags). Falls back through the
// configured category lists, then the full curated category set.
func prTagOptions(cfg Config) []string {
	if len(cfg.TpdbCategories) > 0 {
		return categoryNames(cfg.TpdbCategories)
	}
	if len(cfg.StashdbCategories) > 0 {
		return categoryNames(cfg.StashdbCategories)
	}
	ordered := jobs.OrderedCategories()
	slugs := make([]string, 0, len(ordered))
	for _, c := range ordered {
		slugs = append(slugs, c.Slug)
	}
	return categoryNames(slugs)
}

func trimNamePostfix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 30 {
		return s[:30]
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isSafeURL mirrors Node safeUrl.js deny-list checks for SSRF hardening.
func isSafeURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	if strings.Contains(raw, "@") {
		return false
	}
	rest := raw[strings.Index(raw, "://")+3:]
	host := rest
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, ":"); i >= 0 && strings.Count(host, ":") == 1 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	switch strings.ToLower(host) {
	case "localhost", "metadata.google.internal", "metadata.goog", "169.254.169.254.nip.io":
		return false
	}
	if strings.HasPrefix(host, "169-254-169-254.") {
		return false
	}
	if strings.Contains(host, ":") {
		return false
	}
	if isPrivateIPv4(host) {
		return false
	}
	return true
}

func isPrivateIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	nums := make([]int, 4)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return false
		}
		nums[i] = n
	}
	a, b := nums[0], nums[1]
	switch {
	case a == 0, a == 10, a == 127:
		return true
	case a == 169 && b == 254:
		return true
	case a == 172 && b >= 16 && b <= 31:
		return true
	case a == 192 && b == 168:
		return true
	case a == 192 && b == 0 && nums[2] == 0:
		return true
	case a == 192 && b == 0 && nums[2] == 2:
		return true
	case a == 198 && b == 51 && nums[2] == 100:
		return true
	case a == 203 && b == 0 && nums[2] == 113:
		return true
	case a >= 224 && a <= 239:
		return true
	case a >= 240:
		return true
	case a == 100 && b >= 64 && b <= 127:
		return true
	}
	return false
}
