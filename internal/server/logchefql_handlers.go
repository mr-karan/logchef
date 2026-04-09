package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
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
	SQL                    string                      `json:"sql"`                // WHERE clause conditions only
	FullSQL                string                      `json:"full_sql,omitempty"` // Complete executable SQL (when time params provided)
	GeneratedQuery         string                      `json:"generated_query,omitempty"`
	GeneratedQueryLanguage models.QueryLanguage        `json:"generated_query_language,omitempty"`
	Valid                  bool                        `json:"valid"`
	Error                  *logchefql.ParseError       `json:"error,omitempty"`
	Conditions             []logchefql.FilterCondition `json:"conditions"`
	FieldsUsed             []string                    `json:"fields_used"`
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

func buildLogchefQLSchema(source *models.Source) *logchefql.Schema {
	if source == nil || len(source.Columns) == 0 {
		return nil
	}

	columns := make([]logchefql.ColumnInfo, len(source.Columns))
	for i, col := range source.Columns {
		columns[i] = logchefql.ColumnInfo{
			Name: col.Name,
			Type: col.Type,
		}
	}
	return &logchefql.Schema{Columns: columns}
}

func parseLogchefQLTimeRange(startTime, endTime, timezone string) (*time.Time, *time.Time, error) {
	locationName := timezone
	if locationName == "" {
		locationName = "UTC"
	}

	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return nil, nil, err
	}

	start, err := parseLogchefQLTimeValue(startTime, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid start_time: %w", err)
	}
	end, err := parseLogchefQLTimeValue(endTime, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid end_time: %w", err)
	}

	return &start, &end, nil
}

func parseLogchefQLTimeValue(value string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		var (
			parsed time.Time
			err    error
		)

		switch layout {
		case time.RFC3339Nano, time.RFC3339:
			parsed, err = time.Parse(layout, value)
		default:
			parsed, err = time.ParseInLocation(layout, value, loc)
		}
		if err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
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
	source, err := core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if !source.SupportsQueryLanguage(models.QueryLanguageLogchefQL) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "LogchefQL is not supported for this source", models.ValidationErrorType)
	}

	schema := buildLogchefQLSchema(source)

	// Translate the query
	var response TranslateResponse
	if source.IsVictoriaLogs() {
		result := logchefql.TranslateToLogsQL(req.Query, &logchefql.LogsQLTranslateOptions{
			DefaultTimestampField: source.MetaTSField,
		})
		response = TranslateResponse{
			GeneratedQuery:         result.Query,
			GeneratedQueryLanguage: models.QueryLanguageLogsQL,
			Valid:                  result.Valid,
			Error:                  result.Error,
			Conditions:             result.Conditions,
			FieldsUsed:             result.FieldsUsed,
		}
	} else {
		result := logchefql.Translate(req.Query, schema)
		response = TranslateResponse{
			SQL:                    result.SQL,
			GeneratedQuery:         result.SQL,
			GeneratedQueryLanguage: models.QueryLanguageClickHouseSQL,
			Valid:                  result.Valid,
			Error:                  result.Error,
			Conditions:             result.Conditions,
			FieldsUsed:             result.FieldsUsed,
		}

		if result.Valid && hasTimeParams {
			tableName := source.GetFullTableName()

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

			fullSQL, buildErr := logchefql.BuildFullQuery(params)
			if buildErr == nil {
				response.FullSQL = fullSQL
				response.GeneratedQuery = fullSQL
			}
		}
	}

	// Ensure conditions is never nil
	if response.Conditions == nil {
		response.Conditions = []logchefql.FilterCondition{}
	}
	if response.FieldsUsed == nil {
		response.FieldsUsed = []string{}
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
		StartTime    string                    `json:"start_time"`    // Accepts "2006-01-02 15:04:05" and ISO8601/RFC3339
		EndTime      string                    `json:"end_time"`      // Accepts "2006-01-02 15:04:05" and ISO8601/RFC3339
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
	source, err := core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if !source.SupportsQueryLanguage(models.QueryLanguageLogchefQL) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "LogchefQL is not supported for this source", models.ValidationErrorType)
	}

	schema := buildLogchefQLSchema(source)

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

	var (
		executableQuery         string
		executableQueryLanguage models.QueryLanguage
		queryStartTime          *time.Time
		queryEndTime            *time.Time
	)

	if source.IsVictoriaLogs() {
		translated := logchefql.TranslateToLogsQL(query, &logchefql.LogsQLTranslateOptions{
			DefaultTimestampField: source.MetaTSField,
		})
		if !translated.Valid {
			message := "invalid LogchefQL query"
			if translated.Error != nil {
				message = translated.Error.Error()
			}
			return SendErrorWithType(c, fiber.StatusBadRequest, message, models.ValidationErrorType)
		}

		startTime, endTime, err := parseLogchefQLTimeRange(req.StartTime, req.EndTime, req.Timezone)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}

		executableQuery = translated.Query
		executableQueryLanguage = models.QueryLanguageLogsQL
		queryStartTime = startTime
		queryEndTime = endTime
	} else {
		tableName := source.GetFullTableName()
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

		executableQuery = sql
		executableQueryLanguage = models.QueryLanguageClickHouseSQL
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
	queryCtx, cancel := context.WithCancel(c.Context())
	defer cancel() // Ensure cleanup

	// Add query to tracker
	queryID := queryTracker.AddQuery(user.ID, sourceID, teamID, executableQuery, cancel)
	defer queryTracker.RemoveQuery(queryID)

	// Execute via core function
	queryParams := datasource.QueryRequest{
		RawQuery:     executableQuery,
		StartTime:    queryStartTime,
		EndTime:      queryEndTime,
		Timezone:     req.Timezone,
		Limit:        req.Limit,
		MaxLimit:     s.config.Query.MaxLimit,
		QueryTimeout: req.QueryTimeout,
	}
	result, err := core.QueryLogs(queryCtx, s.datasources, sourceID, queryParams)
	if err != nil {
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Querying is not supported for this source type yet", models.ValidationErrorType)
		}
		s.log.Error("failed to execute logchefql query", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Query execution failed: "+err.Error(), models.DatabaseErrorType)
	}

	// Log successful query execution
	if result != nil {
		user := c.Locals("user").(*models.User)
		s.log.Info("query.execute",
			"user", user.Email,
			"team_id", teamID,
			"source_id", sourceID,
			"mode", "logchefql",
			"query_id", queryID,
			"rows", len(result.Logs),
			"duration_ms", result.Stats.ExecutionTimeMs,
			"limit", req.Limit,
		)
	}

	// Add query_id and generated SQL to response
	responseData := map[string]interface{}{
		"logs":                     result.Logs,
		"columns":                  result.Columns,
		"stats":                    result.Stats,
		"query_id":                 queryID,
		"generated_sql":            executableQuery, // Deprecated legacy field kept for compatibility.
		"generated_query":          executableQuery,
		"generated_query_language": executableQueryLanguage,
	}

	return SendSuccess(c, fiber.StatusOK, responseData)
}
