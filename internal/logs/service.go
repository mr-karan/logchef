package logs

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

type Service struct {
	logRepo      *db.LogRepository
	sourceRepo   *models.SourceRepository
	validators   map[QueryMode]QueryValidator
	sqlValidator *SQLValidator
}

func NewService(logRepo *db.LogRepository, sourceRepo *models.SourceRepository) *Service {
	s := &Service{
		logRepo:      logRepo,
		sourceRepo:   sourceRepo,
		validators:   make(map[QueryMode]QueryValidator),
		sqlValidator: NewSQLValidator(),
	}

	// Register validators for each mode
	s.validators[QueryModeSQL] = s.sqlValidator

	return s
}

type TimeRange struct {
	Start *time.Time
	End   *time.Time
}

func (s *Service) getSource(ctx context.Context, sourceID string) (*models.Source, error) {
	source, err := s.sourceRepo.Get(sourceID)
	if err != nil {
		return nil, fmt.Errorf("source not found: %w", err)
	}
	return source, nil
}

func (s *Service) QueryLogs(ctx context.Context, sourceID string, req QueryRequest) (*models.LogResponse, error) {
	// Validation
	if validator, ok := s.validators[req.Mode]; ok {
		if err := validator.Validate(ctx, &req); err != nil {
			return nil, err
		}
	}

	source, err := s.getSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	// Query execution based on mode
	switch req.Mode {
	case QueryModeBasic:
		return s.logRepo.QueryLogs(ctx, sourceID, req.Params)
	case QueryModeLogchefQL:
		return s.executeLogchefQLQuery(ctx, source, req)
	case QueryModeSQL:
		return s.executeSQLQuery(ctx, source, req)
	default:
		return nil, ErrInvalidMode
	}
}

func (s *Service) GetSchema(ctx context.Context, sourceID string, timeRange TimeRange) ([]models.LogSchema, error) {
	if err := validateTimeRange(timeRange.Start, timeRange.End); err != nil {
		return nil, err
	}

	source, err := s.getSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	schema, err := s.logRepo.GetLogSchema(ctx, source, *timeRange.Start, *timeRange.End)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	return schema, nil
}

func (s *Service) executeLogchefQLQuery(ctx context.Context, source *models.Source, req QueryRequest) (*models.LogResponse, error) {
	query, err := logchefql.Parse(req.Query)
	if err != nil {
		return nil, &QueryError{
			Code:    "SYNTAX_ERROR",
			Message: "Invalid LogchefQL syntax",
			Mode:    QueryModeLogchefQL,
			Details: err.Error(),
		}
	}

	// Convert LogchefQL to SQL
	sqlQuery, args := query.ToSQL(source.TableName)

	// Execute the SQL query
	return s.logRepo.ExecuteRawQuery(ctx, source.ID, sqlQuery, args)
}

func (s *Service) executeSQLQuery(ctx context.Context, source *models.Source, req QueryRequest) (*models.LogResponse, error) {
	return s.logRepo.ExecuteRawQuery(ctx, source.ID, req.Query, nil)
}
