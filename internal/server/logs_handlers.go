package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
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
	// FieldValuesTimeout is the maximum time to wait for field values queries
	// This propagates to ClickHouse as max_execution_time via the context deadline
	FieldValuesTimeout = 15 * time.Second
)

// QueryTracker manages active queries for cancellation support
type QueryTracker struct {
	mu      sync.RWMutex
	queries map[string]*ActiveQuery
}

// ActiveQuery represents an active query with its context for cancellation
type ActiveQuery struct {
	ID        string
	UserID    models.UserID
	SourceID  models.SourceID
	TeamID    models.TeamID
	StartTime time.Time
	SQL       string
	Cancel    context.CancelFunc
}

// Global query tracker instance
var queryTracker = &QueryTracker{
	queries: make(map[string]*ActiveQuery),
}

// AddQuery adds a new active query to the tracker
func (qt *QueryTracker) AddQuery(userID models.UserID, sourceID models.SourceID, teamID models.TeamID, sql string, cancel context.CancelFunc) string {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	queryID := uuid.New().String()
	qt.queries[queryID] = &ActiveQuery{
		ID:        queryID,
		UserID:    userID,
		SourceID:  sourceID,
		TeamID:    teamID,
		StartTime: time.Now(),
		SQL:       sql,
		Cancel:    cancel,
	}

	return queryID
}

// RemoveQuery removes a query from the tracker
func (qt *QueryTracker) RemoveQuery(queryID string) {
	qt.mu.Lock()
	defer qt.mu.Unlock()
	delete(qt.queries, queryID)
}

// CancelQuery cancels a query if it exists and belongs to the user
func (qt *QueryTracker) CancelQuery(queryID string, userID models.UserID) bool {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	query, exists := qt.queries[queryID]
	if !exists {
		return false
	}

	// Only allow users to cancel their own queries
	if query.UserID != userID {
		return false
	}

	// Cancel the context
	query.Cancel()

	// Remove from tracker
	delete(qt.queries, queryID)

	return true
}

// GetUserQueries returns all active queries for a user
func (qt *QueryTracker) GetUserQueries(userID models.UserID) []*ActiveQuery {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	var userQueries []*ActiveQuery
	for _, query := range qt.queries {
		if query.UserID == userID {
			userQueries = append(userQueries, query)
		}
	}

	return userQueries
}

// Cleanup removes queries that have been running for too long (over 1 hour)
func (qt *QueryTracker) Cleanup() {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	for queryID, query := range qt.queries {
		if query.StartTime.Before(cutoff) {
			query.Cancel()
			delete(qt.queries, queryID)
		}
	}
}

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

	// Apply default timeout if not specified
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	// Validate timeout
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	// Get user information for query tracking
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	// Get team ID from params
	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}

	// Check if SQL contains variable placeholders.
	requiredVars := template.ExtractVariableNames(req.RawSQL)

	// Validate that all required variables are provided.
	if len(requiredVars) > 0 && len(req.Variables) == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query contains template variables (%s) but no variables were provided. Please define variable values before executing.", strings.Join(requiredVars, ", ")),
			models.ValidationErrorType)
	}

	// Perform template variable substitution if variables are provided.
	processedSQL := req.RawSQL
	if len(req.Variables) > 0 {
		vars := make([]template.Variable, len(req.Variables))
		for i, v := range req.Variables {
			vars[i] = template.Variable{
				Name:  v.Name,
				Type:  template.VariableType(v.Type),
				Value: v.Value,
			}
		}

		substituted, err := template.SubstituteVariables(req.RawSQL, vars)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest,
				fmt.Sprintf("Variable substitution failed: %v", err), models.ValidationErrorType)
		}
		processedSQL = substituted
	}

	// Create a cancellable context for this query
	queryCtx, cancel := context.WithCancel(c.Context())
	defer cancel() // Ensure cleanup

	// Add query to tracker (use original SQL for tracking, substituted for execution)
	queryID := queryTracker.AddQuery(user.ID, sourceID, teamID, req.RawSQL, cancel)
	defer queryTracker.RemoveQuery(queryID) // Ensure cleanup

	// Prepare parameters for the core query function.
	params := clickhouse.LogQueryParams{
		RawSQL:       processedSQL,
		Limit:        req.Limit,
		QueryTimeout: req.QueryTimeout, // Always non-nil now
	}
	// StartTime, EndTime, and Timezone are no longer passed here;
	// they are expected to be baked into the RawSQL by the frontend.

	// Execute query via core function with cancellable context.
	result, err := core.QueryLogs(queryCtx, s.sqlite, s.clickhouse, s.log, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		// Handle invalid query syntax errors specifically if core.QueryLogs returns them.
		// if errors.Is(err, core.ErrInvalidQuery) ...
		s.log.Error("failed to query logs", "error", err, "source_id", sourceID)
		// Pass the actual error message to the client for better debugging
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to query logs: %v", err), models.DatabaseErrorType)
	}

	// Add query ID to the response for frontend tracking
	if result != nil {
		// Create a map to include the query ID with the result
		responseWithQueryID := map[string]interface{}{
			"query_id": queryID,
			"data":     result.Logs,
			"stats":    result.Stats,
			"columns":  result.Columns,
		}
		return SendSuccess(c, fiber.StatusOK, responseWithQueryID)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleCancelQuery cancels a running query for a specific source
func (s *Server) handleCancelQuery(c *fiber.Ctx) error {
	// Get query ID from params
	queryID := c.Params("queryID")
	if queryID == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Query ID is required", models.ValidationErrorType)
	}

	// Get user information
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	// Try to cancel the query
	cancelled := queryTracker.CancelQuery(queryID, user.ID)
	if !cancelled {
		return SendErrorWithType(c, fiber.StatusNotFound, "Query not found or already completed", models.NotFoundErrorType)
	}

	s.log.Debug("query cancelled", "query_id", queryID, "user_id", user.ID)

	return SendSuccess(c, fiber.StatusOK, map[string]interface{}{
		"message":  "Query cancelled successfully",
		"query_id": queryID,
	})
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
		s.log.Error("failed to get source schema", "error", err, "source_id", sourceID)
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

	// Check if SQL contains variable placeholders.
	requiredVars := template.ExtractVariableNames(req.RawSQL)

	// Validate that all required variables are provided.
	if len(requiredVars) > 0 && len(req.Variables) == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query contains template variables (%s) but no variables were provided. Please define variable values before executing.", strings.Join(requiredVars, ", ")),
			models.ValidationErrorType)
	}

	// Perform template variable substitution if variables are provided.
	processedSQL := req.RawSQL
	if len(req.Variables) > 0 {
		vars := make([]template.Variable, len(req.Variables))
		for i, v := range req.Variables {
			vars[i] = template.Variable{
				Name:  v.Name,
				Type:  template.VariableType(v.Type),
				Value: v.Value,
			}
		}

		substituted, err := template.SubstituteVariables(req.RawSQL, vars)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest,
				fmt.Sprintf("Variable substitution failed: %v", err), models.ValidationErrorType)
		}
		processedSQL = substituted
	}

	// Use window from the request body or default to 1 minute
	window := req.Window
	if window == "" {
		window = "1m" // Default to 1 minute if not specified
	}

	// Prepare parameters for the core histogram function.
	params := core.HistogramParams{
		Window: window,
		Query:  processedSQL, // Pass processed SQL containing filters and time conditions
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

	// Apply default timeout if not specified
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	// Validate timeout
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	// Pass the query timeout (always non-nil now)
	params.QueryTimeout = req.QueryTimeout

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
			s.log.Error("failed to get histogram data", "error", err, "source_id", sourceID)
			// Pass the actual error message to the client for better debugging
			return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to generate histogram data: %v", err), models.DatabaseErrorType)
		}
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// handleGenerateAISQL handles the generation of SQL from natural language queries
func (s *Server) handleGenerateAISQL(c *fiber.Ctx) error {
	if err := s.validateAIConfig(); err != nil {
		return err(c)
	}

	sourceID, teamID, err := s.parseSourceTeamIDs(c)
	if err != nil {
		return err
	}

	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, http.StatusUnauthorized, "Unauthorized", models.AuthenticationErrorType)
	}

	hasAccess, accessErr := core.UserHasAccessToTeamSource(c.Context(), s.sqlite, s.log, user.ID, teamID, sourceID)
	if accessErr != nil {
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to verify source access", models.GeneralErrorType)
	}
	if !hasAccess {
		return SendErrorWithType(c, http.StatusForbidden, "You don't have access to this source", models.AuthorizationErrorType)
	}

	var req models.GenerateSQLRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, http.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.NaturalLanguageQuery == "" {
		return SendErrorWithType(c, http.StatusBadRequest, "Natural language query is required", models.ValidationErrorType)
	}

	source, schemaJSON, tableName, err := s.getSourceSchemaForAI(c, sourceID)
	if err != nil {
		return err
	}
	_ = source

	generatedSQL, err := s.callAIToGenerateSQL(c.Context(), req, schemaJSON, tableName)
	if err != nil {
		return err
	}

	return SendSuccess(c, http.StatusOK, models.GenerateSQLResponse{SQLQuery: generatedSQL})
}

func (s *Server) validateAIConfig() func(*fiber.Ctx) error {
	if !s.config.AI.Enabled {
		return func(c *fiber.Ctx) error {
			return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not enabled", models.GeneralErrorType)
		}
	}
	if s.config.AI.APIKey == "" {
		return func(c *fiber.Ctx) error {
			return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not configured (missing API key)", models.GeneralErrorType)
		}
	}
	return nil
}

func (s *Server) parseSourceTeamIDs(c *fiber.Ctx) (models.SourceID, models.TeamID, error) {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return 0, 0, SendErrorWithType(c, http.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return 0, 0, SendErrorWithType(c, http.StatusBadRequest, "Invalid team ID", models.ValidationErrorType)
	}
	return sourceID, teamID, nil
}

func (s *Server) getSourceSchemaForAI(c *fiber.Ctx, sourceID models.SourceID) (source *models.Source, schemaJSON, tableName string, err error) {
	source, err = core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return nil, "", "", SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if source == nil {
		return nil, "", "", SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}

	if !source.IsConnected {
		health := s.clickhouse.GetCachedHealth(sourceID)
		if health.Status != models.HealthStatusHealthy {
			return nil, "", "", SendErrorWithType(c, http.StatusServiceUnavailable,
				fmt.Sprintf("Source is not currently connected: %s", health.Error), models.ExternalServiceErrorType)
		}
	}

	var client *clickhouse.Client
	client, err = s.clickhouse.GetConnection(sourceID)
	if err != nil {
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to connect to source", models.ExternalServiceErrorType)
	}

	var tableInfo *clickhouse.TableInfo
	tableInfo, err = client.GetTableInfo(c.Context(), source.Connection.Database, source.Connection.TableName)
	if err != nil {
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source schema", models.ExternalServiceErrorType)
	}

	schemaJSON = formatSchemaForAI(tableInfo)
	tableName = source.GetFullTableName()
	return source, schemaJSON, tableName, nil
}

func formatSchemaForAI(tableInfo *clickhouse.TableInfo) string {
	columns := make([]map[string]interface{}, 0, len(tableInfo.Columns))
	for _, col := range tableInfo.Columns {
		columns = append(columns, map[string]interface{}{"name": col.Name, "type": col.Type})
	}
	if len(tableInfo.SortKeys) > 0 {
		columns = append(columns, map[string]interface{}{
			"name": "_sort_keys", "keys": tableInfo.SortKeys,
			"note": "The columns above are sort keys. Queries filtered by these columns will be faster.",
		})
	}
	schemaJSON, _ := json.MarshalIndent(columns, "", "  ")
	return string(schemaJSON)
}

func (s *Server) callAIToGenerateSQL(ctx context.Context, req models.GenerateSQLRequest, schemaJSON, tableName string) (string, error) {
	aiCtx, cancel := context.WithTimeout(ctx, OpenAIRequestTimeout)
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
		return "", fmt.Errorf("failed to initialize AI client: %w", err)
	}

	generatedSQL, err := aiClient.GenerateSQL(aiCtx, req.NaturalLanguageQuery, schemaJSON, tableName, req.CurrentQuery)
	if err != nil {
		if errors.Is(err, ai.ErrInvalidSQLGeneratedByAI) {
			return "", fmt.Errorf("AI could not generate valid SQL: %w", err)
		}
		return "", fmt.Errorf("failed to generate SQL: %w", err)
	}
	return generatedSQL, nil
}

// handleGetLogContext retrieves surrounding logs around a specific timestamp.
// This is similar to grep -C, showing N logs before and M logs after a target log.
// Access is controlled by the requireSourceAccess middleware.
func (s *Server) handleGetLogContext(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Parse request body
	var req models.LogContextRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate required fields
	if req.Timestamp <= 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Timestamp is required and must be positive", models.ValidationErrorType)
	}

	// Apply defaults for limits if not specified
	beforeLimit := req.BeforeLimit
	if beforeLimit <= 0 {
		beforeLimit = 10
	}
	afterLimit := req.AfterLimit
	if afterLimit <= 0 {
		afterLimit = 10
	}

	// Cap limits to prevent excessive queries
	if beforeLimit > 100 {
		beforeLimit = 100
	}
	if afterLimit > 100 {
		afterLimit = 100
	}

	// Prepare parameters for the core function
	params := core.LogContextParams{
		TargetTimestamp: req.Timestamp,
		BeforeLimit:     beforeLimit,
		AfterLimit:      afterLimit,
		BeforeOffset:    req.BeforeOffset,
		AfterOffset:     req.AfterOffset,
		ExcludeBoundary: req.ExcludeBoundary,
	}

	// Execute context query via core function
	result, err := core.GetLogContext(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get log context", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to retrieve log context: %v", err), models.DatabaseErrorType)
	}

	// Return the context response
	return SendSuccess(c, fiber.StatusOK, models.LogContextResponse{
		TargetTimestamp: result.TargetTimestamp,
		BeforeLogs:      result.BeforeLogs,
		TargetLogs:      result.TargetLogs,
		AfterLogs:       result.AfterLogs,
		Stats:           result.Stats,
	})
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
//   - logchefql: LogchefQL query string (optional, filters field values by user's query)
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

	// Get optional LogchefQL query - parsed on backend for proper SQL generation
	logchefqlQuery := c.Query("logchefql", "")

	// Get source information
	source, err := core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source for field values", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}

	// Get ClickHouse client
	client, err := s.clickhouse.GetConnection(sourceID)
	if err != nil {
		s.log.Error("failed to get clickhouse client for field values", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to connect to source", models.ExternalServiceErrorType)
	}

	// Create timeout context - this propagates to ClickHouse as max_execution_time
	// Also allows early termination if client disconnects (e.g., user navigates away)
	ctx, cancel := context.WithTimeout(c.Context(), FieldValuesTimeout)
	defer cancel()

	// Fetch field values with time range filter and user's LogchefQL query
	result, err := client.GetFieldDistinctValues(
		ctx,
		source.Connection.Database,
		source.Connection.TableName,
		clickhouse.FieldValuesParams{
			FieldName:      fieldName,
			FieldType:      fieldType,
			TimestampField: source.MetaTSField,
			StartTime:      startTime,
			EndTime:        endTime,
			Timezone:       timezone,
			Limit:          limit,
			Timeout:        nil,            // Let context deadline handle timeout
			LogchefQL:      logchefqlQuery, // Apply user's query filters
		},
	)
	if err != nil {
		// Check if the error was due to context cancellation (client disconnected)
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("field values request timed out", "source_id", sourceID, "field", fieldName, "timeout", FieldValuesTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
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
//   - logchefql: LogchefQL query string (optional, filters field values by user's query)
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

	// Get optional LogchefQL query - parsed on backend for proper SQL generation
	logchefqlQuery := c.Query("logchefql", "")

	// Get source information
	source, err := core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}

	// Get ClickHouse client
	client, err := s.clickhouse.GetConnection(sourceID)
	if err != nil {
		s.log.Error("failed to get clickhouse client", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to connect to source", models.ExternalServiceErrorType)
	}

	// Create timeout context - this propagates to ClickHouse as max_execution_time
	// Also allows early termination if client disconnects (e.g., user navigates away)
	ctx, cancel := context.WithTimeout(c.Context(), FieldValuesTimeout)
	defer cancel()

	// Fetch all filterable field values with time range filter and user's LogchefQL query
	result, err := client.GetAllLowCardinalityFieldValues(
		ctx,
		source.Connection.Database,
		source.Connection.TableName,
		clickhouse.AllFieldValuesParams{
			TimestampField: source.MetaTSField,
			StartTime:      startTime,
			EndTime:        endTime,
			Timezone:       timezone,
			Limit:          limit,
			Timeout:        nil,            // Let context deadline handle timeout
			LogchefQL:      logchefqlQuery, // Apply user's query filters
		},
	)
	if err != nil {
		// Check if the error was due to context cancellation (client disconnected)
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("field values request timed out", "source_id", sourceID, "timeout", FieldValuesTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		s.log.Error("failed to get field values", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to get field values: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
