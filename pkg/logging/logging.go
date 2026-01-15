// Package logging provides structured logging for the canopy daemon using slog.
// It supports configurable log levels, JSON output, and standard fields for
// correlation (run_id, agent_id, task_id).
//
// Logs are optionally written to $XDG_CACHE_HOME/canopy/daemon.log with simple
// size-based rotation (max 10MB, keeping 3 backup files).
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	// LogFileName is the daemon log file name
	LogFileName = "daemon.log"

	// MaxLogSize is the maximum log file size before rotation (10MB)
	MaxLogSize = 10 * 1024 * 1024

	// MaxBackups is the number of backup log files to keep
	MaxBackups = 3
)

// Context keys for structured logging fields
type contextKey string

const (
	runIDKey     contextKey = "run_id"
	agentIDKey   contextKey = "agent_id"
	taskIDKey    contextKey = "task_id"
	repoIDKey    contextKey = "repo_id"
	requestIDKey contextKey = "request_id"
)

// Config holds logging configuration options
type Config struct {
	// Level sets the minimum log level (default: Info)
	Level slog.Level

	// JSON enables JSON output format (default: false for text)
	JSON bool

	// Output is the writer for log output (default: os.Stderr)
	Output io.Writer

	// AddSource includes source file and line information
	AddSource bool

	// Component is a prefix added to all log messages (e.g., "daemon", "orchestrator")
	Component string
}

// DefaultConfig returns the default logging configuration
func DefaultConfig() Config {
	return Config{
		Level:     slog.LevelInfo,
		JSON:      false,
		Output:    os.Stderr,
		AddSource: false,
	}
}

var (
	// defaultLogger is the package-level logger instance
	defaultLogger *slog.Logger
	loggerMu      sync.RWMutex

	// levelVar allows dynamic level changes
	levelVar = &slog.LevelVar{}
)

func init() {
	// Initialize with default configuration
	defaultLogger = newLogger(DefaultConfig())
}

// newLogger creates a new slog.Logger with the given configuration
func newLogger(cfg Config) *slog.Logger {
	levelVar.Set(cfg.Level)

	output := cfg.Output
	if output == nil {
		output = os.Stderr
	}

	opts := &slog.HandlerOptions{
		Level:     levelVar,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	if cfg.JSON {
		handler = slog.NewJSONHandler(output, opts)
	} else {
		handler = slog.NewTextHandler(output, opts)
	}

	// Wrap with component handler if component is specified
	if cfg.Component != "" {
		handler = &componentHandler{
			Handler:   handler,
			component: cfg.Component,
		}
	}

	return slog.New(handler)
}

// componentHandler adds a component field to all log records
type componentHandler struct {
	slog.Handler
	component string
}

func (h *componentHandler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("component", h.component))
	return h.Handler.Handle(ctx, r)
}

func (h *componentHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &componentHandler{
		Handler:   h.Handler.WithAttrs(attrs),
		component: h.component,
	}
}

func (h *componentHandler) WithGroup(name string) slog.Handler {
	return &componentHandler{
		Handler:   h.Handler.WithGroup(name),
		component: h.component,
	}
}

// Configure sets up the package-level logger with the given configuration
func Configure(cfg Config) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	defaultLogger = newLogger(cfg)
}

// SetLevel dynamically changes the logging level
func SetLevel(level slog.Level) {
	levelVar.Set(level)
}

// GetLevel returns the current logging level
func GetLevel() slog.Level {
	return levelVar.Level()
}

// Logger returns the package-level logger
func Logger() *slog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return defaultLogger
}

// New creates a new logger with the given configuration
func New(cfg Config) *slog.Logger {
	return newLogger(cfg)
}

// With returns a new logger with the given attributes
func With(args ...any) *slog.Logger {
	return Logger().With(args...)
}

// WithComponent returns a logger with a component field
func WithComponent(component string) *slog.Logger {
	return Logger().With("component", component)
}

// WithRunID returns a logger with the run_id field
func WithRunID(runID string) *slog.Logger {
	return Logger().With("run_id", runID)
}

// WithAgentID returns a logger with the agent_id field
func WithAgentID(agentID string) *slog.Logger {
	return Logger().With("agent_id", agentID)
}

// WithTaskID returns a logger with the task_id field
func WithTaskID(taskID string) *slog.Logger {
	return Logger().With("task_id", taskID)
}

// WithContext extracts run_id, agent_id, task_id, and request_id from context and returns a logger with those fields
func WithContext(ctx context.Context) *slog.Logger {
	logger := Logger()
	if runID, ok := ctx.Value(runIDKey).(string); ok && runID != "" {
		logger = logger.With("run_id", runID)
	}
	if agentID, ok := ctx.Value(agentIDKey).(string); ok && agentID != "" {
		logger = logger.With("agent_id", agentID)
	}
	if taskID, ok := ctx.Value(taskIDKey).(string); ok && taskID != "" {
		logger = logger.With("task_id", taskID)
	}
	if repoID, ok := ctx.Value(repoIDKey).(string); ok && repoID != "" {
		logger = logger.With("repo_id", repoID)
	}
	if requestID, ok := ctx.Value(requestIDKey).(string); ok && requestID != "" {
		logger = logger.With("request_id", requestID)
	}
	return logger
}

// ContextWithRunID returns a new context with the run_id value
func ContextWithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey, runID)
}

// ContextWithAgentID returns a new context with the agent_id value
func ContextWithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, agentIDKey, agentID)
}

// ContextWithTaskID returns a new context with the task_id value
func ContextWithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, taskIDKey, taskID)
}

// ContextWithRepoID returns a new context with the repo_id value
func ContextWithRepoID(ctx context.Context, repoID string) context.Context {
	return context.WithValue(ctx, repoIDKey, repoID)
}

// ContextWithRequestID returns a new context with the request_id value
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request_id from context
func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// ContextWithIDs returns a new context with all provided IDs
func ContextWithIDs(ctx context.Context, runID, agentID, taskID string) context.Context {
	if runID != "" {
		ctx = context.WithValue(ctx, runIDKey, runID)
	}
	if agentID != "" {
		ctx = context.WithValue(ctx, agentIDKey, agentID)
	}
	if taskID != "" {
		ctx = context.WithValue(ctx, taskIDKey, taskID)
	}
	return ctx
}

// Package-level convenience functions that use the default logger

// Debug logs at debug level
func Debug(msg string, args ...any) {
	Logger().Debug(msg, args...)
}

// Info logs at info level
func Info(msg string, args ...any) {
	Logger().Info(msg, args...)
}

// Warn logs at warn level
func Warn(msg string, args ...any) {
	Logger().Warn(msg, args...)
}

// Error logs at error level
func Error(msg string, args ...any) {
	Logger().Error(msg, args...)
}

// DebugContext logs at debug level with context
func DebugContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Debug(msg, args...)
}

// InfoContext logs at info level with context
func InfoContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Info(msg, args...)
}

// WarnContext logs at warn level with context
func WarnContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Warn(msg, args...)
}

// ErrorContext logs at error level with context
func ErrorContext(ctx context.Context, msg string, args ...any) {
	WithContext(ctx).Error(msg, args...)
}

// LogPath returns the full path to the daemon log file.
func LogPath() string {
	return filepath.Join(getCacheDir(), LogFileName)
}

// getCacheDir returns the cache directory for canopy, respecting XDG_CACHE_HOME
func getCacheDir() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "canopy")
}

// EnsureLogDir creates the log directory if it doesn't exist.
func EnsureLogDir() error {
	return os.MkdirAll(getCacheDir(), 0755)
}

// OpenLogFile opens the daemon log file for appending.
// Creates the file if it doesn't exist.
// The caller is responsible for closing the returned file.
func OpenLogFile() (*os.File, error) {
	if err := EnsureLogDir(); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := LogPath()

	// Check if rotation is needed before opening
	if err := rotateIfNeeded(logPath); err != nil {
		// Log rotation failure is non-fatal; we can still write to the existing file
		fmt.Fprintf(os.Stderr, "warning: log rotation failed: %v\n", err)
	}

	return os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

// SetupFileLogging configures the logger to write to both
// stderr and the daemon log file. Returns a cleanup function that
// should be called to close the log file.
func SetupFileLogging(cfg Config) (cleanup func(), err error) {
	logFile, err := OpenLogFile()
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Write to both stderr and the log file
	cfg.Output = io.MultiWriter(os.Stderr, logFile)
	Configure(cfg)

	cleanup = func() {
		logFile.Close()
	}

	return cleanup, nil
}

// rotateIfNeeded checks if the log file exceeds MaxLogSize and rotates if needed.
func rotateIfNeeded(logPath string) error {
	info, err := os.Stat(logPath)
	if os.IsNotExist(err) {
		return nil // No file to rotate
	}
	if err != nil {
		return err
	}

	if info.Size() < MaxLogSize {
		return nil // No rotation needed
	}

	return rotate(logPath)
}

// rotate performs log rotation:
// daemon.log.3 -> deleted
// daemon.log.2 -> daemon.log.3
// daemon.log.1 -> daemon.log.2
// daemon.log   -> daemon.log.1
func rotate(logPath string) error {
	// Remove oldest backup if it exists
	oldestBackup := fmt.Sprintf("%s.%d", logPath, MaxBackups)
	os.Remove(oldestBackup)

	// Shift existing backups
	for i := MaxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", logPath, i)
		newPath := fmt.Sprintf("%s.%d", logPath, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				return fmt.Errorf("failed to rename %s to %s: %w", oldPath, newPath, err)
			}
		}
	}

	// Rename current log to .1
	backup1 := fmt.Sprintf("%s.1", logPath)
	if err := os.Rename(logPath, backup1); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", logPath, backup1, err)
	}

	return nil
}

// ParseLevel parses a log level string (debug, info, warn, error)
func ParseLevel(s string) (slog.Level, error) {
	switch s {
	case "debug", "DEBUG":
		return slog.LevelDebug, nil
	case "info", "INFO", "":
		return slog.LevelInfo, nil
	case "warn", "WARN", "warning", "WARNING":
		return slog.LevelWarn, nil
	case "error", "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level: %s", s)
	}
}
