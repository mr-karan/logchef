package clickhouse

import (
	"context"
	"fmt"
	"math"
	"regexp"
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

// ColumnStats retrieves detailed statistics for each column of a specific table.
func (c *Client) ColumnStats(ctx context.Context, database, table string) ([]TableColumnStat, error) {
	// Query system.parts_columns for statistics on active parts.
	engine,err := c.getEngine(ctx, database, table)
	var actualTable string
	if err !=nil {
		fmt.Println(err)
	}
	switch engine{
	case "Distributed":
		var engine_full,err = c.getEngineTable(ctx, database,table)
		if err !=nil {
			fmt.Println(err)
		}
		pattern := `Distributed\(.*?,\s*'(.*?)'\s*,\s*'(.*?)'`
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(engine_full)

		if len(matches) < 3 {
			fmt.Errorf("invalid Distributed table definition")
		}
		actualTable = matches[2]
	default:
		actualTable = table
	}
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
	`, database, actualTable)

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

func (c* Client) getEngine(ctx context.Context, database, table string) (string, error){
	query := fmt.Sprintf(`
		SELECT 
			name,engine 
		FROM system.tables 
		WHERE (database = '%s') AND (table = '%s')
		`, database, table)
	var name, engine string
	rows,err := c.conn.Query(ctx, query)
	if err!=nil{
		return "",fmt.Errorf("Error getting the engine : %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&name, &engine); err !=nil {
			return "", fmt.Errorf("error scanning engine result: %w", err)
		}
	} else {
		return "", fmt.Errorf("no engine found for table %s.%s", database, table)
	}
	return engine, nil
}

func (c* Client) getEngineTable(ctx context.Context, database, table string) (string,error){
	tableQuery := fmt.Sprintf(`
		SELECT 
			engine_full
		FROM 	system.tables
		WHERE (database = '%s') and (table = '%s')
		`	, database, table)
	rows,err := c.conn.Query(ctx, tableQuery)
	var engine_full string
	if err != nil {
		fmt.Println("Error with engine", err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&engine_full); err !=nil {
			return "", fmt.Errorf("error scanning engine result: %w", err)
		}
	}
	return engine_full,nil
}