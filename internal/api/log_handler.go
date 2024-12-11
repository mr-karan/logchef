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

	// Parse time range
	if startTimeStr := c.QueryParam("start_time"); startTimeStr != "" {
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid start_time format")
		}
		params.StartTime = &startTime
	}

	if endTimeStr := c.QueryParam("end_time"); endTimeStr != "" {
		endTime, err := time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid end_time format")
		}
		params.EndTime = &endTime
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

	// Get time range from query params or use defaults
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()

	if startStr := c.QueryParam("start_time"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = t
		}
	}
	if endStr := c.QueryParam("end_time"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = t
		}
	}

	schema, err := h.logRepo.GetLogSchema(c.Request().Context(), sourceID, startTime, endTime)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to get log schema")
	}

	return c.JSON(http.StatusOK, NewResponse(schema))
}
