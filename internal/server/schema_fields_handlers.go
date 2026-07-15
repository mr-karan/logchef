package server

// Source schema and field-values handlers.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// SchemaTimeout is the maximum time to wait for a source schema inspection
// query against the configured datasource before aborting.
const SchemaTimeout = 20 * time.Second

// handleGetSourceSchema retrieves the schema (column names and types) for a specific source.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleGetSourceSchema(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Get schema via core function, bounded by SchemaTimeout so a
	// slow/misbehaving datasource can't hang the request indefinitely.
	ctx, cancel := context.WithTimeout(c.Context(), SchemaTimeout)
	defer cancel()

	schema, err := core.GetSourceSchema(ctx, s.datasources, sourceID)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("schema request timed out", "source_id", sourceID, "timeout", SchemaTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Schema inspection is not supported for this source type yet", models.ValidationErrorType)
		}
		s.log.Error("failed to get source schema", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to retrieve source schema: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, schema)
}

// handleGetFieldValues retrieves distinct values for a specific field within a time range.
// This is optimized for LowCardinality fields but works for any field.
// Access is controlled by the requireSourceAccess middleware.
// Query params:
//   - limit: max number of values to return (default 10, max 100)
//   - type: the field type from source schema (required)
//   - start_time: ISO8601 start time (required for performance)
//   - end_time: ISO8601 end time (required for performance)
//   - timezone: timezone for time conversion (optional, defaults to UTC)
//   - query: datasource-native query string (optional, filters field values by the current query)
//   - logchefql: deprecated alias for query
func (s *Server) handleGetFieldValues(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	fieldName := c.Params("fieldName")
	if fieldName == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Field name is required", models.ValidationErrorType)
	}

	// Get field type from query param (frontend already has this from source details)
	fieldType := c.Query("type", "")
	if fieldType == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Field type is required (pass from source schema)", models.ValidationErrorType)
	}

	// Parse time range parameters (required for performance)
	startTimeStr := c.Query("start_time", "")
	endTimeStr := c.Query("end_time", "")
	if startTimeStr == "" || endTimeStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Time range (start_time, end_time) is required for performance", models.ValidationErrorType)
	}

	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid start_time format (use ISO8601/RFC3339)", models.ValidationErrorType)
	}
	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid end_time format (use ISO8601/RFC3339)", models.ValidationErrorType)
	}

	timezone := c.Query("timezone", "UTC")

	// Parse optional limit query parameter (default 10, max 100)
	limit := c.QueryInt("limit", 10)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	filterQuery := c.Query("query", "")
	queryLanguage := models.QueryLanguage(c.Query("query_language", ""))
	if filterQuery == "" {
		filterQuery = c.Query("logchefql", "")
		if queryLanguage == "" && filterQuery != "" {
			queryLanguage = models.QueryLanguageLogchefQL
		}
	}

	// Create timeout context - this propagates to ClickHouse as max_execution_time
	// Also allows early termination if client disconnects (e.g., user navigates away)
	ctx, cancel := context.WithTimeout(c.Context(), FieldValuesTimeout)
	defer cancel()

	result, err := core.GetFieldValues(ctx, s.datasources, sourceID, core.FieldValuesParams{
		FieldName: fieldName,
		FieldType: fieldType,
		Language:  queryLanguage,
		StartTime: startTime,
		EndTime:   endTime,
		Timezone:  timezone,
		Limit:     limit,
		Timeout:   nil,
		QueryText: filterQuery,
	})
	if err != nil {
		// Check if the error was due to context cancellation (client disconnected)
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("field values request timed out", "source_id", sourceID, "field", fieldName, "timeout", FieldValuesTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Field values are not supported for this source type yet", models.ValidationErrorType)
		}
		if datasource.IsValidationError(err) {
			return SendErrorWithType(c, fiber.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err), models.ValidationErrorType)
		}
		s.log.Error("failed to get field values", "error", err, "source_id", sourceID, "field", fieldName)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to get field values: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGetAllFieldValues retrieves distinct values for all filterable fields within a time range.
// This is useful for populating the field sidebar with filterable values.
// Access is controlled by the requireSourceAccess middleware.
// Query params:
//   - limit: max number of values per field (default 10, max 100)
//   - start_time: ISO8601 start time (required for performance)
//   - end_time: ISO8601 end time (required for performance)
//   - timezone: timezone for time conversion (optional, defaults to UTC)
//   - query: datasource-native query string (optional, filters field values by the current query)
//   - logchefql: deprecated alias for query
func (s *Server) handleGetAllFieldValues(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Parse time range parameters (required for performance)
	startTimeStr := c.Query("start_time", "")
	endTimeStr := c.Query("end_time", "")
	if startTimeStr == "" || endTimeStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Time range (start_time, end_time) is required for performance", models.ValidationErrorType)
	}

	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid start_time format (use ISO8601/RFC3339)", models.ValidationErrorType)
	}
	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid end_time format (use ISO8601/RFC3339)", models.ValidationErrorType)
	}

	timezone := c.Query("timezone", "UTC")

	// Parse optional limit query parameter (default 10, max 100)
	limit := c.QueryInt("limit", 10)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	filterQuery := c.Query("query", "")
	queryLanguage := models.QueryLanguage(c.Query("query_language", ""))
	if filterQuery == "" {
		filterQuery = c.Query("logchefql", "")
		if queryLanguage == "" && filterQuery != "" {
			queryLanguage = models.QueryLanguageLogchefQL
		}
	}

	// Create timeout context - this propagates to ClickHouse as max_execution_time
	// Also allows early termination if client disconnects (e.g., user navigates away)
	ctx, cancel := context.WithTimeout(c.Context(), FieldValuesTimeout)
	defer cancel()

	result, err := core.GetAllFieldValues(ctx, s.datasources, sourceID, core.AllFieldValuesParams{
		Language:  queryLanguage,
		StartTime: startTime,
		EndTime:   endTime,
		Timezone:  timezone,
		Limit:     limit,
		Timeout:   nil,
		QueryText: filterQuery,
	})
	if err != nil {
		// Check if the error was due to context cancellation (client disconnected)
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("field values request timed out", "source_id", sourceID, "timeout", FieldValuesTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Field values are not supported for this source type yet", models.ValidationErrorType)
		}
		if datasource.IsValidationError(err) {
			return SendErrorWithType(c, fiber.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err), models.ValidationErrorType)
		}
		s.log.Error("failed to get field values", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to get field values: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
