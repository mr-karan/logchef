package sources

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/internal/errors"
	"github.com/mr-karan/logchef/pkg/models"
)

type Service struct {
	sourceRepo *models.SourceRepository
	clickhouse *db.Clickhouse
}

func NewService(sourceRepo *models.SourceRepository, clickhouse *db.Clickhouse) *Service {
	return &Service{
		sourceRepo: sourceRepo,
		clickhouse: clickhouse,
	}
}

type CreateSourceRequest struct {
	Name        string `json:"name"`
	SchemaType  string `json:"schema_type"`
	TTLDays     int    `json:"ttl_days"`
	DSN         string `json:"dsn"`
	Description string `json:"description"`
}

func (s *Service) Create(ctx context.Context, req CreateSourceRequest) (*models.Source, error) {
	if err := validateSourceRequest(req); err != nil {
		return nil, err
	}

	// Check if source exists
	existingSource, err := s.sourceRepo.GetByName(req.Name)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to check existing source: %w", err)
	}
	if existingSource != nil {
		return nil, errors.NewConflictError(fmt.Sprintf("source with name '%s' already exists", req.Name))
	}

	source := &models.Source{
		ID:          uuid.New().String(),
		Name:        req.Name,
		TableName:   req.Name,
		SchemaType:  req.SchemaType,
		Description: req.Description,
		DSN:         req.DSN,
	}

	cfg := parseDSN(req.DSN)

	if err := s.clickhouse.CreateSourceTable(source.ID, cfg, source.TableName, req.TTLDays); err != nil {
		return nil, fmt.Errorf("failed to create source: %w", err)
	}

	if err := s.sourceRepo.Create(source); err != nil {
		return nil, fmt.Errorf("failed to create source metadata: %w", err)
	}

	if err := s.clickhouse.GetPool().AddConnection(source.ID, cfg); err != nil {
		_ = s.sourceRepo.Delete(source.ID)
		return nil, fmt.Errorf("failed to initialize connection: %w", err)
	}

	return source, nil
}

func (s *Service) Get(ctx context.Context, id string) (*models.Source, error) {
	source, err := s.sourceRepo.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch source: %w", err)
	}
	return source, nil
}

type UpdateSourceRequest struct {
	TTLDays int `json:"ttl_days"`
}

func (s *Service) Update(ctx context.Context, id string, req UpdateSourceRequest) error {
	source, err := s.sourceRepo.Get(id)
	if err != nil {
		return fmt.Errorf("failed to fetch source: %w", err)
	}

	if req.TTLDays <= 0 {
		return fmt.Errorf("invalid TTL days: %d", req.TTLDays)
	}

	if err := s.clickhouse.UpdateTableTTL(source.ID, source.TableName, req.TTLDays); err != nil {
		return fmt.Errorf("failed to update TTL: %w", err)
	}

	return nil
}

func (s *Service) List(ctx context.Context) ([]*models.Source, error) {
	sources, err := s.sourceRepo.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	if sources == nil {
		sources = []*models.Source{}
	}

	// Initialize connections for all sources
	for _, source := range sources {
		cfg := parseDSN(source.DSN)
		if err := s.clickhouse.GetPool().AddConnection(source.ID, cfg); err != nil {
			slog.Error("failed to initialize connection for source",
				"source_id", source.ID,
				"error", err,
			)
		}
	}

	return sources, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	source, err := s.sourceRepo.Get(id)
	if err != nil {
		return fmt.Errorf("failed to fetch source: %w", err)
	}

	if err := s.sourceRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete source: %w", err)
	}

	if err := s.clickhouse.DropSourceTable(source.ID, source.TableName); err != nil {
		slog.Error("failed to drop clickhouse table",
			"error", err,
			"source_id", source.ID,
			"table", source.TableName,
		)
	}

	return nil
}
