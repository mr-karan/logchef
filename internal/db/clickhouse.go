package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/mr-karan/logchef/pkg/config"
)

type Clickhouse struct {
	pool *ConnectionPool
	conn clickhouse.Conn // Add this field to use the clickhouse package
}

func NewClickhouse() *Clickhouse {
	return &Clickhouse{
		pool: NewConnectionPool(),
	}
}

func (c *Clickhouse) Close() error {
	c.pool.Stop()
	return nil
}

func (c *Clickhouse) GetPool() *ConnectionPool {
	return c.pool
}

func (c *Clickhouse) UpdateTableTTL(sourceID, tableName string, ttlDays int) error {
	ctx := context.Background()
	db, err := c.pool.GetConnection(sourceID)
	if err != nil {
		return err
	}

	// Alter table TTL without SYNC
	alterQuery := fmt.Sprintf(
		"ALTER TABLE %s MODIFY TTL toDateTime(timestamp) + INTERVAL %d DAY SETTINGS mutations_sync = 0",
		tableName,
		ttlDays,
	)

	return db.Exec(ctx, alterQuery)
}

func (c *Clickhouse) DropSourceTable(sourceID, tableName string) error {
	ctx := context.Background()
	db, err := c.pool.GetConnection(sourceID)
	if err != nil {
		return err
	}

	// Drop the table
	if err := db.Exec(ctx, "DROP TABLE IF EXISTS "+tableName); err != nil {
		return err
	}

	// Remove the connection from pool
	return c.pool.RemoveConnection(sourceID)
}

// CreateSourceTable creates a new table for a source using the schema template
func (c *Clickhouse) CreateSourceTable(sourceID string, cfg config.ClickhouseConfig, tableName string, ttlDays int) error {
	// First add the connection to the pool
	if err := c.pool.AddConnection(sourceID, cfg); err != nil {
		return err
	}

	// Get the connection
	db, err := c.pool.GetConnection(sourceID)
	if err != nil {
		return err
	}

	schema := `CREATE TABLE IF NOT EXISTS {{table_name}}
    (
        id UUID DEFAULT generateUUIDv4() CODEC(ZSTD(1)),
        timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
        trace_id String CODEC(ZSTD(1)),
        span_id String CODEC(ZSTD(1)),
        trace_flags UInt32 CODEC(ZSTD(1)),
        severity_text LowCardinality(String) CODEC(ZSTD(1)),
        severity_number Int32 CODEC(ZSTD(1)),
        service_name LowCardinality(String) CODEC(ZSTD(1)),
        namespace LowCardinality(String) CODEC(ZSTD(1)),
        body String CODEC(ZSTD(1)),
        log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),

        INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
        INDEX idx_severity_text severity_text TYPE set(100) GRANULARITY 4,
        INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
        INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
        INDEX idx_body body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
    )
    ENGINE = MergeTree()
    PARTITION BY toDate(timestamp)
    ORDER BY (namespace, service_name, timestamp)
    TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY
    SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;`

	schema = strings.ReplaceAll(schema, "{{table_name}}", tableName)
	schema = strings.ReplaceAll(schema, "{{ttl_day}}", strconv.Itoa(ttlDays))

	ctx := context.Background()
	err = db.Exec(ctx, schema)
	return err
}
