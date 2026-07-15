package server

// Histogram data handler and its request-parsing helpers.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// HistogramTimeout is the maximum time to wait for a histogram query against
// the configured datasource (ClickHouse or VictoriaLogs) before aborting.
// Bounds both slow ClickHouse queries and VictoriaLogs response bodies that
// may otherwise be read without a deadline.
const HistogramTimeout = 30 * time.Second

// handleGetHistogram generates histogram data (log counts over time intervals) for a specific source.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleGetHistogram(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Parse request body containing time range, window, groupBy and optional filter query
	var req models.APIHistogramRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate query_text parameter - empty queries are not allowed
	if strings.TrimSpace(req.QueryText) == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "query_text parameter is required", models.ValidationErrorType)
	}

	processedQuery, errMsg := resolveHistogramQueryText(req)
	if errMsg != "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, errMsg, models.ValidationErrorType)
	}

	params, errMsg := buildHistogramParams(req, processedQuery)
	if errMsg != "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, errMsg, models.ValidationErrorType)
	}

	// Execute histogram query via core function, bounded by HistogramTimeout so
	// a slow/misbehaving datasource can't hang the request indefinitely.
	ctx, cancel := context.WithTimeout(c.Context(), HistogramTimeout)
	defer cancel()

	result, err := core.GetHistogramData(ctx, s.datasources, sourceID, params)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("histogram request timed out", "source_id", sourceID, "timeout", HistogramTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		return s.handleHistogramError(c, sourceID, err)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// resolveHistogramQueryText validates that all template variables referenced
// in the histogram query are provided, then applies substitution. errMsg is
// non-empty (and query empty) on failure.
func resolveHistogramQueryText(req models.APIHistogramRequest) (query, errMsg string) {
	// Check if the query contains variable placeholders.
	requiredVars := template.ExtractVariableNames(req.QueryText)

	// Validate that all required variables are provided.
	if len(requiredVars) > 0 && len(req.Variables) == 0 {
		return "", fmt.Sprintf("Query contains template variables (%s) but no variables were provided. Please define variable values before executing.", strings.Join(requiredVars, ", "))
	}

	// Perform template variable substitution if variables are provided.
	if len(req.Variables) == 0 {
		return req.QueryText, ""
	}

	vars := make([]template.Variable, len(req.Variables))
	for i, v := range req.Variables {
		vars[i] = template.Variable{
			Name:  v.Name,
			Type:  template.VariableType(v.Type),
			Value: v.Value,
		}
	}

	substituted, err := template.SubstituteVariables(req.QueryText, vars)
	if err != nil {
		return "", fmt.Sprintf("Variable substitution failed: %v", err)
	}
	return substituted, ""
}

// buildHistogramParams assembles core.HistogramParams from the request,
// applying window/timezone/timeout defaults and validating the time range and
// timeout. errMsg is non-empty on failure.
func buildHistogramParams(req models.APIHistogramRequest, processedQuery string) (params core.HistogramParams, errMsg string) {
	// Use window from the request body or default to 1 minute
	window := req.Window
	if window == "" {
		window = "1m" // Default to 1 minute if not specified
	}

	// Prepare parameters for the core histogram function.
	params = core.HistogramParams{
		Window:   window,
		Query:    processedQuery, // Pass processed query text containing filters and time conditions
		Timezone: req.Timezone,
	}

	startTime, endTime, err := parseHistogramTimeRange(&req)
	if err != nil {
		return params, err.Error()
	}
	params.StartTime = startTime
	params.EndTime = endTime

	// Only add groupBy if it's not empty
	if req.GroupBy != "" && strings.TrimSpace(req.GroupBy) != "" {
		params.GroupBy = req.GroupBy
	}

	// Use the provided timezone or default to UTC
	if params.Timezone == "" {
		params.Timezone = "UTC"
	}

	// Apply default timeout if not specified
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	// Validate timeout
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return params, err.Error()
	}

	// Pass the query timeout (always non-nil now)
	params.QueryTimeout = req.QueryTimeout

	return params, ""
}

// handleHistogramError maps a core.GetHistogramData error to the appropriate
// HTTP error response.
func (s *Server) handleHistogramError(c *fiber.Ctx, sourceID models.SourceID, err error) error {
	if errors.Is(err, core.ErrSourceNotFound) {
		return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}
	if errors.Is(err, datasource.ErrOperationNotSupported) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Histogram is not supported for this source type yet", models.ValidationErrorType)
	}

	// Check for specific error types
	switch {
	case strings.Contains(err.Error(), "query parameter is required"):
		return SendErrorWithType(c, fiber.StatusBadRequest, "Query parameter is required for histogram data", models.ValidationErrorType)
	case strings.Contains(err.Error(), "invalid histogram window"):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	case strings.Contains(err.Error(), "invalid"):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	default:
		// Handle other errors
		s.log.Error("failed to get histogram data", "error", err, "source_id", sourceID)
		// Pass the actual error message to the client for better debugging
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to generate histogram data: %v", err), models.DatabaseErrorType)
	}
}

func parseRFC3339TimeRange(startTimeRaw, endTimeRaw string) (startPtr, endPtr *time.Time, err error) {
	startTimeRaw = strings.TrimSpace(startTimeRaw)
	endTimeRaw = strings.TrimSpace(endTimeRaw)

	if startTimeRaw == "" && endTimeRaw == "" {
		return nil, nil, nil
	}
	if startTimeRaw == "" || endTimeRaw == "" {
		return nil, nil, fmt.Errorf("start_time and end_time must both be provided")
	}

	startTime, err := time.Parse(time.RFC3339, startTimeRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid start_time format (use ISO8601/RFC3339)")
	}
	endTime, err := time.Parse(time.RFC3339, endTimeRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid end_time format (use ISO8601/RFC3339)")
	}
	return &startTime, &endTime, nil
}

func parseHistogramTimeRange(req *models.APIHistogramRequest) (startPtr, endPtr *time.Time, err error) {
	if req == nil {
		return nil, nil, nil
	}

	if strings.TrimSpace(req.StartTime) != "" || strings.TrimSpace(req.EndTime) != "" {
		return parseRFC3339TimeRange(req.StartTime, req.EndTime)
	}

	if req.StartTimestamp == 0 && req.EndTimestamp == 0 {
		return nil, nil, nil
	}
	if req.StartTimestamp == 0 || req.EndTimestamp == 0 {
		return nil, nil, fmt.Errorf("start_timestamp and end_timestamp must both be provided")
	}

	startTime := time.UnixMilli(req.StartTimestamp)
	endTime := time.UnixMilli(req.EndTimestamp)
	return &startTime, &endTime, nil
}
