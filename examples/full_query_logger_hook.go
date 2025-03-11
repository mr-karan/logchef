package main

import (
	"context"
	"log/slog"
	"time"
)

// FullQueryLoggerHook is a query hook that logs the full query
type FullQueryLoggerHook struct {
	logger *slog.Logger
}

// NewFullQueryLoggerHook creates a new FullQueryLoggerHook
func NewFullQueryLoggerHook(logger *slog.Logger) *FullQueryLoggerHook {
	return &FullQueryLoggerHook{
		logger: logger,
	}
}

// BeforeQuery logs the full query before execution
func (h *FullQueryLoggerHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	h.logger.Info("executing query",
		"query", query,
		"timestamp", time.Now().Format(time.RFC3339),
	)
	return ctx, nil
}

// AfterQuery logs the query result after execution
func (h *FullQueryLoggerHook) AfterQuery(ctx context.Context, query string, err error, duration time.Duration) {
	if err != nil {
		h.logger.Error("query failed",
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
	} else {
		h.logger.Info("query completed",
			"duration_ms", duration.Milliseconds(),
		)
	}
}

// Example usage:
//
// func main() {
//     logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
//     manager := clickhouse.NewManager(logger)
//
//     // Add the full query logger hook
//     fullQueryHook := NewFullQueryLoggerHook(logger)
//     manager.AddQueryHook(fullQueryHook)
//
//     // Now all queries will be logged with the full query text
// }
