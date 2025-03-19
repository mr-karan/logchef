package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"
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
func (s *Service) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {
	source, err := s.db.GetSource(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get health status
	health := s.chDB.GetHealth(source.ID)
	source.IsConnected = health.Status == models.HealthStatusHealthy

	// Fetch the table schema if the source is connected
	if source.IsConnected {
		client, err := s.chDB.GetClient(source.ID)
		if err != nil {
			s.log.Warn("failed to get client for schema retrieval",
				"error", err,
				"source_id", source.ID,
			)
		} else {
			// Get the table schema (column information)
			columns, err := client.GetTableSchema(ctx, source.Connection.Database, source.Connection.TableName)
			if err != nil {
				s.log.Warn("failed to get table schema",
					"error", err,
					"source_id", source.ID,
					"database", source.Connection.Database,
					"table", source.Connection.TableName,
				)
			} else {
				source.Columns = columns
				s.log.Debug("retrieved table schema",
					"source_id", source.ID,
					"column_count", len(columns),
				)
			}

			// Get the CREATE TABLE statement
			createStatement, err := client.GetTableCreateStatement(ctx, source.Connection.Database, source.Connection.TableName)
			if err != nil {
				s.log.Warn("failed to get CREATE TABLE statement",
					"error", err,
					"source_id", source.ID,
					"database", source.Connection.Database,
					"table", source.Connection.TableName,
				)
			} else {
				source.Schema = createStatement
				s.log.Debug("retrieved CREATE TABLE statement",
					"source_id", source.ID,
					"schema_length", len(createStatement),
				)
			}
		}
	}

	return source, nil
}

// CreateSource creates a new source
func (s *Service) CreateSource(ctx context.Context, name string, autoCreateTable bool, conn models.ConnectionInfo, description string, ttlDays int, metaTSField string, metaSeverityField string) (*models.Source, error) {
	// Validate input
	if err := s.validator.ValidateSourceCreation(name, conn, description, ttlDays, metaTSField, metaSeverityField, autoCreateTable); err != nil {
		return nil, err
	}

	source := &models.Source{
		Name:              name,
		MetaIsAutoCreated: autoCreateTable,
		MetaTSField:       metaTSField,
		MetaSeverityField: metaSeverityField,
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
			Message: "Failed to validate source configuration: " + err.Error(),
		}
	}
	if existing != nil {
		return nil, &ValidationError{
			Field:   "table_name",
			Message: fmt.Sprintf("A source for table %s in database %s already exists", source.Connection.TableName, source.Connection.Database),
		}
	}

	// First create a temporary client to validate the connection
	tempClient, err := s.chDB.CreateTemporaryClient(source)
	if err != nil {
		s.log.Error("failed to initialize connection",
			"error", err,
			"host", source.Connection.Host,
			"database", source.Connection.Database,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to connect to the database. Please check your connection details.",
		}
	}
	defer tempClient.Close()

	// If not auto-creating table, verify it exists and validate column types
	if !autoCreateTable {
		// First check if the table exists
		query := fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", source.Connection.Database, source.Connection.TableName)
		if _, err := tempClient.Query(ctx, query); err != nil {
			s.log.Error("failed to verify table exists",
				"error", err,
				"database", source.Connection.Database,
				"table", source.Connection.TableName,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: fmt.Sprintf("Table %s not found in database %s", source.Connection.TableName, source.Connection.Database),
			}
		}

		// Then validate column types
		if err := s.validator.ValidateColumnTypes(ctx, tempClient, source.Connection.Database, source.Connection.TableName, source.MetaTSField, source.MetaSeverityField); err != nil {
			s.log.Error("failed to validate column types",
				"error", err,
				"database", source.Connection.Database,
				"table", source.Connection.TableName,
			)
			return nil, err
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

		if _, err := tempClient.Query(ctx, schema); err != nil {
			s.log.Error("failed to create table",
				"error", err,
				"source_id", source.ID,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: "Failed to create table: " + err.Error(),
			}
		}
	}

	// Now create the source in the database
	if err := s.db.CreateSource(ctx, source); err != nil {
		return nil, err
	}

	// Finally add source to ClickHouse manager
	if err := s.chDB.AddSource(source); err != nil {
		// If we fail to add to the connection pool, delete the source from the database
		_ = s.db.DeleteSource(ctx, source.ID)
		s.log.Error("failed to add source to connection pool",
			"error", err,
			"source_id", source.ID,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to add source to connection pool: " + err.Error(),
		}
	}

	return source, nil
}

// UpdateSource updates an existing source
func (s *Service) UpdateSource(ctx context.Context, id models.SourceID, description string, ttlDays int) (*models.Source, error) {
	// Validate input
	if err := s.validator.ValidateSourceUpdate(description, ttlDays); err != nil {
		return nil, err
	}

	// Get existing source
	source, err := s.db.GetSource(ctx, id)
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
func (s *Service) DeleteSource(ctx context.Context, id models.SourceID) error {
	// Validate source exists
	source, err := s.db.GetSource(ctx, id)
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
func (s *Service) GetSourceHealth(ctx context.Context, id models.SourceID) (models.SourceHealth, error) {
	// Check if source exists
	source, err := s.db.GetSource(ctx, id)
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

// ValidateConnection validates a connection to a ClickHouse database
func (s *Service) ValidateConnection(ctx context.Context, conn models.ConnectionInfo) (*models.ConnectionValidationResult, error) {
	s.log.Debug("validating connection",
		"host", conn.Host,
		"database", conn.Database,
		"table", conn.TableName,
	)

	// Validate input
	if err := s.validator.ValidateConnection(conn); err != nil {
		s.log.Error("connection validation failed",
			"error", err,
			"host", conn.Host,
			"database", conn.Database,
		)
		return nil, err
	}

	// Create a temporary source for validation
	tempSource := &models.Source{
		ID:         models.SourceID(-1), // Temporary ID
		Connection: conn,
	}

	// Try to connect to the database
	client, err := s.chDB.CreateTemporaryClient(tempSource)
	if err != nil {
		s.log.Error("failed to connect to database",
			"error", err,
			"host", conn.Host,
			"database", conn.Database,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to connect to the database: " + err.Error(),
		}
	}
	defer client.Close()

	// Check if the table exists
	if conn.TableName != "" {
		query := fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", conn.Database, conn.TableName)
		_, err := client.Query(ctx, query)
		if err != nil {
			s.log.Error("table not found or not accessible",
				"error", err,
				"host", conn.Host,
				"database", conn.Database,
				"table", conn.TableName,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: fmt.Sprintf("Connection successful, but table '%s' not found or not accessible: %s", conn.TableName, err.Error()),
			}
		}
	}

	s.log.Debug("connection validation successful",
		"host", conn.Host,
		"database", conn.Database,
		"table", conn.TableName,
	)

	return &models.ConnectionValidationResult{
		Message: "Connection successful",
	}, nil
}

// ValidateConnectionWithColumns validates a connection and checks column types
func (s *Service) ValidateConnectionWithColumns(ctx context.Context, conn models.ConnectionInfo, tsField, severityField string) (*models.ConnectionValidationResult, error) {
	s.log.Debug("validating connection with columns",
		"host", conn.Host,
		"database", conn.Database,
		"table", conn.TableName,
		"ts_field", tsField,
		"severity_field", severityField,
	)

	// First validate the basic connection - just directly propagate any errors
	if err := s.validator.ValidateConnection(conn); err != nil {
		return nil, err
	}

	// Create a temporary source for validation
	tempSource := &models.Source{
		ID:         models.SourceID(-1), // Temporary ID
		Connection: conn,
	}

	// Try to connect to the database
	client, err := s.chDB.CreateTemporaryClient(tempSource)
	if err != nil {
		s.log.Error("failed to connect to database",
			"error", err,
			"host", conn.Host,
			"database", conn.Database,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to connect to the database: " + err.Error(),
		}
	}
	defer client.Close()

	// If the connection is successful and we're validating an existing table, check column types
	if conn.TableName != "" && tsField != "" {
		// Validate column types
		if err := s.validator.ValidateColumnTypes(ctx, client, conn.Database, conn.TableName, tsField, severityField); err != nil {
			var validationErr *ValidationError
			if errors.As(err, &validationErr) {
				return nil, validationErr
			}
			return nil, &ValidationError{
				Field:   "columns",
				Message: "Failed to validate column types: " + err.Error(),
			}
		}
	}

	return &models.ConnectionValidationResult{
		Message: "Connection and column types validated successfully",
	}, nil
}

// SourceStats represents the combined statistics for a ClickHouse table
type SourceStats struct {
	TableStats  *clickhouse.TableStat        `json:"table_stats"`
	ColumnStats []clickhouse.TableColumnStat `json:"column_stats"`
}

// GetSourceStats retrieves statistics for a specific source (ClickHouse table)
func (s *Service) GetSourceStats(ctx context.Context, source *models.Source) (*SourceStats, error) {
	s.log.Debug("retrieving source stats",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Get client for the source
	client, err := s.chDB.GetClient(source.ID)
	if err != nil {
		s.log.Error("failed to get client for source",
			"error", err,
			"source_id", source.ID,
		)
		return nil, fmt.Errorf("failed to get client for source: %w", err)
	}

	// Create default empty stats in case we can't get real stats
	defaultTableStats := &clickhouse.TableStat{
		Database:     source.Connection.Database,
		Table:        source.Connection.TableName,
		Compressed:   "0B",
		Uncompressed: "0B",
		ComprRate:    0,
		Rows:         0,
		PartCount:    0,
	}

	// Get table stats with fallback to default
	tableStats, err := client.GetTableStats(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		s.log.Warn("failed to get table stats, using defaults",
			"error", err,
			"source_id", source.ID,
		)
		// Use default stats instead of returning an error
		tableStats = defaultTableStats
	}

	// Get column stats
	var columnStats []clickhouse.TableColumnStat
	columnStatsResult, err := client.GetTableColumnStats(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		s.log.Warn("failed to get column stats, will use schema if available",
			"error", err,
			"source_id", source.ID,
		)
	} else {
		columnStats = columnStatsResult
	}

	// If we have no column stats but we know the columns from the schema,
	// create empty stats for each column for better UX
	if len(columnStats) == 0 && len(source.Columns) > 0 {
		s.log.Info("no column stats found, creating default empty stats",
			"source_id", source.ID,
			"column_count", len(source.Columns),
		)

		for _, col := range source.Columns {
			columnStats = append(columnStats, clickhouse.TableColumnStat{
				Database:     source.Connection.Database,
				Table:        source.Connection.TableName,
				Column:       col.Name,
				Compressed:   "0B",
				Uncompressed: "0B",
				ComprRatio:   0,
				RowsCount:    0,
				AvgRowSize:   0,
			})
		}
	}

	stats := &SourceStats{
		TableStats:  tableStats,
		ColumnStats: columnStats,
	}

	s.log.Debug("successfully retrieved source stats",
		"source_id", source.ID,
		"table_stats_rows", tableStats.Rows,
		"column_stats_count", len(columnStats),
	)

	return stats, nil
}
