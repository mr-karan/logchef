package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func main() {
	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create a ClickHouse manager
	manager := clickhouse.NewManager(logger)

	// Create a verbose query hook that logs all queries
	verboseHook := clickhouse.NewLogQueryHook(logger, true)
	manager.AddQueryHook(verboseHook)

	// Add a source
	source := &models.Source{
		ID: 1,
		Connection: models.ConnectionInfo{
			Host:      "localhost:9000",
			Database:  "logs",
			TableName: "logs",
			Username:  "default",
			Password:  "",
		},
	}

	if err := manager.AddSource(source); err != nil {
		logger.Error("failed to add source", "error", err)
		return
	}

	// Execute a query
	ctx := context.Background()
	params := clickhouse.TimeSeriesParams{
		StartTime: time.Now().Add(-24 * time.Hour),
		EndTime:   time.Now(),
		Window:    clickhouse.TimeWindow1h,
	}

	result, err := manager.GetTimeSeries(ctx, source.ID, source, params)
	if err != nil {
		logger.Error("failed to get time series", "error", err)
		return
	}

	logger.Info("got time series result", "buckets", len(result.Buckets))
}
