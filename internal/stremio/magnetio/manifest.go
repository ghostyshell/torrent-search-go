package magnetio

import (
	"fmt"
	"strings"
)

const (
	addonID      = "com.magnetio.addon"
	addonVersion = "1.1.5"
	addonName    = "Magnetio"
)

// StremioAddonsConfig is the ownership signature from stremio-addons.net.
var StremioAddonsConfig = map[string]string{
	"issuer":    "https://stremio-addons.net",
	"signature": "eyJhbGciOiJkaXIiLCJlbmMiOiJBMTI4Q0JDLUhTMjU2In0..HyLU28BPtTd06szaQlmclQ._4Km4R8rkgZfnwV5BCt1h5h9AzZ8ZSszesrBMq0Wih1O1jnC49ny_zUtFuSbfUj8WdVoqX0wvCHKqEpi5mNYGtYRD_IM9Qr8Qz3wFb2Qsa7Hw53ocomqPJQuH5_Hmonn.-g1fCVlhSWdoY3Nyz3zdVg",
}

// MochOption describes a debrid service available in Magnetio.
type MochOption struct {
	ID             string
	Name           string
	ShortName      string
	ConfigKey      string
	CatalogFlagKey string
	HasCatalog     bool
}

// MochOptions is the canonical list of debrid integrations.
var MochOptions = []MochOption{
	{ID: "rd", Name: "Real-Debrid", ShortName: "RD", ConfigKey: "realDebridApiKey", CatalogFlagKey: "realDebridCatalogEnabled", HasCatalog: true},
	{ID: "pm", Name: "Premiumize", ShortName: "PM", ConfigKey: "premiumizeApiKey", CatalogFlagKey: "premiumizeCatalogEnabled", HasCatalog: true},
	{ID: "dl", Name: "Debrid-Link", ShortName: "DL", ConfigKey: "debridLinkApiKey", CatalogFlagKey: "debridLinkCatalogEnabled", HasCatalog: true},
	{ID: "ed", Name: "EasyDebrid", ShortName: "ED", ConfigKey: "easyDebridApiKey", CatalogFlagKey: "", HasCatalog: false},
	{ID: "oc", Name: "Offcloud", ShortName: "OC", ConfigKey: "offcloudApiKey", CatalogFlagKey: "", HasCatalog: false},
	{ID: "tb", Name: "TorBox", ShortName: "TB", ConfigKey: "torboxApiKey", CatalogFlagKey: "torboxCatalogEnabled", HasCatalog: true},
	{ID: "pu", Name: "Put.io", ShortName: "PU", ConfigKey: "putioApiKey", CatalogFlagKey: "putioCatalogEnabled", HasCatalog: true},
}

// BuildManifest returns the full Magnetio Stremio manifest.
func BuildManifest(cfg Config, baseURL string) map[string]interface{} {
	enabledMochs := getEnabledMochs(cfg)
	catalogs := buildCatalogs(cfg, enabledMochs)

	manifest := map[string]interface{}{
		"id":          addonID,
		"version":     addonVersion,
		"name":        getName(cfg, enabledMochs),
		"description": getDescription(cfg, enabledMochs),
		"logo":        assetURL(baseURL, "magnetio-logo.svg"),
		"background":  assetURL(baseURL, "magnetio-wordmark.svg"),
		"types":       []string{"movie", "series", "anime"},
		"resources": []map[string]interface{}{
			{"name": "stream", "types": []string{"movie", "series", "anime"}, "idPrefixes": []string{"tt", "kitsu"}},
			{"name": "subtitles", "types": []string{"movie", "series", "anime"}, "idPrefixes": []string{"tt", "kitsu"}},
			{"name": "catalog", "types": []string{"movie", "series"}, "idPrefixes": []string{"rd", "pm", "dl", "ed", "oc", "tb", "pu", "magnetio_similar", "tmdb"}},
			{"name": "meta", "types": []string{"movie", "series"}, "idPrefixes": []string{"rd", "pm", "dl", "ed", "oc", "tb", "pu", "tmdb"}},
		},
		"catalogs": catalogs,
		"behaviorHints": map[string]interface{}{
			"configurable": true,
			"p2p":          true,
		},
		"stremioAddonsConfig": StremioAddonsConfig,
	}
	if baseURL != "" {
		if hints, ok := manifest["behaviorHints"].(map[string]interface{}); ok {
			hints["configureUrl"] = strings.TrimSuffix(baseURL, "/") + "/configure"
		}
	}
	return manifest
}

// DummyManifest returns the minimal pre-configuration manifest.
func DummyManifest(baseURL string) map[string]interface{} {
	manifest := map[string]interface{}{
		"id":          addonID,
		"version":     addonVersion,
		"name":        addonName,
		"description": "Magnetio - configure sources, subtitles and debrid services at /configure",
		"logo":        assetURL(baseURL, "magnetio-logo.svg"),
		"background":  assetURL(baseURL, "magnetio-wordmark.svg"),
		"types":       []string{"movie", "series", "anime"},
		"resources": []map[string]interface{}{
			{"name": "stream", "types": []string{"movie", "series", "anime"}, "idPrefixes": []string{"tt", "kitsu"}},
			{"name": "subtitles", "types": []string{"movie", "series", "anime"}, "idPrefixes": []string{"tt", "kitsu"}},
			{"name": "catalog", "types": []string{"movie", "series"}, "idPrefixes": []string{"rd", "pm", "dl", "ed", "oc", "tb", "pu", "magnetio_similar", "tmdb"}},
			{"name": "meta", "types": []string{"movie", "series"}, "idPrefixes": []string{"rd", "pm", "dl", "ed", "oc", "tb", "pu", "tmdb"}},
		},
		"catalogs": []map[string]interface{}{},
		"behaviorHints": map[string]interface{}{
			"configurable": true,
			"p2p":          true,
		},
		"stremioAddonsConfig": StremioAddonsConfig,
	}
	if baseURL != "" {
		if hints, ok := manifest["behaviorHints"].(map[string]interface{}); ok {
			hints["configureUrl"] = strings.TrimSuffix(baseURL, "/") + "/configure"
		}
	}
	return manifest
}

func assetURL(baseURL, filename string) string {
	if baseURL == "" {
		return "/static/" + filename
	}
	return strings.TrimSuffix(baseURL, "/") + "/static/" + filename
}

func getName(cfg Config, enabledMochs []MochOption) string {
	if len(enabledMochs) == 0 {
		return addonName
	}
	parts := make([]string, len(enabledMochs))
	for i, m := range enabledMochs {
		parts[i] = "+" + m.ShortName
	}
	return addonName + " " + strings.Join(parts, "")
}

func getDescription(cfg Config, enabledMochs []MochOption) string {
	base := "Aggregates streams from configurable sources. Includes subtitle support when available."
	if len(enabledMochs) > 0 {
		names := make([]string, len(enabledMochs))
		for i, m := range enabledMochs {
			names[i] = m.Name
		}
		base += "\nDebrid services: " + strings.Join(names, ", ") + "."
	}
	base += "\n\nConfigure at your Stremio settings page."
	return base
}

func buildCatalogs(cfg Config, enabledMochs []MochOption) []map[string]interface{} {
	var catalogs []map[string]interface{}

	// Debrid library catalogs
	for _, m := range enabledMochs {
		if !m.HasCatalog {
			continue
		}
		catalogEnabled := true
		if m.CatalogFlagKey != "" {
			catalogEnabled = getConfigBool(cfg, m.CatalogFlagKey)
		}
		if !catalogEnabled {
			continue
		}
		catalogs = append(catalogs,
			map[string]interface{}{"id": m.ID + "_movie", "type": "movie", "name": m.Name + " - Movies"},
			map[string]interface{}{"id": m.ID + "_series", "type": "series", "name": m.Name + " - Series"},
		)
	}

	// TMDB streaming service catalogs
	if cfg.TMDBApiKey != "" && len(cfg.StreamingServices) > 0 {
		for _, svc := range cfg.StreamingServices {
			for _, typ := range []string{"movie", "series"} {
				catalogs = append(catalogs, map[string]interface{}{
					"id":   fmt.Sprintf("tmdb_%s_%s_%s", svc, typ, cfg.StreamingCountry),
					"type": typ,
					"name": fmt.Sprintf("%s - %s (%s)", svcName(svc), typeName(typ), cfg.StreamingCountry),
				})
			}
		}
	}

	// Similar content recommendations
	if cfg.TMDBApiKey != "" {
		catalogs = append(catalogs,
			map[string]interface{}{
				"id":    "magnetio_similar_movie",
				"type":  "movie",
				"name":  "Magnetio - Similar",
				"extra": []map[string]interface{}{{"name": "genre", "isRequired": true}},
			},
			map[string]interface{}{
				"id":    "magnetio_similar_series",
				"type":  "series",
				"name":  "Magnetio - Similar",
				"extra": []map[string]interface{}{{"name": "genre", "isRequired": true}},
			},
		)
	}

	return catalogs
}

func getEnabledMochs(cfg Config) []MochOption {
	var out []MochOption
	for _, m := range MochOptions {
		if getConfigString(cfg, m.ConfigKey) != "" {
			out = append(out, m)
		}
	}
	return out
}

func getConfigString(cfg Config, key string) string {
	switch key {
	case "realDebridApiKey":
		return cfg.RealDebridApiKey
	case "premiumizeApiKey":
		return cfg.PremiumizeApiKey
	case "debridLinkApiKey":
		return cfg.DebridLinkApiKey
	case "easyDebridApiKey":
		return cfg.EasyDebridApiKey
	case "offcloudApiKey":
		return cfg.OffcloudApiKey
	case "torboxApiKey":
		return cfg.TorboxApiKey
	case "putioApiKey":
		return cfg.PutioApiKey
	}
	return ""
}

func getConfigBool(cfg Config, key string) bool {
	switch key {
	case "realDebridCatalogEnabled":
		return cfg.RealDebridCatalogEnabled
	case "premiumizeCatalogEnabled":
		return cfg.PremiumizeCatalogEnabled
	case "debridLinkCatalogEnabled":
		return cfg.DebridLinkCatalogEnabled
	case "torboxCatalogEnabled":
		return cfg.TorboxCatalogEnabled
	case "putioCatalogEnabled":
		return cfg.PutioCatalogEnabled
	}
	return true
}

var streamingServiceNames = map[string]string{
	"netflix":     "Netflix",
	"prime":       "Prime Video",
	"disney":      "Disney+",
	"hulu":        "Hulu",
	"max":         "Max",
	"apple":       "Apple TV+",
	"peacock":     "Peacock",
	"paramount":   "Paramount+",
	"tubi":        "Tubi",
	"plutotv":     "Pluto TV",
	"crackle":     "Crackle",
	"crunchyroll": "Crunchyroll",
}

func svcName(s string) string {
	if name, ok := streamingServiceNames[strings.ToLower(s)]; ok {
		return name
	}
	// Simple title-case fallback.
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func typeName(t string) string {
	if t == "series" {
		return "Series"
	}
	return "Movies"
}
