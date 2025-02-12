package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

// Initialize sets up the default structured logger
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

	// Create handler with pretty output for development
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	})

	// Set default logger
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Default returns the default logger instance
func Default() *slog.Logger {
	return defaultLogger
}
