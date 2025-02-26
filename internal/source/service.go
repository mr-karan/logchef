package source

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"

	"github.com/google/uuid"
)

// ErrSourceNotFound is returned when a source is not found
var ErrSourceNotFound = fmt.Errorf("source not found")

// Service handles operations related to data sources
type Service struct {
	db        *sqlite.DB
	chDB      *clickhouse.Manager
	log       *slog.Logger
	validator *Validator
}

// New creates a new source service
func New(db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger) *Service {
	return &Service{
		db:        db,
		chDB:      chDB,
		log:       log.With("component", "source_service"),
		validator: NewValidator(),
	}
}

// ListSources returns all sources with their connection status
func (s *Service) ListSources(ctx context.Context) ([]*models.Source, error) {
	sources, err := s.db.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Enrich with connection status
	for _, source := range sources {
		health := s.chDB.GetHealth(source.ID)
		source.IsConnected = health.Status == models.HealthStatusHealthy
	}

	return sources, nil
}

// GetSource retrieves a source by ID
func (s *Service) GetSource(ctx context.Context, id string) (*models.Source, error) {
	source, err := s.db.GetSource(ctx, models.SourceID(id))
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get health status
	health := s.chDB.GetHealth(source.ID)
	source.IsConnected = health.Status == models.HealthStatusHealthy

	return source, nil
}

// CreateSource creates a new source
func (s *Service) CreateSource(ctx context.Context, autoCreateTable bool, conn models.ConnectionInfo, description string, ttlDays int, metaTSField string) (*models.Source, error) {
	// Validate input
	if err := s.validator.ValidateSourceCreation(conn, description, ttlDays, metaTSField, autoCreateTable); err != nil {
		return nil, err
	}

	source := &models.Source{
		ID:                models.SourceID(uuid.New().String()),
		MetaIsAutoCreated: autoCreateTable,
		MetaTSField:       metaTSField,
		Connection:        conn,
		Description:       description,
		TTLDays:           ttlDays,
		Timestamps: models.Timestamps{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Check if source already exists
	existing, err := s.db.GetSourceByName(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		s.log.Error("failed to check existing source",
			"error", err,
			"database", source.Connection.Database,
			"table", source.Connection.TableName,
		)
		return nil, &ValidationError{
			Field:   "source",
			Message: "Failed to validate source configuration. Please try again.",
		}
	}
	if existing != nil {
		return nil, &ValidationError{
			Field:   "table_name",
			Message: fmt.Sprintf("A source for table %s in database %s already exists", source.Connection.TableName, source.Connection.Database),
		}
	}

	// First add source to ClickHouse manager to get a connection for health check
	if err := s.chDB.AddSource(source); err != nil {
		s.log.Error("failed to initialize connection",
			"error", err,
			"source_id", source.ID,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to connect to the database. Please check your connection details.",
		}
	}

	// Get client for health check
	client, err := s.chDB.GetClient(source.ID)
	if err != nil {
		s.log.Error("failed to get client",
			"error", err,
			"source_id", source.ID,
		)
		// Clean up the source from the manager
		_ = s.chDB.RemoveSource(source.ID)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to connect to the database. Please check your connection details.",
		}
	}

	// If not auto-creating table, verify it exists
	if !autoCreateTable {
		query := fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", source.Connection.Database, source.Connection.TableName)
		if _, err := client.Query(ctx, query); err != nil {
			s.log.Error("failed to verify table exists",
				"error", err,
				"source_id", source.ID,
				"database", source.Connection.Database,
				"table", source.Connection.TableName,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: fmt.Sprintf("Table %s not found in database %s", source.Connection.TableName, source.Connection.Database),
			}
		}
	}

	// Save to database
	if err := s.db.CreateSource(ctx, source); err != nil {
		s.log.Error("failed to save source",
			"error", err,
			"source_id", source.ID,
		)
		return nil, &ValidationError{
			Field:   "source",
			Message: "Failed to create source. Please try again.",
		}
	}

	// Create table if needed
	if autoCreateTable {
		// Create table using the schema
		schema := models.OTELLogsTableSchema
		schema = strings.ReplaceAll(schema, "{{database_name}}", source.Connection.Database)
		schema = strings.ReplaceAll(schema, "{{table_name}}", source.Connection.TableName)
		if source.TTLDays > 0 {
			schema = strings.ReplaceAll(schema, "{{ttl_day}}", strconv.Itoa(source.TTLDays))
		} else {
			schema = strings.ReplaceAll(schema, "TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY", "")
		}

		if _, err := client.Query(ctx, schema); err != nil {
			// Try to clean up database entry on error
			_ = s.db.DeleteSource(ctx, source.ID)
			s.log.Error("failed to create table",
				"error", err,
				"source_id", source.ID,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: "Failed to create table. Please try again.",
			}
		}
	}

	return source, nil
}

// UpdateSource updates an existing source
func (s *Service) UpdateSource(ctx context.Context, id string, description string, ttlDays int) (*models.Source, error) {
	// Validate input
	if err := s.validator.ValidateSourceUpdate(description, ttlDays); err != nil {
		return nil, err
	}

	// Get existing source
	source, err := s.db.GetSource(ctx, models.SourceID(id))
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Update fields
	source.Description = description
	source.TTLDays = ttlDays
	source.UpdatedAt = time.Now()

	// Save to database
	if err := s.db.UpdateSource(ctx, source); err != nil {
		s.log.Error("failed to update source",
			"error", err,
			"source_id", id,
		)
		return nil, fmt.Errorf("error updating source: %w", err)
	}

	return source, nil
}

// DeleteSource deletes a source
func (s *Service) DeleteSource(ctx context.Context, id string) error {
	// Validate source exists
	source, err := s.db.GetSource(ctx, models.SourceID(id))
	if err != nil {
		return fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return ErrSourceNotFound
	}

	// First remove from ClickHouse manager to prevent any new operations
	if err := s.chDB.RemoveSource(source.ID); err != nil {
		return fmt.Errorf("error removing Clickhouse connection: %w", err)
	}

	// Then remove from database
	if err := s.db.DeleteSource(ctx, source.ID); err != nil {
		return fmt.Errorf("error removing from database: %w", err)
	}

	return nil
}

// GetSourceHealth retrieves the health status of a source
func (s *Service) GetSourceHealth(ctx context.Context, id string) (models.SourceHealth, error) {
	// Check if source exists
	source, err := s.db.GetSource(ctx, models.SourceID(id))
	if err != nil {
		return models.SourceHealth{}, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return models.SourceHealth{}, ErrSourceNotFound
	}

	// Get health from ClickHouse manager and return it
	health := s.chDB.GetHealth(source.ID)
	return health, nil
}

// InitializeSource initializes a connection for a source
func (s *Service) InitializeSource(ctx context.Context, source *models.Source) error {
	return s.chDB.AddSource(source)
}

// Helper function to convert bool to int
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
