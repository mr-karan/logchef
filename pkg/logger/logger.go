package logger

import (
	"io"
	"log/slog"
	"os"
)

// Default values for logger configuration
const (
	DefaultLevel       = "info"
	DefaultFormat      = "text"
	DefaultAddSource   = true
	DefaultEnvironment = "development"
)

// Global logger instance
var globalLogger *slog.Logger

// Config holds logger configuration options
type Config struct {
	// Level sets the minimum log level
	Level string
	// AddSource adds source code information to log entries
	AddSource bool
	// Output is where logs are written (defaults to os.Stdout)
	Output io.Writer
	// Format specifies the log format ("text" or "json")
	Format string
}

// Setup initializes the global logger with the provided configuration
func Setup(cfg Config) {
	// Apply defaults for empty values
	level := cfg.Level
	if level == "" {
		level = DefaultLevel
	}

	format := cfg.Format
	if format == "" {
		format = DefaultFormat
	}

	// Set default output if not specified
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}

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

	// Create handler based on format
	var handler slog.Handler
	handlerOpts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: cfg.AddSource,
	}

	if format == "json" {
		handler = slog.NewJSONHandler(output, handlerOpts)
	} else {
		handler = slog.NewTextHandler(output, handlerOpts)
	}

	// Set global logger
	globalLogger = slog.New(handler)
	slog.SetDefault(globalLogger)
}

// Initialize is a backward-compatible function for simple logger initialization
func Initialize(level string) {
	Setup(Config{
		Level:     level,
		AddSource: DefaultAddSource,
		Format:    DefaultFormat,
	})
}

// Default returns the global logger instance
func Default() *slog.Logger {
	if globalLogger == nil {
		// Initialize with sensible defaults if not already initialized
		Setup(Config{
			Level:     DefaultLevel,
			AddSource: DefaultAddSource,
			Format:    DefaultFormat,
		})
	}
	return globalLogger
}

// With creates a new logger with the given attributes
func With(attrs ...any) *slog.Logger {
	return Default().With(attrs...)
}

// NewLogger creates a new logger with the given component name
func NewLogger(name string) *slog.Logger {
	return Default().With("component", name)
}
