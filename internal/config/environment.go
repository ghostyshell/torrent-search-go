package config

import (
	"fmt"
	"strings"
)

// EnvironmentValidationResult holds validation results
type EnvironmentValidationResult struct {
	IsValid  bool
	Errors   []string
	Warnings []string
}

// ValidateEnvironment validates required environment variables for readiness probes.
// MongoDB is required. Google OAuth credentials are optional - needed only for the
// full torrent-search UI auth flow, not for Stremio-addon backend mode.
func ValidateEnvironment(cfg *Config) *EnvironmentValidationResult {
	result := &EnvironmentValidationResult{
		IsValid:  true,
		Errors:   []string{},
		Warnings: []string{},
	}

	if cfg.Database.Mongo.URI == "" {
		result.Errors = append(result.Errors, "MONGODB_URI (or MONGO_URL) is required")
		result.IsValid = false
	}

	addonMode := cfg.APIKeys.AddonAPIToken != ""

	// Google Custom Search is optional; warn when absent.
	if cfg.Google.ServiceAccountJSON == "" {
		result.Warnings = append(result.Warnings, "GOOGLE_SERVICE_ACCOUNT_JSON not set - Google Custom Search disabled")
	} else if !isValidJSON(cfg.Google.ServiceAccountJSON) {
		result.Warnings = append(result.Warnings, "GOOGLE_SERVICE_ACCOUNT_JSON may be invalid JSON")
	}

	if cfg.Google.CustomSearchEngineID == "" {
		result.Warnings = append(result.Warnings, "GOOGLE_CUSTOM_SEARCH_ENGINE_ID not set - Google Custom Search disabled")
	}

	// OAuth is only required for the full authenticated UI in production, not addon mode.
	if cfg.IsProduction && !addonMode {
		if cfg.Google.OAuthClientID == "" {
			result.Warnings = append(result.Warnings, "GOOGLE_OAUTH_CLIENT_ID not set - OAuth login disabled")
		}
		if cfg.Google.OAuthClientSecret == "" {
			result.Warnings = append(result.Warnings, "GOOGLE_OAUTH_CLIENT_SECRET not set - OAuth login disabled")
		}
		if cfg.Google.CallbackURL == "" || !strings.HasPrefix(cfg.Google.CallbackURL, "http") {
			result.Warnings = append(result.Warnings, "GOOGLE_CALLBACK_URL not set - OAuth login disabled")
		}
	}

	if !cfg.Redis.Enabled {
		result.Warnings = append(result.Warnings, "REDIS_URL not set - catalog cache and addon jobs disabled")
	}

	if cfg.Railway.IsRailway && cfg.Server.Port == 0 {
		result.Warnings = append(result.Warnings, "PORT should be set by Railway automatically")
	}

	return result
}

// isValidJSON checks if a string is valid JSON
func isValidJSON(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	// Basic check - must start with { and end with }
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return false
	}
	// Try to find key-value pattern
	if !strings.Contains(s, ":") {
		return false
	}
	return true
}

// FormatValidationErrors formats validation errors for logging
func FormatValidationErrors(result *EnvironmentValidationResult) string {
	var sb strings.Builder

	if !result.IsValid {
		sb.WriteString("Environment validation failed:\n")
		for _, err := range result.Errors {
			sb.WriteString(fmt.Sprintf("  - %s\n", err))
		}
	}

	if len(result.Warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, warn := range result.Warnings {
			sb.WriteString(fmt.Sprintf("  - %s\n", warn))
		}
	}

	return sb.String()
}
