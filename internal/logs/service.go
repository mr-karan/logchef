package logs

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

type Service struct {
	logRepo       *db.LogRepository
	sourceRepo    *models.SourceRepository
	logchefqlExec *logchefql.Executor
}

func NewService(logRepo *db.LogRepository, sourceRepo *models.SourceRepository, executor db.QueryExecutor) *Service {
	return &Service{
		logRepo:       logRepo,
		sourceRepo:    sourceRepo,
		logchefqlExec: logchefql.NewExecutor(executor),
	}
}

func (s *Service) QueryLogs(ctx context.Context, sourceID string, req QueryRequest) (*models.LogResponse, error) {
	switch req.Mode {
	case QueryModeBasic:
		// First get the logs
		logs, err := s.logRepo.QueryLogs(ctx, sourceID, req.Params)
		if err != nil {
			return nil, fmt.Errorf("failed to query logs: %w", err)
		}

		// Then get total count if we have logs
		var totalCount int
		if len(logs.Logs) > 0 {
			totalCount, err = s.GetTotalLogsCount(ctx, sourceID, req.Params)
			if err != nil {
				// If total count fails, we can still return the logs
				// Just use the length of logs as count
				totalCount = len(logs.Logs)
			}
		}

		// Calculate if there are more results
		hasMore := logs.HasMore
		if !hasMore && totalCount > 0 {
			hasMore = (req.Params.Offset + req.Params.Limit) < totalCount
		}

		return &models.LogResponse{
			Logs:       logs.Logs,
			TotalCount: totalCount,
			HasMore:    hasMore,
		}, nil
	case QueryModeLogchefQL:
		source, err := s.sourceRepo.Get(sourceID)
		if err != nil {
			return nil, fmt.Errorf("source not found: %w", err)
		}
		return s.logchefqlExec.Execute(ctx, source, req.Query)
	case QueryModeSQL:
		source, err := s.sourceRepo.Get(sourceID)
		if err != nil {
			return nil, fmt.Errorf("source not found: %w", err)
		}
		return s.logRepo.Executor.ExecuteRawQuery(ctx, source, req.Query, nil)
	default:
		return nil, ErrInvalidMode
	}
}

func (s *Service) GetSchema(ctx context.Context, sourceID string, timeRange TimeRange) ([]models.LogSchema, error) {
	if err := timeRange.Validate(); err != nil {
		return nil, err
	}

	source, err := s.sourceRepo.Get(sourceID)
	if err != nil {
		return nil, err
	}

	schema, err := s.logRepo.GetLogSchema(ctx, source, *timeRange.Start, *timeRange.End)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	return schema, nil
}

func (s *Service) GetTotalLogsCount(ctx context.Context, sourceID string, params models.LogQueryParams) (int, error) {
	source, err := s.sourceRepo.Get(sourceID)
	if err != nil {
		return 0, fmt.Errorf("source not found: %w", err)
	}

	return s.logRepo.GetTotalLogsCount(ctx, source, params)
}
