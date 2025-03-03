// Package logger provides a simple, elegant logging solution using Go's slog package.
// It offers a minimal API with sensible defaults for application-wide logging.
package logger

import (
	"log/slog"
	"os"
)

// Global logger instance
var globalLogger *slog.Logger

// Initialize sets up the global logger with the specified log level.
// Source code information is always included for better debugging.
// The logger uses text format for better readability during development.
func Initialize(level string) {
	// Set log level
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	// Create handler with source information enabled
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	})

	// Set global logger
	globalLogger = slog.New(handler)
	slog.SetDefault(globalLogger)
}

// Default returns the global logger instance, initializing it with
// sensible defaults if not already initialized.
func Default() *slog.Logger {
	if globalLogger == nil {
		// Initialize with info level if not already initialized
		Initialize("info")
	}
	return globalLogger
}

// NewLogger creates a new logger with the given component name
func NewLogger(name string) *slog.Logger {
	return Default().With("component", name)
}

// With creates a new logger with the given attributes
func With(attrs ...any) *slog.Logger {
	return Default().With(attrs...)
}
