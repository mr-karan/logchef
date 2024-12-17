package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/mr-karan/logchef/internal/logs"
	"github.com/mr-karan/logchef/pkg/models"
)

func parseIntWithDefault(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return parsed
}

type LogHandler struct {
	service *logs.Service
}

func NewLogHandler(service *logs.Service) *LogHandler {
	return &LogHandler{
		service: service,
	}
}

func (h *LogHandler) GetLogs(c echo.Context) error {
	sourceID := c.Param("sourceId")

	params := models.LogQueryParams{
		TableName:    c.QueryParam("table_name"),
		Limit:        parseIntWithDefault(c.QueryParam("limit"), 50),
		Offset:       parseIntWithDefault(c.QueryParam("offset"), 0),
		ServiceName:  c.QueryParam("service_name"),
		Namespace:    c.QueryParam("namespace"),
		SeverityText: c.QueryParam("severity_text"),
		SearchQuery:  c.QueryParam("search_query"),
	}

	startTime, err := time.Parse(time.RFC3339, c.QueryParam("start_time"))
	if err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid start_time")
	}
	params.StartTime = &startTime

	endTime, err := time.Parse(time.RFC3339, c.QueryParam("end_time"))
	if err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid end_time")
	}
	params.EndTime = &endTime

	totalCount, err := h.service.GetTotalLogsCount(c.Request().Context(), sourceID, params)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to get total count")
	}

	response, err := h.service.QueryLogs(c.Request().Context(), sourceID, logs.QueryRequest{
		Mode:   logs.QueryModeBasic,
		Params: params,
	})
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to fetch logs")
	}

	hasMore := (params.Offset + params.Limit) < totalCount

	enhancedResponse := struct {
		Logs       []models.Log `json:"logs"`
		TotalCount int          `json:"total_count"`
		HasMore    bool         `json:"has_more"`
	}{
		Logs:       response.Logs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}

	return c.JSON(http.StatusOK, NewResponse(enhancedResponse))
}

func (h *LogHandler) QueryLogs(c echo.Context) error {
	sourceID := c.Param("sourceId")

	var req logs.QueryRequest
	if err := c.Bind(&req); err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid request body")
	}

	response, err := h.service.QueryLogs(c.Request().Context(), sourceID, req)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to execute query")
	}

	return c.JSON(http.StatusOK, NewResponse(response))
}

func (h *LogHandler) GetLogSchema(c echo.Context) error {
	sourceID := c.Param("sourceId")

	startTime, err := time.Parse(time.RFC3339, c.QueryParam("start_time"))
	if err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid start_time format")
	}

	endTime, err := time.Parse(time.RFC3339, c.QueryParam("end_time"))
	if err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid end_time format")
	}

	timeRange := logs.TimeRange{
		Start: &startTime,
		End:   &endTime,
	}

	schema, err := h.service.GetSchema(c.Request().Context(), sourceID, timeRange)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to get log schema")
	}

	return c.JSON(http.StatusOK, NewResponse(schema))
}
