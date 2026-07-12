package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	Environment    string               `json:"environment"`
	IsDevelopment  bool                 `json:"isDevelopment"`
	IsProduction   bool                 `json:"isProduction"`
	Server         ServerConfig         `json:"server"`
	CORS           CORSConfig           `json:"cors"`
	Database       DatabaseConfig       `json:"database"`
	Google         GoogleConfig         `json:"google"`
	APIKeys        APIKeysConfig        `json:"apiKeys"`
	Logging        LoggingConfig        `json:"logging"`
	HealthCheck    HealthCheckConfig    `json:"healthCheck"`
	Security       SecurityConfig       `json:"security"`
	Cache          CacheConfig          `json:"cache"`
	BackgroundJobs BackgroundJobsConfig `json:"backgroundJobs"`
	S3             S3Config             `json:"s3"`
	Redis          RedisConfig          `json:"redis"`
	Metadata       MetadataConfig       `json:"metadata"`
	FrontendURL    string               `json:"frontendUrl"`
	Railway        RailwayConfig        `json:"railway"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	Origins        []string `json:"origins"`
	Credentials    bool     `json:"credentials"`
	Methods        []string `json:"methods"`
	AllowedHeaders []string `json:"allowedHeaders"`
	ExposedHeaders []string `json:"exposedHeaders"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Mongo MongoConfig `json:"mongo"`
}

// MongoConfig holds MongoDB configuration
type MongoConfig struct {
	URI    string `json:"uri"`
	DBName string `json:"dbName"`
}

// GoogleConfig holds Google API configuration
type GoogleConfig struct {
	ServiceAccountJSON   string `json:"serviceAccountJson"`
	CustomSearchEngineID string `json:"customSearchEngineId"`
	OAuthClientID        string `json:"oauthClientId"`
	OAuthClientSecret    string `json:"oauthClientSecret"`
	CallbackURL          string `json:"callbackUrl"`
}

// APIKeysConfig holds external API keys
type APIKeysConfig struct {
	RealDebrid    string `json:"realDebrid"`
	AddonAPIToken string `json:"addonApiToken"`
}

// MetadataConfig holds TPDB/StashDB API settings for category warming.
type MetadataConfig struct {
	TPDBAPIKey    string `json:"tpdbApiKey"`
	StashDBAPIKey string `json:"stashdbApiKey"`
	TPDBAPIURL    string `json:"tpdbApiUrl"`
	StashDBAPIURL string `json:"stashdbApiUrl"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level                                     string `json:"level"`
	EnableConsole                             bool   `json:"enableConsole"`
	EnableFile                                bool   `json:"enableFile"`
	LogDir                                    string `json:"logDir"`
	BackgroundJobsLogVersion                  string `json:"backgroundJobsLogVersion"`
	BackgroundJobLogRetentionDays             int    `json:"backgroundJobLogRetentionDays"`
	BackgroundJobLogCompressAfterMs           int64  `json:"backgroundJobLogCompressAfterMs"`
	BackgroundJobLogMaintenanceIntervalMs     int64  `json:"backgroundJobLogMaintenanceIntervalMs"`
	BackgroundJobLogMaintenanceInitialDelayMs int64  `json:"backgroundJobLogMaintenanceInitialDelayMs"`
}

// HealthCheckConfig holds health check configuration
type HealthCheckConfig struct {
	Timeout time.Duration `json:"timeout"`
	Retries int           `json:"retries"`
}

// SecurityConfig holds security settings
type SecurityConfig struct {
	TrustProxy            bool               `json:"trustProxy"`
	RateLimiting          RateLimitingConfig `json:"rateLimiting"`
	EmailAllowlist        []string           `json:"emailAllowlist"`
	MonitoringIPAllowlist []string           `json:"monitoringIpAllowlist"`
	DashboardPassword     string             `json:"dashboardPassword,omitempty"`
}

// RateLimitingConfig holds rate limiting settings
type RateLimitingConfig struct {
	Enabled  bool          `json:"enabled"`
	WindowMs time.Duration `json:"windowMs"`
	Max      int           `json:"max"`
}

// CacheConfig holds cache settings
type CacheConfig struct {
	StreamUrlTTLSeconds    int `json:"streamUrlTtlSeconds"`
	StreamsCacheTTLSeconds int `json:"streamsCacheTtlSeconds"`
}

// DescriptionImageCacheJobConfig holds page counts for the description/image cache job.
type DescriptionImageCacheJobConfig struct {
	PagesBrowseHome int `json:"pagesBrowseHome"`
	PagesHomeQuery  int `json:"pagesHomeQuery"`
	PagesTrans      int `json:"pagesTrans"`
	PagesPerStudio  int `json:"pagesPerStudio"`
}

// SearchResultsCacheJobConfig holds page counts for the filter stream cache job.
type SearchResultsCacheJobConfig struct {
	PagesBrowseHome int `json:"pagesBrowseHome"`
	PagesTrans      int `json:"pagesTrans"`
	PagesPerStudio  int `json:"pagesPerStudio"`
}

// SearchQueryCacheJobConfig holds retention/TTL/sleep settings for the search-query cache job.
type SearchQueryCacheJobConfig struct {
	RetentionDays       int           `json:"retentionDays"`
	RedisTTLSeconds     int           `json:"redisTtlSeconds"`
	SleepBetweenCovers  time.Duration `json:"sleepBetweenCoversMs"`
	SleepBetweenQueries time.Duration `json:"sleepBetweenQueriesMs"`
	SleepBetweenPages   time.Duration `json:"sleepBetweenPagesMs"`
}

// RedisCatalogCacheJobConfig holds interval bounds for the Redis catalog cache job.
type RedisCatalogCacheJobConfig struct {
	IntervalMin time.Duration `json:"intervalMin"`
	IntervalMax time.Duration `json:"intervalMax"`
}

// BackgroundJobsConfig groups tunables for all background warmer jobs.
type BackgroundJobsConfig struct {
	StreamUrlRefresh           JobScheduleConfig              `json:"streamUrlRefresh"`
	DescriptionImageCache      JobScheduleConfig              `json:"descriptionImageCache"`
	DescriptionImageCachePages DescriptionImageCacheJobConfig `json:"descriptionImageCachePages"`
	SearchResultsCache         JobScheduleConfig              `json:"searchResultsCache"`
	SearchResultsCachePages    SearchResultsCacheJobConfig    `json:"searchResultsCachePages"`
	RedisCatalogCache          RedisCatalogCacheJobConfig     `json:"redisCatalogCache"`
	SearchQueryCache           JobScheduleConfig              `json:"searchQueryCache"`
	SearchQueryCacheJobConfig  SearchQueryCacheJobConfig      `json:"searchQueryCacheJobConfig"`
	CategoryWarmer             JobScheduleConfig              `json:"categoryWarmer"`
	MetaEnricher               JobScheduleConfig              `json:"metaEnricher"`
	AtishmkvCatalogSync        JobScheduleConfig              `json:"atishmkvCatalogSync"`
	AtishmkvDirectLinkRefresh  JobScheduleConfig              `json:"atishmkvDirectLinkRefresh"`
	PornripsSync               JobScheduleConfig              `json:"pornripsSync"`
	HentaiSync                 JobScheduleConfig              `json:"hentaiSync"`
	EnrichedScenesSync         JobScheduleConfig              `json:"enrichedScenesSync"`
	PerverzijaSync             JobScheduleConfig              `json:"perverzijaSync"`
	FreepornvideosSync         JobScheduleConfig              `json:"freepornvideosSync"`
	YespornSync                JobScheduleConfig              `json:"yespornSync"`
	PorneecSync                JobScheduleConfig              `json:"porneecSync"`
}

// JobScheduleConfig holds interval + initial delay for a periodic job.
type JobScheduleConfig struct {
	Interval     time.Duration `json:"interval"`
	InitialDelay time.Duration `json:"initialDelay"`
}

// RailwayConfig holds Railway-specific configuration
type RailwayConfig struct {
	IsRailway    bool   `json:"isRailway"`
	Environment  string `json:"environment"`
	StaticURL    string `json:"staticUrl"`
	PublicDomain string `json:"publicDomain"`
}

// S3Config holds S3-compatible object storage configuration.
type S3Config struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	KeyPrefix       string `json:"keyPrefix"`
	TempExpireDays  int    `json:"tempExpireDays"`
	PresignDays     int    `json:"presignDays"`
	Enabled         bool   `json:"enabled"`
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	URL      string `json:"url"`
	Password string `json:"password"`
	Enabled  bool   `json:"enabled"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	isProduction := os.Getenv("NODE_ENV") == "production" || os.Getenv("ENVIRONMENT") == "production"
	isDevelopment := !isProduction
	envName := getEnv("ENVIRONMENT", getEnv("NODE_ENV", "development"))

	cfg := &Config{
		Environment:   envName,
		IsDevelopment: isDevelopment,
		IsProduction:  isProduction,
		Server: ServerConfig{
			Port: getEnvAsInt("PORT", 3001),
			Host: getEnv("HOST", "0.0.0.0"),
		},
		CORS: CORSConfig{
			Origins:     getCorsOrigins(isDevelopment),
			Credentials: true,
			Methods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{
				"Origin", "X-Requested-With", "Content-Type", "Accept",
				"Authorization", "Range",
			},
			ExposedHeaders: []string{
				"Content-Range", "Accept-Ranges", "Content-Length", "Content-Type",
			},
		},
		Database: DatabaseConfig{
			Mongo: MongoConfig{
				URI:    buildMongoURI(),
				DBName: getEnv("MONGODB_DB", "torrent_search"),
			},
		},
		Google: GoogleConfig{
			ServiceAccountJSON:   os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON"),
			CustomSearchEngineID: os.Getenv("GOOGLE_CUSTOM_SEARCH_ENGINE_ID"),
			OAuthClientID:        os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
			OAuthClientSecret:    os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
			CallbackURL:          getEnv("GOOGLE_CALLBACK_URL", "/api/auth/google/callback"),
		},
		APIKeys: APIKeysConfig{
			RealDebrid:    os.Getenv("REAL_DEBRID_API_KEY"),
			AddonAPIToken: os.Getenv("ADDON_API_TOKEN"),
		},
		Logging: LoggingConfig{
			Level:                                 getEnv("LOG_LEVEL", getDefaultLogLevel(isProduction)),
			EnableConsole:                         getEnv("LOGGING_ENABLE_CONSOLE", "true") != "false",
			EnableFile:                            getEnv("LOGGING_ENABLE_FILE", "true") != "false",
			LogDir:                                getEnv("LOG_DIR", getDefaultLogDir()),
			BackgroundJobsLogVersion:              getEnv("BACKGROUND_JOBS_LOG_VERSION", "v1"),
			BackgroundJobLogRetentionDays:         getMaxInt(1, getEnvAsInt("BACKGROUND_JOB_LOG_RETENTION_DAYS", 30)),
			BackgroundJobLogCompressAfterMs:       getMaxInt64(60000, getEnvAsInt64("BACKGROUND_JOB_LOG_COMPRESS_AFTER_MS", 6*60*60*1000)),
			BackgroundJobLogMaintenanceIntervalMs: getMaxInt64(60*60*1000, getEnvAsInt64("BACKGROUND_JOB_LOG_MAINTENANCE_INTERVAL_MS", 24*60*60*1000)),
			BackgroundJobLogMaintenanceInitialDelayMs: getMaxInt64(5*60*1000, getEnvAsInt64("BACKGROUND_JOB_LOG_MAINTENANCE_INITIAL_DELAY_MS", 15*60*1000)),
		},
		HealthCheck: HealthCheckConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
		},
		Security: SecurityConfig{
			TrustProxy: isProduction,
			RateLimiting: RateLimitingConfig{
				Enabled:  isProduction,
				WindowMs: 15 * time.Minute,
				Max:      getMaxInt(100, getEnvAsInt("RATE_LIMIT_MAX", 1000)),
			},
			EmailAllowlist:        getEmailAllowlist(),
			MonitoringIPAllowlist: getMonitoringIPAllowlist(),
			DashboardPassword:     os.Getenv("DASHBOARD_PASSWORD"),
		},
		Cache: CacheConfig{
			// ponytail: 24h > the searchResultsCache job cycle (~12h key + 8h ticker = ~16h),
			// so prewarmed stream URLs stay fresh across cycles instead of expiring before
			// the next run re-refreshes them (12h TTL lost ~25% of coverage once the Jun 27
			// +30 studios slowed the job past the TTL). The play path serves this cached URL
			// directly (no per-user re-resolve on a cache hit), so the TTL governs freshness
			// for play too; RD /download links are stable for premium accounts, so 24h is
			// safe for both the existence-check badge and the play path.
			StreamUrlTTLSeconds:    getMaxInt(60, getEnvAsInt("STREAM_URL_TTL_SECONDS", 24*60*60)),
			StreamsCacheTTLSeconds: getMaxInt(0, getEnvAsInt("STREAMS_CACHE_TTL_SECONDS", 3600)),
		},
		BackgroundJobs: BackgroundJobsConfig{
			StreamUrlRefresh: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("STREAM_URL_REFRESH_INTERVAL_MS", 24*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("STREAM_URL_REFRESH_INITIAL_DELAY_MS", 5*60*1000))) * time.Millisecond,
			},
			DescriptionImageCache: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("DESC_IMAGE_CACHE_INTERVAL_MS", 8*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("DESC_IMAGE_CACHE_INITIAL_DELAY_MS", 10*60*1000))) * time.Millisecond,
			},
			SearchResultsCache: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("SEARCH_RESULTS_CACHE_INTERVAL_MS", 8*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("SEARCH_RESULTS_CACHE_INITIAL_DELAY_MS", 10*60*1000))) * time.Millisecond,
			},
			RedisCatalogCache: RedisCatalogCacheJobConfig{
				IntervalMin: time.Duration(getMaxInt64(5*60*1000, getEnvAsInt64("REDIS_CATALOG_CACHE_INTERVAL_MIN_MS", 25*60*1000))) * time.Millisecond,
				IntervalMax: time.Duration(getMaxInt64(5*60*1000, getEnvAsInt64("REDIS_CATALOG_CACHE_INTERVAL_MAX_MS", 35*60*1000))) * time.Millisecond,
			},
			SearchQueryCache: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("SEARCH_QUERY_CACHE_INTERVAL_MS", 3*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("SEARCH_QUERY_CACHE_INITIAL_DELAY_MS", 10*60*1000))) * time.Millisecond,
			},
			SearchQueryCacheJobConfig: SearchQueryCacheJobConfig{
				RetentionDays:       getMaxInt(1, getEnvAsInt("SEARCH_QUERY_CACHE_RETENTION_DAYS", 1)),
				RedisTTLSeconds:     getMaxInt(60, getEnvAsInt("SEARCH_QUERY_CACHE_REDIS_TTL_SECONDS", 1*60*60)),
				SleepBetweenCovers:  time.Duration(getMaxInt64(0, getEnvAsInt64("SEARCH_QUERY_CACHE_SLEEP_BETWEEN_COVERS_MS", 300))) * time.Millisecond,
				SleepBetweenQueries: time.Duration(getMaxInt64(0, getEnvAsInt64("SEARCH_QUERY_CACHE_SLEEP_BETWEEN_QUERIES_MS", 1500))) * time.Millisecond,
				SleepBetweenPages:   time.Duration(getMaxInt64(0, getEnvAsInt64("SEARCH_QUERY_CACHE_SLEEP_BETWEEN_PAGES_MS", 500))) * time.Millisecond,
			},
			DescriptionImageCachePages: DescriptionImageCacheJobConfig{
				PagesBrowseHome: getMaxInt(1, getEnvAsInt("DESC_IMAGE_CACHE_PAGES_BROWSE_HOME", 3)),
				PagesHomeQuery:  getMaxInt(1, getEnvAsInt("DESC_IMAGE_CACHE_PAGES_HOME_QUERY", 2)),
				PagesTrans:      getMaxInt(0, getEnvAsInt("DESC_IMAGE_CACHE_PAGES_TRANS", 1)),
				PagesPerStudio:  getMaxInt(1, getEnvAsInt("DESC_IMAGE_CACHE_PAGES_PER_STUDIO", 2)),
			},
			SearchResultsCachePages: SearchResultsCacheJobConfig{
				PagesBrowseHome: getMaxInt(1, getEnvAsInt("SEARCH_RESULTS_CACHE_PAGES_BROWSE_HOME", 3)),
				PagesTrans:      getMaxInt(0, getEnvAsInt("SEARCH_RESULTS_CACHE_PAGES_TRANS", 1)),
				PagesPerStudio:  getMaxInt(1, getEnvAsInt("SEARCH_RESULTS_CACHE_PAGES_PER_STUDIO", 2)),
			},
			CategoryWarmer: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("CATEGORY_WARMER_INTERVAL_MS", 3*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("CATEGORY_WARMER_INITIAL_MS", 5*60*1000))) * time.Millisecond,
			},
			MetaEnricher: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(10*1000, getEnvAsInt64("META_ENRICHER_INTERVAL_MS", 60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("META_ENRICHER_INITIAL_MS", 5*1000))) * time.Millisecond,
			},
			AtishmkvCatalogSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("ATISHMKV_SYNC_INTERVAL_MS", 24*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("ATISHMKV_SYNC_INITIAL_DELAY_MS", 5*60*1000))) * time.Millisecond,
			},
			AtishmkvDirectLinkRefresh: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*60*1000, getEnvAsInt64("ATISHMKV_REFRESH_INTERVAL_MS", 4*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("ATISHMKV_REFRESH_INITIAL_DELAY_MS", 1*60*1000))) * time.Millisecond,
			},
			PornripsSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("PORNRIPS_SYNC_INTERVAL_MS", 10*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("PORNRIPS_SYNC_INITIAL_MS", 2*60*1000))) * time.Millisecond,
			},
			HentaiSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("HENTAI_SYNC_INTERVAL_MS", 6*60*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("HENTAI_SYNC_INITIAL_MS", 3*60*1000))) * time.Millisecond,
			},
			EnrichedScenesSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("ENRICHED_SCENES_SYNC_INTERVAL_MS", 10*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("ENRICHED_SCENES_SYNC_INITIAL_MS", 2*60*1000))) * time.Millisecond,
			},
			PerverzijaSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("PERVERZIJA_SYNC_INTERVAL_MS", 10*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("PERVERZIJA_SYNC_INITIAL_MS", 2*60*1000))) * time.Millisecond,
			},
			FreepornvideosSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("FREEPORNVIDEOS_SYNC_INTERVAL_MS", 10*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("FREEPORNVIDEOS_SYNC_INITIAL_MS", 2*60*1000))) * time.Millisecond,
			},
			YespornSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("YESPORN_SYNC_INTERVAL_MS", 10*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("YESPORN_SYNC_INITIAL_MS", 2*60*1000))) * time.Millisecond,
			},
			PorneecSync: JobScheduleConfig{
				Interval:     time.Duration(getMaxInt64(60*1000, getEnvAsInt64("PORNEEC_SYNC_INTERVAL_MS", 10*60*1000))) * time.Millisecond,
				InitialDelay: time.Duration(getMaxInt64(0, getEnvAsInt64("PORNEEC_SYNC_INITIAL_MS", 2*60*1000))) * time.Millisecond,
			},
		},
		S3: S3Config{
			Endpoint:        os.Getenv("S3_ENDPOINT"),
			Region:          getEnv("S3_REGION", "auto"),
			Bucket:          os.Getenv("S3_BUCKET"),
			AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
			KeyPrefix:       getEnv("S3_KEY_PREFIX", "covers/"),
			TempExpireDays:  getMaxInt(1, getEnvAsInt("S3_TEMP_EXPIRE_DAYS", 7)),
			PresignDays:     getMaxInt(1, getEnvAsInt("S3_PRESIGN_DAYS", 7)),
			Enabled:         os.Getenv("S3_ENDPOINT") != "" && os.Getenv("S3_BUCKET") != "" && os.Getenv("S3_ACCESS_KEY_ID") != "" && os.Getenv("S3_SECRET_ACCESS_KEY") != "",
		},
		Redis: RedisConfig{
			URL:      os.Getenv("REDIS_URL"),
			Password: os.Getenv("REDIS_PASSWORD"),
			Enabled:  os.Getenv("REDIS_URL") != "",
		},
		Metadata: MetadataConfig{
			TPDBAPIKey:    os.Getenv("TPDB_API_KEY"),
			StashDBAPIKey: os.Getenv("STASHDB_API_KEY"),
			TPDBAPIURL:    strings.TrimSuffix(getEnv("TPDB_API_URL", "https://api.theporndb.net"), "/"),
			StashDBAPIURL: strings.TrimSuffix(getEnv("STASHDB_API_URL", "https://stashdb.org"), "/"),
		},
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:3000"),
		Railway: RailwayConfig{
			IsRailway:    os.Getenv("RAILWAY_ENVIRONMENT") != "",
			Environment:  os.Getenv("RAILWAY_ENVIRONMENT"),
			StaticURL:    os.Getenv("RAILWAY_STATIC_URL"),
			PublicDomain: os.Getenv("RAILWAY_PUBLIC_DOMAIN"),
		},
	}

	// Try to extract OAuth credentials from service account JSON if not explicitly set
	if cfg.Google.OAuthClientID == "" && cfg.Google.ServiceAccountJSON != "" {
		extractOAuthCredentials(cfg)
	}

	return cfg, nil
}

// extractOAuthCredentials tries to parse OAuth credentials from service account JSON
func extractOAuthCredentials(cfg *Config) {
	var serviceAccount map[string]interface{}
	if err := json.Unmarshal([]byte(cfg.Google.ServiceAccountJSON), &serviceAccount); err != nil {
		return
	}

	// Try to extract OAuth credentials
	if clientID, ok := serviceAccount["oauth_client_id"].(string); ok {
		cfg.Google.OAuthClientID = clientID
	} else if clientID, ok := serviceAccount["client_id"].(string); ok {
		cfg.Google.OAuthClientID = clientID
	}

	if clientSecret, ok := serviceAccount["oauth_client_secret"].(string); ok {
		cfg.Google.OAuthClientSecret = clientSecret
	} else if clientSecret, ok := serviceAccount["client_secret"].(string); ok {
		cfg.Google.OAuthClientSecret = clientSecret
	}
}

func getCorsOrigins(isDevelopment bool) []string {
	if isDevelopment {
		origins := []string{
			"http://localhost:3000",
			"http://localhost:3001",
			"http://127.0.0.1:3000",
			"http://127.0.0.1:3001",
		}
		if frontendURL := os.Getenv("FRONTEND_URL"); frontendURL != "" {
			origins = append(origins, frontendURL)
		}
		return origins
	}

	var origins []string
	if frontendURL := os.Getenv("FRONTEND_URL"); frontendURL != "" {
		origins = append(origins, frontendURL)
	}

	if additionalOrigins := os.Getenv("ADDITIONAL_CORS_ORIGINS"); additionalOrigins != "" {
		for _, origin := range strings.Split(additionalOrigins, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				origins = append(origins, origin)
			}
		}
	}

	if len(origins) == 0 {
		return []string{"*"}
	}
	return origins
}

func getDefaultLogLevel(isProduction bool) string {
	if isProduction {
		return "info"
	}
	return "debug"
}

func getDefaultLogDir() string {
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs")
	}
	return "logs"
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getMaxInt(min, value int) int {
	if value < min {
		return min
	}
	return value
}

func getMaxInt64(min, value int64) int64 {
	if value < min {
		return min
	}
	return value
}

// getEmailAllowlist parses the ALLOWED_EMAILS environment variable
func getEmailAllowlist() []string {
	allowlist := os.Getenv("ALLOWED_EMAILS")
	if allowlist == "" {
		return []string{}
	}

	emails := strings.Split(allowlist, ",")
	result := make([]string, 0, len(emails))
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email != "" {
			result = append(result, strings.ToLower(email))
		}
	}
	return result
}

// getMonitoringIPAllowlist parses the MONITORING_IP_ALLOWLIST environment variable
func getMonitoringIPAllowlist() []string {
	allowlist := os.Getenv("MONITORING_IP_ALLOWLIST")
	if allowlist == "" {
		return []string{}
	}

	ips := strings.Split(allowlist, ",")
	result := make([]string, 0, len(ips))
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			result = append(result, ip)
		}
	}
	return result
}

// buildMongoURI constructs MongoDB URI from env (matches Node buildMongoUri).
func buildMongoURI() string {
	base := os.Getenv("MONGODB_URI")
	if base == "" {
		base = os.Getenv("MONGO_URL")
	}
	if base == "" {
		return ""
	}
	user := os.Getenv("MONGO_USERNAME")
	if user == "" {
		user = os.Getenv("MONGO_USER")
	}
	pass := os.Getenv("MONGO_PASSWORD")
	if pass == "" {
		pass = os.Getenv("MONGO_PASS")
	}
	if user == "" || pass == "" {
		return base
	}
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "mongodb://") && !strings.HasPrefix(lower, "mongodb+srv://") {
		return base
	}
	if strings.Contains(base, "@") {
		return base
	}
	schemeEnd := strings.Index(base, "://")
	if schemeEnd < 0 {
		return base
	}
	scheme := base[:schemeEnd+3]
	rest := base[schemeEnd+3:]
	return scheme + url.QueryEscape(user) + ":" + url.QueryEscape(pass) + "@" + rest
}

// Validate validates the configuration
func (c *Config) Validate() []error {
	var errors []error

	if c.Database.Mongo.URI == "" {
		errors = append(errors, fmt.Errorf("MONGODB_URI (or MONGO_URL) is required"))
	}

	// Validate Google API configuration
	if c.Google.ServiceAccountJSON == "" {
		errors = append(errors, fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON is required"))
	}
	if c.Google.CustomSearchEngineID == "" {
		errors = append(errors, fmt.Errorf("GOOGLE_CUSTOM_SEARCH_ENGINE_ID is required"))
	}

	// Production-specific validations
	if c.IsProduction {
		if c.Google.OAuthClientID == "" {
			errors = append(errors, fmt.Errorf("GOOGLE_OAUTH_CLIENT_ID is required in production"))
		}
		if c.Google.OAuthClientSecret == "" {
			errors = append(errors, fmt.Errorf("GOOGLE_OAUTH_CLIENT_SECRET is required in production"))
		}
	}

	return errors
}
