// Package magnetio implements Stremio protocol handlers for the Magnetio addon.
// It mirrors the configuration and manifest logic from the Node Magnetio addon so
// that the Node edge can become a thin proxy for catalog/meta/manifest.
package magnetio

import (
	"strconv"
	"strings"
)

// Config is the parsed Magnetio addon configuration.
type Config struct {
	Providers                []string
	Sort                     string
	Limit                    int
	Qualities                []string
	Languages                []string
	SubtitleLanguages        []string
	PrewarmDebrid            bool
	PrewarmLimit             int
	ExcludeSizes             []string
	MaxSize                  int
	TMDBApiKey               string
	TMDBCatalogsEnabled      bool
	RPDBApiKey               string
	OMDBApiKey               string
	StreamingServices        []string
	StreamingCountry         string
	RealDebridApiKey         string
	PremiumizeApiKey         string
	DebridLinkApiKey         string
	EasyDebridApiKey         string
	OffcloudApiKey           string
	TorboxApiKey             string
	PutioApiKey              string
	RealDebridCatalogEnabled bool
	PremiumizeCatalogEnabled bool
	DebridLinkCatalogEnabled bool
	TorboxCatalogEnabled     bool
	PutioCatalogEnabled      bool
}

// Provider aliases kept for backwards compatibility with saved URLs.
var publicProviderAliases = map[string]string{
	"s8":  "nyaa",
	"s20": "knaben",
}

// coreProviders are always included.
var coreProviders = []string{"knaben"}

// ParseConfig parses a pipe-delimited Magnetio configuration string.
func ParseConfig(configString string) Config {
	if configString == "" {
		return withCoreProviders(defaultConfig())
	}

	cfg := defaultConfig()
	for _, part := range strings.Split(configString, "|") {
		eq := strings.Index(part, "=")
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(part[:eq]))
		value := strings.TrimSpace(part[eq+1:])
		if value == "" {
			continue
		}

		switch key {
		case "providers":
			cfg.Providers = normalizeProviders(strings.Split(strings.ToLower(value), ","))
		case "sort":
			cfg.Sort = strings.ToLower(value)
		case "limit":
			cfg.Limit = parseInt(value, cfg.Limit)
		case "qualities":
			cfg.Qualities = strings.Split(strings.ToLower(value), ",")
		case "languages":
			cfg.Languages = strings.Split(strings.ToLower(value), ",")
		case "subtitlelanguages":
			cfg.SubtitleLanguages = strings.Split(strings.ToLower(value), ",")
		case "prewarm":
			cfg.PrewarmDebrid = parseBool(value, cfg.PrewarmDebrid)
		case "prewarmlimit":
			cfg.PrewarmLimit = clampPrewarmLimit(parseInt(value, cfg.PrewarmLimit))
		case "excludesizes":
			cfg.ExcludeSizes = strings.Split(strings.ToUpper(value), ",")
		case "maxsize":
			cfg.MaxSize = parseInt(value, 0)
		case "rd":
			cfg.RealDebridApiKey = value
		case "pm":
			cfg.PremiumizeApiKey = value
		case "dl":
			cfg.DebridLinkApiKey = value
		case "ed":
			cfg.EasyDebridApiKey = value
		case "oc":
			cfg.OffcloudApiKey = value
		case "tb":
			cfg.TorboxApiKey = value
		case "pu":
			cfg.PutioApiKey = value
		case "rdcatalog":
			cfg.RealDebridCatalogEnabled = parseBool(value, true)
		case "pmcatalog":
			cfg.PremiumizeCatalogEnabled = parseBool(value, true)
		case "dlcatalog":
			cfg.DebridLinkCatalogEnabled = parseBool(value, true)
		case "tbcatalog":
			cfg.TorboxCatalogEnabled = parseBool(value, true)
		case "pucatalog":
			cfg.PutioCatalogEnabled = parseBool(value, true)
		case "tmdb":
			cfg.TMDBApiKey = value
		case "rpdb":
			cfg.RPDBApiKey = value
		case "omdb":
			cfg.OMDBApiKey = value
		case "streamingservices":
			cfg.StreamingServices = strings.Split(value, ",")
		case "streamingcountry":
			cfg.StreamingCountry = value
		}
	}

	return withCoreProviders(cfg)
}

func defaultConfig() Config {
	return Config{
		Providers:                []string{"knaben", "nyaa", "subsplease", "animetosho", "nekobt"},
		Sort:                     "qualityseeders",
		Limit:                    10,
		SubtitleLanguages:        []string{"en"},
		PrewarmDebrid:            true,
		PrewarmLimit:             3,
		StreamingCountry:         "us",
		RealDebridCatalogEnabled: false,
		PremiumizeCatalogEnabled: false,
		DebridLinkCatalogEnabled: false,
		TorboxCatalogEnabled:     false,
		PutioCatalogEnabled:      false,
	}
}

func withCoreProviders(cfg Config) Config {
	seen := make(map[string]struct{}, len(cfg.Providers))
	for _, p := range cfg.Providers {
		seen[p] = struct{}{}
	}
	for _, core := range coreProviders {
		if _, ok := seen[core]; !ok {
			cfg.Providers = append(cfg.Providers, core)
			seen[core] = struct{}{}
		}
	}
	return cfg
}

func normalizeProviders(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{})
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if alias, ok := publicProviderAliases[p]; ok {
			p = alias
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func parseInt(s string, fallback int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return fallback
}

func parseBool(s string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func clampPrewarmLimit(v int) int {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}
