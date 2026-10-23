package server

import (
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

	translation, err := core.TranslateLogchefQL(c.Context(), s.datasources, src, datasource.LogchefQLCompileRequest{
		Query:     req.Query,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Timezone:  req.Timezone,
		Limit:     req.Limit,
	})
	if err != nil {
		return s.sendQueryError(c, src.SourceID(), err, "Failed to translate query", "Query translation")
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

const logchefQLQueryFailure = "Query execution failed"

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

	prepared, err := core.PrepareLogchefQLQuery(c.Context(), s.datasources, s.config.Query, src, req.LogchefQLQueryRequest)
	if err != nil {
		return s.sendQueryError(c, sourceID, err, logchefQLQueryFailure, logchefQLQueryOperation)
	}
	source, compiled, queryParams := prepared.Source, prepared.Compiled, prepared.Params
	executableQuery := compiled.Query
	executableQueryLanguage := compiled.Language

	// Get user information for query tracking
	user := currentUser(c)
	if user == nil {
		return sendMissingUser(c)
	}

	// Dashboard panel requests may opt into the per-dashboard result cache. The
	// key uses the finalized executable query (post-substitution + compilation);
	// for VictoriaLogs the time range is passed separately and folded into the
	// key, for ClickHouse it is already baked into the compiled SQL.
	effTTL, cacheable := s.dashboardCacheParams(req.Cache)
	cacheable = cacheable && len(req.ExtraStreamFilters) == 0
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

	return s.executePreviewQuery(c, user, previewQuery{
		src:    src,
		source: source,
		params: queryParams,
		cfg: queryStreamConfig{
			logsKey:           "logs",
			failure:           logchefQLQueryFailure,
			operation:         logchefQLQueryOperation,
			includeGenerated:  true,
			generatedSQL:      executableQuery,
			generatedQuery:    executableQuery,
			generatedLanguage: executableQueryLanguage,
			conditions:        compiled.Conditions,
			fieldsUsed:        compiled.FieldsUsed,
		},
		orderColumnsBySchema: true,
		mode:                 "logchefql",
		trackedQuery:         executableQuery,
		requestedLimit:       queryParams.Limit,
		historyQuery:         req.Query,
		historyLanguage:      models.QueryLanguageLogchefQL,
		cacheable:            cacheable,
		cacheKey:             cacheKey,
		cacheTTL:             effTTL,
	})
}
