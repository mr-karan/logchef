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
