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

	// Parse time range with defaults if not provided
	startStr := c.QueryParam("start_time")
	endStr := c.QueryParam("end_time")

	if startStr == "" {
		// Default to 24 hours ago if not specified
		defaultStart := time.Now().Add(-24 * time.Hour)
		params.StartTime = &defaultStart
	} else {
		startTime, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid start_time format")
		}
		params.StartTime = &startTime
	}

	if endStr == "" {
		// Default to current time if not specified
		defaultEnd := time.Now()
		params.EndTime = &defaultEnd
	} else {
		endTime, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			return HandleError(c, err, http.StatusBadRequest, "Invalid end_time format")
		}
		params.EndTime = &endTime
	}

	// Validate time range
	if params.StartTime.After(*params.EndTime) {
		return HandleError(c, fmt.Errorf("invalid time range"), http.StatusBadRequest, "Start time must be before end time")
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

	// Query logs
	response, err := h.logRepo.QueryLogs(c.Request().Context(), sourceID, params)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to query logs")
	}

	return c.JSON(http.StatusOK, NewResponse(response))
}
