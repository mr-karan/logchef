package clickhouse

import (
	"context"
	"log/slog"
	"time"
)

// QueryHook defines an interface for intercepting ClickHouse queries.
// Hooks can modify the context or log query details before and after execution.
type QueryHook interface {
	// BeforeQuery is called before a query is executed.
	// It can return a modified context for the query execution.
	BeforeQuery(ctx context.Context, query string) (context.Context, error)

	// AfterQuery is called after a query has finished executing,
	// regardless of whether it succeeded or failed.
	AfterQuery(ctx context.Context, query string, err error, duration time.Duration)
}

// LogQueryHook is a basic QueryHook implementation that logs query
// execution start and completion/failure, controlled by the Verbose flag.
type LogQueryHook struct {
	logger *slog.Logger
	// Verbose logs all queries if true; otherwise, only logs failed queries.
	Verbose bool
}

// NewLogQueryHook creates a new LogQueryHook.
func NewLogQueryHook(logger *slog.Logger, verbose bool) *LogQueryHook {
	return &LogQueryHook{
		logger:  logger,
		Verbose: verbose,
	}
}

// StructuredQueryLoggerHook implements QueryHook to log query details
// *before* execution using structured logging attributes.
type StructuredQueryLoggerHook struct {
	logger *slog.Logger
}

// NewStructuredQueryLoggerHook creates a new StructuredQueryLoggerHook.
func NewStructuredQueryLoggerHook(logger *slog.Logger) *StructuredQueryLoggerHook {
	return &StructuredQueryLoggerHook{
		// Add a specific hook attribute for easier log filtering.
		logger: logger.With("hook", "structured_query_logger"),
	}
}

// BeforeQuery logs query details using structured logging.
func (h *StructuredQueryLoggerHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	h.logger.Debug("executing query",
		slog.Group("query_details",
			slog.String("sql", query),
			// TODO: Add more context if available (e.g., user ID, request ID from context).
		),
	)
	return ctx, nil
}

// AfterQuery is a no-op for StructuredQueryLoggerHook.
func (h *StructuredQueryLoggerHook) AfterQuery(ctx context.Context, query string, err error, duration time.Duration) {
	// This hook only logs before the query.
}

// BeforeQuery optionally logs the query before execution if Verbose is true.
func (h *LogQueryHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	if h.Verbose {
		h.logger.Debug("executing query", "query", query)
	}
	return ctx, nil
}

// AfterQuery logs query completion status (success/failure) and duration.
// Success is only logged if Verbose is true.
func (h *LogQueryHook) AfterQuery(ctx context.Context, query string, err error, duration time.Duration) {
	if err != nil {
		h.logger.Error("query failed",
			"query", query,
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
	} else if h.Verbose {
		h.logger.Debug("query completed",
			"query", query,
			"duration_ms", duration.Milliseconds(),
		)
	}
}
