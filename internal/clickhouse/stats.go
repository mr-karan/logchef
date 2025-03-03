package clickhouse

import (
	"context"
	"fmt"
)

// TableColumnStat represents statistics for a single column in a ClickHouse table
type TableColumnStat struct {
	Database      string  `json:"database"`
	Table         string  `json:"table"`
	Column        string  `json:"column"`
	Compressed    string  `json:"compressed"`
	Uncompressed  string  `json:"uncompressed"`
	ComprRatio    float64 `json:"compr_ratio"`
	RowsCount     int64   `json:"rows_count"`
	AvgRowSize    float64 `json:"avg_row_size"`
}

// TableStat represents statistics for a ClickHouse table
type TableStat struct {
	Database     string  `json:"database"`
	Table        string  `json:"table"`
	Compressed   string  `json:"compressed"`
	Uncompressed string  `json:"uncompressed"`
	ComprRate    float64 `json:"compr_rate"`
	Rows         int64   `json:"rows"`
	PartCount    int     `json:"part_count"`
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

	var stats []TableColumnStat
	err := c.db.Select(ctx, &stats, query)
	if err != nil {
		return nil, fmt.Errorf("error getting table column stats: %w", err)
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

	var stats []TableStat
	err := c.db.Select(ctx, &stats, query)
	if err != nil {
		return nil, fmt.Errorf("error getting table stats: %w", err)
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no stats found for table %s.%s", database, table)
	}

	return &stats[0], nil
}