package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"

	dashcache "github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// TranslateRequest represents the request body for LogchefQL translation
type TranslateRequest struct {
	Query     string                    `json:"query"`
	StartTime string                    `json:"start_time"` // Optional. Format: "2006-01-02 15:04:05" - required for full_sql
	EndTime   string                    `json:"end_time"`   // Optional. Format: "2006-01-02 15:04:05" - required for full_sql
	Timezone  string                    `json:"timezone"`   // Optional. e.g., "UTC", "Asia/Kolkata" - required for full_sql
	Limit     int                       `json:"limit"`      // Optional. e.g., 100 - defaults to 100
	Variables []models.TemplateVariable `json:"variables,omitempty"`
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

// handleLogchefQLTranslate translates a LogchefQL query to SQL.
// This endpoint is useful for:
// 1. Getting the SQL preview in the frontend
// 2. Extracting filter conditions for the field sidebar
// 3. Validating queries before execution
//
// POST /api/v1/teams/:teamID/sources/:sourceID/logchefql/translate
func (s *Server) handleLogchefQLTranslate(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	req, ok := parseTranslateRequest(c)
	if !ok {
		return nil
	}

	translation, err := core.TranslateLogchefQL(c.RequestCtx(), s.datasources, src, datasource.LogchefQLCompileRequest{
		Query:     req.Query,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Timezone:  req.Timezone,
		Limit:     req.Limit,
	})
	if err != nil {
		return s.sendLogchefQLPrepareError(c, src.SourceID(), err, "Failed to translate query")
	}

	response := TranslateResponse{
		SQL:                    translation.SQL,
		FullSQL:                translation.FullSQL,
		GeneratedQuery:         translation.GeneratedQuery,
		GeneratedQueryLanguage: translation.Language,
		Valid:                  translation.Valid,
		Error:                  translation.Error,
		Conditions:             translation.Conditions,
		FieldsUsed:             translation.FieldsUsed,
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

// parseTranslateRequest parses and defaults the translate request body,
// writing the error response and returning ok=false on failure (the Send*
// helpers return nil, so their return value is not a safe error sentinel. See
// the tail_handlers regression test for why this codebase uses an explicit ok
// bool instead).
func parseTranslateRequest(c fiber.Ctx) (req TranslateRequest, ok bool) {
	if err := c.Bind().Body(&req); err != nil {
		_ = SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
		return TranslateRequest{}, false
	}
	if err := substituteTranslateVariables(&req); err != nil {
		_ = SendErrorWithType(c, fiber.StatusBadRequest, "Variable substitution failed: "+err.Error(), models.ValidationErrorType)
		return TranslateRequest{}, false
	}

	// Apply defaults
	if req.Limit <= 0 {
		req.Limit = 100 // Default limit
	}
	return req, true
}

func substituteTranslateVariables(req *TranslateRequest) error {
	if req == nil {
		return fmt.Errorf("translate request is required")
	}

	variables := make([]template.Variable, len(req.Variables))
	for i, variable := range req.Variables {
		variables[i] = template.Variable{
			Name:  variable.Name,
			Type:  template.VariableType(variable.Type),
			Value: variable.Value,
		}
	}
	query, err := template.SubstituteLogchefQLVariables(req.Query, variables)
	if err != nil {
		return err
	}
	req.Query = query
	return nil
}

// sendLogchefQLPrepareError writes the response for an error from
// core.PrepareLogchefQLQuery or core.TranslateLogchefQL. compileFailure is the
// message for a datasource compile failure.
func (s *Server) sendLogchefQLPrepareError(c fiber.Ctx, sourceID models.SourceID, err error, compileFailure string) error {
	if reqErr, ok := errors.AsType[*core.QueryRequestError](err); ok {
		return SendErrorWithType(c, fiber.StatusBadRequest, reqErr.Message, models.ValidationErrorType)
	}
	if errors.Is(err, core.ErrSourceNotFound) {
		return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}
	if errors.Is(err, core.ErrLogchefQLNotSupported) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "LogchefQL is not supported for this source", models.ValidationErrorType)
	}
	if _, ok := errors.AsType[*core.LogchefQLCompileError](err); ok {
		s.log.Error("failed to compile logchefql query", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, compileFailure, models.GeneralErrorType)
	}
	s.log.Error("failed to get source", "error", err, "source_id", sourceID)
	return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
}

// handleLogchefQLValidate validates a LogchefQL query without translating to SQL.
// This is a lightweight endpoint for real-time validation in the editor.
//
// POST /api/v1/teams/:teamID/sources/:sourceID/logchefql/validate
func (s *Server) handleLogchefQLValidate(c fiber.Ctx) error {
	var req ValidateRequest
	if err := c.Bind().Body(&req); err != nil {
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

func (s *Server) handleLogchefQLQueryError(c fiber.Ctx, sourceID models.SourceID, err error) error {
	if admissionErr, ok := errors.AsType[*QueryAdmissionError](err); ok {
		return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
	}
	// A cached fill surfaces the caller's own cancellation, which the explorer
	// and dashboard both trigger on every re-query. That is not a failure.
	if errors.Is(err, context.Canceled) {
		return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
	}
	if errors.Is(err, datasource.ErrOperationNotSupported) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Querying is not supported for this source type yet", models.ValidationErrorType)
	}
	s.log.Error("failed to execute logchefql query", "error", err, "source_id", sourceID)
	return SendErrorWithType(c, fiber.StatusInternalServerError, "Query execution failed: "+err.Error(), models.DatabaseErrorType)
}

// handleLogchefQLQuery executes a LogchefQL query directly.
// This is an alternative to the existing logs/query endpoint that accepts raw SQL.
// The backend handles the full translation and execution.
//
// POST /api/v1/teams/:teamID/sources/:sourceID/logchefql/query
func (s *Server) handleLogchefQLQuery(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	sourceID, teamID := src.SourceID(), src.TeamID()

	var req struct {
		core.LogchefQLQueryRequest
		// Cache opts this request into the dashboard result cache. Omitted for
		// explorer/ad-hoc queries so they are never cached.
		Cache *models.CacheDirective `json:"cache,omitempty"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	prepared, err := core.PrepareLogchefQLQuery(c.RequestCtx(), s.datasources, s.config.Query, src, req.LogchefQLQueryRequest)
	if err != nil {
		return s.sendLogchefQLPrepareError(c, sourceID, err, "Failed to compile query")
	}
	source, compiled, queryParams := prepared.Source, prepared.Compiled, prepared.Params
	executableQuery := compiled.Query
	executableQueryLanguage := compiled.Language

	// Get user information for query tracking
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	// Dashboard panel requests may opt into the per-dashboard result cache. The
	// key uses the finalized executable query (post-substitution + compilation);
	// for VictoriaLogs the time range is passed separately and folded into the
	// key, for ClickHouse it is already baked into the compiled SQL.
	effTTL, cacheable := s.dashboardCacheParams(req.Cache)
	var cacheKey [32]byte
	if cacheable {
		cacheKey = dashcache.ComputeKey(dashcache.KeyInput{
			EndpointKind:     "logchefql-logs",
			TeamID:           int64(teamID),
			SourceID:         int64(sourceID),
			SourceRevision:   source.UpdatedAt.UnixNano(),
			EffTTLSeconds:    int64(effTTL / time.Second),
			Language:         string(executableQueryLanguage),
			FinalizedQuery:   executableQuery,
			CanonicalStart:   canonCacheTime(queryParams.StartTime),
			CanonicalEnd:     canonCacheTime(queryParams.EndTime),
			Timezone:         queryParams.Timezone,
			EffectiveLimit:   int64(queryParams.Limit),
			QueryTimeoutSecs: int64(*queryParams.QueryTimeout),
		})
	}

	// ClickHouse-backed sources stream the response body row-by-row so server
	// memory stays bounded regardless of result size. Other source types
	// (VictoriaLogs) keep the buffered path.
	if source.IsClickHouse() {
		cfg := queryStreamConfig{
			logsKey:           "logs",
			includeGenerated:  true,
			generatedSQL:      executableQuery,
			generatedQuery:    executableQuery,
			generatedLanguage: executableQueryLanguage,
			conditions:        compiled.Conditions,
			fieldsUsed:        compiled.FieldsUsed,
		}
		// OOM guardrail: only the dashboard-directive path buffers (bounded by
		// max_entry_bytes); on overflow the fill errors and we fall through to the
		// unbuffered streaming path below, which is left byte-for-byte unchanged.
		if cacheable {
			fillTimeout := time.Duration(*queryParams.QueryTimeout) * time.Second
			if handled, err := s.tryServeDashboardCache(c, cacheKey, effTTL, fillTimeout, s.fillClickHouseStream(user.ID, teamID, sourceID, queryParams, cfg)); handled {
				return err
			} else if err != nil {
				s.log.Error("failed to stream query", "error", err, "source_id", sourceID, "mode", "logchefql")
				return writeDashboardStreamError(c, err)
			}
		}
		return s.streamPreviewQuery(c, sourceID, teamID, user, queryParams,
			cfg, executableQuery, "logchefql", queryParams.Limit,
			req.Query, models.QueryLanguageLogchefQL)
	}

	// Non-streaming providers (VictoriaLogs) already buffer; serve dashboard
	// panels from the cache when eligible.
	if cacheable {
		fillTimeout := time.Duration(*queryParams.QueryTimeout) * time.Second
		fill := s.dashboardQueryFill(user.ID, teamID, sourceID, queryParams.RawQuery, func(ctx context.Context, queryID string) ([]byte, error) {
			result, err := core.QueryLogs(ctx, s.datasources, src, queryParams)
			if err != nil {
				return nil, err
			}
			resp := map[string]any{
				"logs":                     result.Logs,
				"columns":                  core.ResultColumns(source, result),
				"stats":                    result.Stats,
				"query_id":                 queryID,
				"generated_sql":            executableQuery,
				"generated_query":          executableQuery,
				"generated_query_language": executableQueryLanguage,
				"warnings":                 result.Warnings,
				"conditions":               compiled.Conditions,
				"fields_used":              compiled.FieldsUsed,
			}
			return json.Marshal(NewSuccessResponse(resp))
		})
		if handled, err := s.tryServeDashboardCache(c, cacheKey, effTTL, fillTimeout, fill); handled {
			return err
		} else if err != nil {
			return s.handleLogchefQLQueryError(c, sourceID, err)
		}
	}

	// Buffered fallback for non-streaming providers.
	// Create a cancellable context for this query
	queryCtx, cancel := context.WithCancel(c.RequestCtx())
	defer cancel() // Ensure cleanup

	// Add query to tracker atomically with admission control.
	queryID, err := queryTracker.StartQuery(
		QueryClassPreview,
		user.ID,
		sourceID,
		teamID,
		executableQuery,
		cancel,
		s.config.Query.MaxConcurrentPerUser,
		s.config.Query.MaxConcurrentGlobal,
	)
	if err != nil {
		if admissionErr, ok := errors.AsType[*QueryAdmissionError](err); ok {
			return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to track query", models.GeneralErrorType)
	}
	defer queryTracker.RemoveQuery(queryID)

	// Execute via core function
	result, err := core.QueryLogs(queryCtx, s.datasources, src, queryParams)
	if err != nil {
		return s.handleLogchefQLQueryError(c, sourceID, err)
	}

	// Log successful query execution
	if result != nil {
		s.log.Info("query.execute",
			"user", user.Email,
			"team_id", teamID,
			"source_id", sourceID,
			"mode", "logchefql",
			"query_id", queryID,
			"rows", len(result.Logs),
			"duration_ms", result.Stats.ExecutionTimeMs,
			"limit_requested", queryParams.Limit,
			"limit_applied", result.Stats.LimitApplied,
			"truncated", result.Stats.Truncated,
		)
		s.recordQueryHistory(user, teamID, sourceID, req.Query, models.QueryLanguageLogchefQL,
			int64(result.Stats.ExecutionTimeMs), int64(len(result.Logs)))
	}

	// Add query_id and generated SQL to response
	columns := core.ResultColumns(source, result)
	responseData := map[string]any{
		"logs":                     result.Logs,
		"columns":                  columns,
		"stats":                    result.Stats,
		"query_id":                 queryID,
		"generated_sql":            executableQuery, // Deprecated legacy field kept for compatibility.
		"generated_query":          executableQuery,
		"generated_query_language": executableQueryLanguage,
		"warnings":                 result.Warnings,
		"conditions":               compiled.Conditions,
		"fields_used":              compiled.FieldsUsed,
	}

	return SendSuccess(c, fiber.StatusOK, responseData)
}
