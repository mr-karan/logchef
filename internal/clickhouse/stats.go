package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

const (
	// Keep stats queries fast so one slow sub-query doesn't block the whole response.
	tableStatsTimeoutSeconds     = 2
	ingestionStatsTimeoutSeconds = 3
)

func statsQueryContext(ctx context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return ctx, func() {}
	}

	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	queryCtx = clickhouse.Context(queryCtx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": timeoutSeconds,
	}))
	return queryCtx, cancel
}

// TableStat represents overall statistics for a ClickHouse table,
// typically retrieved from system.parts.
type TableStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Compressed   string  `json:"compressed"`   // Total size on disk (human-readable).
	Uncompressed string  `json:"uncompressed"` // Total original size (human-readable).
	ComprRate    float64 `json:"compr_rate"`   // Overall compression rate.
	Rows         uint64  `json:"rows"`         // Total rows in the table partition/part.
	PartCount    uint64  `json:"part_count"`   // Number of data parts.
}

// IngestionBucket represents ingestion volume for a given time bucket.
type IngestionBucket struct {
	Bucket time.Time `json:"bucket"`
	Rows   uint64    `json:"rows"`
}

// IngestionStats represents recent ingestion activity for a table.
type IngestionStats struct {
	Rows1h        uint64            `json:"rows_1h"`
	Rows24h       uint64            `json:"rows_24h"`
	Rows7d        uint64            `json:"rows_7d"`
	LatestTS      *time.Time        `json:"latest_ts,omitempty"`
	HourlyBuckets []IngestionBucket `json:"hourly_buckets"`
	DailyBuckets  []IngestionBucket `json:"daily_buckets"`
}

// TableStats retrieves overall statistics for a specific table from active parts.
func (c *Client) TableStats(ctx context.Context, database, table string) (*TableStat, error) {
	// Query system.parts for aggregated table statistics.
	query := fmt.Sprintf(`
		SELECT
			database,
			table,
			formatReadableSize(sum(data_compressed_bytes) AS size) AS compressed,
			formatReadableSize(sum(data_uncompressed_bytes) AS usize) AS uncompressed,
			round(usize / size, 2) AS compr_rate,
			sum(rows) AS rows,
			count() AS part_count
		FROM system.parts
		WHERE (active = 1) AND (database = '%s') AND (table = '%s')
		GROUP BY
			database,
			table
		ORDER BY size DESC
	`, database, table) // Note: ORDER BY might not be necessary if only one row is expected.

	queryCtx, cancel := statsQueryContext(ctx, tableStatsTimeoutSeconds)
	defer cancel()

	rows, err := c.conn.Query(queryCtx, query)
	if err != nil {
		return nil, fmt.Errorf("error executing table stats query: %w", err)
	}
	defer rows.Close()

	var stats []TableStat // Use a slice in case query returns multiple rows unexpectedly.
	for rows.Next() {
		var stat TableStat
		if err := rows.Scan(
			&stat.Database,
			&stat.Table,
			&stat.Compressed,
			&stat.Uncompressed,
			&stat.ComprRate,
			&stat.Rows,
			&stat.PartCount,
		); err != nil {
			return nil, fmt.Errorf("error scanning table stats row: %w", err)
		}

		// Replace NaN resulting from division by zero with 0.
		if math.IsNaN(stat.ComprRate) {
			stat.ComprRate = 0
		}

		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table stats rows: %w", err)
	}

	// If no active parts found, return default empty stats.
	if len(stats) == 0 {
		return &TableStat{
			Database:     database,
			Table:        table,
			Compressed:   "0B",
			Uncompressed: "0B",
			ComprRate:    0,
			Rows:         0,
			PartCount:    0,
		}, nil
	}

	// Return the first row (should be the only one).
	return &stats[0], nil
}

func quoteIdentifier(name string) string {
	trimmed := strings.TrimSpace(name)
	if strings.HasPrefix(trimmed, "`") && strings.HasSuffix(trimmed, "`") {
		return trimmed
	}
	escaped := strings.ReplaceAll(trimmed, "`", "``")
	return "`" + escaped + "`"
}

type ingestionActivityRow struct {
	bucket time.Time
	rows   uint64
	rows1h uint64
	latest *time.Time
}

func ingestionActivityQuery(database, table, timestampField string) (string, error) {
	if timestampField == "" {
		return "", fmt.Errorf("timestamp field is required for ingestion stats")
	}
	tsField := quoteIdentifier(timestampField)
	qualifiedTable := fmt.Sprintf("%s.%s", quoteIdentifier(database), quoteIdentifier(table))
	return fmt.Sprintf(`
		WITH now64(3) AS anchor
		SELECT toStartOfHour(%s) AS bucket,
			count() AS rows,
			countIf(%s >= anchor - INTERVAL 1 HOUR AND %s <= anchor) AS rows_1h,
			max(%s) AS latest_ts
		FROM %s
		WHERE %s >= anchor - INTERVAL 24 HOUR AND %s <= anchor
		GROUP BY bucket
		ORDER BY bucket ASC
	`, tsField, tsField, tsField, tsField, qualifiedTable, tsField, tsField), nil
}

func accumulateIngestionActivity(rows []ingestionActivityRow) *IngestionStats {
	stats := &IngestionStats{HourlyBuckets: make([]IngestionBucket, 0, len(rows))}
	for _, row := range rows {
		stats.HourlyBuckets = append(stats.HourlyBuckets, IngestionBucket{Bucket: row.bucket, Rows: row.rows})
		stats.Rows24h += row.rows
		stats.Rows1h += row.rows1h
		if row.latest != nil && (stats.LatestTS == nil || row.latest.After(*stats.LatestTS)) {
			stats.LatestTS = row.latest
		}
	}
	return stats
}

func activityError(queryCtx context.Context, operation string, err error) error {
	if errors.Is(queryCtx.Err(), context.DeadlineExceeded) || isTimeoutError(err) {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// IngestionStats retrieves bounded, recent ingestion activity in one query.
func (c *Client) IngestionStats(ctx context.Context, database, table, timestampField string) (*IngestionStats, error) {
	query, err := ingestionActivityQuery(database, table, timestampField)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, ingestionStatsTimeoutSeconds*time.Second)
	defer cancel()
	queryCtx = clickhouse.Context(queryCtx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": ingestionStatsTimeoutSeconds,
		"max_threads":        2,
	}))
	rows, err := c.conn.Query(queryCtx, query)
	if err != nil {
		return nil, activityError(queryCtx, "error executing ingestion activity query", err)
	}
	defer rows.Close()
	resultRows := make([]ingestionActivityRow, 0)
	for rows.Next() {
		var row ingestionActivityRow
		if err := rows.Scan(&row.bucket, &row.rows, &row.rows1h, &row.latest); err != nil {
			return nil, activityError(queryCtx, "scan ingestion activity row", err)
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, activityError(queryCtx, "iterate ingestion activity rows", err)
	}
	return accumulateIngestionActivity(resultRows), nil
}
