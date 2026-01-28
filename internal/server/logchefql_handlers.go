package server

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// TranslateRequest represents the request body for LogchefQL translation
type TranslateRequest struct {
	Query     string `json:"query"`
	StartTime string `json:"start_time"` // Optional. Format: "2006-01-02 15:04:05" - required for full_sql
	EndTime   string `json:"end_time"`   // Optional. Format: "2006-01-02 15:04:05" - required for full_sql
	Timezone  string `json:"timezone"`   // Optional. e.g., "UTC", "Asia/Kolkata" - required for full_sql
	Limit     int    `json:"limit"`      // Optional. e.g., 100 - defaults to 100
}

// TranslateResponse represents the response for LogchefQL translation
type TranslateResponse struct {
	SQL        string                      `json:"sql"`                // WHERE clause conditions only
	FullSQL    string                      `json:"full_sql,omitempty"` // Complete executable SQL (when time params provided)
	Valid      bool                        `json:"valid"`
	Error      *logchefql.ParseError       `json:"error,omitempty"`
	Conditions []logchefql.FilterCondition `json:"conditions"`
	FieldsUsed []string                    `json:"fields_used"`
}

// ValidateRequest represents the request body for LogchefQL validation
type ValidateRequest struct {
	Query string `json:"query"`
}

// ValidateResponse represents the response for LogchefQL validation
type ValidateResponse struct {
	Valid bool                  `json:"valid"`
	Error *logchefql.ParseError `json:"error,omitempty"`
}

// handleLogchefQLTranslate translates a LogchefQL query to SQL.
// This endpoint is useful for:
// 1. Getting the SQL preview in the frontend
// 2. Extracting filter conditions for the field sidebar
// 3. Validating queries before execution
//
// POST /api/v1/teams/:teamID/sources/:sourceID/logchefql/translate
func (s *Server) handleLogchefQLTranslate(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	var req TranslateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Apply defaults
	if req.Limit <= 0 {
		req.Limit = 100 // Default limit
	}

	// Time params are optional - only needed for full_sql generation
	// Check if all time params are provided for full SQL generation
	hasTimeParams := req.StartTime != "" && req.EndTime != "" && req.Timezone != ""

	// Get source information for schema
	source, err := core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}

	// Build schema from source columns
	var schema *logchefql.Schema
	if len(source.Columns) > 0 {
		columns := make([]logchefql.ColumnInfo, len(source.Columns))
		for i, col := range source.Columns {
			columns[i] = logchefql.ColumnInfo{
				Name: col.Name,
				Type: col.Type,
			}
		}
		schema = &logchefql.Schema{Columns: columns}
	}

	// Translate the query
	result := logchefql.Translate(req.Query, schema)

	response := TranslateResponse{
		SQL:        result.SQL,
		Valid:      result.Valid,
		Error:      result.Error,
		Conditions: result.Conditions,
		FieldsUsed: result.FieldsUsed,
	}

	// Ensure conditions is never nil
	if response.Conditions == nil {
		response.Conditions = []logchefql.FilterCondition{}
	}
	if response.FieldsUsed == nil {
		response.FieldsUsed = []string{}
	}

	// Build the full SQL only if time params are provided
	if result.Valid && hasTimeParams {
		tableName := source.Connection.Database + "." + source.Connection.TableName

		params := logchefql.QueryBuildParams{
			LogchefQL:      req.Query,
			Schema:         schema,
			TableName:      tableName,
			TimestampField: source.MetaTSField,
			StartTime:      req.StartTime,
			EndTime:        req.EndTime,
			Timezone:       req.Timezone,
			Limit:          req.Limit,
		}

		fullSQL, err := logchefql.BuildFullQuery(params)
		if err == nil {
			response.FullSQL = fullSQL
		}
	}

	return SendSuccess(c, fiber.StatusOK, response)
}

// handleLogchefQLValidate validates a LogchefQL query without translating to SQL.
// This is a lightweight endpoint for real-time validation in the editor.
//
// POST /api/v1/teams/:teamID/sources/:sourceID/logchefql/validate
func (s *Server) handleLogchefQLValidate(c *fiber.Ctx) error {
	var req ValidateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate the query
	result := logchefql.Validate(req.Query)

	response := ValidateResponse{
		Valid: result.Valid,
		Error: result.Error,
	}

	return SendSuccess(c, fiber.StatusOK, response)
}

// handleLogchefQLQuery executes a LogchefQL query directly.
// This is an alternative to the existing logs/query endpoint that accepts raw SQL.
// The backend handles the full translation and execution.
//
// POST /api/v1/teams/:teamID/sources/:sourceID/logchefql/query
func (s *Server) handleLogchefQLQuery(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Parse request
	var req struct {
		Query        string                    `json:"query"`
		StartTime    string                    `json:"start_time"`    // ISO8601 format
		EndTime      string                    `json:"end_time"`      // ISO8601 format
		Timezone     string                    `json:"timezone"`      // Timezone for time conversion
		Limit        int                       `json:"limit"`         // Result limit
		QueryTimeout *int                      `json:"query_timeout"` // Optional timeout in seconds
		Variables    []models.TemplateVariable `json:"variables,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate required fields
	if req.StartTime == "" || req.EndTime == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "start_time and end_time are required", models.ValidationErrorType)
	}

	// Apply defaults
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	// Validate timeout
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	// Get source information
	source, err := core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}

	// Build schema from source columns
	var schema *logchefql.Schema
	if len(source.Columns) > 0 {
		columns := make([]logchefql.ColumnInfo, len(source.Columns))
		for i, col := range source.Columns {
			columns[i] = logchefql.ColumnInfo{
				Name: col.Name,
				Type: col.Type,
			}
		}
		schema = &logchefql.Schema{Columns: columns}
	}

	// Substitute variables in the query if provided
	query := req.Query
	if len(req.Variables) > 0 {
		vars := make([]template.Variable, len(req.Variables))
		for i, v := range req.Variables {
			vars[i] = template.Variable{
				Name:  v.Name,
				Type:  template.VariableType(v.Type),
				Value: v.Value,
			}
		}
		substituted, err := template.SubstituteVariables(query, vars)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Variable substitution failed: "+err.Error(), models.ValidationErrorType)
		}
		query = substituted
	}

	// Build the full SQL query
	tableName := source.Connection.Database + "." + source.Connection.TableName
	params := logchefql.QueryBuildParams{
		LogchefQL:      query,
		Schema:         schema,
		TableName:      tableName,
		TimestampField: source.MetaTSField,
		StartTime:      req.StartTime,
		EndTime:        req.EndTime,
		Timezone:       req.Timezone,
		Limit:          req.Limit,
	}

	sql, err := logchefql.BuildFullQuery(params)
	if err != nil {
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

	// Execute the query (reuse existing query execution logic)
	// Create a cancellable context for this query
	queryCtx, cancel := c.Context(), func() {}
	defer cancel()

	// Add query to tracker
	queryID := queryTracker.AddQuery(user.ID, sourceID, teamID, sql, cancel)
	defer queryTracker.RemoveQuery(queryID)

	// Execute via core function
	queryParams := clickhouse.LogQueryParams{
		RawSQL:       sql,
		Limit:        req.Limit,
		QueryTimeout: req.QueryTimeout,
	}
	result, err := core.QueryLogs(queryCtx, s.sqlite, s.clickhouse, s.log, sourceID, queryParams)
	if err != nil {
		s.log.Error("failed to execute logchefql query", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Query execution failed: "+err.Error(), models.DatabaseErrorType)
	}

	// Add query_id and generated SQL to response
	responseData := map[string]interface{}{
		"logs":          result.Logs,
		"columns":       result.Columns,
		"stats":         result.Stats,
		"query_id":      queryID,
		"generated_sql": sql, // For "Show SQL" feature
	}

	return SendSuccess(c, fiber.StatusOK, responseData)
}
