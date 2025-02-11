package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"backend-v2/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Client represents a ClickHouse client using native protocol
type Client struct {
	conn driver.Conn
	log  *slog.Logger
}

// ClientOptions represents options for creating a new client
type ClientOptions struct {
	Host     string
	Database string
	Username string
	Password string
	Settings map[string]interface{}
}

// NewClient creates a new ClickHouse client
func NewClient(opts ClientOptions, log *slog.Logger) (*Client, error) {
	// Ensure host has port for native protocol
	host := opts.Host
	if !strings.Contains(host, ":") {
		host = host + ":9000" // Default ClickHouse native protocol port
	}

	options := &clickhouse.Options{
		Addr: []string{host},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.Username,
			Password: opts.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Debug:       true, // Enable debug logging temporarily
		DialTimeout: 10 * time.Second,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		Protocol: clickhouse.Native,
	}

	// Add any additional settings
	if opts.Settings != nil {
		for k, v := range opts.Settings {
			options.Settings[k] = v
		}
	}

	log.Debug("creating clickhouse connection",
		"host", host,
		"database", opts.Database,
		"protocol", "native",
	)

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create clickhouse connection: %w", err)
	}

	// Test connection
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	return &Client{
		conn: conn,
		log:  log,
	}, nil
}

// Query executes a query and returns the results
func (c *Client) Query(ctx context.Context, query string) (*models.QueryResult, error) {
	c.log.Debug("executing query",
		"query", query,
		"context", fmt.Sprintf("%+v", ctx),
	)

	// Check if this is a DDL statement
	if isDDLStatement(query) {
		c.log.Debug("executing DDL statement")
		return c.execDDL(ctx, query)
	}

	// Start timing the query
	startTime := time.Now()

	// Execute query
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		c.log.Error("query failed",
			"error", err,
			"query", query,
		)
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Get column information
	columnTypes := rows.ColumnTypes()
	columnNames := rows.Columns()

	c.log.Debug("query executed successfully",
		"columns", columnNames,
		"num_columns", len(columnTypes),
	)

	// Create column info
	columns := make([]models.ColumnInfo, len(columnTypes))
	for i, ct := range columnTypes {
		columns[i] = models.ColumnInfo{
			Name: columnNames[i],
			Type: ct.DatabaseTypeName(),
		}
	}

	// Prepare slice for scanning
	values := make([]interface{}, len(columnTypes))
	for i := range values {
		values[i] = reflect.New(columnTypes[i].ScanType()).Interface()
	}

	// Scan rows
	var data []map[string]interface{}
	for rows.Next() {
		if err := rows.Scan(values...); err != nil {
			c.log.Error("failed to scan row",
				"error", err,
			)
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create a map for the current row
		rowData := make(map[string]interface{})
		for i, name := range columnNames {
			// Get the actual value from the pointer
			val := reflect.ValueOf(values[i]).Elem().Interface()
			rowData[name] = val
		}
		data = append(data, rowData)
	}

	if err := rows.Err(); err != nil {
		c.log.Error("error during row iteration",
			"error", err,
		)
		return nil, fmt.Errorf("error during row iteration: %w", err)
	}

	// Get query statistics
	stats := models.QueryStats{
		ExecutionTimeMs: float64(time.Since(startTime).Milliseconds()),
		RowsRead:        len(data),
	}

	c.log.Debug("query completed",
		"rows_read", stats.RowsRead,
		"execution_time_ms", stats.ExecutionTimeMs,
	)

	return &models.QueryResult{
		Data:    data,
		Columns: columns,
		Stats:   stats,
	}, nil
}

// execDDL executes a DDL statement
func (c *Client) execDDL(ctx context.Context, query string) (*models.QueryResult, error) {
	if err := c.conn.Exec(ctx, query); err != nil {
		c.log.Error("DDL execution failed",
			"error", err,
			"query", query,
		)
		return nil, fmt.Errorf("failed to execute DDL: %w", err)
	}

	// Return empty result for DDL statements
	return &models.QueryResult{
		Data:    []map[string]interface{}{},
		Columns: []models.ColumnInfo{},
		Stats:   models.QueryStats{},
	}, nil
}

// isDDLStatement checks if the query is a DDL statement
func isDDLStatement(query string) bool {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	ddlPrefixes := []string{
		"CREATE", "ALTER", "DROP", "RENAME", "TRUNCATE",
	}

	for _, prefix := range ddlPrefixes {
		if strings.HasPrefix(upperQuery, prefix) {
			return true
		}
	}
	return false
}

// CheckHealth performs a health check
func (c *Client) CheckHealth(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// Close closes the connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// GetTimeSeries returns time series data for log counts
func (c *Client) GetTimeSeries(ctx context.Context, params TimeSeriesParams) (*TimeSeriesResult, error) {
	// Convert TimeWindow to ClickHouse interval function and seconds
	var intervalFunc, intervalSeconds string
	switch params.Window {
	case TimeWindow1m:
		intervalFunc = "toIntervalMinute"
		intervalSeconds = "60"
	case TimeWindow5m:
		intervalFunc = "toIntervalMinute"
		intervalSeconds = "300"
	case TimeWindow15m:
		intervalFunc = "toIntervalMinute"
		intervalSeconds = "900"
	case TimeWindow1h:
		intervalFunc = "toIntervalHour"
		intervalSeconds = "3600"
	case TimeWindow6h:
		intervalFunc = "toIntervalHour"
		intervalSeconds = "21600"
	case TimeWindow24h:
		intervalFunc = "toIntervalHour"
		intervalSeconds = "86400"
	default:
		return nil, fmt.Errorf("unsupported time window: %s", params.Window)
	}

	// Build and execute query with zero-filling using numbers table
	query := fmt.Sprintf(`
		WITH
			toDateTime64('%s', 3) as start_time,
			toDateTime64('%s', 3) as end_time,
			%s(1) as interval
		SELECT
			toUnixTimestamp64Milli(time_bucket) * 1000 as timestamp,
			coalesce(log_count, 0) as count
		FROM (
			-- Generate all time buckets
			SELECT
				toStartOfInterval(
					start_time + toIntervalSecond(number * %s),
					interval
				) as time_bucket
			FROM numbers(1 + dateDiff('%s', start_time, end_time))
			WHERE time_bucket <= end_time
		) AS time_buckets
		LEFT JOIN (
			-- Actual log counts
			SELECT
				toStartOfInterval(timestamp, interval) AS time_bucket,
				count() AS log_count
			FROM %s
			WHERE timestamp >= start_time
			AND timestamp <= end_time
			GROUP BY time_bucket
		) AS counts
		USING time_bucket
		ORDER BY time_bucket ASC`,
		params.StartTime.Format("2006-01-02 15:04:05.000"),
		params.EndTime.Format("2006-01-02 15:04:05.000"),
		intervalFunc,
		intervalSeconds,
		getIntervalUnit(params.Window),
		params.Table,
	)

	c.log.Debug("executing time series query",
		"query", query,
		"start_time", params.StartTime,
		"end_time", params.EndTime,
		"window", params.Window,
	)

	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute time series query: %w", err)
	}
	defer rows.Close()

	var result TimeSeriesResult

	// Scan results
	for rows.Next() {
		var bucket TimeSeriesBucket
		if err := rows.Scan(&bucket.Timestamp, &bucket.Count); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result.Buckets = append(result.Buckets, bucket)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration: %w", err)
	}

	return &result, nil
}

// getIntervalUnit returns the appropriate unit for dateDiff based on the window
func getIntervalUnit(window TimeWindow) string {
	switch window {
	case TimeWindow1m, TimeWindow5m, TimeWindow15m:
		return "minute"
	case TimeWindow1h, TimeWindow6h, TimeWindow24h:
		return "hour"
	default:
		return "minute"
	}
}
