// Package logger provides a simple wrapper around slog for structured logging
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// New creates a new logger with the specified level.
// Source is logged as "source":"file.go:123" (short form).
func New(debug bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Shorten source from full path to "file.go:line"
			if a.Key == slog.SourceKey {
				if src, ok := a.Value.Any().(*slog.Source); ok {
					a.Value = slog.StringValue(fmt.Sprintf("%s:%d", filepath.Base(src.File), src.Line))
				}
			}
			return a
		},
	})

	return slog.New(handler)
}
