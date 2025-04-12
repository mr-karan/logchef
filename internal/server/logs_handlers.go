package server

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/logs"

	"github.com/gofiber/fiber/v2"
)

// TimeSeriesRequest represents the request parameters for time series data
type TimeSeriesRequest struct {
	StartTimestamp int64                 `query:"start_timestamp"`
	EndTimestamp   int64                 `query:"end_timestamp"`
	Window         clickhouse.TimeWindow `query:"window"`
}

// handleQueryTeamSourceLogs handles POST /api/v1/teams/:teamId/sources/:sourceId/logs/query
func (s *Server) handleQueryTeamSourceLogs(c *fiber.Ctx) error {
	teamID := c.Params("teamId")
	sourceIDStr := c.Params("sourceId")
	if teamID == "" || sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID and source ID are required")
	}

	// Convert string to int for SourceID
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	var req models.LogQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Set defaults
	if req.Limit <= 0 {
		req.Limit = 100
	}

	// Convert to ClickHouse params
	params := clickhouse.LogQueryParams{
		RawSQL: req.RawSQL,
		Limit:  req.Limit,
	}

	// If time range is provided, add it to params
	if req.StartTimestamp > 0 {
		params.StartTime = time.UnixMilli(req.StartTimestamp)
	}
	if req.EndTimestamp > 0 {
		params.EndTime = time.UnixMilli(req.EndTimestamp)
	}

	// Execute query - authorization already checked by middleware
	result, err := s.logsService.QueryLogs(c.Context(), sourceID, params)
	if err != nil {
		if errors.Is(err, logs.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error querying logs: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGetTeamSourceSchema handles GET /api/v1/teams/:teamId/sources/:sourceId/schema
func (s *Server) handleGetTeamSourceSchema(c *fiber.Ctx) error {
	teamID := c.Params("teamId")
	sourceIDStr := c.Params("sourceId")
	if teamID == "" || sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID and source ID are required")
	}

	// Convert string to int for SourceID
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	// Get source schema - authorization already checked by middleware
	schema, err := s.logsService.GetSourceSchema(c.Context(), sourceID)
	if err != nil {
		if errors.Is(err, logs.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error getting source schema: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, schema)
}

// handleGetTeamSourceHistogram handles POST /api/v1/teams/:teamId/sources/:sourceId/logs/histogram
func (s *Server) handleGetTeamSourceHistogram(c *fiber.Ctx) error {
	teamID := c.Params("teamId")
	sourceIDStr := c.Params("sourceId")

	if teamID == "" || sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID and source ID are required")
	}

	// Convert string to int for SourceID
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	// Parse request body (same structure as log query)
	var req models.LogQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Set default window
	window := "1m" // Default to 1 minute window instead of 1 hour for more granular data
	if windowParam := c.Query("window"); windowParam != "" {
		// Validate window parameter
		validWindows := map[string]bool{
			"1m":  true,
			"5m":  true,
			"15m": true,
			"1h":  true,
			"6h":  true,
			"24h": true,
		}
		if validWindows[windowParam] {
			window = windowParam
		}
	}

	// Set defaults for limit if needed
	if req.Limit <= 0 {
		req.Limit = 100
	}

	// Create histogram params
	params := logs.HistogramParams{
		StartTime: time.UnixMilli(req.StartTimestamp),
		EndTime:   time.UnixMilli(req.EndTimestamp),
		Window:    window,
		RawSQL:    req.RawSQL,
	}

	// Execute histogram query - authorization already checked by middleware
	result, err := s.logsService.GetHistogramData(c.Context(), sourceID, params)
	if err != nil {
		if errors.Is(err, logs.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error getting histogram data: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
