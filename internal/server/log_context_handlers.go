package server

// Log-context ("grep -C" style) handler.

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// handleGetLogContext returns logs surrounding a specific timestamp (grep -C
// for logs). Routed through the datasource service; sources whose provider
// lacks the log_context capability get a 400.
func (s *Server) handleGetLogContext(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	sourceID := src.SourceID()

	var req models.LogContextRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.Timestamp <= 0 && req.TimestampRFC3339 == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Timestamp is required and must be positive", models.ValidationErrorType)
	}
	targetTime := time.UnixMilli(req.Timestamp)
	if req.TimestampRFC3339 != "" {
		parsed, err := time.Parse(time.RFC3339Nano, req.TimestampRFC3339)
		if err != nil || parsed.IsZero() {
			return SendErrorWithType(c, fiber.StatusBadRequest, "timestamp_rfc3339 must be a valid RFC3339 timestamp", models.ValidationErrorType)
		}
		targetTime = parsed.UTC()
	}

	beforeLimit := req.BeforeLimit
	if beforeLimit <= 0 {
		beforeLimit = 10
	}
	afterLimit := req.AfterLimit
	if afterLimit <= 0 {
		afterLimit = 10
	}
	// Cap limits to prevent excessive queries
	if beforeLimit > 100 {
		beforeLimit = 100
	}
	if afterLimit > 100 {
		afterLimit = 100
	}

	result, err := core.GetLogContext(c.Context(), s.datasources, src, core.LogContextParams{
		TargetTimestamp: targetTime.UnixMilli(),
		TargetTime:      &targetTime,
		BeforeLimit:     beforeLimit,
		AfterLimit:      afterLimit,
		BeforeOffset:    req.BeforeOffset,
		AfterOffset:     req.AfterOffset,
		ExcludeBoundary: req.ExcludeBoundary,
	})
	if err != nil {
		return s.sendQueryError(c, sourceID, err, "Failed to retrieve log context", "Log context")
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
