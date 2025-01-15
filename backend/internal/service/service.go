package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"backend-v2/internal/sqlite"
	"backend-v2/pkg/clickhouse"
	"backend-v2/pkg/models"
)

// Service manages the application's core functionality
type Service struct {
	sqlite     *sqlite.DB
	clickhouse *clickhouse.Pool
}

// New creates a new service instance
func New(sqliteDB *sqlite.DB) *Service {
	return &Service{
		sqlite:     sqliteDB,
		clickhouse: clickhouse.NewPool(),
	}
}

// InitializeSource initializes a Clickhouse connection for a source
func (s *Service) InitializeSource(ctx context.Context, source *models.Source) error {
	return s.clickhouse.AddSource(source)
}

// ListSources returns all sources with their connection status
func (s *Service) ListSources(ctx context.Context) ([]*models.Source, error) {
	sources, err := s.sqlite.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Enrich with connection status
	for _, source := range sources {
		health, err := s.clickhouse.GetHealth(source.ID)
		if err == nil {
			source.IsConnected = health.IsHealthy
		}
	}

	return sources, nil
}

// GetSource retrieves a source by ID
func (s *Service) GetSource(ctx context.Context, id string) (*models.Source, error) {
	source, err := s.sqlite.GetSource(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return nil, nil
	}

	// Get health status
	health, err := s.clickhouse.GetHealth(id)
	if err == nil {
		source.IsConnected = health.IsHealthy
	}

	return source, nil
}

// CreateSource creates a new source
func (s *Service) CreateSource(ctx context.Context, tableName, schemaType, dsn, description string, ttlDays int) (*models.Source, error) {
	source := &models.Source{
		ID:          uuid.New().String(),
		TableName:   tableName,
		SchemaType:  schemaType,
		DSN:         dsn,
		Description: description,
		TTLDays:     ttlDays,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Validate source configuration - this will also set the Database field from DSN
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("invalid source configuration: %w", err)
	}

	// Check if source already exists
	existing, err := s.sqlite.GetSourceByName(ctx, source.Database, source.TableName)
	if err != nil {
		return nil, fmt.Errorf("error checking for existing source: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("source with table name %s already exists in database %s", source.TableName, source.Database)
	}

	// First save to SQLite
	if err := s.sqlite.CreateSource(ctx, source); err != nil {
		return nil, fmt.Errorf("error saving to SQLite: %w", err)
	}

	// Then establish Clickhouse connection
	if err := s.clickhouse.AddSource(source); err != nil {
		// If Clickhouse connection fails, remove from SQLite
		if delErr := s.sqlite.DeleteSource(ctx, source.ID); delErr != nil {
			return nil, fmt.Errorf("error rolling back SQLite after Clickhouse failure: %w", delErr)
		}
		return nil, fmt.Errorf("error connecting to Clickhouse: %w", err)
	}

	// Get the connection to create tables
	conn, err := s.clickhouse.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting connection: %w", err)
	}

	// Create necessary tables
	if err := conn.CreateSource(ctx, source.TTLDays); err != nil {
		// Cleanup on failure
		if delErr := s.DeleteSource(ctx, source.ID); delErr != nil {
			return nil, fmt.Errorf("error cleaning up after table creation failure: %w", delErr)
		}
		return nil, fmt.Errorf("error creating tables: %w", err)
	}

	return source, nil
}

// DeleteSource deletes a source
func (s *Service) DeleteSource(ctx context.Context, id string) error {
	// First remove from Clickhouse to prevent any new operations
	if err := s.clickhouse.RemoveSource(id); err != nil {
		return fmt.Errorf("error removing Clickhouse connection: %w", err)
	}

	// Then remove from SQLite
	if err := s.sqlite.DeleteSource(ctx, id); err != nil {
		return fmt.Errorf("error removing from SQLite: %w", err)
	}

	return nil
}

// HealthUpdates returns a channel that receives source health updates
func (s *Service) HealthUpdates() <-chan models.SourceHealth {
	return s.clickhouse.HealthUpdates()
}

// Close closes all connections
func (s *Service) Close() error {
	var lastErr error

	if err := s.clickhouse.Close(); err != nil {
		lastErr = fmt.Errorf("error closing Clickhouse connections: %w", err)
	}

	return lastErr
}

// ExploreSource retrieves the schema information for a source
func (s *Service) ExploreSource(ctx context.Context, sourceID string) ([]models.ColumnInfo, error) {
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("source not found")
	}

	columns, err := s.clickhouse.DescribeTable(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("error describing table: %w", err)
	}

	return columns, nil
}

// QueryLogs retrieves logs from a source with pagination
func (s *Service) QueryLogs(ctx context.Context, sourceID string, limit, offset int) ([]map[string]interface{}, error) {
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("source not found")
	}

	if limit <= 0 {
		limit = 100 // default limit
	}
	if limit > 1000 {
		limit = 1000 // max limit to prevent excessive queries
	}

	logs, err := s.clickhouse.QueryLogs(ctx, source, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("error querying logs: %w", err)
	}

	return logs, nil
}
