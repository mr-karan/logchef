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
	// If ttlDays is -1, don't set TTL
	if ttlDays == -1 {
		ttlDays = 0
	}

	// Create the database if it doesn't exist
	if c.Source.Database == "" {
		return fmt.Errorf("database name is required")
	}

	if err := c.DB.Exec(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", c.Source.Database)); err != nil {
		return fmt.Errorf("error creating database: %w", err)
	}

	// Create tables based on source type
	switch SourceType(c.Source.SchemaType) {
	case SourceTypeOTEL:
		return c.createOTELSource(ctx, ttlDays)
	case SourceTypeHTTP:
		return c.createHTTPSource(ctx, ttlDays)
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

// createHTTPSource creates tables for HTTP source
func (c *Connection) createHTTPSource(ctx context.Context, ttlDays int) error {
	schema := models.HTTPLogsTableSchema

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
