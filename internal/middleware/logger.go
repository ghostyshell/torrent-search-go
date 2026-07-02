package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
	"torrent-search-go/internal/config"
)

// Logger wraps slog for structured logging
type Logger struct {
	*slog.Logger
	cfg       *config.Config
	fileMu    sync.Mutex
	dashboard bool
}

// ResponseWriter wraps http.ResponseWriter to capture status code and size
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *ResponseWriter) WriteHeader(code int) {
	if rw.written {
		return
	}
	rw.statusCode = code
	rw.written = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = 200
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// NewLogger creates a new logger instance
func NewLogger(cfg *config.Config) *Logger {
	levelOpts := &slog.HandlerOptions{Level: getLogLevel(cfg.Logging.Level)}
	var handler slog.Handler

	switch {
	case cfg.Logging.EnableFile && cfg.Logging.EnableConsole:
		if fileHandler := newFileLogHandler(cfg, levelOpts); fileHandler != nil {
			handler = &fanoutHandler{handlers: []slog.Handler{
				slog.NewJSONHandler(os.Stdout, levelOpts),
				fileHandler,
			}}
		} else {
			handler = slog.NewJSONHandler(os.Stdout, levelOpts)
		}
	case cfg.Logging.EnableFile:
		if fileHandler := newFileLogHandler(cfg, levelOpts); fileHandler != nil {
			handler = fileHandler
		} else {
			handler = slog.NewJSONHandler(os.Stdout, levelOpts)
		}
	default:
		handler = slog.NewJSONHandler(os.Stdout, levelOpts)
	}

	return &Logger{
		Logger:    slog.New(handler),
		cfg:       cfg,
		dashboard: cfg.Logging.EnableFile,
	}
}

type fanoutHandler struct {
	handlers []slog.Handler
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		out[i] = handler.WithAttrs(attrs)
	}
	return &fanoutHandler{handlers: out}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		out[i] = handler.WithGroup(name)
	}
	return &fanoutHandler{handlers: out}
}

func newFileLogHandler(cfg *config.Config, opts *slog.HandlerOptions) slog.Handler {
	if err := os.MkdirAll(cfg.Logging.LogDir, 0755); err != nil {
		return nil
	}
	logFile := filepath.Join(cfg.Logging.LogDir, "app.log")
	rotator := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     7,
		Compress:   true,
	}
	return slog.NewJSONHandler(rotator, opts)
}

// getLogLevel converts string level to slog level
func getLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithRequestID adds a request ID to the logger
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		Logger:    l.Logger.With(slog.String("requestId", requestID)),
		cfg:       l.cfg,
		dashboard: l.dashboard,
	}
}

// WithGroup creates a new logger with a group
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		Logger:    l.Logger.WithGroup(name),
		cfg:       l.cfg,
		dashboard: l.dashboard,
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, args ...interface{}) {
	l.Logger.Debug(msg, args...)
}

// Info logs an info message
func (l *Logger) Info(msg string, args ...interface{}) {
	l.Logger.Info(msg, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.Logger.Warn(msg, args...)
}

// Error logs an error message
func (l *Logger) Error(msg string, args ...interface{}) {
	l.Logger.Error(msg, args...)
}

// LogRequest logs an HTTP request
func (l *Logger) LogRequest(method, path string, statusCode int, duration time.Duration, args ...interface{}) {
	l.Info("HTTP request",
		append([]interface{}{
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", statusCode),
			slog.Duration("duration", duration),
		}, args...)...,
	)

	if l.dashboard {
		l.writeDashboardLog("info", "HTTP request", map[string]interface{}{
			"method":   method,
			"path":     path,
			"url":      path,
			"status":   statusCode,
			"duration": duration.Milliseconds(),
		})
	}
}

func (l *Logger) writeDashboardLog(level, message string, meta map[string]interface{}) {
	if !l.cfg.Logging.EnableFile {
		return
	}
	entry := map[string]interface{}{
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"level":       strings.ToUpper(level),
		"message":     message,
		"environment": l.cfg.Environment,
	}
	for k, v := range meta {
		entry[k] = v
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	if err := os.MkdirAll(l.cfg.Logging.LogDir, 0o755); err != nil {
		return
	}
	allPath := filepath.Join(l.cfg.Logging.LogDir, "all.log")
	_ = appendToFile(allPath, line)
	if level != "all" {
		levelPath := filepath.Join(l.cfg.Logging.LogDir, level+".log")
		_ = appendToFile(levelPath, line)
	}
}

func appendToFile(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// LogJobStart logs the start of a background job
func (l *Logger) LogJobStart(jobName string, args ...interface{}) {
	l.Info("Job started",
		append([]interface{}{
			slog.String("job", jobName),
			slog.Time("startTime", time.Now().UTC()),
		}, args...)...,
	)
}

// LogJobComplete logs the completion of a background job
func (l *Logger) LogJobComplete(jobName string, duration time.Duration, success bool, args ...interface{}) {
	l.Info("Job completed",
		append([]interface{}{
			slog.String("job", jobName),
			slog.Duration("duration", duration),
			slog.Bool("success", success),
			slog.Time("endTime", time.Now().UTC()),
		}, args...)...,
	)
}

// LogJobError logs an error in a background job
func (l *Logger) LogJobError(jobName string, err error, args ...interface{}) {
	l.Error("Job error",
		append([]interface{}{
			slog.String("job", jobName),
			slog.String("error", err.Error()),
		}, args...)...,
	)
}
