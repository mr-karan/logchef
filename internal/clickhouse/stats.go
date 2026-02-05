package clickhouse

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// TableColumnStat represents statistics for a single column in a ClickHouse table,
// typically retrieved from system.parts_columns.
type TableColumnStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Column       string  `json:"column"`
	Compressed   string  `json:"compressed"`   // Size on disk (human-readable).
	Uncompressed string  `json:"uncompressed"` // Original size (human-readable).
	ComprRatio   float64 `json:"compr_ratio"`  // Compression ratio.
	RowsCount    uint64  `json:"rows_count"`   // Number of rows in the column chunk.
	AvgRowSize   float64 `json:"avg_row_size"` // Average row size in bytes.
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

// ColumnStats retrieves detailed statistics for each column of a specific table.
func (c *Client) ColumnStats(ctx context.Context, database, table string) ([]TableColumnStat, error) {
	// Query system.parts_columns for statistics on active parts.
	query := fmt.Sprintf(`
		SELECT
			database,
			table,
			column,
			formatReadableSize(sum(column_data_compressed_bytes) AS size) AS compressed,
			formatReadableSize(sum(column_data_uncompressed_bytes) AS usize) AS uncompressed,
			round(usize / size, 2) AS compr_ratio,
			sum(rows) AS rows_cnt,
			round(usize / rows_cnt, 2) AS avg_row_size
		FROM system.parts_columns
		WHERE (active = 1) AND (database = '%s') AND (table = '%s')
		GROUP BY
			database,
			table,
			column
		ORDER BY size DESC
	`, database, table)

	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error executing column stats query: %w", err)
	}
	defer rows.Close()

	var stats []TableColumnStat
	for rows.Next() {
		var stat TableColumnStat
		if err := rows.Scan(
			&stat.Database,
			&stat.Table,
			&stat.Column,
			&stat.Compressed,
			&stat.Uncompressed,
			&stat.ComprRatio,
			&stat.RowsCount,
			&stat.AvgRowSize,
		); err != nil {
			return nil, fmt.Errorf("error scanning column stats row: %w", err)
		}

		// Replace NaN values resulting from division by zero with 0.
		if math.IsNaN(stat.ComprRatio) {
			stat.ComprRatio = 0
		}
		if math.IsNaN(stat.AvgRowSize) {
			stat.AvgRowSize = 0
		}

		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating column stats rows: %w", err)
	}

	return stats, nil
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

	rows, err := c.conn.Query(ctx, query)
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

// IngestionStats retrieves recent ingestion statistics for a specific table.
func (c *Client) IngestionStats(ctx context.Context, database, table, timestampField string) (*IngestionStats, error) {
	if timestampField == "" {
		return nil, fmt.Errorf("timestamp field is required for ingestion stats")
	}

	tsField := quoteIdentifier(timestampField)
	qualifiedTable := fmt.Sprintf("%s.%s", quoteIdentifier(database), quoteIdentifier(table))

	stats := &IngestionStats{
		HourlyBuckets: []IngestionBucket{},
		DailyBuckets:  []IngestionBucket{},
	}

	summaryQuery := fmt.Sprintf(`
		SELECT
			maxOrNull(%s) AS latest_ts,
			countIf(%s >= now() - INTERVAL 1 HOUR) AS rows_1h,
			countIf(%s >= now() - INTERVAL 24 HOUR) AS rows_24h,
			countIf(%s >= now() - INTERVAL 7 DAY) AS rows_7d
		FROM %s
	`, tsField, tsField, tsField, tsField, qualifiedTable)

	summaryRows, err := c.conn.Query(ctx, summaryQuery)
	if err != nil {
		return nil, fmt.Errorf("error executing ingestion summary query: %w", err)
	}
	defer summaryRows.Close()

	if summaryRows.Next() {
		var latest *time.Time
		if err := summaryRows.Scan(&latest, &stats.Rows1h, &stats.Rows24h, &stats.Rows7d); err != nil {
			return nil, fmt.Errorf("error scanning ingestion summary row: %w", err)
		}
		stats.LatestTS = latest
	}

	if err := summaryRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ingestion summary rows: %w", err)
	}

	hourlyQuery := fmt.Sprintf(`
		SELECT
			toStartOfHour(%s) AS bucket,
			count() AS rows
		FROM %s
		WHERE %s >= now() - INTERVAL 24 HOUR
		GROUP BY bucket
		ORDER BY bucket ASC
	`, tsField, qualifiedTable, tsField)

	hourlyRows, err := c.conn.Query(ctx, hourlyQuery)
	if err != nil {
		return nil, fmt.Errorf("error executing hourly ingestion query: %w", err)
	}
	defer hourlyRows.Close()

	for hourlyRows.Next() {
		var bucket time.Time
		var rows uint64
		if err := hourlyRows.Scan(&bucket, &rows); err != nil {
			return nil, fmt.Errorf("error scanning hourly ingestion row: %w", err)
		}
		stats.HourlyBuckets = append(stats.HourlyBuckets, IngestionBucket{Bucket: bucket, Rows: rows})
	}

	if err := hourlyRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating hourly ingestion rows: %w", err)
	}

	dailyQuery := fmt.Sprintf(`
		SELECT
			toStartOfDay(%s) AS bucket,
			count() AS rows
		FROM %s
		WHERE %s >= now() - INTERVAL 30 DAY
		GROUP BY bucket
		ORDER BY bucket ASC
	`, tsField, qualifiedTable, tsField)

	dailyRows, err := c.conn.Query(ctx, dailyQuery)
	if err != nil {
		return nil, fmt.Errorf("error executing daily ingestion query: %w", err)
	}
	defer dailyRows.Close()

	for dailyRows.Next() {
		var bucket time.Time
		var rows uint64
		if err := dailyRows.Scan(&bucket, &rows); err != nil {
			return nil, fmt.Errorf("error scanning daily ingestion row: %w", err)
		}
		stats.DailyBuckets = append(stats.DailyBuckets, IngestionBucket{Bucket: bucket, Rows: rows})
	}

	if err := dailyRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating daily ingestion rows: %w", err)
	}

	return stats, nil
}
