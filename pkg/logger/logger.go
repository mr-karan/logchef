// Package logger provides a simple wrapper around slog for structured logging
package logger

import (
	"log/slog"
	"os"
	"path/filepath"
)

// New creates a new logger with the specified level.
// Source paths are shortened to basename (e.g., "middleware.go")
// while keeping the structured source object intact.
func New(debug bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key != slog.SourceKey {
				return a
			}
			src, ok := a.Value.Any().(*slog.Source)
			if !ok || src == nil || src.File == "" {
				return a
			}
			src.File = filepath.Base(src.File)
			return a
		},
	})

	return slog.New(handler)
}
