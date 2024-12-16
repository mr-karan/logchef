package main

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/mr-karan/logchef/internal/logs"
)

type LogHandler struct {
	service *logs.Service
}

func NewLogHandler(service *logs.Service) *LogHandler {
	return &LogHandler{
		service: service,
	}
}

func (h *LogHandler) QueryLogs(c echo.Context) error {
	sourceID := c.Param("sourceId")

	var req logs.QueryRequest
	if err := c.Bind(&req); err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid request body")
	}

	response, err := h.service.QueryLogs(c.Request().Context(), sourceID, req)
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to query logs")
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
