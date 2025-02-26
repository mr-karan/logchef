package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Client represents a ClickHouse client using native protocol
type Client struct {
	conn       driver.Conn
	logger     *slog.Logger
	lastHealth models.SourceHealth
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
func NewClient(opts ClientOptions, logger *slog.Logger) (*Client, error) {
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

	logger.Debug("creating clickhouse connection",
		"host", host,
		"database", opts.Database,
		"protocol", "native",
	)

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("creating clickhouse connection: %w", err)
	}

	// Test connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging clickhouse: %w", err)
	}

	return &Client{
		conn:   conn,
		logger: logger,
	}, nil
}

// Query executes a query and returns the results
func (c *Client) Query(ctx context.Context, query string) (*models.QueryResult, error) {
	start := time.Now()
	defer func() {
		c.logger.Debug("query completed",
			"duration_ms", time.Since(start).Milliseconds(),
			"query_length", len(query),
		)
	}()

	// Check if this is a DDL statement
	if isDDLStatement(query) {
		return c.execDDL(ctx, query)
	}

	// Execute the query
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	// Get column information
	columnTypes := rows.ColumnTypes()
	columns := make([]models.ColumnInfo, len(columnTypes))
	for i, ct := range columnTypes {
		columns[i] = models.ColumnInfo{
			Name: ct.Name(),
			Type: ct.DatabaseTypeName(),
		}
	}

	// Process rows
	var result []map[string]interface{}
	for rows.Next() {
		// Create properly typed scan targets based on column types
		values := make([]interface{}, len(columnTypes))
		for i := range values {
			values[i] = reflect.New(columnTypes[i].ScanType()).Interface()
		}

		// Scan the row into the values
		if err := rows.Scan(values...); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}

		// Create a map for this row
		row := make(map[string]interface{})
		for i, col := range columns {
			// Get the value from the typed pointer
			val := reflect.ValueOf(values[i]).Elem().Interface()
			row[col.Name] = val
		}

		result = append(result, row)
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	// Create query result
	queryResult := &models.QueryResult{
		Logs:    result,
		Columns: columns,
		Stats: models.QueryStats{
			RowsRead:        len(result),
			ExecutionTimeMs: float64(time.Since(start).Milliseconds()),
		},
	}

	return queryResult, nil
}

// execDDL executes a DDL statement
func (c *Client) execDDL(ctx context.Context, query string) (*models.QueryResult, error) {
	start := time.Now()

	if err := c.conn.Exec(ctx, query); err != nil {
		return nil, fmt.Errorf("executing DDL: %w", err)
	}

	// Return empty result for DDL
	return &models.QueryResult{
		Logs:    []map[string]interface{}{},
		Columns: []models.ColumnInfo{},
		Stats: models.QueryStats{
			RowsRead:        0,
			ExecutionTimeMs: float64(time.Since(start).Milliseconds()),
		},
	}, nil
}

// isDDLStatement checks if a query is a DDL statement
func isDDLStatement(query string) bool {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(upperQuery, "CREATE") ||
		strings.HasPrefix(upperQuery, "ALTER") ||
		strings.HasPrefix(upperQuery, "DROP") ||
		strings.HasPrefix(upperQuery, "TRUNCATE") ||
		strings.HasPrefix(upperQuery, "RENAME")
}

// CheckHealth checks if the connection is healthy
func (c *Client) CheckHealth(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// Close closes the connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// GetTimeSeries retrieves time series data for log counts
func (c *Client) GetTimeSeries(ctx context.Context, params TimeSeriesParams, tableName string) (*TimeSeriesResult, error) {
	start := time.Now()
	defer func() {
		c.logger.Debug("time series query completed",
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	// Determine the interval based on the time range
	interval := getIntervalUnit(params.Window)

	// Build the query using QueryBuilder
	qb := NewQueryBuilder(tableName)
	query := qb.BuildTimeSeriesQuery(params.StartTime.Unix(), params.EndTime.Unix(), interval)

	// Execute the query
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("executing time series query: %w", err)
	}
	defer rows.Close()

	// Process results
	var buckets []TimeSeriesBucket
	for rows.Next() {
		var ts int64
		var count int64
		if err := rows.Scan(&ts, &count); err != nil {
			return nil, fmt.Errorf("scanning time series row: %w", err)
		}
		buckets = append(buckets, TimeSeriesBucket{
			Timestamp: ts,
			Count:     count,
		})
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating time series rows: %w", err)
	}

	return &TimeSeriesResult{
		Buckets: buckets,
	}, nil
}

// getIntervalUnit returns the appropriate interval unit for a time window
func getIntervalUnit(window TimeWindow) string {
	switch window {
	case TimeWindow1m:
		return "Minute"
	case TimeWindow5m, TimeWindow15m:
		return "Minute"
	case TimeWindow1h:
		return "Hour"
	case TimeWindow6h, TimeWindow24h:
		return "Day"
	default:
		return "Hour"
	}
}

// GetLogContext retrieves logs before and after a specific timestamp
func (c *Client) GetLogContext(ctx context.Context, params LogContextParams, tableName string) (*LogContextResult, error) {
	start := time.Now()
	defer func() {
		c.logger.Debug("log context query completed",
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	// Build queries using QueryBuilder
	qb := NewQueryBuilder(tableName)
	beforeQuery, targetQuery, afterQuery := qb.BuildLogContextQueries(
		params.TargetTime.Unix(),
		params.BeforeLimit,
		params.AfterLimit,
	)

	// Execute queries
	beforeResult, err := c.Query(ctx, beforeQuery)
	if err != nil {
		return nil, fmt.Errorf("querying before logs: %w", err)
	}

	targetResult, err := c.Query(ctx, targetQuery)
	if err != nil {
		return nil, fmt.Errorf("querying target logs: %w", err)
	}

	afterResult, err := c.Query(ctx, afterQuery)
	if err != nil {
		return nil, fmt.Errorf("querying after logs: %w", err)
	}

	// Reverse the before logs to get chronological order
	beforeLogs := beforeResult.Logs
	for i, j := 0, len(beforeLogs)-1; i < j; i, j = i+1, j-1 {
		beforeLogs[i], beforeLogs[j] = beforeLogs[j], beforeLogs[i]
	}

	return &LogContextResult{
		BeforeLogs: beforeLogs,
		TargetLogs: targetResult.Logs,
		AfterLogs:  afterResult.Logs,
		Stats: models.QueryStats{
			RowsRead:        len(beforeLogs) + len(targetResult.Logs) + len(afterResult.Logs),
			ExecutionTimeMs: float64(time.Since(start).Milliseconds()),
		},
	}, nil
}
