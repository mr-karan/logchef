package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
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

type QueryClass string

const (
	QueryClassPreview QueryClass = "preview"
	QueryClassExport  QueryClass = "export"
	QueryClassTail    QueryClass = "tail"
)

type QueryAdmissionError struct {
	Message string
}

func (e *QueryAdmissionError) Error() string {
	return e.Message
}

// ActiveQuery represents an active query with its context for cancellation
type ActiveQuery struct {
	ID        string
	Class     QueryClass
	UserID    models.UserID
	SourceID  models.SourceID
	TeamID    models.TeamID
	StartTime time.Time
	QueryText string
	Cancel    context.CancelFunc
}

// Global query tracker instance
var queryTracker = &QueryTracker{
	queries: make(map[string]*ActiveQuery),
}

func init() {
	// Periodic cleanup of stale query tracker entries every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			queryTracker.Cleanup()
		}
	}()
}

func inferResponseColumnType(value any) string {
	switch v := value.(type) {
	case nil:
		return "String"
	case bool:
		return "Bool"
	case int, int8, int16, int32, int64:
		return "Int64"
	case uint, uint8, uint16, uint32, uint64:
		return "UInt64"
	case float32, float64:
		return "Float64"
	case string:
		if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return "DateTime64"
		}
		return "String"
	case []any:
		return "Array"
	default:
		return "JSON"
	}
}

func normalizeResultColumns(source *models.Source, result *models.QueryResult) []models.ColumnInfo {
	if result != nil && len(result.Columns) > 0 {
		return result.Columns
	}

	if result == nil || len(result.Logs) == 0 {
		return []models.ColumnInfo{}
	}

	sampledRows := result.Logs
	if len(sampledRows) > 25 {
		sampledRows = sampledRows[:25]
	}

	present := make(map[string]struct{}, len(result.Logs[0]))
	inferredTypes := make(map[string]string, len(result.Logs[0]))
	for _, row := range sampledRows {
		for key, value := range row {
			present[key] = struct{}{}
			if _, ok := inferredTypes[key]; !ok && value != nil {
				inferredTypes[key] = inferResponseColumnType(value)
			}
		}
	}

	columns := make([]models.ColumnInfo, 0, len(present))
	if source != nil {
		for _, col := range source.Columns {
			if _, ok := present[col.Name]; !ok {
				continue
			}

			colType := col.Type
			if colType == "" {
				colType = inferredTypes[col.Name]
			}
			if colType == "" {
				colType = "String"
			}

			columns = append(columns, models.ColumnInfo{
				Name: col.Name,
				Type: colType,
			})
			delete(present, col.Name)
		}
	}

	extraNames := make([]string, 0, len(present))
	for name := range present {
		extraNames = append(extraNames, name)
	}
	sort.Strings(extraNames)

	for _, name := range extraNames {
		colType := inferredTypes[name]
		if colType == "" {
			colType = "String"
		}
		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: colType,
		})
	}

	return columns
}

// StartQuery registers a new active query atomically with admission control.
func (qt *QueryTracker) StartQuery(class QueryClass, userID models.UserID, sourceID models.SourceID, teamID models.TeamID, sql string, cancel context.CancelFunc, maxPerUser, maxGlobal int) (string, error) {
	queryID := uuid.New().String()
	if err := qt.StartQueryWithID(queryID, class, userID, sourceID, teamID, sql, cancel, maxPerUser, maxGlobal); err != nil {
		return "", err
	}
	return queryID, nil
}

// StartQueryWithID registers a new active query using a caller-provided ID.
func (qt *QueryTracker) StartQueryWithID(queryID string, class QueryClass, userID models.UserID, sourceID models.SourceID, teamID models.TeamID, sql string, cancel context.CancelFunc, maxPerUser, maxGlobal int) error {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	userActive := 0
	classActive := 0
	for _, query := range qt.queries {
		if query.Class != class {
			continue
		}
		classActive++
		if query.UserID == userID {
			userActive++
		}
	}

	if maxPerUser > 0 && userActive >= maxPerUser {
		return &QueryAdmissionError{Message: fmt.Sprintf("Too many active %s queries for this user. Limit is %d.", class, maxPerUser)}
	}
	if maxGlobal > 0 && classActive >= maxGlobal {
		return &QueryAdmissionError{Message: fmt.Sprintf("Too many active %s queries globally. Limit is %d.", class, maxGlobal)}
	}

	qt.queries[queryID] = &ActiveQuery{
		ID:        queryID,
		Class:     class,
		UserID:    userID,
		SourceID:  sourceID,
		TeamID:    teamID,
		StartTime: time.Now(),
		QueryText: sql,
		Cancel:    cancel,
	}
	return nil
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
func (s *Server) handleQueryLogs(c *fiber.Ctx) error { //nolint:gocyclo // request handler, inherently branchy
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	var req models.APIQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Apply preview timeout policy.
	if req.QueryTimeout == nil {
		defaultTimeout := s.config.Query.DefaultTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	if s.config.Query.MaxTimeoutSeconds > 0 && *req.QueryTimeout > s.config.Query.MaxTimeoutSeconds {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query timeout cannot exceed %d seconds for Run", s.config.Query.MaxTimeoutSeconds),
			models.ValidationErrorType)
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

	// Check if the query contains variable placeholders.
	requiredVars := template.ExtractVariableNames(req.QueryText)

	// Validate that all required variables are provided.
	if len(requiredVars) > 0 && len(req.Variables) == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query contains template variables (%s) but no variables were provided. Please define variable values before executing.", strings.Join(requiredVars, ", ")),
			models.ValidationErrorType)
	}

	// Perform template variable substitution if variables are provided.
	processedQuery := req.QueryText
	if len(req.Variables) > 0 {
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
			return SendErrorWithType(c, fiber.StatusBadRequest,
				fmt.Sprintf("Variable substitution failed: %v", err), models.ValidationErrorType)
		}
		processedQuery = substituted
	}

	// Create a cancellable context for this query
	queryCtx, cancel := context.WithCancel(c.Context())
	defer cancel() // Ensure cleanup

	// Add query to tracker atomically with admission control.
	queryID, err := queryTracker.StartQuery(
		QueryClassPreview,
		user.ID,
		sourceID,
		teamID,
		req.QueryText,
		cancel,
		s.config.Query.MaxConcurrentPerUser,
		s.config.Query.MaxConcurrentGlobal,
	)
	if err != nil {
		var admissionErr *QueryAdmissionError
		if errors.As(err, &admissionErr) {
			return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to track query", models.GeneralErrorType)
	}
	defer queryTracker.RemoveQuery(queryID) // Ensure cleanup

	// Prepare parameters for the core query function.
	params := datasource.QueryRequest{
		RawQuery:         processedQuery,
		Timezone:         req.Timezone,
		Limit:            req.Limit,
		DefaultLimit:     s.config.Query.DefaultPreviewLimit,
		MaxLimit:         s.config.Query.MaxPreviewLimit,
		MaxResponseBytes: s.config.Query.MaxResponseBytes,
		QueryTimeout:     req.QueryTimeout,
	}
	if req.StartTime != "" || req.EndTime != "" {
		startTime, endTime, err := parseRFC3339TimeRange(req.StartTime, req.EndTime)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		params.StartTime = startTime
		params.EndTime = endTime
	}

	// Execute query via core function with cancellable context.
	result, err := core.QueryLogs(queryCtx, s.datasources, sourceID, params)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Querying is not supported for this source type yet", models.ValidationErrorType)
		}
		if datasource.IsValidationError(err) {
			return SendErrorWithType(c, fiber.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err), models.ValidationErrorType)
		}
		s.log.Error("failed to query logs", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to query logs: %v", err), models.DatabaseErrorType)
	}

	// Log successful query execution
	if result != nil {
		user := c.Locals("user").(*models.User)
		s.log.Info("query.execute",
			"user", user.Email,
			"team_id", teamID,
			"source_id", sourceID,
			"mode", "sql",
			"query_id", queryID,
			"rows", len(result.Logs),
			"duration_ms", result.Stats.ExecutionTimeMs,
			"limit_requested", req.Limit,
			"limit_applied", result.Stats.LimitApplied,
			"truncated", result.Stats.Truncated,
		)
	}

	// Add query ID to the response for frontend tracking
	if result != nil {
		columns := normalizeResultColumns(nil, result)
		// Create a map to include the query ID with the result
		responseWithQueryID := map[string]any{
			"query_id": queryID,
			"data":     result.Logs,
			"stats":    result.Stats,
			"columns":  columns,
			"warnings": result.Warnings,
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

	return SendSuccess(c, fiber.StatusOK, map[string]any{
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
	schema, err := core.GetSourceSchema(c.Context(), s.datasources, sourceID)
	if err != nil {
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

	// Execute histogram query via core function.
	result, err := core.GetHistogramData(c.Context(), s.datasources, sourceID, params)
	if err != nil {
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
	source, err = core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return nil, "", "", SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if source == nil {
		return nil, "", "", SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}
	if !source.HasCapability(string(datasource.CapabilityAISQLGeneration)) {
		return nil, "", "", SendErrorWithType(c, http.StatusBadRequest, "AI SQL generation is only supported for ClickHouse sources", models.ValidationErrorType)
	}

	if !source.IsConnected {
		return nil, "", "", SendErrorWithType(c, http.StatusServiceUnavailable, "Source is not currently connected", models.ExternalServiceErrorType)
	}
	if len(source.Columns) == 0 {
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source schema", models.ExternalServiceErrorType)
	}

	schemaJSON = formatSchemaForAI(source)
	tableName = source.GetFullTableName()
	return source, schemaJSON, tableName, nil
}

func formatSchemaForAI(source *models.Source) string {
	columns := make([]map[string]interface{}, 0, len(source.Columns))
	for _, col := range source.Columns {
		columns = append(columns, map[string]interface{}{"name": col.Name, "type": col.Type})
	}
	if len(source.SortKeys) > 0 {
		columns = append(columns, map[string]interface{}{
			"name": "_sort_keys", "keys": source.SortKeys,
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

// handleGetLogContext returns logs surrounding a specific timestamp (grep -C
// for logs). Routed through the datasource service; sources whose provider
// lacks the log_context capability get a 400.
func (s *Server) handleGetLogContext(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	var req models.LogContextRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.Timestamp <= 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Timestamp is required and must be positive", models.ValidationErrorType)
	}

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

	result, err := core.GetLogContext(c.Context(), s.datasources, sourceID, core.LogContextParams{
		TargetTimestamp: req.Timestamp,
		BeforeLimit:     beforeLimit,
		AfterLimit:      afterLimit,
		BeforeOffset:    req.BeforeOffset,
		AfterOffset:     req.AfterOffset,
		ExcludeBoundary: req.ExcludeBoundary,
	})
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Log context is not supported for this source type", models.ValidationErrorType)
		}
		s.log.Error("failed to get log context", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to retrieve log context: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
