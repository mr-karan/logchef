package server

// Log-context ("grep -C" style) handler.

import (
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// handleGetLogContext returns logs surrounding a specific timestamp (grep -C
// for logs). Routed through the datasource service; sources whose provider
// lacks the log_context capability get a 400.
func (s *Server) handleGetLogContext(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	var req models.LogContextRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.Timestamp <= 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Timestamp is required and must be positive", models.ValidationErrorType)
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

	result, err := core.GetLogContext(c.Context(), s.datasources, sourceID, core.LogContextParams{
		TargetTimestamp: req.Timestamp,
		BeforeLimit:     beforeLimit,
		AfterLimit:      afterLimit,
		BeforeOffset:    req.BeforeOffset,
		AfterOffset:     req.AfterOffset,
		ExcludeBoundary: req.ExcludeBoundary,
	})
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Log context is not supported for this source type", models.ValidationErrorType)
		}
		s.log.Error("failed to get log context", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to retrieve log context: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
