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
	Capabilities() []Capability
	SupportedQueryLanguages() []models.QueryLanguage
	SupportedSavedQueryEditorModes() []models.SavedQueryEditorMode
	SupportedAlertEditorModes() []models.AlertEditorMode
	PrepareSource(context.Context, *models.CreateSourceRequest) (*models.Source, error)
	ValidateConnection(context.Context, *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error)
	UpdateSource(context.Context, *models.Source, *models.UpdateSourceRequest) (*SourceUpdateResult, error)
	PopulateSourceDetails(context.Context, *models.Source) error
	QueryLogs(context.Context, *models.Source, QueryRequest) (*models.QueryResult, error)
	GetSourceSchema(context.Context, *models.Source) ([]models.ColumnInfo, error)
	Histogram(context.Context, *models.Source, HistogramRequest) (*HistogramResult, error)
	GetFieldValues(context.Context, *models.Source, FieldValuesRequest) (*FieldValuesResult, error)
	GetAllFieldValues(context.Context, *models.Source, AllFieldValuesRequest) (AllFieldValuesResult, error)
	GetSourceStats(context.Context, *models.Source) (*SourceStats, error)
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

type Capability string

const (
	CapabilitySchemaInspection Capability = "schema_inspection"
	CapabilityHistogram        Capability = "histogram"
	CapabilityFieldValues      Capability = "field_values"
	CapabilitySourceStats      Capability = "source_stats"
	CapabilityAISQLGeneration  Capability = "ai_sql_generation"
)

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

	if err := s.ApplySourceMetadata(source); err != nil {
		return nil, err
	}
	source.IsConnected = provider.CheckSourceConnectionStatus(ctx, source)

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

func (s *Service) ValidateSavedQuerySupport(ctx context.Context, sourceID models.SourceID, language models.QueryLanguage, mode models.SavedQueryEditorMode) error {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return err
	}

	normalizedLanguage := models.NormalizeQueryLanguage(language)
	if !supportsQueryLanguage(provider.SupportedQueryLanguages(), normalizedLanguage) {
		return fmt.Errorf("%s does not support saved query language %q", source.SourceType, normalizedLanguage)
	}

	normalizedMode := models.NormalizeSavedQueryEditorMode(mode)
	if !supportsSavedQueryEditorMode(provider.SupportedSavedQueryEditorModes(), normalizedMode) {
		return fmt.Errorf("%s does not support saved query editor mode %q", source.SourceType, normalizedMode)
	}

	return nil
}

func (s *Service) ValidateAlertSupport(ctx context.Context, sourceID models.SourceID, language models.QueryLanguage, mode models.AlertEditorMode) error {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return err
	}

	normalizedLanguage := models.NormalizeQueryLanguage(language)
	if !supportsQueryLanguage(provider.SupportedQueryLanguages(), normalizedLanguage) {
		return fmt.Errorf("%s does not support alert query language %q", source.SourceType, normalizedLanguage)
	}

	normalizedMode := models.NormalizeAlertEditorMode(mode)
	if !supportsAlertEditorMode(provider.SupportedAlertEditorModes(), normalizedMode) {
		return fmt.Errorf("%s does not support alert editor mode %q", source.SourceType, normalizedMode)
	}

	return nil
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

func (s *Service) EvaluateAlert(ctx context.Context, sourceID models.SourceID, req AlertQueryRequest) (*models.QueryResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.EvaluateAlert(ctx, source, req)
}

func (s *Service) GetFieldValues(ctx context.Context, sourceID models.SourceID, req FieldValuesRequest) (*FieldValuesResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.GetFieldValues(ctx, source, req)
}

func (s *Service) GetAllFieldValues(ctx context.Context, sourceID models.SourceID, req AllFieldValuesRequest) (AllFieldValuesResult, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.GetAllFieldValues(ctx, source, req)
}

func (s *Service) GetSourceStats(ctx context.Context, sourceID models.SourceID) (*SourceStats, error) {
	source, provider, err := s.sourceAndProvider(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return provider.GetSourceStats(ctx, source)
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

func (s *Service) ApplySourceMetadata(source *models.Source) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}

	provider, err := s.ProviderForSource(source)
	if err != nil {
		return err
	}

	queryLanguages := provider.SupportedQueryLanguages()
	source.QueryLanguages = make([]models.QueryLanguage, 0, len(queryLanguages))
	for _, language := range queryLanguages {
		normalized := models.NormalizeQueryLanguage(language)
		if normalized == "" {
			continue
		}
		source.QueryLanguages = append(source.QueryLanguages, normalized)
	}

	capabilities := provider.Capabilities()
	source.Capabilities = make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability == "" {
			continue
		}
		source.Capabilities = append(source.Capabilities, string(capability))
	}

	return nil
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

func supportsQueryLanguage(supported []models.QueryLanguage, language models.QueryLanguage) bool {
	for _, candidate := range supported {
		if models.NormalizeQueryLanguage(candidate) == language {
			return true
		}
	}
	return false
}

func supportsSavedQueryEditorMode(supported []models.SavedQueryEditorMode, mode models.SavedQueryEditorMode) bool {
	for _, candidate := range supported {
		if models.NormalizeSavedQueryEditorMode(candidate) == mode {
			return true
		}
	}
	return false
}

func supportsAlertEditorMode(supported []models.AlertEditorMode, mode models.AlertEditorMode) bool {
	for _, candidate := range supported {
		if models.NormalizeAlertEditorMode(candidate) == mode {
			return true
		}
	}
	return false
}
