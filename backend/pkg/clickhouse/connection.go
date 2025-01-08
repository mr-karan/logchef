package clickhouse

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"backend-v2/pkg/models"
)

// Connection represents a single Clickhouse connection with its metadata
type Connection struct {
	Source     *models.Source
	DB         driver.Conn
	LastHealth models.SourceHealth
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

	return &Connection{
		Source: source,
		DB:     conn,
		LastHealth: models.SourceHealth{
			ID:        source.ID,
			IsHealthy: true,
			LastCheck: time.Now(),
		},
	}, nil
}

// parseSourceOptions converts a Source into Clickhouse connection options
func parseSourceOptions(source *models.Source) (*clickhouse.Options, error) {
	return &clickhouse.Options{
		Addr: []string{source.DSN},
		Auth: clickhouse.Auth{
			Database: source.GetDatabaseName(),
			Username: "default", // TODO: Add these to Source model if needed
			Password: "",
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
