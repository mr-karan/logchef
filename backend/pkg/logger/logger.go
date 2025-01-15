package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"
)

// Config holds logger configuration
type Config struct {
	Level      string
	JSONOutput bool
}

// Initialize sets up the global logger
func Initialize(cfg Config) error {
	// Set log level
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = slog.LevelInfo
	}

	// Configure handler options
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Format timestamps consistently
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   a.Key,
					Value: slog.StringValue(a.Value.Time().Format(time.RFC3339)),
				}
			}
			return a
		},
	}

	// Create handler
	var handler slog.Handler
	if cfg.JSONOutput {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	// Set as default logger
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return nil
}

// Default returns the global logger
func Default() *slog.Logger {
	return slog.Default()
}

// WithContext returns a logger with request context fields
func WithContext(ctx context.Context) *slog.Logger {
	if reqID := RequestIDFromContext(ctx); reqID != "" {
		return Default().With("request_id", reqID)
	}
	return Default()
}

// RequestIDFromContext extracts request ID from context
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if reqID, ok := ctx.Value("request_id").(string); ok {
		return reqID
	}
	return ""
}

// Output returns a new logger that writes to the given writer
func Output(w io.Writer, opts *slog.HandlerOptions) *slog.Logger {
	if opts == nil {
		opts = &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		}
	}

	handler := slog.NewTextHandler(w, opts)
	return slog.New(handler)
}
