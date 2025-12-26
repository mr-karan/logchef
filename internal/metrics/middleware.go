package metrics

import (
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/pkg/models"
)

// MetricsConfig holds configuration for the metrics middleware
type MetricsConfig struct {
	// EnableResponseSizeMetrics enables response size tracking (may impact performance)
	EnableResponseSizeMetrics bool
	// PathNormalizer normalizes endpoint paths for consistent labeling
	PathNormalizer func(path string) string
}

// DefaultConfig returns default configuration for metrics middleware
func DefaultConfig() MetricsConfig {
	return MetricsConfig{
		EnableResponseSizeMetrics: true,
		PathNormalizer:            NormalizeEndpointPath,
	}
}

// Middleware returns a Fiber middleware that collects HTTP metrics
func Middleware(config ...MetricsConfig) fiber.Handler {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(c *fiber.Ctx) error {
		// Skip metrics collection for the metrics endpoint itself
		if c.Path() == "/metrics" {
			return c.Next()
		}

		// Record start time and increment active requests
		start := time.Now()
		IncrementActiveRequests()

		// Ensure we decrement active requests even if panic occurs
		defer DecrementActiveRequests()

		// Process the request
		err := c.Next()

		// Calculate duration
		duration := time.Since(start)

		// Get normalized endpoint path
		endpoint := cfg.PathNormalizer(c.Route().Path)
		method := c.Method()
		statusCode := c.Response().StatusCode()

		// Calculate response size if enabled
		var responseSize int64
		if cfg.EnableResponseSizeMetrics {
			responseSize = int64(len(c.Response().Body()))
		}

		// Get user context if available for enhanced metrics
		var user *models.User
		if userFromContext, ok := c.Locals("user").(*models.User); ok {
			user = userFromContext
		}

		// Record HTTP request metrics with user context
		RecordHTTPRequest(method, endpoint, statusCode, duration, responseSize, user)

		// If there was an error, try to extract error type from locals
		if err != nil || statusCode >= 400 {
			errorType := extractErrorType(c, err)
			RecordHTTPError(method, endpoint, errorType, statusCode, user)
		}

		return err
	}
}

// NormalizeEndpointPath normalizes Fiber route paths for consistent metric labeling
func NormalizeEndpointPath(path string) string {
	// If path is empty or just "/", return it as-is
	if path == "" || path == "/" {
		return "/"
	}

	// Remove trailing slash for consistency
	if strings.HasSuffix(path, "/") && len(path) > 1 {
		path = path[:len(path)-1]
	}

	// Fiber route paths already contain parameter placeholders like :teamID
	// We just need to normalize some common variations
	normalized := path

	// Replace some common parameter patterns for better grouping
	replacements := map[string]string{
		":userID":       "{userID}",
		":teamID":       "{teamID}",
		":sourceID":     "{sourceID}",
		":tokenID":      "{tokenID}",
		":collectionID": "{collectionID}",
	}

	for old, new := range replacements {
		normalized = strings.ReplaceAll(normalized, old, new)
	}

	return normalized
}

// extractErrorType attempts to extract error type information from the Fiber context
func extractErrorType(c *fiber.Ctx, _ error) string {
	// First, try to get error type from response locals (if set by error handlers)
	if errorType, ok := c.Locals("error_type").(string); ok {
		return errorType
	}

	// Fallback to categorizing by status code
	statusCode := c.Response().StatusCode()
	switch {
	case statusCode == 400:
		return "validation_error"
	case statusCode == 401:
		return "authentication_error"
	case statusCode == 403:
		return "authorization_error"
	case statusCode == 404:
		return "not_found_error"
	case statusCode == 409:
		return "conflict_error"
	case statusCode == 422:
		return "validation_error"
	case statusCode >= 500:
		return "internal_error"
	case statusCode >= 400:
		return "client_error"
	default:
		return "unknown_error"
	}
}

// MetricsHandler returns a Fiber handler that serves Prometheus metrics
func MetricsHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")

		// Get metrics in Prometheus format
		metrics.WritePrometheus(c.Response().BodyWriter(), true)

		return nil
	}
}
