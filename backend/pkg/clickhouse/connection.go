package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
)

// Connection represents a single Clickhouse connection with its metadata
type Connection struct {
	Source     *models.Source
	DB         driver.Conn
	LastHealth models.SourceHealth
	log        *slog.Logger
}

// loggingConn wraps a driver.Conn to add query logging
type loggingConn struct {
	driver.Conn
	log *slog.Logger
}

func (c *loggingConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	start := time.Now()
	
	// Add UUID to string conversion for all queries
	query = strings.ReplaceAll(query, "UUID", "toString(UUID)")
	
	rows, err := c.Conn.Query(ctx, query, args...)
	duration := time.Since(start)

	if err != nil {
		c.log.Error("query failed",
			"query", query,
			"duration_ms", duration.Milliseconds(),
			"args_count", len(args),
			"error", err,
		)
		return rows, err
	}

	c.log.Info("query executed",
		"query", query,
		"duration_ms", duration.Milliseconds(),
		"args_count", len(args),
	)
	return rows, nil
}

// NewConnection creates a new Clickhouse connection
func NewConnection(source *models.Source) (*Connection, error) {
	// Validate source configuration
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("invalid source configuration: %w", err)
	}

	options, err := parseSourceOptions(source)
	if err != nil {
		return nil, fmt.Errorf("error parsing source options: %w", err)
	}

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("error pinging connection: %w", err)
	}

	// Wrap connection with logging
	log := logger.Default().With("component", "clickhouse", "source_id", source.ID)
	loggingConn := &loggingConn{
		Conn: conn,
		log:  log,
	}

	return &Connection{
		Source: source,
		DB:     loggingConn,
		LastHealth: models.SourceHealth{
			ID:        source.ID,
			IsHealthy: true,
			LastCheck: time.Now(),
		},
		log: log,
	}, nil
}

// parseSourceOptions converts a Source into Clickhouse connection options
func parseSourceOptions(source *models.Source) (*clickhouse.Options, error) {
	// Parse DSN to extract host and port
	// DSN format: tcp://[username:password@]host:port?database=dbname
	dsn := source.DSN
	if dsn == "" {
		return nil, fmt.Errorf("DSN is required")
	}

	// Remove protocol prefix
	dsn = strings.TrimPrefix(dsn, "tcp://")
	dsn = strings.TrimPrefix(dsn, "http://")

	// Extract credentials and host:port
	var username, password string
	hostPort := dsn

	// Handle credentials if present
	if atIndex := strings.Index(dsn, "@"); atIndex >= 0 {
		creds := dsn[:atIndex]
		hostPort = dsn[atIndex+1:]

		if colonIndex := strings.Index(creds, ":"); colonIndex >= 0 {
			username = creds[:colonIndex]
			password = creds[colonIndex+1:]
		} else {
			username = creds
		}
	}

	// Split off query parameters to get clean host:port
	if idx := strings.Index(hostPort, "?"); idx >= 0 {
		hostPort = hostPort[:idx]
	}

	// If no credentials provided, use defaults
	if username == "" {
		username = "default"
	}

	return &clickhouse.Options{
		Addr: []string{hostPort},
		Auth: clickhouse.Auth{
			Database: source.Database,
			Username: username,
			Password: password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": defaultMaxExecutionTime,
		},
		DialTimeout: defaultTimeout,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		MaxOpenConns:     defaultMaxOpenConns,
		MaxIdleConns:     defaultMaxIdleConns,
		ConnMaxLifetime:  defaultConnMaxLifetime,
		ConnOpenStrategy: clickhouse.ConnOpenInOrder,
		BlockBufferSize:  defaultBlockBufferSize,
	}, nil
}

// Close closes the connection
func (c *Connection) Close() error {
	return c.DB.Close()
}

// CheckHealth performs a health check on the connection
func (c *Connection) CheckHealth(ctx context.Context) models.SourceHealth {
	health := models.SourceHealth{
		ID:        c.Source.ID,
		LastCheck: time.Now(),
	}

	if err := c.DB.Ping(ctx); err != nil {
		health.Error = err.Error()
		health.IsHealthy = false
		return health
	}

	// Additional check: try to execute a simple query
	if err := c.DB.Exec(ctx, "SELECT 1"); err != nil {
		health.Error = err.Error()
		health.IsHealthy = false
		return health
	}

	health.IsHealthy = true
	return health
}

// CreateOTELLogsTable creates the OTEL logs table with the given TTL
func (c *Connection) CreateOTELLogsTable(ctx context.Context, ttlDays int) error {
	schema := models.OTELLogsTableSchema
	// TODO: Replace placeholders with actual values
	// schema = strings.ReplaceAll(schema, "{{database_name}}", ...)

	return c.DB.Exec(ctx, schema)
}

// QueryLogs queries logs from the source with pagination
func (c *Connection) QueryLogs(ctx context.Context, limit, offset int) ([]map[string]interface{}, error) {
	// Create query with limit and offset
	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", c.Source.GetFullTableName(), limit, offset)

	// Execute query
	rows, err := c.DB.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error querying logs: %w", err)
	}
	defer rows.Close()

	// Create type converter
	converter := NewTypeConverter()

	// Scan rows with type conversion
	var results []map[string]interface{}
	for rows.Next() {
		result, err := converter.ScanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}
