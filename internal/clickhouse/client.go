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
	queryHooks []QueryHook
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

	client := &Client{
		conn:       conn,
		logger:     logger,
		queryHooks: []QueryHook{},
	}

	// Add default log query hook if debug logging is enabled
	client.AddQueryHook(NewLogQueryHook(logger, false))

	return client, nil
}

// AddQueryHook adds a query hook to the client
func (c *Client) AddQueryHook(hook QueryHook) {
	c.queryHooks = append(c.queryHooks, hook)
}

// executeQueryWithHooks executes a query with hooks
func (c *Client) executeQueryWithHooks(ctx context.Context, query string, fn func(context.Context) error) error {
	var err error
	start := time.Now()

	// Call before hooks
	for _, hook := range c.queryHooks {
		ctx, err = hook.BeforeQuery(ctx, query)
		if err != nil {
			return err
		}
	}

	// Execute the query
	err = fn(ctx)
	duration := time.Since(start)

	// Call after hooks
	for _, hook := range c.queryHooks {
		hook.AfterQuery(ctx, query, err, duration)
	}

	return err
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

	var rows driver.Rows

	// Execute the query with hooks
	err := c.executeQueryWithHooks(ctx, query, func(ctx context.Context) error {
		var err error
		rows, err = c.conn.Query(ctx, query)
		return err
	})

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

	var execErr error
	err := c.executeQueryWithHooks(ctx, query, func(ctx context.Context) error {
		execErr = c.conn.Exec(ctx, query)
		return execErr
	})

	if err != nil {
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

	var rows driver.Rows
	// Execute the query with hooks
	err := c.executeQueryWithHooks(ctx, query, func(ctx context.Context) error {
		var err error
		rows, err = c.conn.Query(ctx, query)
		return err
	})

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

// GetTableSchema retrieves the schema (column names and types) for a ClickHouse table
func (c *Client) GetTableSchema(ctx context.Context, database, table string) ([]models.ColumnInfo, error) {
	start := time.Now()
	defer func() {
		c.logger.Debug("schema query completed",
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	c.logger.Debug("retrieving table schema",
		"database", database,
		"table", table,
	)

	// First try using DESCRIBE TABLE
	columns, err := c.getTableSchemaUsingDescribe(ctx, database, table)
	if err != nil {
		c.logger.Warn("failed to get schema using DESCRIBE TABLE, trying fallback method",
			"error", err,
			"database", database,
			"table", table,
		)

		// If DESCRIBE TABLE fails, try using system.columns
		columns, err = c.getTableSchemaUsingSystemColumns(ctx, database, table)
		if err != nil {
			c.logger.Error("failed to get schema using system.columns",
				"error", err,
				"database", database,
				"table", table,
			)
			return nil, err
		}
	}

	c.logger.Debug("retrieved table schema",
		"database", database,
		"table", table,
		"column_count", len(columns),
	)

	return columns, nil
}

// getTableSchemaUsingDescribe retrieves schema using DESCRIBE TABLE
func (c *Client) getTableSchemaUsingDescribe(ctx context.Context, database, table string) ([]models.ColumnInfo, error) {
	// Use DESCRIBE TABLE to get column information
	query := fmt.Sprintf("DESCRIBE TABLE `%s`.`%s`", database, table)

	// Execute the query
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("executing schema query: %w", err)
	}
	defer rows.Close()

	// Get column information from the result set
	columnTypes := rows.ColumnTypes()
	c.logger.Debug("describe table column types",
		"count", len(columnTypes),
		"names", fmt.Sprintf("%v", columnTypes),
	)

	// Process results
	var columns []models.ColumnInfo
	for rows.Next() {
		// Create scan targets based on the number of columns
		scanTargets := make([]interface{}, len(columnTypes))
		for i := range scanTargets {
			scanTargets[i] = new(string)
		}

		if err := rows.Scan(scanTargets...); err != nil {
			return nil, fmt.Errorf("scanning schema row: %w", err)
		}

		// Extract name and type (first two columns)
		if len(scanTargets) < 2 {
			return nil, fmt.Errorf("unexpected column count in DESCRIBE TABLE result: %d", len(scanTargets))
		}

		name := *(scanTargets[0].(*string))
		dataType := *(scanTargets[1].(*string))

		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: dataType,
		})
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating schema rows: %w", err)
	}

	return columns, nil
}

// getTableSchemaUsingSystemColumns retrieves schema using system.columns table
func (c *Client) getTableSchemaUsingSystemColumns(ctx context.Context, database, table string) ([]models.ColumnInfo, error) {
	// Query system.columns to get column information
	query := fmt.Sprintf("SELECT name, type FROM system.columns WHERE database = '%s' AND table = '%s' ORDER BY position", database, table)

	// Execute the query
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("executing system.columns query: %w", err)
	}
	defer rows.Close()

	// Process results
	var columns []models.ColumnInfo
	for rows.Next() {
		var name, dataType string

		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, fmt.Errorf("scanning system.columns row: %w", err)
		}

		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: dataType,
		})
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating system.columns rows: %w", err)
	}

	return columns, nil
}

// GetTableCreateStatement retrieves the CREATE TABLE statement for a ClickHouse table
func (c *Client) GetTableCreateStatement(ctx context.Context, database, table string) (string, error) {
	start := time.Now()
	defer func() {
		c.logger.Debug("create table query completed",
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()

	// Use SHOW CREATE TABLE to get the create statement
	query := fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", database, table)

	// Execute the query
	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return "", fmt.Errorf("executing create table query: %w", err)
	}
	defer rows.Close()

	// SHOW CREATE TABLE returns a single row with a single column
	var createStatement string
	if rows.Next() {
		if err := rows.Scan(&createStatement); err != nil {
			return "", fmt.Errorf("scanning create table result: %w", err)
		}
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterating create table result: %w", err)
	}

	return createStatement, nil
}
