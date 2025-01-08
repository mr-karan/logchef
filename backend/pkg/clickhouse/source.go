package clickhouse

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"backend-v2/pkg/models"
)

// SourceType represents the type of source
type SourceType string

const (
	// SourceTypeOTEL represents OpenTelemetry source type
	SourceTypeOTEL SourceType = "otel"
	// SourceTypeHTTP represents HTTP source type
	SourceTypeHTTP SourceType = "http"
	// SourceTypeCustom represents custom source type
	SourceTypeCustom SourceType = "custom"

	// DefaultTTLDays is the default TTL for tables in days
	DefaultTTLDays = 30
)

// CreateSource creates the necessary tables and structures for a source
func (c *Connection) CreateSource(ctx context.Context, ttlDays int) error {
	if ttlDays <= 0 {
		ttlDays = DefaultTTLDays
	}

	// Create the database if it doesn't exist
	dbName := c.Source.GetDatabaseName()
	if dbName == "" {
		return fmt.Errorf("invalid database name in table name: %s", c.Source.TableName)
	}

	if err := c.DB.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", dbName)); err != nil {
		return fmt.Errorf("error creating database: %w", err)
	}

	// Create tables based on source type
	switch SourceType(c.Source.SchemaType) {
	case SourceTypeOTEL:
		return c.createOTELSource(ctx, ttlDays)
	case SourceTypeHTTP:
		return fmt.Errorf("HTTP source type not implemented yet")
	case SourceTypeCustom:
		return fmt.Errorf("custom source type not implemented yet")
	default:
		return fmt.Errorf("unknown source type: %s", c.Source.SchemaType)
	}
}

// createOTELSource creates tables for OpenTelemetry source
func (c *Connection) createOTELSource(ctx context.Context, ttlDays int) error {
	schema := models.OTELLogsTableSchema

	// Replace placeholders with actual values
	schema = strings.ReplaceAll(schema, "{{database_name}}", c.Source.GetDatabaseName())
	schema = strings.ReplaceAll(schema, "{{table_name}}", c.Source.GetTableName())
	schema = strings.ReplaceAll(schema, "{{ttl_day}}", strconv.Itoa(ttlDays))

	// Create the table
	if err := c.DB.Exec(ctx, schema); err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	return nil
}
