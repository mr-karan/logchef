package metrics

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

type contextKey string

const (
	ctxKeyStartTime contextKey = "unified_metrics_start_time"
	ctxKeyQueryType contextKey = "unified_metrics_query_type"
)

// ClickHouseMetrics provides a metrics collector for ClickHouse operations
type ClickHouseMetrics struct {
	source *models.Source
}

// NewClickHouseMetrics creates a metrics collector for a specific source
func NewClickHouseMetrics(source *models.Source) *ClickHouseMetrics {
	return &ClickHouseMetrics{
		source: source,
	}
}

// RecordQueryMetrics records comprehensive query execution metrics
func (m *ClickHouseMetrics) RecordQueryMetrics(
	queryType string,
	success bool,
	duration time.Duration,
	rowsReturned int64,
	errorType string,
	timedOut bool,
	user *models.User,
) {
	// Record unified query metrics with rich context
	RecordQuery(m.source, queryType, success, duration, rowsReturned, user)

	// Record timeout if applicable
	if timedOut {
		RecordQueryTimeout(m.source, queryType)
	}

	// Record error if applicable
	if !success && errorType != "" {
		RecordQueryError(m.source, errorType)
	}
}

// RecordHistogramMetrics records histogram generation metrics
func (m *ClickHouseMetrics) RecordHistogramMetrics(success bool, duration time.Duration, user *models.User) {
	RecordHistogram(m.source, success, duration, user)
}

// RecordConnectionValidation records connection validation metrics
func (m *ClickHouseMetrics) RecordConnectionValidation(success bool) {
	RecordClickHouseValidation(m.source, success)
}

// RecordReconnection records reconnection attempt metrics
func (m *ClickHouseMetrics) RecordReconnection(success bool) {
	RecordClickHouseReconnection(m.source, success)
}

// UpdateConnectionStatus updates the connection health status
func (m *ClickHouseMetrics) UpdateConnectionStatus(healthy bool) {
	RecordClickHouseConnectionStatus(m.source, healthy)
}

// QueryMetricsHelper provides a helper for timing query operations
type QueryMetricsHelper struct {
	metrics   *ClickHouseMetrics
	queryType string
	startTime time.Time
	user      *models.User
}

// StartQuery begins tracking a query operation
func (m *ClickHouseMetrics) StartQuery(queryType string, user *models.User) *QueryMetricsHelper {
	return &QueryMetricsHelper{
		metrics:   m,
		queryType: queryType,
		startTime: time.Now(),
		user:      user,
	}
}

// Finish completes the query tracking and records metrics
func (h *QueryMetricsHelper) Finish(success bool, rowsReturned int64, errorType string, timedOut bool) {
	duration := time.Since(h.startTime)
	h.metrics.RecordQueryMetrics(h.queryType, success, duration, rowsReturned, errorType, timedOut, h.user)
}

// MetricsQueryHook implements the ClickHouse QueryHook interface
// This requires the source to be available, so it's best used when you have source context
type MetricsQueryHook struct {
	source *models.Source
}

// NewMetricsQueryHook creates a new metrics query hook
func NewMetricsQueryHook(source *models.Source) *MetricsQueryHook {
	return &MetricsQueryHook{
		source: source,
	}
}

// BeforeQuery is called before query execution
func (h *MetricsQueryHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	// Store query start time and type in context
	queryType := DetermineQueryType(query)

	// Store in context for use in AfterQuery
	ctx = context.WithValue(ctx, ctxKeyStartTime, time.Now())
	ctx = context.WithValue(ctx, ctxKeyQueryType, queryType)

	return ctx, nil
}

// AfterQuery is called after query execution
func (h *MetricsQueryHook) AfterQuery(ctx context.Context, query string, err error, duration time.Duration) {
	// Extract values from context
	queryType, _ := ctx.Value(ctxKeyQueryType).(string)

	if queryType == "" {
		queryType = DetermineQueryType(query)
	}

	// Determine success and error type
	success := err == nil
	errorType := DetermineErrorType(err)

	// Check if it was a timeout
	timedOut := contains(trimAndLower(errorType), "timeout")

	// Try to get user from context (may not always be available in hooks)
	var user *models.User
	if userFromContext := ctx.Value("current_user"); userFromContext != nil {
		if u, ok := userFromContext.(*models.User); ok {
			user = u
		}
	}

	// Record metrics
	RecordQuery(h.source, queryType, success, duration, -1, user)

	if timedOut {
		RecordQueryTimeout(h.source, queryType)
	}

	if !success && errorType != "" {
		RecordQueryError(h.source, errorType)
	}
}
