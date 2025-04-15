package server

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	// "github.com/mr-karan/logchef/internal/logs" // Removed

	"github.com/gofiber/fiber/v2"
)

// TimeSeriesRequest - consider if this is still needed or replaced by core/handler specific structs
// type TimeSeriesRequest struct {
// 	StartTimestamp int64                 `query:"start_timestamp"`
// 	EndTimestamp   int64                 `query:"end_timestamp"`
// 	Window         clickhouse.TimeWindow `query:"window"`
// }

// handleQueryLogs handles requests to query logs for a specific source.
// It expects start/end timestamps, limit, and a raw SQL query in the request body.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	var req models.LogQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Apply default limit if not specified.
	if req.Limit <= 0 {
		req.Limit = 100 // Consider making this configurable.
	}

	// Prepare parameters for the core query function.
	params := clickhouse.LogQueryParams{
		RawSQL: req.RawSQL,
		Limit:  req.Limit,
	}
	if req.StartTimestamp > 0 {
		params.StartTime = time.UnixMilli(req.StartTimestamp)
	}
	if req.EndTimestamp > 0 {
		params.EndTime = time.UnixMilli(req.EndTimestamp)
	}

	// Execute query via core function.
	result, err := core.QueryLogs(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		// Handle invalid query syntax errors specifically if core.QueryLogs returns them.
		// if errors.Is(err, core.ErrInvalidQuery) ...
		s.log.Error("failed to query logs via core function", slog.Any("error", err), "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to query logs", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGetSourceSchema retrieves the schema (column names and types) for a specific source.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleGetSourceSchema(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Get schema via core function.
	schema, err := core.GetSourceSchema(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source schema via core function", slog.Any("error", err), "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to retrieve source schema", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, schema)
}

// handleGetHistogram generates histogram data (log counts over time intervals) for a specific source.
// It accepts time range and optional filter query parameters.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleGetHistogram(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Parse time range and optional filter query from request body.
	var req models.LogQueryRequest // Re-use LogQueryRequest for convenience
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Get time window granularity from query parameter.
	window := c.Query("window", "1m") // Default to 1 minute.

	// Prepare parameters for the core histogram function.
	params := core.HistogramParams{
		StartTime: time.UnixMilli(req.StartTimestamp),
		EndTime:   time.UnixMilli(req.EndTimestamp),
		Window:    window,
		Query:     req.RawSQL, // Pass potential filter query (WHERE clause).
	}

	// Execute histogram query via core function.
	result, err := core.GetHistogramData(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}

		// Check if the error is related to an invalid window parameter
		if strings.Contains(err.Error(), "invalid histogram window") {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}

		// Handle other errors
		s.log.Error("failed to get histogram data via core function", slog.Any("error", err), "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to generate histogram data", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
