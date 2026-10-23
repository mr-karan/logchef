package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	dashcache "github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// AIRequestTimeout is the maximum time to wait for the AI provider to respond.
	// Reasoning models spend several seconds thinking before emitting any text, and
	// the generator may follow up with a repair attempt, so this is generous.
	AIRequestTimeout = 90 * time.Second
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
	// Histograms and dashboard fills are admitted apart from preview. An
	// explorer view issues a log query and a histogram together, and a
	// dashboard refresh issues one fill per panel; charging either to the
	// preview budget makes ordinary exploration reject itself.
	QueryClassHistogram QueryClass = "histogram"
	QueryClassDashboard QueryClass = "dashboard"
)

// admissionLimits returns the per-user and global caps for class. Dashboard
// fills use the cache's own fill budget: the fill is shared work on behalf of
// every waiter on that panel, and a multi-panel refresh would reject its own
// panels under the interactive preview cap.
func (s *Server) admissionLimits(class QueryClass) (maxPerUser, maxGlobal int) {
	if class == QueryClassDashboard {
		// The cache's fill semaphore already bounds concurrent fills globally,
		// so admission only has to keep one user from taking the whole budget.
		return s.config.DashboardCache.MaxConcurrentFills, 0
	}
	return s.config.Query.MaxConcurrentPerUser, s.config.Query.MaxConcurrentGlobal
}

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

const logsQueryFailure = "Failed to query logs"

// handleQueryLogs handles requests to query logs for a specific source.
// Access is controlled by the requireTeamHasSource middleware.
func (s *Server) handleQueryLogs(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	sourceID, teamID := src.SourceID(), src.TeamID()

	var req models.APIQueryRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Get user information for query tracking
	user := currentUser(c)
	if user == nil {
		return sendMissingUser(c)
	}

	params, err := core.PrepareSourceSQLQuery(c.Context(), s.datasources, s.config.Query, src, req)
	if err != nil {
		return s.sendQueryError(c, src.SourceID(), err, logsQueryFailure, logsQueryOperation)
	}
	processedQuery := params.RawQuery

	source, err := core.LoadSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		return s.sendQueryError(c, src.SourceID(), err, logsQueryFailure, logsQueryOperation)
	}
	if err := datasource.ValidateSourceStreamFilters(source.SourceType, req.ExtraStreamFilters); err != nil {
		return s.sendQueryError(c, src.SourceID(), err, logsQueryFailure, logsQueryOperation)
	}
	// Dashboard panel requests may opt into the per-dashboard result cache. The
	// cache key is computed from the finalized (post-substitution) executable
	// query and the resolved parameters; source.UpdatedAt invalidates entries on
	// a source config change. Explorer/ad-hoc requests carry no directive and
	// stay uncached, preserving the streaming path exactly.
	effTTL, cacheable := s.dashboardCacheParams(req.Cache)
	cacheable = cacheable && len(req.ExtraStreamFilters) == 0
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
			QueryTimeoutSecs: int64(*params.QueryTimeout),
		})
	}

	return s.executePreviewQuery(c, user, previewQuery{
		src:             src,
		source:          source,
		params:          params,
		cfg:             queryStreamConfig{logsKey: "data", failure: logsQueryFailure, operation: logsQueryOperation},
		mode:            "sql",
		trackedQuery:    req.QueryText,
		requestedLimit:  req.Limit,
		historyQuery:    req.QueryText,
		historyLanguage: models.QueryLanguageClickHouseSQL,
		cacheable:       cacheable,
		cacheKey:        cacheKey,
		cacheTTL:        effTTL,
	})
}

// previewQuery is a prepared /logs/query or /logchefql/query request. The two
// handlers differ only in preparation; executePreviewQuery runs both.
type previewQuery struct {
	src    access.AuthorizedSource
	source *models.Source
	params datasource.QueryRequest
	cfg    queryStreamConfig
	// orderColumnsBySchema uses source schema order for inferred columns; false sorts by name.
	orderColumnsBySchema bool
	mode                 string
	trackedQuery         string
	requestedLimit       int
	historyQuery         string
	historyLanguage      models.QueryLanguage
	cacheable            bool
	cacheKey             [32]byte
	cacheTTL             time.Duration
}

// executePreviewQuery serves q from the dashboard cache when it is cacheable,
// streams it from ClickHouse, and otherwise runs it buffered.
func (s *Server) executePreviewQuery(c fiber.Ctx, user *models.User, q previewQuery) error {
	sourceID, teamID := q.src.SourceID(), q.src.TeamID()
	fillTimeout := time.Duration(*q.params.QueryTimeout) * time.Second

	// ClickHouse-backed sources stream the response body row-by-row so server
	// memory stays bounded regardless of result size. Other source types
	// (VictoriaLogs) keep the buffered path.
	if q.source.IsClickHouse() {
		// OOM guardrail: only the dashboard-directive path buffers (bounded by
		// max_entry_bytes); on overflow the fill errors and we fall through to the
		// unbuffered streaming path below, which is left byte-for-byte unchanged.
		if q.cacheable {
			if handled, err := s.tryServeDashboardCache(c, q.cacheKey, q.cacheTTL, fillTimeout, s.fillClickHouseStream(user.ID, teamID, sourceID, q.params, q.cfg)); handled {
				return err
			} else if err != nil {
				s.log.Error("failed to stream query", "error", err, "source_id", sourceID, "mode", q.mode)
				return s.writeDashboardStreamError(c, sourceID, err, q.cfg.failure, q.cfg.operation)
			}
		}
		return s.streamPreviewQuery(c, user, q)
	}

	// Non-streaming providers (VictoriaLogs) already buffer; serve dashboard
	// panels from the cache when eligible.
	if q.cacheable {
		fill := s.dashboardQueryFill(user.ID, teamID, sourceID, q.params.RawQuery, func(ctx context.Context, queryID string) ([]byte, error) {
			result, err := core.QueryLogs(ctx, s.datasources, q.src, q.params)
			if err != nil {
				return nil, err
			}
			return json.Marshal(NewSuccessResponse(q.bufferedResponse(result, queryID)))
		})
		if handled, err := s.tryServeDashboardCache(c, q.cacheKey, q.cacheTTL, fillTimeout, fill); handled {
			return err
		} else if err != nil {
			return s.sendQueryError(c, sourceID, err, q.cfg.failure, q.cfg.operation)
		}
	}

	queryCtx, cancel := context.WithCancel(c.Context())
	defer cancel()

	queryID, err := queryTracker.StartQuery(
		QueryClassPreview,
		user.ID,
		sourceID,
		teamID,
		q.trackedQuery,
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

	result, err := core.QueryLogs(queryCtx, s.datasources, q.src, q.params)
	if err != nil {
		return s.sendQueryError(c, sourceID, err, q.cfg.failure, q.cfg.operation)
	}

	s.log.Info("query.execute",
		"user", user.Email,
		"team_id", teamID,
		"source_id", sourceID,
		"mode", q.mode,
		"query_id", queryID,
		"rows", len(result.Logs),
		"duration_ms", result.Stats.ExecutionTimeMs,
		"limit_requested", q.requestedLimit,
		"limit_applied", result.Stats.LimitApplied,
		"truncated", result.Stats.Truncated,
	)
	s.recordQueryHistory(user, teamID, sourceID, q.historyQuery, q.historyLanguage,
		int64(result.Stats.ExecutionTimeMs), int64(len(result.Logs)))

	return SendSuccess(c, fiber.StatusOK, q.bufferedResponse(result, queryID))
}

// bufferedResponse has the same keys as the streamed response for q.
func (q previewQuery) bufferedResponse(result *models.QueryResult, queryID string) map[string]any {
	var columnsSource *models.Source
	if q.orderColumnsBySchema {
		columnsSource = q.source
	}
	resp := map[string]any{
		"query_id":    queryID,
		q.cfg.logsKey: result.Logs,
		"stats":       result.Stats,
		"columns":     core.ResultColumns(columnsSource, result),
		"warnings":    result.Warnings,
	}
	if q.cfg.includeGenerated {
		resp["generated_sql"] = q.cfg.generatedSQL // Deprecated legacy field kept for compatibility.
		resp["generated_query"] = q.cfg.generatedQuery
		resp["generated_query_language"] = q.cfg.generatedLanguage
		resp["conditions"] = q.cfg.conditions
		resp["fields_used"] = q.cfg.fieldsUsed
	}
	return resp
}

// handleCancelQuery cancels a running query for a specific source
func (s *Server) handleCancelQuery(c fiber.Ctx) error {
	// Get query ID from params
	queryID := c.Params("queryID")
	if queryID == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Query ID is required", models.ValidationErrorType)
	}

	// Get user information
	user := currentUser(c)
	if user == nil {
		return sendMissingUser(c)
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
