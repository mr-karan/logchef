package clickhouse

import (
	"context"
	"fmt"
	"math"
)

// TableColumnStat represents statistics for a single column in a ClickHouse table
type TableColumnStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Column       string  `json:"column"`
	Compressed   string  `json:"compressed"`
	Uncompressed string  `json:"uncompressed"`
	ComprRatio   float64 `json:"compr_ratio"`
	RowsCount    uint64  `json:"rows_count"`
	AvgRowSize   float64 `json:"avg_row_size"`
}

// TableStat represents statistics for a ClickHouse table
type TableStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Compressed   string  `json:"compressed"`
	Uncompressed string  `json:"uncompressed"`
	ComprRate    float64 `json:"compr_rate"`
	Rows         uint64  `json:"rows"`
	PartCount    uint64  `json:"part_count"`
}

// GetTableColumnStats retrieves column statistics for a specific table
func (c *Client) GetTableColumnStats(ctx context.Context, database, table string) ([]TableColumnStat, error) {
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

		// Check for NaN values and replace them with 0
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

// GetTableStats retrieves overall statistics for a specific table
func (c *Client) GetTableStats(ctx context.Context, database, table string) (*TableStat, error) {
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
	`, database, table)

	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error executing table stats query: %w", err)
	}
	defer rows.Close()

	var stats []TableStat
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

		// Check for NaN values and replace them with 0
		if math.IsNaN(stat.ComprRate) {
			stat.ComprRate = 0
		}

		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table stats rows: %w", err)
	}

	// Return default empty stats if no data found
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

	return &stats[0], nil
}
