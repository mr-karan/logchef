package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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
	log := logger.Default().With(
		"source_id", source.ID,
		"database", source.Database,
		"table", source.TableName,
	)

	// Validate source configuration
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("invalid source configuration: %w", err)
	}

	options, err := parseSourceOptions(source)
	if err != nil {
		return nil, fmt.Errorf("error parsing source options: %w", err)
	}

	log.Info("creating new clickhouse connection",
		"host", options.Addr[0],
		"database", options.Auth.Database,
		"username", options.Auth.Username,
	)

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
	// Parse DSN
	// DSN format: clickhouse://host:port?username=user&password=pass&database=db
	dsn := source.DSN
	if dsn == "" {
		return nil, fmt.Errorf("DSN is required")
	}

	// Parse the URL
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN format: %w", err)
	}

	// Verify scheme
	if u.Scheme != "clickhouse" {
		return nil, fmt.Errorf("invalid DSN scheme, expected 'clickhouse', got '%s'", u.Scheme)
	}

	// Get query parameters
	q := u.Query()
	username := q.Get("username")
	if username == "" {
		username = "default"
	}
	password := q.Get("password")

	return &clickhouse.Options{
		Addr: []string{u.Host}, // Host:Port
		Auth: clickhouse.Auth{
			Database: source.Database,
			Username: username,
			Password: password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": defaultMaxExecutionTime,
		},
		DialTimeout:      defaultTimeout,
		Compression:      &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
		Protocol:         clickhouse.Native,
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

	// Create a channel to receive profile info
	profileChan := make(chan *clickhouse.ProfileInfo, 1)

	// Add profile info callback to context
	ctx = clickhouse.Context(ctx,
		clickhouse.WithProfileInfo(func(p *clickhouse.ProfileInfo) {
			profileChan <- p
		}),
	)

	// Run a simple query to check connection
	start := time.Now()
	if err := c.DB.Ping(ctx); err != nil {
		health.IsHealthy = false
		health.Error = err.Error()
		health.Latency = time.Since(start)
		return health
	}

	// Run a simple SELECT 1 query to get profile info
	query := "SELECT 1"
	if err := c.DB.QueryRow(ctx, query).Scan(new(uint8)); err != nil {
		health.IsHealthy = false
		health.Error = fmt.Sprintf("query failed: %v", err)
		health.Latency = time.Since(start)
		return health
	}

	// Get profile info from channel
	select {
	case profile := <-profileChan:
		c.log.Debug("received profile info",
			"bytes", profile.Bytes,
			"rows", profile.Rows,
			"blocks", profile.Blocks,
		)
	case <-time.After(time.Second):
		c.log.Warn("timeout waiting for profile info")
	}

	health.IsHealthy = true
	health.Latency = time.Since(start)
	return health
}

// CreateSource creates the necessary tables and structures for a source
func (c *Connection) CreateSource(ctx context.Context, ttlDays int) error {
	c.log.Info("creating source tables",
		"schema_type", c.Source.SchemaType,
		"ttl_days", ttlDays,
	)

	switch c.Source.SchemaType {
	case models.SchemaTypeManaged:
		if err := c.createManagedTables(ctx, ttlDays); err != nil {
			return fmt.Errorf("error creating managed tables: %w", err)
		}
		if err := c.createAttributesView(ctx); err != nil {
			return fmt.Errorf("error creating attributes view: %w", err)
		}
	case models.SchemaTypeUnmanaged:
		// For unmanaged sources, we just verify the table exists
		query := fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", c.Source.Database, c.Source.TableName)
		if err := c.DB.Exec(ctx, query); err != nil {
			return fmt.Errorf("error verifying table exists: %w", err)
		}
		c.log.Debug("verified unmanaged table exists",
			"database", c.Source.Database,
			"table", c.Source.TableName,
		)
	default:
		return fmt.Errorf("invalid schema type: %s", c.Source.SchemaType)
	}

	return nil
}

// createManagedTables creates the tables for managed sources
func (c *Connection) createManagedTables(ctx context.Context, ttlDays int) error {
	c.log.Debug("creating managed tables",
		"table", c.Source.TableName,
		"ttl_days", ttlDays,
	)

	schema := models.OTELLogsTableSchema

	// Replace placeholders with actual values
	schema = strings.ReplaceAll(schema, "{{database_name}}", c.Source.Database)
	schema = strings.ReplaceAll(schema, "{{table_name}}", c.Source.TableName)

	// Only add TTL if ttlDays > 0
	if ttlDays > 0 {
		schema = strings.ReplaceAll(schema, "{{ttl_day}}", strconv.Itoa(ttlDays))
	} else {
		// Remove TTL clause if no TTL is set
		schema = strings.ReplaceAll(schema, "TTL timestamp + INTERVAL {{ttl_day}} DAY", "")
	}

	// Create the table
	if err := c.DB.Exec(ctx, schema); err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	return nil
}

// createAttributesView creates a materialized view for storing unique attribute keys
func (c *Connection) createAttributesView(ctx context.Context) error {
	viewName := fmt.Sprintf("%s_attributes", c.Source.TableName)
	c.log.Debug("creating attributes view",
		"view", viewName,
		"source_table", c.Source.TableName,
	)

	// Create materialized view with built-in storage
	query := fmt.Sprintf(`
		CREATE MATERIALIZED VIEW IF NOT EXISTS %s.%s
		ENGINE = ReplacingMergeTree
		ORDER BY attribute_key
		AS SELECT DISTINCT
			arrayJoin(mapKeys(log_attributes)) as attribute_key
		FROM %s.%s
	`, c.Source.Database, viewName, c.Source.Database, c.Source.TableName)

	if err := c.DB.Exec(ctx, query); err != nil {
		return fmt.Errorf("error creating materialized view: %w", err)
	}

	return nil
}

// GetUniqueAttributes returns the unique attribute keys from log_attributes
func (c *Connection) GetUniqueAttributes(ctx context.Context) ([]string, error) {
	c.log.Debug("fetching unique attributes",
		"table", c.Source.TableName,
	)

	// Query the materialized view directly
	query := fmt.Sprintf(`
		SELECT attribute_key
		FROM %s.%s FINAL
		ORDER BY attribute_key
	`, c.Source.Database, fmt.Sprintf("%s_attributes", c.Source.TableName))

	rows, err := c.DB.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error querying unique attributes: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("error scanning attribute key: %w", err)
		}
		result = append(result, key)
	}

	return result, nil
}

// GetTableSchema returns the CREATE TABLE statement for the source table
func (c *Connection) GetTableSchema(ctx context.Context) (string, error) {
	c.log.Debug("getting table schema",
		"table", c.Source.TableName,
	)

	query := fmt.Sprintf(`
		SHOW CREATE TABLE %s.%s
	`, c.Source.Database, c.Source.TableName)

	var schema string
	if err := c.DB.QueryRow(ctx, query).Scan(&schema); err != nil {
		return "", fmt.Errorf("error getting table schema: %w", err)
	}

	return schema, nil
}
