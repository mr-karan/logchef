package db

import (
	"log/slog"
	"time"
)

// QueryLogger wraps query execution with logging
type QueryLogger struct {
	sourceID string
}

func NewQueryLogger(sourceID string) *QueryLogger {
	return &QueryLogger{sourceID: sourceID}
}

func (l *QueryLogger) LogQuery(query string, args []interface{}, err error, duration time.Duration) {
	if err != nil {
		slog.Error("clickhouse query failed",
			"source_id", l.sourceID,
			"query", query,
			"args", args,
			"error", err,
			"duration_ms", duration.Milliseconds(),
		)
		return
	}

	slog.Debug("clickhouse query executed",
		"source_id", l.sourceID,
		"query", query,
		"args", args,
		"duration_ms", duration.Milliseconds(),
	)
}
