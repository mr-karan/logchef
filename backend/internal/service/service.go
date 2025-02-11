package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"backend-v2/internal/sqlite"
	"backend-v2/pkg/clickhouse"
	"backend-v2/pkg/models"
	"backend-v2/pkg/querybuilder"

	"github.com/google/uuid"
)

// Service manages the application's core functionality
type Service struct {
	sqlite *sqlite.DB
	pool   *clickhouse.Pool
}

// New creates a new service instance
func New(sqliteDB *sqlite.DB) *Service {
	return &Service{
		sqlite: sqliteDB,
		pool:   clickhouse.NewPool(sqliteDB),
	}
}

// buildFilterQuery builds a query from filter parameters
func buildFilterQuery(params clickhouse.LogQueryParams, tableName string) string {
	// Create query builder based on mode
	opts := querybuilder.Options{
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Limit:     params.Limit,
		Sort:      params.Sort,
	}

	builder := querybuilder.NewFilterBuilder(tableName, opts, params.Conditions)
	query, err := builder.Build()
	if err != nil {
		return ""
	}

	return query.SQL
}

// QueryLogs retrieves logs from a source with pagination and time range
func (s *Service) QueryLogs(ctx context.Context, sourceID string, params clickhouse.LogQueryParams) (*models.QueryResult, error) {
	// Get source from SQLite
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get client from pool
	client, err := s.pool.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	// Build query based on mode
	var query string
	switch params.Mode {
	case models.QueryModeRawSQL:
		if params.RawSQL == "" {
			return nil, fmt.Errorf("raw SQL query cannot be empty")
		}
		// Use the query builder to handle the raw SQL
		builder := querybuilder.NewRawSQLBuilder(source.GetFullTableName(), params.RawSQL, params.Limit)
		builtQuery, err := builder.Build()
		if err != nil {
			return nil, fmt.Errorf("error building raw SQL query: %w", err)
		}
		query = builtQuery.SQL
	case models.QueryModeLogChefQL:
		if params.LogChefQL == "" {
			return nil, fmt.Errorf("LogchefQL query cannot be empty")
		}
		// TODO: Implement LogChefQL parsing
		return nil, fmt.Errorf("LogChefQL mode not implemented")
	case models.QueryModeFilters:
		// Build filter query
		query = buildFilterQuery(params, source.GetFullTableName())
	default:
		return nil, fmt.Errorf("unsupported query mode: %s", params.Mode)
	}

	// Execute query
	return client.Query(ctx, query)
}

// InitializeSource initializes a Clickhouse connection for a source
func (s *Service) InitializeSource(ctx context.Context, source *models.Source) error {
	return s.pool.AddSource(source)
}

// ListSources returns all sources with their connection status
func (s *Service) ListSources(ctx context.Context) ([]*models.Source, error) {
	sources, err := s.sqlite.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Enrich with connection status
	for _, source := range sources {
		health, err := s.pool.GetHealth(source.ID)
		if err == nil {
			source.IsConnected = health.Status == models.HealthStatusHealthy
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
	health, err := s.pool.GetHealth(id)
	if err == nil {
		source.IsConnected = health.Status == models.HealthStatusHealthy
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
		if err := s.pool.AddSource(source); err != nil {
			return nil, fmt.Errorf("error initializing ClickHouse connection: %w", err)
		}
		client, err := s.pool.GetClient(source.ID)
		if err != nil {
			return nil, fmt.Errorf("error getting ClickHouse client: %w", err)
		}
		query := fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", source.Connection.Database, source.Connection.TableName)
		if _, err := client.Query(ctx, query); err != nil {
			return nil, fmt.Errorf("error verifying table exists: %w", err)
		}
	}

	// First save to SQLite
	if err := s.sqlite.CreateSource(ctx, source); err != nil {
		return nil, fmt.Errorf("error saving to SQLite: %w", err)
	}

	// Add to connection pool (for managed sources, this is done here)
	if source.SchemaType == models.SchemaTypeManaged {
		if err := s.pool.AddSource(source); err != nil {
			// Try to clean up SQLite entry on error
			_ = s.sqlite.DeleteSource(ctx, source.ID)
			return nil, fmt.Errorf("error initializing ClickHouse connection: %w", err)
		}

		// Create the table
		client, err := s.pool.GetClient(source.ID)
		if err != nil {
			// Try to clean up SQLite entry on error
			_ = s.sqlite.DeleteSource(ctx, source.ID)
			return nil, fmt.Errorf("error getting ClickHouse client: %w", err)
		}

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
	if err := s.pool.RemoveSource(id); err != nil {
		return fmt.Errorf("error removing Clickhouse connection: %w", err)
	}

	// Then remove from SQLite
	if err := s.sqlite.DeleteSource(ctx, id); err != nil {
		return fmt.Errorf("error removing from SQLite: %w", err)
	}

	return nil
}

// Close closes all connections
func (s *Service) Close() error {
	var lastErr error

	if err := s.pool.Close(); err != nil {
		lastErr = fmt.Errorf("error closing Clickhouse connections: %w", err)
	}

	return lastErr
}

// GetTimeSeries retrieves time series data for log counts
func (s *Service) GetTimeSeries(ctx context.Context, sourceID string, params clickhouse.TimeSeriesParams) (*clickhouse.TimeSeriesResult, error) {
	// Get source from SQLite
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get client from pool
	client, err := s.pool.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	// Set table name in params
	params.Table = source.GetFullTableName()

	// Get time series data
	return client.GetTimeSeries(ctx, params)
}
