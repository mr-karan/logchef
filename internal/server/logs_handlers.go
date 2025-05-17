package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
	// "github.com/mr-karan/logchef/internal/logs" // Removed
)

// TimeSeriesRequest - consider if this is still needed or replaced by core/handler specific structs
// type TimeSeriesRequest struct {
// 	StartTimestamp int64                 `query:"start_timestamp"`
// 	EndTimestamp   int64                 `query:"end_timestamp"`
// 	Window         clickhouse.TimeWindow `query:"window"`
// }

// Added constant
const (
	// OpenAIRequestTimeout is the maximum time to wait for OpenAI to respond
	OpenAIRequestTimeout = 15 * time.Second
)

// handleQueryLogs handles requests to query logs for a specific source.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	var req models.APIQueryRequest
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
	// StartTime, EndTime, and Timezone are no longer passed here;
	// they are expected to be baked into the RawSQL by the frontend.

	// Execute query via core function.
	result, err := core.QueryLogs(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		// Handle invalid query syntax errors specifically if core.QueryLogs returns them.
		// if errors.Is(err, core.ErrInvalidQuery) ...
		s.log.Error("failed to query logs via core function", slog.Any("error", err), "source_id", sourceID)
		// Pass the actual error message to the client for better debugging
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to query logs: %v", err), models.DatabaseErrorType)
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
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to retrieve source schema: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, schema)
}

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

	// Validate raw_sql parameter - empty SQL is not allowed
	if strings.TrimSpace(req.RawSQL) == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "raw_sql parameter is required", models.ValidationErrorType)
	}

	// Use window from the request body or default to 1 minute
	window := req.Window
	if window == "" {
		window = "1m" // Default to 1 minute if not specified
	}

	// Prepare parameters for the core histogram function.
	params := core.HistogramParams{
		Window: window,
		Query:  req.RawSQL, // Pass raw SQL containing filters and time conditions
	}

	// Only add groupBy if it's not empty
	if req.GroupBy != "" && strings.TrimSpace(req.GroupBy) != "" {
		params.GroupBy = req.GroupBy
	}

	// Use the provided timezone or default to UTC
	if req.Timezone != "" {
		params.Timezone = req.Timezone
	} else {
		params.Timezone = "UTC"
	}

	// Execute histogram query via core function.
	result, err := core.GetHistogramData(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
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
			s.log.Error("failed to get histogram data via core function", slog.Any("error", err), "source_id", sourceID)
			// Pass the actual error message to the client for better debugging
			return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to generate histogram data: %v", err), models.DatabaseErrorType)
		}
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGenerateAISQL handles the generation of SQL from natural language queries
func (s *Server) handleGenerateAISQL(c *fiber.Ctx) error {
	// Check if AI features are enabled in the configuration first.
	if !s.config.AI.Enabled {
		s.log.Warn("attempt to use AI SQL generation when AI features are disabled")
		// Using GeneralErrorType for now, consider defining specific error types like FeatureDisabledErrorType
		return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not enabled on this server.", models.GeneralErrorType)
	}

	// Check for API Key if AI is enabled
	if s.config.AI.APIKey == "" {
		s.log.Error("AI features are enabled, but OpenAI API key is not configured.")
		// Using GeneralErrorType for now, consider defining specific error types like ConfigurationErrorType
		return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not properly configured on this server (missing API key).", models.GeneralErrorType)
	}

	// Parse source ID from URL parameter
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, http.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}

	// Parse team ID from URL parameter
	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, http.StatusBadRequest, "Invalid team ID", models.ValidationErrorType)
	}

	// Get user claims from context
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, http.StatusUnauthorized, "Unauthorized, user context not found", models.AuthenticationErrorType)
	}

	// Verify user has access to the source
	hasAccess, err := core.UserHasAccessToTeamSource(c.Context(), s.sqlite, s.log, user.ID, teamID, sourceID)
	if err != nil {
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to verify source access", models.GeneralErrorType)
	}
	if !hasAccess {
		return SendErrorWithType(c, http.StatusForbidden, "You don't have access to this source", models.AuthorizationErrorType)
	}

	// Parse request body using the new struct name
	var req models.GenerateSQLRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, http.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate request
	if req.NaturalLanguageQuery == "" {
		return SendErrorWithType(c, http.StatusBadRequest, "Natural language query is required", models.ValidationErrorType)
	}

	// Get the source to verify it exists and is connected
	source, err := core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if source == nil {
		return SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}

	if !source.IsConnected {
		health := s.clickhouse.GetCachedHealth(sourceID)
		if health.Status != models.HealthStatusHealthy {
			return SendErrorWithType(c, http.StatusServiceUnavailable,
				fmt.Sprintf("Source is not currently connected: %s", health.Error), models.ExternalServiceErrorType)
		}
	}

	client, err := s.clickhouse.GetConnection(sourceID)
	if err != nil {
		s.log.Error("failed to get clickhouse client", slog.Any("error", err), "source_id", sourceID)
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to connect to source", models.ExternalServiceErrorType)
	}

	tableInfo, err := client.GetTableInfo(c.Context(), source.Connection.Database, source.Connection.TableName)
	if err != nil {
		s.log.Error("failed to get source schema", slog.Any("error", err), "source_id", sourceID)
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source schema", models.ExternalServiceErrorType)
	}

	formattedColumns := make([]map[string]interface{}, 0, len(tableInfo.Columns))
	for _, col := range tableInfo.Columns {
		formattedColumns = append(formattedColumns, map[string]interface{}{
			"name": col.Name,
			"type": col.Type,
		})
	}

	if len(tableInfo.SortKeys) > 0 {
		formattedColumns = append(formattedColumns, map[string]interface{}{
			"name": "_sort_keys",
			"keys": tableInfo.SortKeys,
			"note": "The columns above are sort keys. Queries filtered by these columns will be faster.",
		})
	}

	schemaJSON, err := json.MarshalIndent(formattedColumns, "", "  ")
	if err != nil {
		s.log.Error("failed to marshal schema to JSON", slog.Any("error", err))
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to process schema", models.GeneralErrorType)
	}

	tableName := source.GetFullTableName()

	ctx, cancel := context.WithTimeout(c.Context(), OpenAIRequestTimeout)
	defer cancel()

	aiClient, err := ai.NewClient(ai.ClientOptions{
		APIKey:      s.config.AI.APIKey,
		Model:       s.config.AI.Model,
		MaxTokens:   s.config.AI.MaxTokens,
		Temperature: s.config.AI.Temperature,
		Timeout:     OpenAIRequestTimeout,
		BaseURL:     s.config.AI.BaseURL,
	}, s.log)

	if err != nil {
		s.log.Error("failed to initialize OpenAI client", slog.Any("error", err))
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to initialize AI client", models.ExternalServiceErrorType)
	}

	generatedSQL, err := aiClient.GenerateSQL(
		ctx,
		req.NaturalLanguageQuery,
		string(schemaJSON),
		tableName,
	)

	if err != nil {
		if errors.Is(err, ai.ErrInvalidSQLGeneratedByAI) {
			s.log.Warn("AI failed to generate valid SQL", "query", req.NaturalLanguageQuery, "error", err)
			return SendErrorWithType(c, http.StatusBadRequest, fmt.Sprintf("AI could not generate a valid SQL query from your input: %v", err), models.ValidationErrorType)
		}
		s.log.Error("failed to generate SQL using AI client", slog.Any("error", err))
		return SendErrorWithType(c, http.StatusInternalServerError, fmt.Sprintf("Failed to generate SQL: %v", err), models.ExternalServiceErrorType)
	}

	return SendSuccess(c, http.StatusOK, models.GenerateSQLResponse{
		SQLQuery: generatedSQL,
	})
}
