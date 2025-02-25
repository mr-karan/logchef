package server

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"logchef/internal/clickhouse"
	"logchef/internal/logs"
	"logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// TimeSeriesRequest represents the request parameters for time series data
type TimeSeriesRequest struct {
	StartTimestamp int64                 `query:"start_timestamp"`
	EndTimestamp   int64                 `query:"end_timestamp"`
	Window         clickhouse.TimeWindow `query:"window"`
}

// handleQueryLogs handles POST /api/v1/sources/:id/logs/search
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

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

	// Execute query
	result, err := s.logsService.QueryLogs(c.Context(), id, params)
	if err != nil {
		if errors.Is(err, logs.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error querying logs: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGetTimeSeries handles GET /api/v1/sources/:id/logs/timeseries
func (s *Server) handleGetTimeSeries(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	// Parse query parameters
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	window := c.Query("window", "1h")

	if startTimeStr == "" || endTimeStr == "" {
		return SendError(c, fiber.StatusBadRequest, "start_time and end_time are required")
	}

	startTime, err := strconv.ParseInt(startTimeStr, 10, 64)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid start_time")
	}

	endTime, err := strconv.ParseInt(endTimeStr, 10, 64)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid end_time")
	}

	// Convert window to TimeWindow
	var timeWindow clickhouse.TimeWindow
	switch window {
	case "1m":
		timeWindow = clickhouse.TimeWindow1m
	case "5m":
		timeWindow = clickhouse.TimeWindow5m
	case "15m":
		timeWindow = clickhouse.TimeWindow15m
	case "1h":
		timeWindow = clickhouse.TimeWindow1h
	case "6h":
		timeWindow = clickhouse.TimeWindow6h
	case "24h":
		timeWindow = clickhouse.TimeWindow24h
	default:
		timeWindow = clickhouse.TimeWindow1h
	}

	params := clickhouse.TimeSeriesParams{
		StartTime: time.UnixMilli(startTime),
		EndTime:   time.UnixMilli(endTime),
		Window:    timeWindow,
	}

	result, err := s.logsService.GetTimeSeries(c.Context(), id, params)
	if err != nil {
		if errors.Is(err, logs.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error getting time series: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGetLogContext handles POST /api/v1/sources/:id/logs/context
func (s *Server) handleGetLogContext(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	var req models.LogContextRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Set source ID from path
	req.SourceID = id

	// Set defaults
	if req.BeforeLimit <= 0 {
		req.BeforeLimit = 5
	}
	if req.AfterLimit <= 0 {
		req.AfterLimit = 5
	}

	// Validate request
	validator := logs.NewValidator()
	if err := validator.ValidateLogContextRequest(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, err.Error())
	}

	resp, err := s.logsService.GetLogContext(c.Context(), &req)
	if err != nil {
		if errors.Is(err, logs.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error getting log context: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, resp)
}
