package handlers

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/cache"
	"torrent-search-go/internal/config"
	"torrent-search-go/internal/crypto"
	"torrent-search-go/internal/services/realdebrid"
	"torrent-search-go/internal/services/redis"
	pkgmodels "torrent-search-go/pkg/models"
)

// Allowed job names (mirrors Node.js ALLOWED_JOB_NAMES)
var allowedJobNames = map[string]bool{
	"storageCleanup":            true,
	"streamUrlRefresh":          true,
	"descriptionImageCache":     true,
	"searchResultsCache":        true,
	"jobLogMaintenance":         true,
	"redisCatalogCache":         true,
	"searchQueryCache":          true,
	"coverStorageMaintenance":   true,
	"categoryWarmer":            true,
	"metaEnricher":              true,
	"pornripsSync":              true,
	"hentaiSync":                true,
	"enrichedScenesSync":        true,
	"perverzijaSync":            true,
	"freepornvideosSync":        true,
	"yespornSync":               true,
	"porneecSync":               true,
	"atishmkvCatalogSync":       true,
	"atishmkvDirectLinkRefresh": true,
}

var allowedLogLevels = map[string]bool{
	"all": true, "info": true, "warn": true, "error": true, "debug": true,
}

var (
	dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	fileRE = regexp.MustCompile(`^[\w.\-]+\.log(\.gz)?$`)
)

func isAllowedJobName(name string) bool {
	return allowedJobNames[name]
}

// ─── Handler ─────────────────────────────────────────────────────────────────

// MonitoringHandler handles monitoring endpoints
type MonitoringHandler struct {
	storage      *StorageProvider
	config       *config.Config
	startTime    time.Time
	mu           sync.RWMutex
	apiStats     *apiUsageStats
	taskStats    *backgroundTaskStats
	jobScheduler *JobScheduler
	ddosGuard    DDoSGuard
	redis        *redis.Client
}

// DDoSGuard is the interface MonitoringHandler uses to interact with the DDoS guard.
// Implemented by *middleware.DDoSGuard in main.go.
type DDoSGuard interface {
	BlockIP(ip, notes string) error
	UnblockIP(ip string) error
	GetTopIPs(n int) []pkgmodels.IPTrafficStat
	BlockedIPs(ctx context.Context) ([]*pkgmodels.BlockedIP, error)
}

// apiUsageStats tracks in-memory API usage
type apiUsageStats struct {
	totalRequests      int64
	requestsByEndpoint map[string]int64
	requestsByMethod   map[string]int64
	requestsByStatus   map[string]int64
	recentRequests     []map[string]interface{}
	startTime          time.Time
}

// backgroundTaskStats tracks background task stats
type backgroundTaskStats struct {
	storageCleanup            *taskStats
	streamUrlRefresh          *taskStats
	descriptionImage          *taskStats
	searchResults             *taskStats
	redisCatalogCache         *taskStats
	searchQueryCache          *taskStats
	coverStorageMaintenance   *taskStats
	categoryWarmer            *taskStats
	metaEnricher              *taskStats
	atishmkvCatalogSync       *taskStats
	atishmkvDirectLinkRefresh *taskStats
	pornripsSync              *taskStats
	hentaiSync                *taskStats
	enrichedScenesSync        *taskStats
	perverzijaSync            *taskStats
	freepornvideosSync        *taskStats
	yespornSync               *taskStats
	porneecSync               *taskStats
}

// taskStats holds stats for a single background task
type taskStats struct {
	lastRun    *time.Time
	nextRun    *time.Time
	intervalMs int64
	status     string
	results    []map[string]interface{}
}

// NewMonitoringHandler creates a new monitoring handler
func NewMonitoringHandler(storage *StorageProvider, cfg *config.Config) *MonitoringHandler {
	now := time.Now()
	return &MonitoringHandler{
		storage:   storage,
		config:    cfg,
		startTime: now,
		apiStats: &apiUsageStats{
			startTime:          now,
			requestsByEndpoint: make(map[string]int64),
			requestsByMethod:   make(map[string]int64),
			requestsByStatus:   make(map[string]int64),
			recentRequests:     make([]map[string]interface{}, 0),
		},
		taskStats: &backgroundTaskStats{
			storageCleanup:            &taskStats{intervalMs: 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			streamUrlRefresh:          &taskStats{intervalMs: 24 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			descriptionImage:          &taskStats{intervalMs: 6 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			searchResults:             &taskStats{intervalMs: 6 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			redisCatalogCache:         &taskStats{intervalMs: 30 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			searchQueryCache:          &taskStats{intervalMs: 2 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			coverStorageMaintenance:   &taskStats{intervalMs: 5 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			categoryWarmer:            &taskStats{intervalMs: 3 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			metaEnricher:              &taskStats{intervalMs: 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			atishmkvCatalogSync:       &taskStats{intervalMs: 24 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			atishmkvDirectLinkRefresh: &taskStats{intervalMs: 4 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			pornripsSync:              &taskStats{intervalMs: 10 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			hentaiSync:                &taskStats{intervalMs: 6 * 60 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			enrichedScenesSync:        &taskStats{intervalMs: 10 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			perverzijaSync:            &taskStats{intervalMs: 10 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			freepornvideosSync:        &taskStats{intervalMs: 10 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			yespornSync:               &taskStats{intervalMs: 10 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
			porneecSync:               &taskStats{intervalMs: 10 * 60 * 1000, status: "idle", results: make([]map[string]interface{}, 0)},
		},
	}
}

// SetJobScheduler wires the background job scheduler for manual triggers.
func (h *MonitoringHandler) SetJobScheduler(scheduler *JobScheduler) {
	h.jobScheduler = scheduler
}

// jobsRoot returns the root directory for job log files
func (h *MonitoringHandler) jobsRoot() string {
	version := h.config.Logging.BackgroundJobsLogVersion
	if version == "" {
		version = "v1"
	}
	return filepath.Join(h.config.Logging.LogDir, "background-jobs", version)
}

// RecordAPIRequest tracks an HTTP request for the monitoring dashboard.
func (h *MonitoringHandler) RecordAPIRequest(method, path string, statusCode int, duration time.Duration, userAgent string) {
	endpoint := path
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	statusGroup := fmt.Sprintf("%dxx", statusCode/100)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.apiStats.totalRequests++
	h.apiStats.requestsByEndpoint[endpoint]++
	h.apiStats.requestsByMethod[method]++
	h.apiStats.requestsByStatus[statusGroup]++

	if len(userAgent) > 100 {
		userAgent = userAgent[:100]
	}
	entry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"method":    method,
		"path":      path,
		"status":    statusCode,
		"duration":  duration.Milliseconds(),
	}
	if userAgent != "" {
		entry["userAgent"] = userAgent
	}
	h.apiStats.recentRequests = append([]map[string]interface{}{entry}, h.apiStats.recentRequests...)
	if len(h.apiStats.recentRequests) > 100 {
		h.apiStats.recentRequests = h.apiStats.recentRequests[:100]
	}
}

// InitScheduledNextRuns sets initial next-run times when the scheduler starts.
// The values mirror the actual JobScheduleConfig intervals/initial delays so the
// dashboard doesn't show misleading "next run" times before the first tick.
func (h *MonitoringHandler) InitScheduledNextRuns() {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	setNext := func(t *taskStats, delay time.Duration) {
		if delay <= 0 {
			delay = time.Duration(t.intervalMs) * time.Millisecond
		}
		next := now.Add(delay)
		t.nextRun = &next
	}
	cfg := h.config.BackgroundJobs
	setNext(h.taskStats.storageCleanup, 0)
	setNext(h.taskStats.streamUrlRefresh, cfg.StreamUrlRefresh.InitialDelay)
	setNext(h.taskStats.descriptionImage, cfg.DescriptionImageCache.InitialDelay)
	setNext(h.taskStats.searchResults, cfg.SearchResultsCache.InitialDelay)
	setNext(h.taskStats.redisCatalogCache, cfg.RedisCatalogCache.IntervalMin)
	setNext(h.taskStats.searchQueryCache, cfg.SearchQueryCache.InitialDelay)
	setNext(h.taskStats.coverStorageMaintenance, 10*time.Minute)
	setNext(h.taskStats.categoryWarmer, cfg.CategoryWarmer.InitialDelay)
	setNext(h.taskStats.metaEnricher, cfg.MetaEnricher.InitialDelay)
	setNext(h.taskStats.atishmkvCatalogSync, cfg.AtishmkvCatalogSync.InitialDelay)
	setNext(h.taskStats.atishmkvDirectLinkRefresh, cfg.AtishmkvDirectLinkRefresh.InitialDelay)
	setNext(h.taskStats.pornripsSync, cfg.PornripsSync.InitialDelay)
	setNext(h.taskStats.hentaiSync, cfg.HentaiSync.InitialDelay)
	setNext(h.taskStats.enrichedScenesSync, cfg.EnrichedScenesSync.InitialDelay)
	setNext(h.taskStats.perverzijaSync, cfg.PerverzijaSync.InitialDelay)
	setNext(h.taskStats.freepornvideosSync, cfg.FreepornvideosSync.InitialDelay)
	setNext(h.taskStats.yespornSync, cfg.YespornSync.InitialDelay)
	setNext(h.taskStats.porneecSync, cfg.PorneecSync.InitialDelay)
}

func (h *MonitoringHandler) updateTaskStats(taskName string, result map[string]interface{}, manual bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var t *taskStats
	switch taskName {
	case "storageCleanup":
		t = h.taskStats.storageCleanup
	case "streamUrlRefresh":
		t = h.taskStats.streamUrlRefresh
	case "descriptionImageCache":
		t = h.taskStats.descriptionImage
	case "searchResultsCache":
		t = h.taskStats.searchResults
	case "redisCatalogCache":
		t = h.taskStats.redisCatalogCache
	case "searchQueryCache":
		t = h.taskStats.searchQueryCache
	case "coverStorageMaintenance":
		t = h.taskStats.coverStorageMaintenance
	case "categoryWarmer":
		t = h.taskStats.categoryWarmer
	case "metaEnricher":
		t = h.taskStats.metaEnricher
	case "atishmkvCatalogSync":
		t = h.taskStats.atishmkvCatalogSync
	case "atishmkvDirectLinkRefresh":
		t = h.taskStats.atishmkvDirectLinkRefresh
	case "pornripsSync":
		t = h.taskStats.pornripsSync
	case "hentaiSync":
		t = h.taskStats.hentaiSync
	case "enrichedScenesSync":
		t = h.taskStats.enrichedScenesSync
	case "perverzijaSync":
		t = h.taskStats.perverzijaSync
	case "freepornvideosSync":
		t = h.taskStats.freepornvideosSync
	case "yespornSync":
		t = h.taskStats.yespornSync
	case "porneecSync":
		t = h.taskStats.porneecSync
	default:
		return
	}

	now := time.Now()
	t.lastRun = &now
	if !manual {
		next := now.Add(time.Duration(t.intervalMs) * time.Millisecond)
		t.nextRun = &next
	}

	success, _ := result["success"].(bool)
	if !success {
		t.status = "error"
	} else {
		t.status = "completed"
	}

	entry := map[string]interface{}{"timestamp": now.UTC().Format(time.RFC3339)}
	for k, v := range result {
		entry[k] = v
	}
	t.results = append([]map[string]interface{}{entry}, t.results...)
	if len(t.results) > 20 {
		t.results = t.results[:20]
	}

	finalStatus := t.status
	go func() {
		time.Sleep(5 * time.Second)
		h.mu.Lock()
		defer h.mu.Unlock()
		if t.status == finalStatus && finalStatus != "running" {
			t.status = "idle"
		}
	}()
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

// GetDashboardData returns combined monitoring dashboard data
func (h *MonitoringHandler) GetDashboardData(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startTime)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	h.mu.RLock()
	totalReqs := h.apiStats.totalRequests
	byMethod := copyMapInt64(h.apiStats.requestsByMethod)
	byStatus := copyMapStringInt64(h.apiStats.requestsByStatus)
	h.mu.RUnlock()

	uptimeMinutes := uptime.Minutes()
	requestsPerMin := 0.0
	if uptimeMinutes > 0 {
		requestsPerMin = float64(totalReqs) / uptimeMinutes
	}

	// DB stats (best-effort)
	tableStats, _ := h.storage.GetTableStats(r.Context())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"system": map[string]interface{}{
			"uptime": int(uptime.Seconds()),
			"memory": map[string]int{
				"rss":       int(m.Alloc / 1024 / 1024),
				"heapUsed":  int(m.HeapAlloc / 1024 / 1024),
				"heapTotal": int(m.HeapSys / 1024 / 1024),
			},
			"goVersion":   runtime.Version(),
			"nodeVersion": runtime.Version(),
			"environment": h.config.Environment,
		},
		"database": tableStats,
		"api": map[string]interface{}{
			"totalRequests":     totalReqs,
			"requestsPerMinute": requestsPerMin,
			"byMethod":          byMethod,
			"byStatus":          byStatus,
		},
		"backgroundTasks": h.serializeTaskStats(),
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *MonitoringHandler) serializeTaskStats() []map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ts := h.taskStats
	return []map[string]interface{}{
		taskStatMap("storageCleanup", ts.storageCleanup),
		taskStatMap("streamUrlRefresh", ts.streamUrlRefresh),
		taskStatMap("descriptionImageCache", ts.descriptionImage),
		taskStatMap("searchResultsCache", ts.searchResults),
		taskStatMap("redisCatalogCache", ts.redisCatalogCache),
		taskStatMap("searchQueryCache", ts.searchQueryCache),
		taskStatMap("coverStorageMaintenance", ts.coverStorageMaintenance),
		taskStatMap("categoryWarmer", ts.categoryWarmer),
		taskStatMap("metaEnricher", ts.metaEnricher),
		taskStatMap("atishmkvCatalogSync", ts.atishmkvCatalogSync),
		taskStatMap("atishmkvDirectLinkRefresh", ts.atishmkvDirectLinkRefresh),
	}
}

func taskStatMap(name string, t *taskStats) map[string]interface{} {
	var lastResult interface{}
	if len(t.results) > 0 {
		lastResult = t.results[0]
	}
	return map[string]interface{}{
		"name":       name,
		"status":     t.status,
		"lastRun":    t.lastRun,
		"nextRun":    t.nextRun,
		"lastResult": lastResult,
	}
}

// ─── Logs ────────────────────────────────────────────────────────────────────

// GetLogs returns recent application log entries
func (h *MonitoringHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	if level == "" {
		level = "all"
	}
	if !allowedLogLevels[level] {
		writeError(w, "Invalid log level", "INVALID_LEVEL", http.StatusBadRequest)
		return
	}
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}

	logFile := "all.log"
	if level != "all" {
		logFile = level + ".log"
	}
	logPath := filepath.Join(h.config.Logging.LogDir, logFile)
	logs := readRecentLogEntries(logPath, limit)
	if len(logs) == 0 && logFile == "all.log" {
		logs = readRecentLogEntries(filepath.Join(h.config.Logging.LogDir, "app.log"), limit)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"level":   level,
		"count":   len(logs),
		"logs":    logs,
	})
}

// readRecentLogEntries reads up to limit recent log entries, newest first.
func readRecentLogEntries(path string, limit int) []map[string]interface{} {
	lines := tailLogLines(path, limit)
	if len(lines) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		if entry := parseDashboardLogLine(lines[i]); entry != nil {
			out = append(out, entry)
		}
	}
	return out
}

func tailLogLines(path string, limit int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return nil
	}

	maxBytes := int64(1024 * 1024)
	if int64(limit)*2048 > maxBytes {
		maxBytes = int64(limit) * 2048
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	text := string(data)
	if start > 0 {
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func parseDashboardLogLine(line string) map[string]interface{} {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return map[string]interface{}{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"level":     "INFO",
			"message":   line,
			"raw":       line,
		}
	}

	entry := map[string]interface{}{}
	if ts, ok := raw["timestamp"].(string); ok {
		entry["timestamp"] = ts
	} else if ts, ok := raw["time"].(string); ok {
		entry["timestamp"] = ts
	} else {
		entry["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	}

	if lvl, ok := raw["level"].(string); ok {
		entry["level"] = strings.ToUpper(lvl)
	} else if lvl, ok := raw["level"].(float64); ok {
		switch int(lvl) {
		case -4:
			entry["level"] = "DEBUG"
		case 0:
			entry["level"] = "INFO"
		case 4:
			entry["level"] = "WARN"
		case 8:
			entry["level"] = "ERROR"
		default:
			entry["level"] = "INFO"
		}
	} else {
		entry["level"] = "INFO"
	}

	if msg, ok := raw["message"].(string); ok {
		entry["message"] = msg
	} else if msg, ok := raw["msg"].(string); ok {
		entry["message"] = msg
	} else {
		entry["message"] = line
	}

	for _, key := range []string{"method", "url", "path", "status", "duration", "environment", "job", "runId"} {
		if v, ok := raw[key]; ok {
			entry[key] = v
		}
	}
	if _, ok := entry["url"]; !ok {
		if path, ok := raw["path"].(string); ok {
			entry["url"] = path
		}
	}
	return entry
}

func readAppLogsFiltered(logDir, needle string, limit int) []map[string]interface{} {
	lines := tailLogLines(filepath.Join(logDir, "all.log"), 200)
	if len(lines) == 0 {
		lines = tailLogLines(filepath.Join(logDir, "app.log"), 200)
	}
	var filtered []string
	for _, line := range lines {
		if strings.Contains(line, needle) {
			filtered = append(filtered, line)
		}
	}
	if len(filtered) > 50 {
		filtered = filtered[len(filtered)-50:]
	}
	out := make([]map[string]interface{}, 0, len(filtered))
	for i := len(filtered) - 1; i >= 0; i-- {
		if entry := parseDashboardLogLine(filtered[i]); entry != nil {
			out = append(out, entry)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (h *MonitoringHandler) readLatestJobLogEntries(jobName string, limit int) []map[string]interface{} {
	jobPath := filepath.Join(h.jobsRoot(), jobName)
	entries, err := os.ReadDir(jobPath)
	if err != nil {
		return []map[string]interface{}{}
	}

	var latestFile string
	var latestMtime time.Time
	for _, dateEntry := range entries {
		if !dateEntry.IsDir() || !dateRE.MatchString(dateEntry.Name()) {
			continue
		}
		datePath := filepath.Join(jobPath, dateEntry.Name())
		files, err := os.ReadDir(datePath)
		if err != nil {
			continue
		}
		for _, fe := range files {
			if fe.IsDir() || !fileRE.MatchString(fe.Name()) {
				continue
			}
			info, err := fe.Info()
			if err != nil {
				continue
			}
			if latestFile == "" || info.ModTime().After(latestMtime) {
				latestFile = filepath.Join(datePath, fe.Name())
				latestMtime = info.ModTime()
			}
		}
	}
	if latestFile == "" {
		return []map[string]interface{}{}
	}

	lines := tailLogLines(latestFile, limit*2)
	out := make([]map[string]interface{}, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(lines[i]), &raw); err != nil {
			continue
		}
		if typ, _ := raw["type"].(string); typ == "job_start" || typ == "job_end" {
			continue
		}
		if entry := parseDashboardLogLine(lines[i]); entry != nil {
			out = append(out, entry)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ─── Background task stats ───────────────────────────────────────────────────

// GetBackgroundTaskStats returns background task statistics
func (h *MonitoringHandler) GetBackgroundTaskStats(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tasks": map[string]interface{}{
			"storageCleanup":          h.taskStats.storageCleanup,
			"streamUrlRefresh":        h.taskStats.streamUrlRefresh,
			"descriptionImageCache":   h.taskStats.descriptionImage,
			"searchResultsCache":      h.taskStats.searchResults,
			"redisCatalogCache":       h.taskStats.redisCatalogCache,
			"searchQueryCache":        h.taskStats.searchQueryCache,
			"coverStorageMaintenance": h.taskStats.coverStorageMaintenance,
			"categoryWarmer":          h.taskStats.categoryWarmer,
			"metaEnricher":            h.taskStats.metaEnricher,
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// GetApiUsageStats returns API usage statistics
func (h *MonitoringHandler) GetApiUsageStats(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	totalReqs := h.apiStats.totalRequests
	uptime := time.Since(h.apiStats.startTime)
	byMethod := copyMapInt64(h.apiStats.requestsByMethod)
	byStatus := copyMapStringInt64(h.apiStats.requestsByStatus)
	byEndpoint := copyMapInt64(h.apiStats.requestsByEndpoint)
	recent := make([]map[string]interface{}, len(h.apiStats.recentRequests))
	copy(recent, h.apiStats.recentRequests)
	h.mu.RUnlock()

	requestsPerMinute := 0.0
	if mins := uptime.Minutes(); mins > 0 {
		requestsPerMinute = float64(totalReqs) / mins
	}

	// Top 20 endpoints
	type kv struct {
		Endpoint string `json:"endpoint"`
		Count    int64  `json:"count"`
	}
	var topEndpoints []kv
	for ep, cnt := range byEndpoint {
		topEndpoints = append(topEndpoints, kv{ep, cnt})
	}
	sort.Slice(topEndpoints, func(i, j int) bool { return topEndpoints[i].Count > topEndpoints[j].Count })
	if len(topEndpoints) > 20 {
		topEndpoints = topEndpoints[:20]
	}
	if topEndpoints == nil {
		topEndpoints = []kv{}
	}
	if len(recent) > 50 {
		recent = recent[:50]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats": map[string]interface{}{
			"totalRequests":     totalReqs,
			"requestsPerMinute": requestsPerMinute,
			"uptime":            int(uptime.Seconds()),
			"byMethod":          byMethod,
			"byStatus":          byStatus,
			"topEndpoints":      topEndpoints,
		},
		"recentRequests": recent,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	})
}

// ─── Stream URL refresh ──────────────────────────────────────────────────────

// GetStreamUrlRefreshLogs returns stream URL refresh job logs
func (h *MonitoringHandler) GetStreamUrlRefreshLogs(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	includeAppLogs := r.URL.Query().Get("includeAppLogs") == "true"

	h.mu.RLock()
	t := h.taskStats.streamUrlRefresh
	results := append([]map[string]interface{}{}, t.results...)
	status := t.status
	lastRun := t.lastRun
	nextRun := t.nextRun
	h.mu.RUnlock()

	if len(results) > limit {
		results = results[:limit]
	}

	var recentAppLogs []map[string]interface{}
	if includeAppLogs {
		recentAppLogs = readAppLogsFiltered(h.config.Logging.LogDir, "[Stream Refresh]", 50)
		if len(recentAppLogs) == 0 {
			recentAppLogs = h.readLatestJobLogEntries("streamUrlRefresh", 50)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"logs":          results,
		"recentAppLogs": recentAppLogs,
		"count":         len(results),
		"appLogsCount":  len(recentAppLogs),
		"status":        status,
		"lastRun":       lastRun,
		"nextRun":       nextRun,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	})
}

// TriggerStreamUrlRefresh triggers a manual stream URL refresh job
func (h *MonitoringHandler) TriggerStreamUrlRefresh(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runStreamURLRefresh(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Stream URL refresh job triggered",
		"status":  "triggered",
	})
}

// ─── Description image cache ─────────────────────────────────────────────────

// GetDescriptionImageCacheLogs returns description image cache logs
func (h *MonitoringHandler) GetDescriptionImageCacheLogs(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	includeAppLogs := r.URL.Query().Get("includeAppLogs") == "true"

	h.mu.RLock()
	t := h.taskStats.descriptionImage
	results := append([]map[string]interface{}{}, t.results...)
	status := t.status
	lastRun := t.lastRun
	nextRun := t.nextRun
	h.mu.RUnlock()

	if len(results) > limit {
		results = results[:limit]
	}

	var recentAppLogs []map[string]interface{}
	if includeAppLogs {
		recentAppLogs = readAppLogsFiltered(h.config.Logging.LogDir, "[DescImageCache]", 50)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"logs":          results,
		"recentAppLogs": recentAppLogs,
		"count":         len(results),
		"appLogsCount":  len(recentAppLogs),
		"status":        status,
		"lastRun":       lastRun,
		"nextRun":       nextRun,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	})
}

// TriggerDescriptionImageCache triggers a description image cache job
func (h *MonitoringHandler) TriggerDescriptionImageCache(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runWithTaskLock("descriptionImageCache", func() {
				sched.runDescriptionImageCache(true, false)
			})
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Description image cache job triggered",
		"status":  "triggered",
	})
}

// TriggerDescriptionImageCacheForceRefresh forces a description image cache refresh
func (h *MonitoringHandler) TriggerDescriptionImageCacheForceRefresh(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runWithTaskLock("descriptionImageCache", func() {
				sched.runDescriptionImageCache(true, true)
			})
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"message":      "Description image cache force refresh triggered",
		"status":       "triggered",
		"forceRefresh": true,
	})
}

// ─── Search results cache ─────────────────────────────────────────────────────

// GetSearchResultsCacheLogs returns search results cache logs
func (h *MonitoringHandler) GetSearchResultsCacheLogs(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	includeAppLogs := r.URL.Query().Get("includeAppLogs") == "true"

	h.mu.RLock()
	t := h.taskStats.searchResults
	results := append([]map[string]interface{}{}, t.results...)
	status := t.status
	lastRun := t.lastRun
	nextRun := t.nextRun
	h.mu.RUnlock()

	if len(results) > limit {
		results = results[:limit]
	}

	var recentAppLogs []map[string]interface{}
	if includeAppLogs {
		recentAppLogs = readAppLogsFiltered(h.config.Logging.LogDir, "[FilterStreamCache]", 50)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"logs":          results,
		"recentAppLogs": recentAppLogs,
		"count":         len(results),
		"appLogsCount":  len(recentAppLogs),
		"status":        status,
		"lastRun":       lastRun,
		"nextRun":       nextRun,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	})
}

// TriggerSearchResultsCache triggers a search results cache job
func (h *MonitoringHandler) TriggerSearchResultsCache(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runWithTaskLock("searchResultsCache", func() {
				sched.runSearchResultsCache(true)
			})
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Search results cache job triggered",
		"status":  "triggered",
	})
}

// ─── Redis catalog cache ─────────────────────────────────────────────────────

// GetRedisCatalogCacheLogs returns Redis catalog cache job logs
func (h *MonitoringHandler) GetRedisCatalogCacheLogs(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	includeAppLogs := r.URL.Query().Get("includeAppLogs") == "true"

	h.mu.RLock()
	t := h.taskStats.redisCatalogCache
	results := append([]map[string]interface{}{}, t.results...)
	status := t.status
	lastRun := t.lastRun
	nextRun := t.nextRun
	h.mu.RUnlock()

	if len(results) > limit {
		results = results[:limit]
	}

	var recentAppLogs []map[string]interface{}
	if includeAppLogs {
		recentAppLogs = readAppLogsFiltered(h.config.Logging.LogDir, "[RedisCatalog]", 50)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"logs":          results,
		"recentAppLogs": recentAppLogs,
		"count":         len(results),
		"appLogsCount":  len(recentAppLogs),
		"status":        status,
		"lastRun":       lastRun,
		"nextRun":       nextRun,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	})
}

// TriggerRedisCatalogCache triggers a Redis catalog cache job
func (h *MonitoringHandler) TriggerRedisCatalogCache(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runRedisCatalogCache(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Redis catalog cache job triggered",
		"status":  "triggered",
	})
}

// ─── Search query cache ────────────────────────────────────────────────────────

// GetSearchQueryCacheLogs returns search query cache job logs
func (h *MonitoringHandler) GetSearchQueryCacheLogs(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	includeAppLogs := r.URL.Query().Get("includeAppLogs") == "true"

	h.mu.RLock()
	t := h.taskStats.searchQueryCache
	results := append([]map[string]interface{}{}, t.results...)
	status := t.status
	lastRun := t.lastRun
	nextRun := t.nextRun
	h.mu.RUnlock()

	if len(results) > limit {
		results = results[:limit]
	}

	var recentAppLogs []map[string]interface{}
	if includeAppLogs {
		recentAppLogs = readAppLogsFiltered(h.config.Logging.LogDir, "[SearchQueryCache]", 50)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"logs":          results,
		"recentAppLogs": recentAppLogs,
		"count":         len(results),
		"appLogsCount":  len(recentAppLogs),
		"status":        status,
		"lastRun":       lastRun,
		"nextRun":       nextRun,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	})
}

// TriggerSearchQueryCache triggers a search query cache job
func (h *MonitoringHandler) TriggerSearchQueryCache(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runSearchQueryCache(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Search query cache job triggered",
		"status":  "triggered",
	})
}

// ─── Category warmer ─────────────────────────────────────────────────────────

// GetCategoryWarmerLogs returns category warmer job logs.
func (h *MonitoringHandler) GetCategoryWarmerLogs(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	includeAppLogs := r.URL.Query().Get("includeAppLogs") == "true"

	h.mu.RLock()
	t := h.taskStats.categoryWarmer
	results := append([]map[string]interface{}{}, t.results...)
	status := t.status
	lastRun := t.lastRun
	nextRun := t.nextRun
	h.mu.RUnlock()

	if len(results) > limit {
		results = results[:limit]
	}

	var recentAppLogs []map[string]interface{}
	if includeAppLogs {
		recentAppLogs = readAppLogsFiltered(h.config.Logging.LogDir, "[CategoryWarmer]", 50)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"logs":          results,
		"recentAppLogs": recentAppLogs,
		"count":         len(results),
		"appLogsCount":  len(recentAppLogs),
		"status":        status,
		"lastRun":       lastRun,
		"nextRun":       nextRun,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	})
}

// TriggerCategoryWarmer triggers a category warmer job.
func (h *MonitoringHandler) TriggerCategoryWarmer(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runCategoryWarmer(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Category warmer job triggered",
		"status":  "triggered",
	})
}

// ─── Cover storage maintenance ─────────────────────────────────────────────────

// TriggerCoverStorageMaintenance triggers a cover storage maintenance job
func (h *MonitoringHandler) TriggerCoverStorageMaintenance(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runCoverStorageMaintenance(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Cover storage maintenance triggered",
		"status":  "triggered",
	})
}

// TriggerAtishmkvCatalogSync triggers an AtishMKV catalog sync job.
func (h *MonitoringHandler) TriggerAtishmkvCatalogSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runAtishmkvCatalogSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "AtishMKV catalog sync triggered",
		"status":  "triggered",
	})
}

// TriggerAtishmkvDirectLinkRefresh triggers an AtishMKV direct-link refresh job.
func (h *MonitoringHandler) TriggerAtishmkvDirectLinkRefresh(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runAtishmkvDirectLinkRefresh(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "AtishMKV direct link refresh triggered",
		"status":  "triggered",
	})
}

// TriggerPornripsSync triggers a PornRips sync job (ingest + enrich in one tick).
func (h *MonitoringHandler) TriggerPornripsSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runPornripsSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "PornRips sync triggered",
		"status":  "triggered",
	})
}

// TriggerHentaiSync triggers a hentai sync job (ingest in one tick).
func (h *MonitoringHandler) TriggerHentaiSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runHentaiSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Hentai sync triggered",
		"status":  "triggered",
	})
}

// TriggerEnrichedScenesSync triggers an enriched-scenes sync job (discover +
// torrent-match in one tick).
func (h *MonitoringHandler) TriggerEnrichedScenesSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runEnrichedScenesSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Enriched scenes sync triggered",
		"status":  "triggered",
	})
}

// TriggerPerverzijaSync triggers a Perverzija sync job (ingest + enrich + genre
// precompute in one tick).
func (h *MonitoringHandler) TriggerPerverzijaSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runPerverzijaSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Perverzija sync triggered",
		"status":  "triggered",
	})
}

// TriggerFreepornvideosSync triggers a FreePornVideos sync job (ingest + enrich
// + genre precompute in one tick).
func (h *MonitoringHandler) TriggerFreepornvideosSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runFreepornvideosSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "FreePornVideos sync triggered",
		"status":  "triggered",
	})
}

// TriggerYespornSync triggers a YesPorn sync job (ingest + enrich + genre
// precompute in one tick).
func (h *MonitoringHandler) TriggerYespornSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runYespornSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "YesPorn sync triggered",
		"status":  "triggered",
	})
}

// TriggerPorneecSync triggers a Porneec sync job (ingest + enrich + genre
// precompute in one tick).
func (h *MonitoringHandler) TriggerPorneecSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if sched := h.jobScheduler; sched != nil {
			sched.runPorneecSync(true)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Porneec sync triggered",
		"status":  "triggered",
	})
}

// ─── Debug ───────────────────────────────────────────────────────────────────

// DebugFavorites returns debug information about favorites from the database
func (h *MonitoringHandler) DebugFavorites(w http.ResponseWriter, r *http.Request) {
	stats, err := h.storage.GetFavoriteStats(r.Context())
	if err != nil {
		writeError(w, "Failed to retrieve favorite stats", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats":   stats,
	})
}

// DebugFavoriteEntry returns a raw favorite entry by ID for troubleshooting.
func (h *MonitoringHandler) DebugFavoriteEntry(w http.ResponseWriter, r *http.Request) {
	favoriteEntryID := r.PathValue("favoriteEntryId")
	if favoriteEntryID == "" {
		writeError(w, "favoriteEntryId is required", "MISSING_PARAM", http.StatusBadRequest)
		return
	}

	row, err := h.storage.GetFavoriteEntryByID(r.Context(), favoriteEntryID)
	if err != nil {
		writeError(w, "Failed to retrieve favorite entry", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"favoriteEntryId": favoriteEntryID,
		"found":           row != nil,
		"data":            row,
	})
}

// DebugStreamRefresh diagnoses per-user RD keys and samples refresh failures.
func (h *MonitoringHandler) DebugStreamRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	groups, err := h.storage.GetFavoritesForStreamRefresh(ctx)
	if err != nil {
		writeError(w, "Failed to load favorites", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	rd := realdebrid.NewClient()
	users := make([]map[string]interface{}, 0, len(groups))
	var totalFavorites int

	for _, group := range groups {
		totalFavorites += len(group.Favorites)
		entry := map[string]interface{}{
			"userIdPrefix":   prefixID(group.UserID),
			"favoritesCount": len(group.Favorites),
		}

		if group.UserID == "" {
			entry["status"] = "skipped_anonymous"
			users = append(users, entry)
			continue
		}

		encKey, err := h.storage.GetRealDebridKey(ctx, group.UserID)
		if err != nil || encKey == "" {
			entry["status"] = "no_api_key"
			users = append(users, entry)
			continue
		}

		apiKey, err := crypto.DecryptSecret(encKey)
		if err != nil || apiKey == "" {
			entry["status"] = "decrypt_failed"
			entry["decryptError"] = errString(err)
			users = append(users, entry)
			continue
		}

		entry["hasEncryptedKey"] = true
		if rdUser, err := rd.ValidateAPIKey(ctx, apiKey); err != nil {
			entry["status"] = "rd_key_invalid"
			entry["rdError"] = err.Error()
		} else {
			entry["status"] = "rd_key_valid"
			if username, ok := rdUser["username"].(string); ok {
				entry["rdUsername"] = username
			}
		}

		if len(group.Favorites) > 0 {
			sample := group.Favorites[0]
			entry["sampleTorrent"] = truncateStr(sample.TorrentName, 60)
			if _, err := rd.RefreshStreamURL(ctx, apiKey, sample.MagnetLink); err != nil {
				entry["sampleRefreshError"] = err.Error()
			} else {
				entry["sampleRefreshError"] = nil
			}
		}

		users = append(users, entry)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"totalUsers":     len(groups),
		"totalFavorites": totalFavorites,
		"users":          users,
	})
}

func prefixID(id string) string {
	if id == "" {
		return "anonymous"
	}
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "..."
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ─── Job log listing ─────────────────────────────────────────────────────────

// FileEntry represents a job log file entry in the listing
type FileEntry struct {
	Job          string `json:"job"`
	Date         string `json:"date"`
	Name         string `json:"name"`
	Compressed   bool   `json:"compressed"`
	SizeBytes    int64  `json:"sizeBytes"`
	Mtime        string `json:"mtime"`
	RelativePath string `json:"relativePath"`
}

// ListJobLogs lists job log files from the filesystem
func (h *MonitoringHandler) ListJobLogs(w http.ResponseWriter, r *http.Request) {
	root := h.jobsRoot()
	jobFilter := r.URL.Query().Get("job")
	dateFrom := r.URL.Query().Get("dateFrom")
	dateTo := r.URL.Query().Get("dateTo")

	if jobFilter != "" && !isAllowedJobName(jobFilter) {
		writeError(w, "Invalid job name", "INVALID_JOB", http.StatusBadRequest)
		return
	}
	if dateFrom != "" && !dateRE.MatchString(dateFrom) {
		writeError(w, "Invalid dateFrom (use YYYY-MM-DD)", "INVALID_DATE", http.StatusBadRequest)
		return
	}
	if dateTo != "" && !dateRE.MatchString(dateTo) {
		writeError(w, "Invalid dateTo (use YYYY-MM-DD)", "INVALID_DATE", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		version := h.config.Logging.BackgroundJobsLogVersion
		if version == "" {
			version = "v1"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"root":       root,
			"logVersion": version,
			"count":      0,
			"files":      []FileEntry{},
		})
		return
	}

	var files []FileEntry

	jobEntries, err := os.ReadDir(root)
	if err != nil {
		writeError(w, "Failed to read job logs directory", "FS_ERROR", http.StatusInternalServerError)
		return
	}

	for _, jd := range jobEntries {
		if !jd.IsDir() {
			continue
		}
		jobName := jd.Name()
		if !isAllowedJobName(jobName) {
			continue
		}
		if jobFilter != "" && jobName != jobFilter {
			continue
		}

		jobPath := filepath.Join(root, jobName)
		dateEntries, err := os.ReadDir(jobPath)
		if err != nil {
			continue
		}

		for _, dd := range dateEntries {
			if !dd.IsDir() {
				continue
			}
			dateStr := dd.Name()
			if !dateRE.MatchString(dateStr) {
				continue
			}
			if dateFrom != "" && dateStr < dateFrom {
				continue
			}
			if dateTo != "" && dateStr > dateTo {
				continue
			}

			datePath := filepath.Join(jobPath, dateStr)
			fileEntries, err := os.ReadDir(datePath)
			if err != nil {
				continue
			}

			for _, fe := range fileEntries {
				if fe.IsDir() {
					continue
				}
				name := fe.Name()
				if !fileRE.MatchString(name) {
					continue
				}
				fullPath := filepath.Join(datePath, name)
				info, err := os.Stat(fullPath)
				if err != nil {
					continue
				}
				files = append(files, FileEntry{
					Job:          jobName,
					Date:         dateStr,
					Name:         name,
					Compressed:   strings.HasSuffix(name, ".log.gz"),
					SizeBytes:    info.Size(),
					Mtime:        info.ModTime().UTC().Format(time.RFC3339),
					RelativePath: jobName + "/" + dateStr + "/" + name,
				})
			}
		}
	}

	// Sort by mtime descending (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Mtime > files[j].Mtime
	})

	version := h.config.Logging.BackgroundJobsLogVersion
	if version == "" {
		version = "v1"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"root":       root,
		"logVersion": version,
		"count":      len(files),
		"files":      files,
	})
}

// ─── Job log search ───────────────────────────────────────────────────────────

// SearchJobLogs searches for a string across job log files
func (h *MonitoringHandler) SearchJobLogs(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, "Query parameter q is required", "MISSING_PARAM", http.StatusBadRequest)
		return
	}

	sortOrder := "desc"
	if r.URL.Query().Get("sort") == "asc" {
		sortOrder = "asc"
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v >= 1 && v <= 200 {
		limit = v
	}

	job := r.URL.Query().Get("job")
	dateFrom := r.URL.Query().Get("dateFrom")
	dateTo := r.URL.Query().Get("dateTo")
	includeCompressed := r.URL.Query().Get("includeCompressed") != "false"

	if job != "" && !isAllowedJobName(job) {
		writeError(w, "Invalid job name", "INVALID_JOB", http.StatusBadRequest)
		return
	}
	if dateFrom != "" && !dateRE.MatchString(dateFrom) {
		writeError(w, "Invalid dateFrom", "INVALID_DATE", http.StatusBadRequest)
		return
	}
	if dateTo != "" && !dateRE.MatchString(dateTo) {
		writeError(w, "Invalid dateTo", "INVALID_DATE", http.StatusBadRequest)
		return
	}

	root := h.jobsRoot()
	paths := h.collectLogFiles(root, job, dateFrom, dateTo, includeCompressed)

	// Sort paths by mtime
	type pathWithMtime struct {
		path  string
		mtime time.Time
	}
	var pwm []pathWithMtime
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			pwm = append(pwm, pathWithMtime{p, info.ModTime()})
		}
	}
	if sortOrder == "desc" {
		sort.Slice(pwm, func(i, j int) bool { return pwm[i].mtime.After(pwm[j].mtime) })
	} else {
		sort.Slice(pwm, func(i, j int) bool { return pwm[i].mtime.Before(pwm[j].mtime) })
	}

	needle := strings.ToLower(q)

	type Match struct {
		File string `json:"file"`
		Line string `json:"line"`
	}
	var matches []Match
	skipped := 0

outer:
	for _, pw := range pwm {
		fileHits, err := h.searchInFile(pw.path, needle)
		if err != nil {
			continue
		}

		// Optionally reverse hits within the file for desc ordering
		if sortOrder == "desc" {
			for i, j := 0, len(fileHits)-1; i < j; i, j = i+1, j-1 {
				fileHits[i], fileHits[j] = fileHits[j], fileHits[i]
			}
		}

		rel, _ := filepath.Rel(root, pw.path)
		rel = filepath.ToSlash(rel)

		for _, line := range fileHits {
			if skipped < offset {
				skipped++
				continue
			}
			if len(matches) >= limit {
				break outer
			}
			matches = append(matches, Match{File: rel, Line: line})
		}
	}

	nextOffset := offset + len(matches)
	hasMore := len(matches) == limit

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"query":      q,
		"sort":       sortOrder,
		"offset":     offset,
		"limit":      limit,
		"returned":   len(matches),
		"nextOffset": nextOffset,
		"hasMore":    hasMore,
		"matches":    matches,
	})
}

// collectLogFiles gathers log file paths matching the given filters
func (h *MonitoringHandler) collectLogFiles(root, job, dateFrom, dateTo string, includeCompressed bool) []string {
	var out []string
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return out
	}

	jobEntries, err := os.ReadDir(root)
	if err != nil {
		return out
	}

	for _, jd := range jobEntries {
		if !jd.IsDir() {
			continue
		}
		jobName := jd.Name()
		if !isAllowedJobName(jobName) {
			continue
		}
		if job != "" && jobName != job {
			continue
		}

		jobPath := filepath.Join(root, jobName)
		dateEntries, err := os.ReadDir(jobPath)
		if err != nil {
			continue
		}

		for _, dd := range dateEntries {
			if !dd.IsDir() {
				continue
			}
			dateStr := dd.Name()
			if !dateRE.MatchString(dateStr) {
				continue
			}
			if dateFrom != "" && dateStr < dateFrom {
				continue
			}
			if dateTo != "" && dateStr > dateTo {
				continue
			}

			datePath := filepath.Join(jobPath, dateStr)
			fileEntries, err := os.ReadDir(datePath)
			if err != nil {
				continue
			}

			for _, fe := range fileEntries {
				if fe.IsDir() {
					continue
				}
				name := fe.Name()
				if strings.HasSuffix(name, ".log.gz") && !includeCompressed {
					continue
				}
				if !(strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz")) {
					continue
				}
				if !fileRE.MatchString(name) {
					continue
				}
				out = append(out, filepath.Join(datePath, name))
			}
		}
	}
	return out
}

// searchInFile returns lines in a file (plain or gzip) that contain needle (case-insensitive)
func (h *MonitoringHandler) searchInFile(filePath, needle string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(filePath, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

	var matches []string
	scanner := bufio.NewScanner(reader)
	// Increase scanner buffer for long log lines
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), needle) {
			matches = append(matches, line)
		}
	}
	return matches, scanner.Err()
}

// ─── Job log file serving ────────────────────────────────────────────────────

// ServeJobLogFile streams a job log file (gunzips .gz files on the fly)
func (h *MonitoringHandler) ServeJobLogFile(w http.ResponseWriter, r *http.Request) {
	job := r.URL.Query().Get("job")
	date := r.URL.Query().Get("date")
	name := r.URL.Query().Get("name")

	if job == "" || date == "" || name == "" {
		writeError(w, "job, date, and name are required", "MISSING_PARAM", http.StatusBadRequest)
		return
	}
	if !isAllowedJobName(job) {
		writeError(w, "Invalid job name", "INVALID_JOB", http.StatusBadRequest)
		return
	}
	if !dateRE.MatchString(date) {
		writeError(w, "Invalid date (use YYYY-MM-DD)", "INVALID_DATE", http.StatusBadRequest)
		return
	}
	if !fileRE.MatchString(name) {
		writeError(w, "Invalid file name", "INVALID_NAME", http.StatusBadRequest)
		return
	}

	root := h.jobsRoot()
	fullPath := filepath.Join(root, job, date, name)

	// Path traversal protection
	normRoot := filepath.Clean(root) + string(os.PathSeparator)
	normPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(normPath, normRoot) {
		writeError(w, "Access denied", "FORBIDDEN", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		writeError(w, "Log file not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	displayName := strings.TrimSuffix(name, ".gz")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, displayName))
	w.Header().Set("X-Job-Log-File", displayName)

	if strings.HasSuffix(name, ".gz") {
		f, err := os.Open(fullPath)
		if err != nil {
			writeError(w, "Failed to open log file", "FS_ERROR", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		gz, err := gzip.NewReader(f)
		if err != nil {
			writeError(w, "Failed to decompress log file", "DECOMPRESS_ERROR", http.StatusInternalServerError)
			return
		}
		defer gz.Close()

		io.Copy(w, gz) //nolint:errcheck
		return
	}

	http.ServeFile(w, r, fullPath)
}

// ─── Job log maintenance ─────────────────────────────────────────────────────

// TriggerJobLogMaintenance runs gzip compression + retention cleanup in the background
func (h *MonitoringHandler) TriggerJobLogMaintenance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Job log maintenance started",
	})
	go h.RunJobLogMaintenance()
}

// RunJobLogMaintenance compresses old .log files and deletes old date directories
func (h *MonitoringHandler) RunJobLogMaintenance() {
	root := h.jobsRoot()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return
	}

	compressAfter := time.Duration(h.config.Logging.BackgroundJobLogCompressAfterMs) * time.Millisecond
	retentionDays := h.config.Logging.BackgroundJobLogRetentionDays
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

	jobEntries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	for _, jd := range jobEntries {
		if !jd.IsDir() || !isAllowedJobName(jd.Name()) {
			continue
		}
		jobPath := filepath.Join(root, jd.Name())
		dateEntries, err := os.ReadDir(jobPath)
		if err != nil {
			continue
		}

		for _, dd := range dateEntries {
			if !dd.IsDir() {
				continue
			}
			dateStr := dd.Name()
			if !dateRE.MatchString(dateStr) {
				continue
			}

			// Delete directories beyond retention window
			t, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			if t.Before(cutoffDate) {
				os.RemoveAll(filepath.Join(jobPath, dateStr)) //nolint:errcheck
				continue
			}

			// Compress .log files older than compressAfter threshold
			datePath := filepath.Join(jobPath, dateStr)
			fileEntries, err := os.ReadDir(datePath)
			if err != nil {
				continue
			}
			for _, fe := range fileEntries {
				if fe.IsDir() {
					continue
				}
				name := fe.Name()
				// Only compress plain .log files (not already .log.gz)
				if !strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz") {
					continue
				}
				fullPath := filepath.Join(datePath, name)
				info, err := os.Stat(fullPath)
				if err != nil {
					continue
				}
				if time.Since(info.ModTime()) > compressAfter {
					h.compressLogFile(fullPath)
				}
			}
		}
	}
}

// compressLogFile gzip-compresses a .log file, writing .log.gz, then removes the original
func (h *MonitoringHandler) compressLogFile(path string) {
	src, err := os.Open(path)
	if err != nil {
		return
	}

	gzPath := path + ".gz"
	dst, err := os.Create(gzPath)
	if err != nil {
		src.Close()
		return
	}

	gz := gzip.NewWriter(dst)
	if _, err := io.Copy(gz, src); err != nil {
		src.Close()
		gz.Close()
		dst.Close()
		os.Remove(gzPath)
		return
	}
	gz.Close()
	dst.Close()
	src.Close()

	os.Remove(path) //nolint:errcheck - original removed after successful compression
}

// ─── Job Scheduler ───────────────────────────────────────────────────────────

// JobScheduler handles background job scheduling
type JobScheduler struct {
	storage    *StorageProvider
	config     *config.Config
	monitoring *MonitoringHandler
	runner     interface {
		StorageCleanup(ctx context.Context) error
		StreamURLRefresh(ctx context.Context) (map[string]interface{}, error)
		DescriptionImageCache(ctx context.Context, forceRefresh bool) (map[string]interface{}, error)
		SearchResultsCache(ctx context.Context) (map[string]interface{}, error)
		RedisCatalogCache(ctx context.Context) (map[string]interface{}, error)
		SearchQueryCache(ctx context.Context) (map[string]interface{}, error)
		CoverStorageMaintenance(ctx context.Context) (map[string]interface{}, error)
		CategoryWarmer(ctx context.Context) (map[string]interface{}, error)
		MetaEnricher(ctx context.Context) (map[string]interface{}, error)
		AtishmkvCatalogSync(ctx context.Context) (map[string]interface{}, error)
		AtishmkvDirectLinkRefresh(ctx context.Context) (map[string]interface{}, error)
		PornripsSync(ctx context.Context) (map[string]interface{}, error)
		HentaiSync(ctx context.Context) (map[string]interface{}, error)
		EnrichedScenesSync(ctx context.Context) (map[string]interface{}, error)
		PerverzijaSync(ctx context.Context) (map[string]interface{}, error)
		FreepornvideosSync(ctx context.Context) (map[string]interface{}, error)
		YespornSync(ctx context.Context) (map[string]interface{}, error)
		PorneecSync(ctx context.Context) (map[string]interface{}, error)
	}
	tickers     []*time.Ticker
	done        chan struct{}
	taskLocks   map[string]*sync.Mutex
	taskLocksMu sync.Mutex // guards taskLocks map against concurrent lazy-init
}

// NewJobScheduler creates a new job scheduler
// ─── IP Block / DDoS management ──────────────────────────────────────────────

// SetDDoSGuard wires the DDoS guard after construction (called from main.go).
func (h *MonitoringHandler) SetDDoSGuard(g DDoSGuard) {
	h.mu.Lock()
	h.ddosGuard = g
	h.mu.Unlock()
}

// SetRedisClient wires the Redis client used by the cache viewer/buster
// endpoints. May be nil when Redis is disabled; the handlers degrade gracefully.
func (h *MonitoringHandler) SetRedisClient(c *redis.Client) {
	h.mu.Lock()
	h.redis = c
	h.mu.Unlock()
}

// GetIPTraffic returns the top 50 IPs with per-window request counts.
func (h *MonitoringHandler) GetIPTraffic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.ddosGuard == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "traffic": []interface{}{}})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"traffic": h.ddosGuard.GetTopIPs(50),
	})
}

// GetBlockedIPs returns the list of blocked IPs from MongoDB.
func (h *MonitoringHandler) GetBlockedIPs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.ddosGuard == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "blocked": []interface{}{}})
		return
	}
	ips, err := h.ddosGuard.BlockedIPs(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	if ips == nil {
		ips = []*pkgmodels.BlockedIP{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "blocked": ips})
}

// BlockIP manually blocks an IP address.
func (h *MonitoringHandler) BlockIP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var body struct {
		IP    string `json:"ip"`
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IP == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "ip is required"})
		return
	}
	if h.ddosGuard == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "ddos guard not initialised"})
		return
	}
	if err := h.ddosGuard.BlockIP(body.IP, body.Notes); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "ip": body.IP})
}

// UnblockIP removes an IP from the blocklist.
func (h *MonitoringHandler) UnblockIP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	ip := r.PathValue("ip")
	if ip == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "ip is required"})
		return
	}
	if h.ddosGuard == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "ddos guard not initialised"})
		return
	}
	if err := h.ddosGuard.UnblockIP(ip); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "ip": ip})
}

func NewJobScheduler(storage *StorageProvider, cfg *config.Config, monitoring *MonitoringHandler, runner interface {
	StorageCleanup(ctx context.Context) error
	StreamURLRefresh(ctx context.Context) (map[string]interface{}, error)
	DescriptionImageCache(ctx context.Context, forceRefresh bool) (map[string]interface{}, error)
	SearchResultsCache(ctx context.Context) (map[string]interface{}, error)
	RedisCatalogCache(ctx context.Context) (map[string]interface{}, error)
	SearchQueryCache(ctx context.Context) (map[string]interface{}, error)
	CoverStorageMaintenance(ctx context.Context) (map[string]interface{}, error)
	CategoryWarmer(ctx context.Context) (map[string]interface{}, error)
	MetaEnricher(ctx context.Context) (map[string]interface{}, error)
	AtishmkvCatalogSync(ctx context.Context) (map[string]interface{}, error)
	AtishmkvDirectLinkRefresh(ctx context.Context) (map[string]interface{}, error)
	PornripsSync(ctx context.Context) (map[string]interface{}, error)
	HentaiSync(ctx context.Context) (map[string]interface{}, error)
	EnrichedScenesSync(ctx context.Context) (map[string]interface{}, error)
	PerverzijaSync(ctx context.Context) (map[string]interface{}, error)
	FreepornvideosSync(ctx context.Context) (map[string]interface{}, error)
	YespornSync(ctx context.Context) (map[string]interface{}, error)
	PorneecSync(ctx context.Context) (map[string]interface{}, error)
}) *JobScheduler {
	return &JobScheduler{
		storage:    storage,
		config:     cfg,
		monitoring: monitoring,
		runner:     runner,
		tickers:    make([]*time.Ticker, 0),
		done:       make(chan struct{}),
		taskLocks:  make(map[string]*sync.Mutex),
	}
}

// Start starts all background jobs
func (s *JobScheduler) Start() {
	if s.monitoring != nil {
		s.monitoring.InitScheduledNextRuns()
	}
	s.startStorageCleanup()
	s.startStreamUrlRefresh()
	s.startDescriptionImageCache()
	s.startSearchResultsCache()
	s.startJobLogMaintenance()
	s.startRedisCatalogCache()
	s.startSearchQueryCache()
	s.startCoverStorageMaintenance()
	s.startCategoryWarmer()
	s.startMetaEnricher()
	s.startAtishmkvCatalogSync()
	s.startAtishmkvDirectLinkRefresh()
	s.startPornripsSync()
	s.startHentaiSync()
	s.startEnrichedScenesSync()
	s.startPerverzijaSync()
	s.startFreepornvideosSync()
	s.startYespornSync()
	s.startPorneecSync()
}

// Stop stops all background jobs
func (s *JobScheduler) Stop(done <-chan struct{}) {
	close(s.done)
	for _, ticker := range s.tickers {
		ticker.Stop()
	}
}

func (s *JobScheduler) startStorageCleanup() {
	s.runPeriodic("storageCleanup", 1*time.Hour, 0, func() {
		s.runStorageCleanup(false)
	})
}

func (s *JobScheduler) runStorageCleanup(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		s.monitoring.taskStats.storageCleanup.status = "running"
		now := time.Now()
		s.monitoring.taskStats.storageCleanup.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "storageCleanup", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "Starting storage cleanup")
		runErr := s.runner.StorageCleanup(ctx)
		if runErr != nil {
			logLine("error", runErr.Error())
			return map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}, runErr
		}
		logLine("info", "Storage cleanup completed")
		return map[string]interface{}{"success": true, "manual": manual}, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("storageCleanup", result, manual)
	}
}

func (s *JobScheduler) startStreamUrlRefresh() {
	s.runPeriodic("streamUrlRefresh", s.config.BackgroundJobs.StreamUrlRefresh.Interval, s.config.BackgroundJobs.StreamUrlRefresh.InitialDelay, func() {
		s.runStreamURLRefresh(false)
	})
}

func (s *JobScheduler) runStreamURLRefresh(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.streamUrlRefresh.status = "running"
		s.monitoring.taskStats.streamUrlRefresh.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "streamUrlRefresh", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[Stream Refresh] Job started")
		results, runErr := s.runner.StreamURLRefresh(ctx)
		if runErr != nil {
			logLine("error", "[Stream Refresh] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[Stream Refresh] %s: %v", k, v))
		}
		logLine("info", "[Stream Refresh] Job completed")
		out := map[string]interface{}{"success": true, "manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("streamUrlRefresh", result, manual)
	}
}

func (s *JobScheduler) startDescriptionImageCache() {
	s.runPeriodic("descriptionImageCache", s.config.BackgroundJobs.DescriptionImageCache.Interval, s.config.BackgroundJobs.DescriptionImageCache.InitialDelay, func() {
		s.runDescriptionImageCache(false, false)
	})
}

func (s *JobScheduler) runDescriptionImageCache(manual, forceRefresh bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.descriptionImage.status = "running"
		s.monitoring.taskStats.descriptionImage.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "descriptionImageCache", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", fmt.Sprintf("[DescImageCache] Job started (forceRefresh=%v)", forceRefresh))
		results, runErr := s.runner.DescriptionImageCache(ctx, forceRefresh)
		if runErr != nil {
			logLine("error", "[DescImageCache] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual, "forceRefresh": forceRefresh}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[DescImageCache] %s: %v", k, v))
		}
		logLine("info", "[DescImageCache] Job completed")
		out := map[string]interface{}{"manual": manual, "forceRefresh": forceRefresh}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if forceRefresh {
			result["forceRefresh"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("descriptionImageCache", result, manual)
	}
}

func (s *JobScheduler) startSearchResultsCache() {
	s.runPeriodic("searchResultsCache", s.config.BackgroundJobs.SearchResultsCache.Interval, s.config.BackgroundJobs.SearchResultsCache.InitialDelay, func() {
		s.runSearchResultsCache(false)
	})
}

func (s *JobScheduler) runSearchResultsCache(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.searchResults.status = "running"
		s.monitoring.taskStats.searchResults.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "searchResultsCache", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[FilterStreamCache] Job started")
		results, runErr := s.runner.SearchResultsCache(ctx)
		if runErr != nil {
			// The runner now treats context expiry as a partial stop; don't mark
			// the whole job as a hard failure just because it ran out of time.
			if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
				logLine("warn", "[FilterStreamCache] Job stopped early: "+runErr.Error())
				out := map[string]interface{}{"manual": manual, "stopped": true, "stopReason": runErr.Error()}
				for k, v := range results {
					out[k] = v
				}
				return out, nil
			}
			logLine("error", "[FilterStreamCache] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[FilterStreamCache] %s: %v", k, v))
		}
		logLine("info", "[FilterStreamCache] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("searchResultsCache", result, manual)
	}
}

func (s *JobScheduler) startJobLogMaintenance() {
	interval := time.Duration(s.config.Logging.BackgroundJobLogMaintenanceIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	initialDelay := time.Duration(s.config.Logging.BackgroundJobLogMaintenanceInitialDelayMs) * time.Millisecond
	if initialDelay <= 0 {
		initialDelay = 15 * time.Minute
	}

	s.runPeriodic("jobLogMaintenance", interval, initialDelay, func() {
		if s.monitoring != nil {
			s.monitoring.RunJobLogMaintenance()
		}
	})
}

func (s *JobScheduler) startRedisCatalogCache() {
	if !s.config.Redis.Enabled {
		return
	}
	cfg := s.config.BackgroundJobs.RedisCatalogCache
	minMs := int64(cfg.IntervalMin / time.Millisecond)
	maxMs := int64(cfg.IntervalMax / time.Millisecond)
	if maxMs < minMs {
		maxMs = minMs
	}
	// First run uses the same min-max jitter; runPeriodic handles the initial delay below.
	nextDelay := func() time.Duration {
		diff := maxMs - minMs
		if diff <= 0 {
			return cfg.IntervalMin
		}
		n, _ := rand.Int(rand.Reader, big.NewInt(diff+1))
		return time.Duration(minMs+n.Int64()) * time.Millisecond
	}
	s.runPeriodicWithDynamicDelay("redisCatalogCache", 3*time.Minute, nextDelay, func() {
		s.runRedisCatalogCache(false)
	})
}

func (s *JobScheduler) runRedisCatalogCache(manual bool) {
	if !s.config.Redis.Enabled {
		return
	}
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.redisCatalogCache.status = "running"
		s.monitoring.taskStats.redisCatalogCache.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "redisCatalogCache", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[RedisCatalog] Job started")
		results, runErr := s.runner.RedisCatalogCache(ctx)
		if runErr != nil {
			logLine("error", "[RedisCatalog] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[RedisCatalog] %s: %v", k, v))
		}
		logLine("info", "[RedisCatalog] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("redisCatalogCache", result, manual)
	}
}

func (s *JobScheduler) startSearchQueryCache() {
	if !s.config.Redis.Enabled {
		return
	}
	s.runPeriodic("searchQueryCache", s.config.BackgroundJobs.SearchQueryCache.Interval, s.config.BackgroundJobs.SearchQueryCache.InitialDelay, func() {
		s.runSearchQueryCache(false)
	})
}

func (s *JobScheduler) runSearchQueryCache(manual bool) {
	if !s.config.Redis.Enabled {
		return
	}
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.searchQueryCache.status = "running"
		s.monitoring.taskStats.searchQueryCache.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "searchQueryCache", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[SearchQueryCache] Job started")
		results, runErr := s.runner.SearchQueryCache(ctx)
		if runErr != nil {
			logLine("error", "[SearchQueryCache] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[SearchQueryCache] %s: %v", k, v))
		}
		logLine("info", "[SearchQueryCache] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("searchQueryCache", result, manual)
	}
}

func (s *JobScheduler) startCoverStorageMaintenance() {
	if !s.config.S3.Enabled {
		return
	}
	s.runPeriodic("coverStorageMaintenance", 5*time.Hour, 10*time.Minute, func() {
		s.runCoverStorageMaintenance(false)
	})
}

func (s *JobScheduler) runCoverStorageMaintenance(manual bool) {
	if !s.config.S3.Enabled {
		return
	}
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.coverStorageMaintenance.status = "running"
		s.monitoring.taskStats.coverStorageMaintenance.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "coverStorageMaintenance", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[CoverStorageMaintenance] Job started")
		results, runErr := s.runner.CoverStorageMaintenance(ctx)
		if runErr != nil {
			logLine("error", "[CoverStorageMaintenance] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[CoverStorageMaintenance] %s: %v", k, v))
		}
		logLine("info", "[CoverStorageMaintenance] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("coverStorageMaintenance", result, manual)
	}
}

func (s *JobScheduler) startCategoryWarmer() {
	if !s.config.Redis.Enabled {
		return
	}
	cfg := s.config.BackgroundJobs.CategoryWarmer
	s.runPeriodic("categoryWarmer", cfg.Interval, cfg.InitialDelay, func() {
		s.runCategoryWarmer(false)
	})
}

func (s *JobScheduler) runCategoryWarmer(manual bool) {
	if !s.config.Redis.Enabled {
		return
	}
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.categoryWarmer.status = "running"
		s.monitoring.taskStats.categoryWarmer.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "categoryWarmer", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[CategoryWarmer] Job started")
		results, runErr := s.runner.CategoryWarmer(ctx)
		if runErr != nil {
			logLine("error", "[CategoryWarmer] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[CategoryWarmer] %s: %v", k, v))
		}
		logLine("info", "[CategoryWarmer] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("categoryWarmer", result, manual)
	}
}

func (s *JobScheduler) startMetaEnricher() {
	if !s.config.Redis.Enabled {
		return
	}
	cfg := s.config.BackgroundJobs.MetaEnricher
	s.runPeriodic("metaEnricher", cfg.Interval, cfg.InitialDelay, func() {
		s.runMetaEnricher(false)
	})
}

func (s *JobScheduler) runMetaEnricher(manual bool) {
	if !s.config.Redis.Enabled {
		return
	}
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.metaEnricher.status = "running"
		s.monitoring.taskStats.metaEnricher.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "metaEnricher", func(logLine func(string, string)) (map[string]interface{}, error) {
		results, runErr := s.runner.MetaEnricher(ctx)
		if runErr != nil {
			return map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}, runErr
		}
		return results, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("metaEnricher", result, manual)
	}
}

func (s *JobScheduler) startAtishmkvCatalogSync() {
	cfg := s.config.BackgroundJobs.AtishmkvCatalogSync
	s.runPeriodic("atishmkvCatalogSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runAtishmkvCatalogSync(false)
	})
}

func (s *JobScheduler) runAtishmkvCatalogSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.atishmkvCatalogSync.status = "running"
		s.monitoring.taskStats.atishmkvCatalogSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "atishmkvCatalogSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[AtishMKV Catalog Sync] Job started")
		results, runErr := s.runner.AtishmkvCatalogSync(ctx)
		if runErr != nil {
			logLine("error", "[AtishMKV Catalog Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[AtishMKV Catalog Sync] %s: %v", k, v))
		}
		logLine("info", "[AtishMKV Catalog Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("atishmkvCatalogSync", result, manual)
	}
}

func (s *JobScheduler) startAtishmkvDirectLinkRefresh() {
	cfg := s.config.BackgroundJobs.AtishmkvDirectLinkRefresh
	s.runPeriodic("atishmkvDirectLinkRefresh", cfg.Interval, cfg.InitialDelay, func() {
		s.runAtishmkvDirectLinkRefresh(false)
	})
}

func (s *JobScheduler) runAtishmkvDirectLinkRefresh(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.atishmkvDirectLinkRefresh.status = "running"
		s.monitoring.taskStats.atishmkvDirectLinkRefresh.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "atishmkvDirectLinkRefresh", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[AtishMKV Direct Link Refresh] Job started")
		results, runErr := s.runner.AtishmkvDirectLinkRefresh(ctx)
		if runErr != nil {
			logLine("error", "[AtishMKV Direct Link Refresh] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[AtishMKV Direct Link Refresh] %s: %v", k, v))
		}
		logLine("info", "[AtishMKV Direct Link Refresh] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("atishmkvDirectLinkRefresh", result, manual)
	}
}

func (s *JobScheduler) startPornripsSync() {
	cfg := s.config.BackgroundJobs.PornripsSync
	s.runPeriodic("pornripsSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runPornripsSync(false)
	})
}

func (s *JobScheduler) runPornripsSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.pornripsSync.status = "running"
		s.monitoring.taskStats.pornripsSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "pornripsSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[PornRips Sync] Job started")
		results, runErr := s.runner.PornripsSync(ctx)
		if runErr != nil {
			logLine("error", "[PornRips Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[PornRips Sync] %s: %v", k, v))
		}
		logLine("info", "[PornRips Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("pornripsSync", result, manual)
	}
}

func (s *JobScheduler) startHentaiSync() {
	cfg := s.config.BackgroundJobs.HentaiSync
	s.runPeriodic("hentaiSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runHentaiSync(false)
	})
}

func (s *JobScheduler) runHentaiSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.hentaiSync.status = "running"
		s.monitoring.taskStats.hentaiSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "hentaiSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[Hentai Sync] Job started")
		results, runErr := s.runner.HentaiSync(ctx)
		if runErr != nil {
			logLine("error", "[Hentai Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[Hentai Sync] %s: %v", k, v))
		}
		logLine("info", "[Hentai Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("hentaiSync", result, manual)
	}
}

func (s *JobScheduler) startEnrichedScenesSync() {
	cfg := s.config.BackgroundJobs.EnrichedScenesSync
	s.runPeriodic("enrichedScenesSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runEnrichedScenesSync(false)
	})
}

func (s *JobScheduler) runEnrichedScenesSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.enrichedScenesSync.status = "running"
		s.monitoring.taskStats.enrichedScenesSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "enrichedScenesSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[Enriched Scenes Sync] Job started")
		results, runErr := s.runner.EnrichedScenesSync(ctx)
		if runErr != nil {
			logLine("error", "[Enriched Scenes Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[Enriched Scenes Sync] %s: %v", k, v))
		}
		logLine("info", "[Enriched Scenes Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("enrichedScenesSync", result, manual)
	}
}

func (s *JobScheduler) startPerverzijaSync() {
	cfg := s.config.BackgroundJobs.PerverzijaSync
	s.runPeriodic("perverzijaSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runPerverzijaSync(false)
	})
}

func (s *JobScheduler) runPerverzijaSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.perverzijaSync.status = "running"
		s.monitoring.taskStats.perverzijaSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "perverzijaSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[Perverzija Sync] Job started")
		results, runErr := s.runner.PerverzijaSync(ctx)
		if runErr != nil {
			logLine("error", "[Perverzija Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[Perverzija Sync] %s: %v", k, v))
		}
		logLine("info", "[Perverzija Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("perverzijaSync", result, manual)
	}
}

func (s *JobScheduler) startFreepornvideosSync() {
	cfg := s.config.BackgroundJobs.FreepornvideosSync
	s.runPeriodic("freepornvideosSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runFreepornvideosSync(false)
	})
}

func (s *JobScheduler) runFreepornvideosSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.freepornvideosSync.status = "running"
		s.monitoring.taskStats.freepornvideosSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "freepornvideosSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[FreePornVideos Sync] Job started")
		results, runErr := s.runner.FreepornvideosSync(ctx)
		if runErr != nil {
			logLine("error", "[FreePornVideos Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[FreePornVideos Sync] %s: %v", k, v))
		}
		logLine("info", "[FreePornVideos Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("freepornvideosSync", result, manual)
	}
}

func (s *JobScheduler) startYespornSync() {
	cfg := s.config.BackgroundJobs.YespornSync
	s.runPeriodic("yespornSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runYespornSync(false)
	})
}

func (s *JobScheduler) runYespornSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.yespornSync.status = "running"
		s.monitoring.taskStats.yespornSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "yespornSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[YesPorn Sync] Job started")
		results, runErr := s.runner.YespornSync(ctx)
		if runErr != nil {
			logLine("error", "[YesPorn Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[YesPorn Sync] %s: %v", k, v))
		}
		logLine("info", "[YesPorn Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("yespornSync", result, manual)
	}
}

func (s *JobScheduler) startPorneecSync() {
	cfg := s.config.BackgroundJobs.PorneecSync
	s.runPeriodic("porneecSync", cfg.Interval, cfg.InitialDelay, func() {
		s.runPorneecSync(false)
	})
}

func (s *JobScheduler) runPorneecSync(manual bool) {
	if s.monitoring != nil {
		s.monitoring.mu.Lock()
		now := time.Now()
		s.monitoring.taskStats.porneecSync.status = "running"
		s.monitoring.taskStats.porneecSync.lastRun = &now
		s.monitoring.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := RunWithJobFileLogging(s.config, "porneecSync", func(logLine func(string, string)) (map[string]interface{}, error) {
		logLine("info", "[Porneec Sync] Job started")
		results, runErr := s.runner.PorneecSync(ctx)
		if runErr != nil {
			logLine("error", "[Porneec Sync] "+runErr.Error())
			out := map[string]interface{}{"success": false, "error": runErr.Error(), "manual": manual}
			return out, runErr
		}
		for k, v := range results {
			logLine("info", fmt.Sprintf("[Porneec Sync] %s: %v", k, v))
		}
		logLine("info", "[Porneec Sync] Job completed")
		out := map[string]interface{}{"manual": manual}
		for k, v := range results {
			out[k] = v
		}
		return out, nil
	})

	if s.monitoring != nil {
		if result == nil {
			result = map[string]interface{}{}
		}
		if manual {
			result["manual"] = true
		}
		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
		}
		s.monitoring.updateTaskStats("porneecSync", result, manual)
	}
}

// A named mutex per task prevents overlapping runs when a previous invocation is still active.
func (s *JobScheduler) runPeriodic(taskName string, interval, initialDelay time.Duration, fn func()) {
	if interval <= 0 || fn == nil {
		return
	}

	ticker := time.NewTicker(interval)
	s.tickers = append(s.tickers, ticker)

	go func() {
		if initialDelay > 0 {
			select {
			case <-time.After(initialDelay):
			case <-s.done:
				return
			}
		}

		s.runWithTaskLock(taskName, fn)
		for {
			select {
			case <-ticker.C:
				s.runWithTaskLock(taskName, fn)
			case <-s.done:
				return
			}
		}
	}()
}

// runWithTaskLock executes fn while holding a per-task mutex. If the task is
// already running, the call is skipped and a warning is logged.
func (s *JobScheduler) runWithTaskLock(taskName string, fn func()) {
	lock := s.taskLock(taskName)
	if !lock.TryLock() {
		log.Printf("[Scheduler] Skipping scheduled run of %s: previous run still active", taskName)
		return
	}
	defer lock.Unlock()
	fn()
}

// taskLock returns the mutex for a given task, creating it on first use.
// Guarded by taskLocksMu to avoid the concurrent map read+write data race
// when multiple job goroutines lazily register the same lock.
func (s *JobScheduler) taskLock(taskName string) *sync.Mutex {
	s.taskLocksMu.Lock()
	defer s.taskLocksMu.Unlock()
	if lock, ok := s.taskLocks[taskName]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.taskLocks[taskName] = lock
	return lock
}

// runPeriodicWithDynamicDelay runs fn once after initialDelay, then re-computes the
// next delay after each invocation via nextDelay(). This lets the Redis catalog cache
// use 25-35 min jitter instead of a fixed interval.
func (s *JobScheduler) runPeriodicWithDynamicDelay(taskName string, initialDelay time.Duration, nextDelay func() time.Duration, fn func()) {
	if fn == nil {
		return
	}

	go func() {
		if initialDelay > 0 {
			select {
			case <-time.After(initialDelay):
			case <-s.done:
				return
			}
		}

		for {
			s.runWithTaskLock(taskName, fn)
			delay := nextDelay()
			select {
			case <-time.After(delay):
			case <-s.done:
				return
			}
		}
	}()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func copyMapInt64(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyMapStringInt64(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// GetRedisCaches reports every registered Redis cache group with its live key
// count, for the admin dashboard viewer. Degrades to redisEnabled=false when
// Redis is not configured so the UI can show a callout instead of erroring.
func (h *MonitoringHandler) GetRedisCaches(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	rc := h.redis
	h.mu.RUnlock()

	if rc == nil || !rc.IsConfigured() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":      true,
			"redisEnabled": false,
			"groups":       []interface{}{},
		})
		return
	}

	groups := make([]map[string]interface{}, 0, len(cache.Groups))
	for _, g := range cache.Groups {
		count, err := rc.CountPrefix(r.Context(), g.Prefix)
		if err != nil {
			count = 0
		}
		groups = append(groups, map[string]interface{}{
			"prefix":      g.Prefix,
			"label":       g.Label,
			"description": g.Description,
			"ttlSeconds":  g.TTLSeconds,
			"keyCount":    count,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"redisEnabled": true,
		"groups":       groups,
	})
}

// BustRedisCache deletes every key under one registered prefix. The prefix is
// whitelisted via cache.Lookup so callers cannot delete keys outside the
// registry. Body: {"prefix":"cat:hs:v2:"}.
func (h *MonitoringHandler) BustRedisCache(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prefix string `json:"prefix"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid JSON body"})
		return
	}
	if body.Prefix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "prefix is required"})
		return
	}
	if _, ok := cache.Lookup(body.Prefix); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "unknown cache prefix"})
		return
	}

	h.mu.RLock()
	rc := h.redis
	h.mu.RUnlock()
	if rc == nil || !rc.IsConfigured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "redis is not configured"})
		return
	}

	deleted, err := rc.DelByPrefix(r.Context(), body.Prefix)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"prefix":  body.Prefix,
		"deleted": deleted,
	})
}

// BustAllRedisCaches deletes every key under every registered prefix and
// returns the total deleted. Convenience for the dashboard's "Bust all" button.
func (h *MonitoringHandler) BustAllRedisCaches(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	rc := h.redis
	h.mu.RUnlock()
	if rc == nil || !rc.IsConfigured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"success": false, "error": "redis is not configured"})
		return
	}

	var total int64
	for _, g := range cache.Groups {
		deleted, err := rc.DelByPrefix(r.Context(), g.Prefix)
		if err != nil {
			// Surface how much was already deleted before the failure so the
			// operator knows the bust was partial, not atomic.
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success":      false,
				"error":        err.Error(),
				"prefix":       g.Prefix,
				"deletedSoFar": total,
			})
			return
		}
		total += deleted
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": total,
		"groups":  len(cache.Groups),
	})
}
