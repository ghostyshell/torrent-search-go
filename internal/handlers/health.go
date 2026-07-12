package handlers

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"time"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/models"
	"torrent-search-go/internal/services/scraper"
	storagemodels "torrent-search-go/pkg/models"
)

const health1337xTimeout = 90 * time.Second

// HealthHandler handles health check endpoints
type HealthHandler struct {
	config   *config.Config
	scrapers *scraper.Service // may be nil
	db       DatabaseHealthChecker
}

// DatabaseHealthChecker checks database connectivity for health probes.
type DatabaseHealthChecker interface {
	HealthCheck() (*storagemodels.HealthStatus, error)
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status       string                    `json:"status"`
	Timestamp    string                    `json:"timestamp"`
	Environment  string                    `json:"environment"`
	Uptime       int64                     `json:"uptime"`
	Version      string                    `json:"version,omitempty"`
	Memory       *MemoryUsage              `json:"memory,omitempty"`
	System       *SystemInfo               `json:"system,omitempty"`
	Services     map[string]*ServiceHealth `json:"services,omitempty"`
	ResponseTime int64                     `json:"responseTime,omitempty"`
}

// MemoryUsage represents memory usage information
type MemoryUsage struct {
	RSS       int `json:"rss"`
	HeapTotal int `json:"heapTotal"`
	HeapUsed  int `json:"heapUsed"`
	External  int `json:"external"`
}

// SystemInfo represents system information
type SystemInfo struct {
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
	PID       int    `json:"pid"`
}

// ServiceHealth represents the health of a service
type ServiceHealth struct {
	Status       string `json:"status"`
	Type         string `json:"type,omitempty"`
	Error        string `json:"error,omitempty"`
	Timestamp    string `json:"timestamp"`
	ResponseTime int64  `json:"responseTime,omitempty"`
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(cfg *config.Config, scrapers *scraper.Service, db DatabaseHealthChecker) *HealthHandler {
	return &HealthHandler{
		config:   cfg,
		scrapers: scrapers,
		db:       db,
	}
}

// Health returns basic health status
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:      "healthy",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Environment: h.config.Environment,
		Uptime:      int64(time.Since(startTime).Seconds()),
	}

	writeJSON(w, http.StatusOK, response)
}

// DetailedHealth returns comprehensive health status
func (h *HealthHandler) DetailedHealth(w http.ResponseWriter, r *http.Request) {
	reqStart := time.Now()

	response := &HealthResponse{
		Status:      "healthy",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Environment: h.config.Environment,
		Version:     getVersion(),
		Uptime:      int64(time.Since(startTime).Seconds()),
		Memory:      getMemoryUsage(),
		System:      getSystemInfo(),
		Services:    make(map[string]*ServiceHealth),
	}

	// Check database health
	response.Services["database"] = h.checkDatabaseHealth(r)

	// Check Google API health
	response.Services["google"] = h.checkGoogleAPIHealth()

	// Determine overall health status - only hard dependencies mark unhealthy.
	for name, service := range response.Services {
		if service.Status == "unhealthy" {
			if name == "database" {
				response.Status = "unhealthy"
			} else {
				response.Status = "degraded"
			}
			break
		}
		if service.Status == "degraded" && response.Status == "healthy" {
			response.Status = "degraded"
		}
	}

	response.ResponseTime = time.Since(reqStart).Milliseconds()

	statusCode := http.StatusOK
	if response.Status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	writeJSON(w, statusCode, response)
}

// Ready returns readiness probe status
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := []map[string]interface{}{}

	// Check database connectivity
	dbCheck := h.checkDatabaseReadiness(r)
	checks = append(checks, dbCheck)

	// Check environment variables
	envCheck := h.checkEnvironmentVariables()
	checks = append(checks, envCheck)

	// Determine if ready
	allReady := true
	for _, check := range checks {
		if check["status"] != "ready" {
			allReady = false
			break
		}
	}

	response := map[string]interface{}{
		"ready":     allReady,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"checks":    checks,
	}

	statusCode := http.StatusOK
	if !allReady {
		statusCode = http.StatusServiceUnavailable
	}

	writeJSON(w, statusCode, response)
}

// Live returns liveness probe status
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"alive":     true,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    int64(time.Since(startTime).Seconds()),
	}

	writeJSON(w, http.StatusOK, response)
}

// Health1337x runs a live diagnostic against the 1337x scraper
func (h *HealthHandler) Health1337x(w http.ResponseWriter, r *http.Request) {
	ts := time.Now().UTC().Format(time.RFC3339)

	if h.scrapers == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":    "unknown",
			"scraper":   "1337x",
			"timestamp": ts,
			"message":   "Scraper service not configured",
		})
		return
	}

	sc, ok := h.scrapers.GetScraper("1337x")
	if !ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":    "not_configured",
			"scraper":   "1337x",
			"timestamp": ts,
			"message":   "1337x scraper is not registered",
		})
		return
	}

	testStart := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), health1337xTimeout)
	defer cancel()

	var results []models.Torrent
	var err error
	if live, ok := sc.(interface {
		SearchLive(context.Context, string, int, models.SearchOptions) ([]models.Torrent, error)
	}); ok {
		results, err = live.SearchLive(ctx, "ubuntu", 1, models.SearchOptions{MaxResults: 3})
	} else {
		results, err = sc.Search(ctx, "ubuntu", 1, models.SearchOptions{MaxResults: 3})
	}
	elapsed := time.Since(testStart).Milliseconds()

	response := map[string]interface{}{
		"status":       "healthy",
		"scraper":      "1337x",
		"timestamp":    ts,
		"responseTime": elapsed,
		"resultsCount": len(results),
	}

	if err != nil {
		response["status"] = "degraded"
		response["error"] = err.Error()
		delete(response, "resultsCount")
		writeJSON(w, http.StatusOK, response)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

// checkDatabaseHealth checks the database health
func (h *HealthHandler) checkDatabaseHealth(r *http.Request) *ServiceHealth {
	if h.db == nil {
		return &ServiceHealth{
			Status:    "unhealthy",
			Type:      "mongodb",
			Error:     "database client not configured",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	status, err := h.db.HealthCheck()
	if err != nil || status == nil || status.Status != "healthy" {
		errMsg := "database health check failed"
		if err != nil {
			errMsg = err.Error()
		}
		return &ServiceHealth{
			Status:    "unhealthy",
			Type:      "mongodb",
			Error:     errMsg,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	return &ServiceHealth{
		Status:       "healthy",
		Type:         "mongodb",
		Timestamp:    status.Timestamp,
		ResponseTime: status.ResponseTime,
	}
}

// checkDatabaseReadiness checks if database is ready
func (h *HealthHandler) checkDatabaseReadiness(r *http.Request) map[string]interface{} {
	check := map[string]interface{}{
		"name":   "database",
		"status": "ready",
	}

	if h.db == nil {
		check["status"] = "not_ready"
		check["error"] = "database client not configured"
		return check
	}

	status, err := h.db.HealthCheck()
	if err != nil || status == nil || status.Status != "healthy" {
		check["status"] = "not_ready"
		if err != nil {
			check["error"] = err.Error()
		} else {
			check["error"] = "database not healthy"
		}
	}

	return check
}

// checkGoogleAPIHealth checks Google API configuration (optional for addon backend).
func (h *HealthHandler) checkGoogleAPIHealth() *ServiceHealth {
	if h.config.Google.ServiceAccountJSON == "" || h.config.Google.CustomSearchEngineID == "" {
		return &ServiceHealth{
			Status:    "optional",
			Type:      "google",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	if !isValidGoogleJSON(h.config.Google.ServiceAccountJSON) {
		return &ServiceHealth{
			Status:    "degraded",
			Type:      "google",
			Error:     "GOOGLE_SERVICE_ACCOUNT_JSON may be invalid JSON",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	return &ServiceHealth{
		Status:    "healthy",
		Type:      "google",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

func isValidGoogleJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && strings.Contains(s, ":")
}

// checkEnvironmentVariables checks required environment variables
func (h *HealthHandler) checkEnvironmentVariables() map[string]interface{} {
	result := config.ValidateEnvironment(h.config)

	check := map[string]interface{}{
		"name":   "environment",
		"status": "ready",
	}

	if !result.IsValid {
		check["status"] = "not_ready"
		check["errors"] = result.Errors
	}

	return check
}

// Helper functions

var startTime = time.Now()

func getVersion() string {
	return "1.0.0"
}

func getMemoryUsage() *MemoryUsage {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &MemoryUsage{
		RSS:       int(m.Sys / 1024 / 1024),
		HeapTotal: int(m.HeapSys / 1024 / 1024),
		HeapUsed:  int(m.HeapAlloc / 1024 / 1024),
		External:  int(m.HeapInuse / 1024 / 1024),
	}
}

func getSystemInfo() *SystemInfo {
	return &SystemInfo{
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		PID:       0, // Use 0 instead of runtime.Getpid() for simplicity
	}
}

// writeJSON is now in helpers.go - this duplicate is removed
