package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	dashcache "github.com/mr-karan/logchef/internal/cache"
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
	// AIRequestTimeout is the maximum time to wait for the AI provider to respond
	AIRequestTimeout = 15 * time.Second
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

	// ClickHouse-backed sources stream the response body row-by-row so server
	// memory stays bounded regardless of result size (the OOM this endpoint used
	// to hit came from buffering the full result set into a []map before
	// marshaling). Other source types (VictoriaLogs) keep the buffered path.
	source, err := core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	// Dashboard panel requests may opt into the per-dashboard result cache. The
	// cache key is computed from the finalized (post-substitution) executable
	// query and the resolved parameters; source.UpdatedAt invalidates entries on
	// a source config change. Explorer/ad-hoc requests carry no directive and
	// stay uncached, preserving the streaming path exactly.
	effTTL, cacheable := s.dashboardCacheParams(req.Cache)
	var cacheKey [32]byte
	if cacheable {
		effLimit := req.Limit
		if effLimit <= 0 {
			effLimit = s.config.Query.DefaultPreviewLimit
		}
		if s.config.Query.MaxPreviewLimit > 0 && effLimit > s.config.Query.MaxPreviewLimit {
			effLimit = s.config.Query.MaxPreviewLimit
		}
		cacheKey = dashcache.ComputeKey(dashcache.KeyInput{
			EndpointKind:     "logs",
			TeamID:           int64(teamID),
			SourceID:         int64(sourceID),
			SourceRevision:   source.UpdatedAt.UnixNano(),
			EffTTLSeconds:    int64(effTTL / time.Second),
			Language:         string(models.QueryLanguageClickHouseSQL),
			FinalizedQuery:   processedQuery,
			CanonicalStart:   canonCacheTime(params.StartTime),
			CanonicalEnd:     canonCacheTime(params.EndTime),
			Timezone:         req.Timezone,
			EffectiveLimit:   int64(effLimit),
			QueryTimeoutSecs: int64(*req.QueryTimeout),
		})
	}

	if source.IsClickHouse() {
		cfg := queryStreamConfig{logsKey: "data"}
		// OOM guardrail: only the dashboard-directive path buffers (bounded by
		// max_entry_bytes); on overflow the fill errors and we fall through to the
		// unbuffered streaming path below, which is left byte-for-byte unchanged.
		if cacheable {
			fillTimeout := time.Duration(*req.QueryTimeout) * time.Second
			if handled, err := s.tryServeDashboardCache(c, cacheKey, effTTL, fillTimeout, s.fillClickHouseStream(sourceID, params, cfg)); handled {
				return err
			}
		}
		return s.streamPreviewQuery(c, sourceID, teamID, user, params,
			cfg, req.QueryText, "sql", req.Limit,
			req.QueryText, models.QueryLanguageClickHouseSQL)
	}

	// Non-streaming providers (VictoriaLogs) already buffer; serve dashboard
	// panels from the cache when eligible.
	if cacheable {
		fillTimeout := time.Duration(*req.QueryTimeout) * time.Second
		fill := func(ctx context.Context) ([]byte, error) {
			result, err := core.QueryLogs(ctx, s.datasources, sourceID, params)
			if err != nil {
				return nil, err
			}
			resp := map[string]any{
				"query_id": uuid.New().String(),
				"data":     result.Logs,
				"stats":    result.Stats,
				"columns":  normalizeResultColumns(nil, result),
				"warnings": result.Warnings,
			}
			return json.Marshal(NewSuccessResponse(resp))
		}
		if handled, err := s.tryServeDashboardCache(c, cacheKey, effTTL, fillTimeout, fill); handled {
			return err
		}
	}

	// Buffered fallback for non-streaming providers.
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
		s.recordQueryHistory(user, teamID, sourceID, req.QueryText, models.QueryLanguageClickHouseSQL,
			int64(result.Stats.ExecutionTimeMs), int64(len(result.Logs)))
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
