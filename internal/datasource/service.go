package datasource

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

type Provider interface {
	Type() models.SourceType
	PrepareSource(context.Context, *models.CreateSourceRequest) (*models.Source, error)
	ValidateConnection(context.Context, *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error)
	QueryLogs(context.Context, *models.Source, QueryRequest) (*models.QueryResult, error)
	GetSourceSchema(context.Context, *models.Source) ([]models.ColumnInfo, error)
	Histogram(context.Context, *models.Source, HistogramRequest) (*HistogramResult, error)
	LogContext(context.Context, *models.Source, LogContextRequest) (*LogContextResult, error)
	EvaluateAlert(context.Context, *models.Source, AlertQueryRequest) (*models.QueryResult, error)
	InitializeSource(context.Context, *models.Source) error
	RemoveSource(models.SourceID) error
	CheckSourceConnectionStatus(context.Context, *models.Source) bool
	GetSourceHealth(context.Context, models.SourceID) models.SourceHealth
}

type Service struct {
	db        *sqlite.DB
	log       *slog.Logger
	providers map[models.SourceType]Provider
}

func NewService(db *sqlite.DB, log *slog.Logger) *Service {
	return &Service{
		db:        db,
		log:       log.With("component", "datasource_service"),
		providers: make(map[models.SourceType]Provider),
	}
}

func (s *Service) Register(provider Provider) {
	if provider == nil {
		return
	}
	s.providers[provider.Type()] = provider
}

func (s *Service) ProviderForSourceType(sourceType models.SourceType) (Provider, error) {
	normalized := models.NormalizeSourceType(sourceType)
	provider, ok := s.providers[normalized]
	if !ok {
		return nil, fmt.Errorf("no provider registered for source type %q", normalized)
	}
	return provider, nil
}

func (s *Service) ProviderForSource(source *models.Source) (Provider, error) {
	if source == nil {
		return nil, fmt.Errorf("source is required")
	}
	return s.ProviderForSourceType(source.SourceType)
}

func (s *Service) InitializeSource(ctx context.Context, source *models.Source) error {
	provider, err := s.ProviderForSource(source)
	if err != nil {
		return err
	}
	return provider.InitializeSource(ctx, source)
}

func (s *Service) CreateSource(ctx context.Context, req *models.CreateSourceRequest) (*models.Source, error) {
	if req == nil {
		return nil, fmt.Errorf("create source request is required")
	}

	provider, err := s.ProviderForSourceType(req.SourceType)
	if err != nil {
		return nil, err
	}

	source, err := provider.PrepareSource(ctx, req)
	if err != nil {
		return nil, err
	}

	existingSource, err := s.db.GetSourceByIdentityKey(ctx, source.IdentityKey)
	if err == nil && existingSource != nil {
		return nil, fmt.Errorf("source identity %q already exists (ID: %d): %w", source.IdentityKey, existingSource.ID, ErrSourceAlreadyExists)
	}
	if err != nil && !sqlite.IsNotFoundError(err) && !sqlite.IsSourceNotFoundError(err) {
		return nil, fmt.Errorf("check existing source identity: %w", err)
	}

	if err := s.db.CreateSource(ctx, source); err != nil {
		return nil, fmt.Errorf("save source configuration: %w", err)
	}

	if err := provider.InitializeSource(ctx, source); err != nil {
		if delErr := s.db.DeleteSource(ctx, source.ID); delErr != nil {
			s.log.Error("failed to rollback datasource after initialization error",
				"source_id", source.ID,
				"delete_error", delErr,
			)
		}
		return nil, fmt.Errorf("initialize source: %w", err)
	}

	return source, nil
}

func (s *Service) ValidateConnection(ctx context.Context, req *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error) {
	if req == nil {
		return nil, fmt.Errorf("validate connection request is required")
	}

	provider, err := s.ProviderForSourceType(req.SourceType)
	if err != nil {
		return nil, err
	}

	return provider.ValidateConnection(ctx, req)
}

func (s *Service) QueryLogs(ctx context.Context, sourceID models.SourceID, req QueryRequest) (*models.QueryResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.QueryLogs(ctx, source, req)
}

func (s *Service) GetSourceSchema(ctx context.Context, sourceID models.SourceID) ([]models.ColumnInfo, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.GetSourceSchema(ctx, source)
}

func (s *Service) Histogram(ctx context.Context, sourceID models.SourceID, req HistogramRequest) (*HistogramResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.Histogram(ctx, source, req)
}

func (s *Service) LogContext(ctx context.Context, sourceID models.SourceID, req LogContextRequest) (*LogContextResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.LogContext(ctx, source, req)
}

func (s *Service) EvaluateAlert(ctx context.Context, sourceID models.SourceID, req AlertQueryRequest) (*models.QueryResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.EvaluateAlert(ctx, source, req)
}

func (s *Service) RemoveSource(source *models.Source) error {
	provider, err := s.ProviderForSource(source)
	if err != nil {
		return err
	}
	return provider.RemoveSource(source.ID)
}

func (s *Service) CheckSourceConnectionStatus(ctx context.Context, source *models.Source) bool {
	provider, err := s.ProviderForSource(source)
	if err != nil {
		s.log.Warn("failed to resolve provider for source status check", "source_id", source.ID, "error", err)
		return false
	}
	return provider.CheckSourceConnectionStatus(ctx, source)
}

func (s *Service) GetSourceHealth(ctx context.Context, sourceID models.SourceID) (models.SourceHealth, error) {
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return models.SourceHealth{}, err
	}

	provider, err := s.ProviderForSource(source)
	if err != nil {
		return models.SourceHealth{
			SourceID:    sourceID,
			Status:      models.HealthStatusUnhealthy,
			Error:       err.Error(),
			LastChecked: source.UpdatedAt,
		}, nil
	}

	return provider.GetSourceHealth(ctx, sourceID), nil
}

func (s *Service) InitializeAllSources(ctx context.Context) error {
	sources, err := s.db.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("list sources: %w", err)
	}

	for _, source := range sources {
		if source == nil {
			continue
		}

		s.log.Info("initializing datasource connection",
			"source_id", source.ID,
			"source_type", source.SourceType,
			"identity_key", source.IdentityKey)

		if err := s.InitializeSource(ctx, source); err != nil {
			s.log.Warn("failed to initialize datasource connection, continuing",
				"source_id", source.ID,
				"source_type", source.SourceType,
				"error", err)
		}
	}

	return nil
}

func (s *Service) sourceAndProvider(ctx context.Context, sourceID models.SourceID) (*models.Source, Provider, error) {
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}

	provider, err := s.ProviderForSource(source)
	if err != nil {
		return nil, nil, err
	}

	return source, provider, nil
}
