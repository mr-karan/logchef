package service

import (
	"context"
	"fmt"

	"backend-v2/pkg/clickhouse"
)

// QueryLogs retrieves logs from a source with pagination and time range
func (s *Service) QueryLogs(ctx context.Context, sourceID string, params clickhouse.LogQueryParams) (*clickhouse.LogQueryResult, error) {
	// Get source from SQLite
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("source not found")
	}

	// Query logs
	return s.clickhouse.QueryLogs(ctx, sourceID, params)
}
