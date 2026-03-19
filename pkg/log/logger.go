// Package log provides a structured logger for the v2raymg project.
// It wraps Go's slog package with a simple interface and a package-level
// global instance so callers can use log.Info(...) without passing a logger.
//
// Usage:
//
//	// Use global logger (defaults to INFO level, text format, stderr)
//	log.Info("server started", "port", 8080)
//	log.Warn("config missing, using defaults")
//	log.Error("failed to connect", "err", err)
//
//	// Replace global logger (e.g. at startup)
//	log.SetDefault(log.New(log.WithLevel(log.LevelDebug), log.WithFormat(log.FormatJSON)))
package log

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"
)

// Logger is the logging interface used across the project.
// All methods accept a message and optional key-value pairs (slog-style).
// The *f variants accept a format string and arguments (fmt.Sprintf-style).
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	// With returns a new Logger with the given key-value pairs pre-attached.
	With(args ...any) Logger
}

// global is the package-level default logger instance.
var global Logger = New()

// SetDefault replaces the package-level default logger.
func SetDefault(l Logger) {
	global = l
}

// Default returns the current package-level default logger.
func Default() Logger {
	return global
}

// Package-level convenience functions that delegate to the global logger.
// These use logSkip to correctly attribute the caller (one extra frame vs direct logger use).

func Debug(msg string, args ...any) { logSkip(global, slog.LevelDebug, msg, args...) }
func Info(msg string, args ...any)  { logSkip(global, slog.LevelInfo, msg, args...) }
func Warn(msg string, args ...any)  { logSkip(global, slog.LevelWarn, msg, args...) }
func Error(msg string, args ...any) { logSkip(global, slog.LevelError, msg, args...) }

func Debugf(format string, args ...any) { logSkip(global, slog.LevelDebug, fmt.Sprintf(format, args...)) }
func Infof(format string, args ...any)  { logSkip(global, slog.LevelInfo, fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { logSkip(global, slog.LevelWarn, fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { logSkip(global, slog.LevelError, fmt.Sprintf(format, args...)) }

// logSkip emits a log record with caller depth adjusted for package-level functions.
func logSkip(l Logger, level slog.Level, msg string, args ...any) {
	sl, ok := l.(*slogLogger)
	if !ok {
		// fallback for non-slog implementations
		switch level {
		case slog.LevelDebug:
			l.Debug(msg, args...)
		case slog.LevelInfo:
			l.Info(msg, args...)
		case slog.LevelWarn:
			l.Warn(msg, args...)
		default:
			l.Error(msg, args...)
		}
		return
	}
	if !sl.inner.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip: runtime.Callers, logSkip, log.{Debug,Info,...}
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = sl.inner.Handler().Handle(context.Background(), r)
}

// With returns a new logger derived from the global with pre-attached fields.
func With(args ...any) Logger { return global.With(args...) }

// slogLogger wraps slog.Logger to implement Logger.
type slogLogger struct {
	inner *slog.Logger
}

// New creates a new Logger with the given options.
// If no options are provided, defaults are used (INFO level, text format, stderr).
func New(opts ...Option) Logger {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	return &slogLogger{inner: buildSlogLogger(o)}
}

// log emits a log record with the correct caller source (skipping the wrapper layer).
func (l *slogLogger) log(level slog.Level, msg string, args ...any) {
	if !l.inner.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip: runtime.Callers, slogLogger.log, slogLogger.{Debug,Info,...}
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = l.inner.Handler().Handle(context.Background(), r)
}

func (l *slogLogger) Debug(msg string, args ...any)  { l.log(slog.LevelDebug, msg, args...) }
func (l *slogLogger) Info(msg string, args ...any)   { l.log(slog.LevelInfo, msg, args...) }
func (l *slogLogger) Warn(msg string, args ...any)   { l.log(slog.LevelWarn, msg, args...) }
func (l *slogLogger) Error(msg string, args ...any)  { l.log(slog.LevelError, msg, args...) }
func (l *slogLogger) Debugf(format string, args ...any) { l.log(slog.LevelDebug, fmt.Sprintf(format, args...)) }
func (l *slogLogger) Infof(format string, args ...any)  { l.log(slog.LevelInfo, fmt.Sprintf(format, args...)) }
func (l *slogLogger) Warnf(format string, args ...any)  { l.log(slog.LevelWarn, fmt.Sprintf(format, args...)) }
func (l *slogLogger) Errorf(format string, args ...any) { l.log(slog.LevelError, fmt.Sprintf(format, args...)) }

func (l *slogLogger) With(args ...any) Logger {
	return &slogLogger{inner: l.inner.With(args...)}
}
