package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/internal/models"
)

type LogHandler struct {
	logRepo    *db.LogRepository
	sourceRepo *models.SourceRepository
}

func NewLogHandler(logRepo *db.LogRepository, sourceRepo *models.SourceRepository) *LogHandler {
	return &LogHandler{
		logRepo:    logRepo,
		sourceRepo: sourceRepo,
	}
}

// Add this helper function at the top of the file
func validateTimeRange(start, end *time.Time) error {
	// Ensure both times are provided
	if start == nil || end == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Both start_time and end_time are required")
	}

	// Ensure start time is not after end time
	if start.After(*end) {
		return echo.NewHTTPError(http.StatusBadRequest, "start_time cannot be after end_time")
	}

	return nil
}

// QueryLogs handles log querying with filters and pagination
func (h *LogHandler) QueryLogs(c echo.Context) error {
	sourceID := c.Param("sourceId")

	// Validate source exists and get table name
	source, err := h.sourceRepo.Get(sourceID)
	if err != nil {
		return HandleError(c, err, http.StatusNotFound, "Source not found")
	}

	// Parse query parameters
	params := models.LogQueryParams{
		TableName: source.TableName,
	}

	// Parse and validate time range
	startTime, err := time.Parse(time.RFC3339, c.QueryParam("start_time"))
	if err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid start_time format")
	}
	params.StartTime = &startTime

	endTime, err := time.Parse(time.RFC3339, c.QueryParam("end_time"))
	if err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid end_time format")
	}
	params.EndTime = &endTime

	// Validate time range
	if err := validateTimeRange(params.StartTime, params.EndTime); err != nil {
		return err
	}

	// Parse other filters
	params.ServiceName = c.QueryParam("service_name")
	params.Namespace = c.QueryParam("namespace")
	params.SeverityText = c.QueryParam("severity_text")
	params.SearchQuery = c.QueryParam("q")

	// Parse pagination
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid limit")
		}
		params.Limit = limit
	}

	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid offset")
		}
		params.Offset = offset
	}

	// Set default limit if not provided
	if params.Limit == 0 {
		params.Limit = 100 // default limit
	}

	// Query logs
	response, err := h.logRepo.QueryLogs(c.Request().Context(), sourceID, params)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to query logs")
	}

	return c.JSON(http.StatusOK, NewResponse(response))
}

// GetLogSchema returns the schema for a given source based on recent logs
func (h *LogHandler) GetLogSchema(c echo.Context) error {
	sourceID := c.Param("sourceId")

	// Parse time range from query params
	var startTime, endTime time.Time
	var err error

	if startStr := c.QueryParam("start_time"); startStr != "" {
		startTime, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid start_time format")
		}
	} else {
		startTime = time.Now().Add(-1 * time.Hour)
	}

	if endStr := c.QueryParam("end_time"); endStr != "" {
		endTime, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid end_time format")
		}
	} else {
		endTime = time.Now()
	}

	// Validate time range
	if err := validateTimeRange(&startTime, &endTime); err != nil {
		return err
	}

	schema, err := h.logRepo.GetLogSchema(c.Request().Context(), sourceID, startTime, endTime)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to get log schema")
	}

	return c.JSON(http.StatusOK, NewResponse(schema))
}
