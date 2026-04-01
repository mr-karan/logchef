// Package logger provides a simple wrapper around slog for structured logging
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// New creates a new logger with the specified level.
// Source is logged as a flat "source":"file.go:77" string.
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
			a.Value = slog.StringValue(fmt.Sprintf("%s:%d", filepath.Base(src.File), src.Line))
			return a
		},
	})

	return slog.New(handler)
}
