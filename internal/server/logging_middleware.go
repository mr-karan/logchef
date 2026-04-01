package server

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mr-karan/logchef/pkg/models"
)

// requestLogger emits one canonical log line per HTTP request (Stripe-style).
// Runs after auth middleware so user context is available.
// See: https://stripe.com/blog/canonical-log-lines
func requestLogger(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Skip noisy paths
		path := c.Path()
		if strings.HasPrefix(path, "/api/v1/health") ||
			path == "/metrics" || path == "/ready" ||
			strings.HasPrefix(path, "/assets/") {
			return c.Next()
		}

		start := time.Now()

		// Process request
		chainErr := c.Next()

		// Capture after response is finalized (including error handler)
		duration := time.Since(start)
		status := c.Response().StatusCode()

		// If the handler returned an error that Fiber's error handler processed,
		// the status code is already set correctly on the response.
		if chainErr != nil {
			if e, ok := chainErr.(*fiber.Error); ok {
				status = e.Code
			} else if status == 200 {
				status = 500 // raw error with no status set
			}
		}

		// Build canonical log line — one line per request with all useful context
		attrs := []slog.Attr{
			slog.String("method", c.Method()),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Int64("duration_ms", duration.Milliseconds()),
		}

		// User context
		if user, ok := c.Locals("user").(*models.User); ok && user != nil {
			attrs = append(attrs, slog.String("user", user.Email))
		}

		// Resource context from path params
		if teamID := c.Params("teamID"); teamID != "" {
			attrs = append(attrs, slog.String("team_id", teamID))
		}
		if sourceID := c.Params("sourceID"); sourceID != "" {
			attrs = append(attrs, slog.String("source_id", sourceID))
		}

		// Convert to []any for slog
		args := make([]any, len(attrs))
		for i, a := range attrs {
			args[i] = a
		}

		// Single canonical log line per request
		if status >= 500 {
			log.Error("http", args...)
		} else if status >= 400 {
			log.Warn("http", args...)
		} else {
			log.Info("http", args...)
		}

		return chainErr
	}
}
