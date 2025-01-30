package service

import (
	"backend-v2/pkg/models"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

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
func (s *Service) CreateSource(ctx context.Context, schemaType string, conn models.ConnectionInfo, description string, ttlDays int) (*models.Source, error) {
	source := &models.Source{
		ID:          uuid.New().String(),
		SchemaType:  schemaType,
		Connection:  conn,
		Description: description,
		TTLDays:     ttlDays,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Check if source already exists
	existing, err := s.sqlite.GetSourceByName(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		return nil, fmt.Errorf("error checking for existing source: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("source with table name %s already exists in database %s", source.Connection.TableName, source.Connection.Database)
	}

	// For unmanaged sources, verify table exists in ClickHouse
	if source.SchemaType == models.SchemaTypeUnmanaged {
		// First add source to pool to get a connection
		if err := s.clickhouse.AddSource(source); err != nil {
			return nil, fmt.Errorf("error initializing ClickHouse connection: %w", err)
		}
		conn, err := s.clickhouse.GetConnection(source.ID)
		if err != nil {
			return nil, fmt.Errorf("error getting ClickHouse connection: %w", err)
		}
		if err := conn.DB.Exec(ctx, fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", source.Connection.Database, source.Connection.TableName)); err != nil {
			return nil, fmt.Errorf("error verifying table exists: %w", err)
		}
	}

	// First save to SQLite
	if err := s.sqlite.CreateSource(ctx, source); err != nil {
		return nil, fmt.Errorf("error saving to SQLite: %w", err)
	}

	// Add to connection pool (for managed sources, this is done here)
	if source.SchemaType == models.SchemaTypeManaged {
		if err := s.clickhouse.AddSource(source); err != nil {
			// Try to clean up SQLite entry on error
			_ = s.sqlite.DeleteSource(ctx, source.ID)
			return nil, fmt.Errorf("error initializing ClickHouse connection: %w", err)
		}
		conn, err := s.clickhouse.GetConnection(source.ID)
		if err != nil {
			// Try to clean up SQLite entry on error
			_ = s.sqlite.DeleteSource(ctx, source.ID)
			return nil, fmt.Errorf("error getting ClickHouse connection: %w", err)
		}
		if err := conn.CreateSource(ctx, source.TTLDays); err != nil {
			// Try to clean up SQLite entry on error
			_ = s.sqlite.DeleteSource(ctx, source.ID)
			return nil, fmt.Errorf("error creating ClickHouse table: %w", err)
		}
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
func (s *Service) ExploreSource(ctx context.Context, source *models.Source) error {
	// Get columns
	columns, err := s.clickhouse.DescribeTable(ctx, source)
	if err != nil {
		return fmt.Errorf("error describing table: %w", err)
	}
	source.Columns = columns

	// Get CREATE TABLE statement
	schema, err := s.clickhouse.GetTableSchema(ctx, source)
	if err != nil {
		return fmt.Errorf("error getting table schema: %w", err)
	}
	source.Schema = schema

	return nil
}
