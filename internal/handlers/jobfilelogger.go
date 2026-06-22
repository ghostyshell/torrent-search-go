package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"torrent-search-go/internal/config"
)

var jobLogMu sync.Mutex

// RunWithJobFileLogging executes fn while writing NDJSON job logs (Node-compatible layout).
func RunWithJobFileLogging(cfg *config.Config, jobName string, fn func(logLine func(level, message string)) (map[string]interface{}, error)) (map[string]interface{}, error) {
	version := cfg.Logging.BackgroundJobsLogVersion
	if version == "" {
		version = "v1"
	}

	runID := fmt.Sprintf("%s-%s", time.Now().UTC().Format("2006-01-02T15-04-05.000Z"), randomHex(4))
	dateStr := time.Now().UTC().Format("2006-01-02")
	dir := filepath.Join(cfg.Logging.LogDir, "background-jobs", version, jobName, dateStr)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fn(func(string, string) {})
	}

	fileName := runID + ".log"
	fullPath := filepath.Join(dir, fileName)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fn(func(string, string) {})
	}
	defer f.Close()

	appendLine := func(v interface{}) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		jobLogMu.Lock()
		defer jobLogMu.Unlock()
		_, _ = f.Write(append(b, '\n'))
	}

	appendLine(map[string]interface{}{
		"v":          1,
		"type":       "job_start",
		"ts":         time.Now().UTC().Format(time.RFC3339Nano),
		"job":        jobName,
		"runId":      runID,
		"file":       fileName,
		"logVersion": version,
	})

	logLine := func(level, message string) {
		entry := map[string]interface{}{
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			"level":     strings.ToUpper(level),
			"message":   message,
			"job":       jobName,
			"runId":     runID,
		}
		appendLine(entry)
		appendDashboardLog(cfg, entry)
	}

	result, runErr := fn(logLine)

	appendLine(map[string]interface{}{
		"v":     1,
		"type":  "job_end",
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"job":   jobName,
		"runId": runID,
	})

	if result == nil {
		result = map[string]interface{}{}
	}
	if runErr != nil {
		result["success"] = false
		result["error"] = runErr.Error()
	} else if _, ok := result["success"]; !ok {
		result["success"] = true
	}
	return result, runErr
}

func appendDashboardLog(cfg *config.Config, entry map[string]interface{}) {
	if !cfg.Logging.EnableFile {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	jobLogMu.Lock()
	defer jobLogMu.Unlock()

	if err := os.MkdirAll(cfg.Logging.LogDir, 0o755); err != nil {
		return
	}
	allPath := filepath.Join(cfg.Logging.LogDir, "all.log")
	_ = appendToLogFile(allPath, line)
	level, _ := entry["level"].(string)
	if level != "" {
		levelPath := filepath.Join(cfg.Logging.LogDir, strings.ToLower(level)+".log")
		_ = appendToLogFile(levelPath, line)
	}
}

func appendToLogFile(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
