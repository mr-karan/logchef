// Package logger provides a simple wrapper around slog for structured logging
package logger

import (
	"log/slog"
	"os"
)

// New creates a new logger with the specified level
// If component is provided, it will be added as a "component" attribute
func New(debug bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}

	// Create handler with appropriate level
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	})

	return slog.New(handler)
}
