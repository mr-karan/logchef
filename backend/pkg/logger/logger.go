package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Config holds logger configuration
type Config struct {
	Level      string
	JSONOutput bool
	AppName    string // Name of the application
	Version    string // Version of the application
	Env        string // Environment (dev, prod, etc)
}

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// Context keys
	requestIDKey contextKey = "request_id"
	traceIDKey   contextKey = "trace_id"
	spanIDKey    contextKey = "span_id"

	// Default values
	defaultLevel = slog.LevelInfo
	defaultEnv   = "development"
)

// Initialize sets up the global logger
func Initialize(cfg Config) error {
	// Set log level
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = defaultLevel
	}

	// Set default environment if not provided
	if cfg.Env == "" {
		cfg.Env = defaultEnv
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
					Value: slog.StringValue(a.Value.Time().Format(time.RFC3339Nano)),
				}
			}

			// Clean up source field for better readability
			if a.Key == slog.SourceKey {
				source := a.Value.Any().(*slog.Source)
				file := filepath.Base(source.File)
				function := filepath.Base(source.Function)
				return slog.Attr{
					Key: a.Key,
					Value: slog.StringValue(fmt.Sprintf("%s:%d:%s",
						file, source.Line, function)),
				}
			}

			return a
		},
	}

	// Always use JSON handler for structured logging
	handler := slog.NewJSONHandler(os.Stdout, opts)

	// Add global attributes
	logger := slog.New(handler).With(
		"app", cfg.AppName,
		"version", cfg.Version,
		"env", cfg.Env,
		"pid", fmt.Sprintf("%d", os.Getpid()),
	)

	// Set as default logger
	slog.SetDefault(logger)
	return nil
}

// Default returns the global logger
func Default() *slog.Logger {
	return slog.Default()
}

// WithContext returns a logger with request context fields
func WithContext(ctx context.Context) *slog.Logger {
	logger := Default()

	// Add context fields if present
	if reqID := ctx.Value(requestIDKey); reqID != nil {
		logger = logger.With("request_id", reqID)
	}
	if traceID := ctx.Value(traceIDKey); traceID != nil {
		logger = logger.With("trace_id", traceID)
	}
	if spanID := ctx.Value(spanIDKey); spanID != nil {
		logger = logger.With("span_id", spanID)
	}

	// Add caller information
	if pc, file, line, ok := runtime.Caller(1); ok {
		fn := runtime.FuncForPC(pc)
		logger = logger.With(
			"caller", fmt.Sprintf("%s:%d:%s",
				filepath.Base(file),
				line,
				filepath.Base(fn.Name())),
		)
	}

	return logger
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// WithTraceID adds a trace ID to the context
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithSpanID adds a span ID to the context
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, spanIDKey, spanID)
}

// Output returns a new logger that writes to the given writer
func Output(w io.Writer, opts *slog.HandlerOptions) *slog.Logger {
	if opts == nil {
		opts = &slog.HandlerOptions{
			Level:     defaultLevel,
			AddSource: true,
		}
	}

	handler := slog.NewJSONHandler(w, opts)
	return slog.New(handler)
}
