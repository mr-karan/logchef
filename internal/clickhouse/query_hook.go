package clickhouse

import (
	"context"
	"log/slog"
	"time"
)

// QueryHook defines the interface for query hooks
type QueryHook interface {
	// BeforeQuery is called before a query is executed
	BeforeQuery(ctx context.Context, query string) (context.Context, error)

	// AfterQuery is called after a query is executed
	AfterQuery(ctx context.Context, query string, err error, duration time.Duration)
}

// LogQueryHook implements QueryHook to log queries
type LogQueryHook struct {
	logger *slog.Logger
	// Verbose determines whether to log all queries or only failed ones
	Verbose bool
}

// NewLogQueryHook creates a new LogQueryHook
func NewLogQueryHook(logger *slog.Logger, verbose bool) *LogQueryHook {
	return &LogQueryHook{
		logger:  logger,
		Verbose: verbose,
	}
}

// StructuredQueryLoggerHook implements QueryHook to log query details in a structured format
type StructuredQueryLoggerHook struct {
	logger *slog.Logger
}

// NewStructuredQueryLoggerHook creates a new StructuredQueryLoggerHook
func NewStructuredQueryLoggerHook(logger *slog.Logger) *StructuredQueryLoggerHook {
	return &StructuredQueryLoggerHook{
		logger: logger.With("hook", "structured_query_logger"),
	}
}

// BeforeQuery logs the query details before execution using structured logging
func (h *StructuredQueryLoggerHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	h.logger.Debug("executing query",
		slog.Group("query_details",
			slog.String("sql", query),
			// TODO: Add more context if available, e.g., user ID, request ID from context
		),
	)
	return ctx, nil
}

// AfterQuery is a no-op for this hook as it only logs before execution
func (h *StructuredQueryLoggerHook) AfterQuery(ctx context.Context, query string, err error, duration time.Duration) {
	// No action needed after query execution for this specific hook
}

// BeforeQuery logs the query before execution
func (h *LogQueryHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	if h.Verbose {
		h.logger.Info("executing query", "query", query)
	}
	return ctx, nil
}

// AfterQuery logs the query result after execution
func (h *LogQueryHook) AfterQuery(ctx context.Context, query string, err error, duration time.Duration) {
	if err != nil {
		h.logger.Error("query failed",
			"query", query,
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
	} else if h.Verbose {
		h.logger.Info("query completed",
			"query", query,
			"duration_ms", duration.Milliseconds(),
		)
	}
}
