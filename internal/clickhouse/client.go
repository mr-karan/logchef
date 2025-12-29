package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Default values for query execution
const (
	// DefaultQueryTimeout is the default max_execution_time in seconds if not specified
	DefaultQueryTimeout = 60
	// MaxQueryTimeout is the maximum allowed timeout to prevent resource abuse
	MaxQueryTimeout = 300 // 5 minutes
)

// Client represents a connection to a ClickHouse database using the native protocol.
// It provides methods for executing queries and retrieving metadata.
type Client struct {
	conn       driver.Conn // Underlying ClickHouse native connection.
	logger     *slog.Logger
	queryHooks []QueryHook         // Hooks to execute before/after queries.
	mu         sync.Mutex          // Protects shared resources within the client if any
	opts       *clickhouse.Options // Stores connection options for reconnection
	sourceID   string              // Source ID for metrics tracking
	source     *models.Source      // Source model for metrics with meaningful labels
	metrics    *metrics.ClickHouseMetrics
}

// ClientOptions holds configuration for establishing a new ClickHouse client connection.
type ClientOptions struct {
	Host     string                 // Hostname or IP address.
	Database string                 // Target database name.
	Username string                 // Username for authentication.
	Password string                 // Password for authentication.
	Settings map[string]interface{} // Additional ClickHouse settings (e.g., max_execution_time).
	SourceID string                 // Source ID for metrics tracking.
	Source   *models.Source         // Source model for enhanced metrics.
}

// ExtendedColumnInfo provides detailed column metadata, including nullability,
// primary key status, default expressions, and comments, supplementing models.ColumnInfo.
type ExtendedColumnInfo struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	IsNullable        bool   `json:"is_nullable"`
	IsPrimaryKey      bool   `json:"is_primary_key"`
	DefaultExpression string `json:"default_expression,omitempty"`
	Comment           string `json:"comment,omitempty"`
}

// TableInfo represents comprehensive metadata about a ClickHouse table, including
// engine details, column definitions (basic and extended), sorting keys, and the CREATE statement.
type TableInfo struct {
	Database     string               `json:"database"`
	Name         string               `json:"name"`
	Engine       string               `json:"engine"`                  // e.g., "MergeTree", "Distributed"
	EngineParams []string             `json:"engine_params,omitempty"` // Parameters extracted from engine_full.
	Columns      []models.ColumnInfo  `json:"columns"`                 // Basic column info (Name, Type).
	ExtColumns   []ExtendedColumnInfo `json:"ext_columns,omitempty"`   // Detailed column info.
	SortKeys     []string             `json:"sort_keys"`               // Parsed sorting key columns.
	CreateQuery  string               `json:"create_query,omitempty"`  // Full CREATE TABLE statement.
}

// NewClient establishes a new connection to a ClickHouse server using the native protocol.
// It takes connection options and a logger, creates the connection, and returns a Client instance.
// Note: This does not automatically verify the connection with a ping - callers should do that if needed.
func NewClient(opts ClientOptions, logger *slog.Logger) (*Client, error) {
	// Ensure host includes the native protocol port (default 9000) if not specified.
	host := opts.Host
	if !strings.Contains(host, ":") {
		host += ":9000"
	}

	options := &clickhouse.Options{
		Addr: []string{host},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.Username,
			Password: opts.Password,
		},
		Settings: clickhouse.Settings{
			// Default settings.
			"max_execution_time": 60,
		},
		DialTimeout: 10 * time.Second,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		Protocol: clickhouse.Native,
	}

	// Apply any additional user-provided settings.
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

	client := &Client{
		conn:       conn,
		logger:     logger,
		queryHooks: []QueryHook{}, // Initialize hooks slice.
		opts:       options,
		sourceID:   opts.SourceID,
		source:     opts.Source,
	}

	// Apply a default hook for basic query logging.
	client.AddQueryHook(NewLogQueryHook(logger, false)) // Verbose logging disabled by default.

	// Add metrics hook if source is provided
	if opts.Source != nil {
		client.AddQueryHook(metrics.NewMetricsQueryHook(opts.Source))
		client.metrics = metrics.NewClickHouseMetrics(opts.Source)
	}

	return client, nil
}

// AddQueryHook registers a hook to be executed before and after queries run by this client.
func (c *Client) AddQueryHook(hook QueryHook) {
	c.queryHooks = append(c.queryHooks, hook)
}

// executeQueryWithHooks wraps the execution of a query function (`fn`)
// with the registered BeforeQuery and AfterQuery hooks.
func (c *Client) executeQueryWithHooks(ctx context.Context, query string, fn func(context.Context) error) error {
	var err error
	start := time.Now()

	// Execute BeforeQuery hooks.
	for _, hook := range c.queryHooks {
		ctx, err = hook.BeforeQuery(ctx, query)
		if err != nil {
			// If a hook fails, log and return the error immediately.
			c.logger.Error("query hook BeforeQuery failed", "hook", fmt.Sprintf("%T", hook), "error", err)
			return fmt.Errorf("BeforeQuery hook failed: %w", err)
		}
	}

	// Execute the actual query function.
	err = fn(ctx) // This might be conn.Query, conn.Exec, etc.
	duration := time.Since(start)

	// Execute AfterQuery hooks, regardless of query success/failure.
	for _, hook := range c.queryHooks {
		// Hooks should ideally handle logging internally if needed.
		hook.AfterQuery(ctx, query, err, duration)
	}

	return err // Return the error from the query function itself.
}

// Query executes a SELECT query, processes the results, and applies query hooks.
// It automatically handles DDL statements by calling execDDL.
// The params argument is now unused but kept for potential future structured query building.
func (c *Client) Query(ctx context.Context, query string /* params LogQueryParams - Removed */) (*models.QueryResult, error) {
	return c.QueryWithTimeout(ctx, query, nil)
}

// QueryWithTimeout executes a SELECT query with a timeout setting.
// The timeoutSeconds parameter is required and will always be applied.
func (c *Client) QueryWithTimeout(ctx context.Context, query string, timeoutSeconds *int) (*models.QueryResult, error) {
	start := time.Now()          // Used for calculating total duration including hook overhead.
	queryStartTime := time.Now() // Separate timer for actual DB execution
	var queryDuration time.Duration

	// Start query metrics tracking
	var queryHelper *metrics.QueryMetricsHelper
	if c.metrics != nil {
		queryType := metrics.DetermineQueryType(query)
		queryHelper = c.metrics.StartQuery(queryType, nil) // User context not available in client
	}

	// Ensure timeout is provided (should always be the case now)
	if timeoutSeconds == nil {
		defaultTimeout := DefaultQueryTimeout
		timeoutSeconds = &defaultTimeout
	}

	defer func() {
		c.logger.Debug("query processing complete",
			"duration_ms", time.Since(start).Milliseconds(),
			"query_length", len(query),
			"timeout_seconds", *timeoutSeconds,
		)
	}()

	// Delegate DDL statements (CREATE, ALTER, DROP, etc.) to execDDL.
	if isDDLStatement(query) {
		return c.execDDLWithTimeout(ctx, query, timeoutSeconds)
	}

	var rows driver.Rows
	var resultData []map[string]interface{}
	var columnsInfo []models.ColumnInfo

	// Execute the core query logic within the hook wrapper.
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		var queryErr error
		queryStartTime = time.Now() // Reset timer before execution

		// Always apply timeout setting
		hookCtx = clickhouse.Context(hookCtx, clickhouse.WithSettings(clickhouse.Settings{
			"max_execution_time": *timeoutSeconds,
		}))
		c.logger.Debug("applying query timeout", "timeout_seconds", *timeoutSeconds)

		rows, queryErr = c.conn.Query(hookCtx, query)
		if queryErr != nil {
			return queryErr
		}

		// Close rows when we're done processing them
		defer func() {
			if rows != nil {
				rows.Close()
			}
		}()

		// Get column names and types.
		columnTypes := rows.ColumnTypes()
		columnsInfo = make([]models.ColumnInfo, len(columnTypes)) // Use new name
		scanDest := make([]interface{}, len(columnTypes))         // Prepare scan destinations.
		for i, ct := range columnTypes {
			columnsInfo[i] = models.ColumnInfo{
				Name: ct.Name(),
				Type: ct.DatabaseTypeName(),
			}
			// Use reflection to create pointers of the correct underlying type for Scan.
			scanDest[i] = reflect.New(ct.ScanType()).Interface()
		}

		// Process rows.
		resultData = make([]map[string]interface{}, 0) // Initialize slice.
		for rows.Next() {
			if err := rows.Scan(scanDest...); err != nil {
				return fmt.Errorf("scanning row: %w", err)
			}

			rowMap := make(map[string]interface{})
			for i, col := range columnsInfo { // Use new name
				// Dereference the pointer to get the actual scanned value.
				rowMap[col.Name] = reflect.ValueOf(scanDest[i]).Elem().Interface()
			}
			resultData = append(resultData, rowMap)
		}
		queryDuration = time.Since(queryStartTime) // Capture DB execution duration

		// Check for errors during row iteration.
		return rows.Err()
	})

	// Complete metrics tracking
	if queryHelper != nil {
		success := err == nil
		rowsReturned := int64(-1)
		if success && resultData != nil {
			rowsReturned = int64(len(resultData))
		}
		errorType := metrics.DetermineErrorType(err)
		timedOut := false // TODO: better timeout detection
		queryHelper.Finish(success, rowsReturned, errorType, timedOut)
	}

	// Handle errors from either query execution or row processing.
	if err != nil {
		return nil, fmt.Errorf("executing query or processing results: %w", err)
	}

	// Construct the final result.
	queryResult := &models.QueryResult{
		Logs:    resultData,
		Columns: columnsInfo,
		Stats: models.QueryStats{
			RowsRead:        len(resultData), // Use length of returned data as approximation
			BytesRead:       0,               // Cannot reliably get BytesRead currently
			ExecutionTimeMs: float64(queryDuration.Milliseconds()),
		},
	}

	return queryResult, nil
}

// execDDLWithTimeout executes a DDL statement with a timeout setting.
// The timeoutSeconds parameter is required and will always be applied.
func (c *Client) execDDLWithTimeout(ctx context.Context, query string, timeoutSeconds *int) (*models.QueryResult, error) {
	start := time.Now()

	// Ensure timeout is provided (should always be the case now)
	if timeoutSeconds == nil {
		defaultTimeout := DefaultQueryTimeout
		timeoutSeconds = &defaultTimeout
	}

	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		// Always apply timeout setting
		hookCtx = clickhouse.Context(hookCtx, clickhouse.WithSettings(clickhouse.Settings{
			"max_execution_time": *timeoutSeconds,
		}))
		c.logger.Debug("applying DDL query timeout", "timeout_seconds", *timeoutSeconds)

		return c.conn.Exec(hookCtx, query)
	})

	if err != nil {
		return nil, fmt.Errorf("executing DDL query: %w", err)
	}

	// Return empty result for DDL statements.
	return &models.QueryResult{
		Logs:    []map[string]interface{}{},
		Columns: []models.ColumnInfo{},
		Stats: models.QueryStats{
			RowsRead:        0,
			ExecutionTimeMs: float64(time.Since(start).Milliseconds()),
		},
	}, nil
}

// isDDLStatement checks if a query string likely represents a DDL statement.
func isDDLStatement(query string) bool {
	// Simple prefix check after trimming and uppercasing.
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	ddlPrefixes := []string{"CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME"}
	for _, prefix := range ddlPrefixes {
		if strings.HasPrefix(upperQuery, prefix) {
			return true
		}
	}
	return false
}

// Close terminates the underlying database connection with a timeout.
func (c *Client) Close() error {
	c.logger.Debug("closing clickhouse connection")

	// Create a timeout context for the close operation
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Create a channel to signal completion
	done := make(chan error, 1)

	// Close the connection in a goroutine
	go func() {
		done <- c.conn.Close()
	}()

	// Wait for close to complete or timeout
	select {
	case err := <-done:
		// Connection closed normally
		return err
	case <-ctx.Done():
		// Timeout occurred
		c.logger.Warn("timeout while closing clickhouse connection, abandoning")
		return fmt.Errorf("timeout while closing connection")
	}
}

// Reconnect attempts to re-establish the connection to the ClickHouse server.
// This is useful for recovering from connection failures during health checks.
func (c *Client) Reconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	success := false
	defer func() {
		if c.metrics != nil {
			c.metrics.RecordReconnection(success)
			c.metrics.UpdateConnectionStatus(success)
		}
	}()

	// Only attempt reconnect if connection exists but is failing
	if c.conn != nil {
		// Try to close the existing connection first with a timeout
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()

		closeComplete := make(chan struct{})
		go func() {
			_ = c.conn.Close() // Ignore close errors
			close(closeComplete)
		}()

		// Wait for close to complete or timeout
		select {
		case <-closeComplete:
			// Successfully closed
			c.logger.Debug("successfully closed old connection for reconnect")
		case <-closeCtx.Done():
			// Timeout occurred
			c.logger.Warn("timeout closing old connection for reconnect, proceeding anyway")
		}
	}

	// Use stored connection options
	if c.opts == nil {
		return fmt.Errorf("missing connection options for reconnect")
	}

	// Create a new connection with the same settings
	newConn, err := clickhouse.Open(c.opts)
	if err != nil {
		return fmt.Errorf("reconnecting to clickhouse: %w", err)
	}

	// Test the new connection with a short timeout
	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pingCancel()

	if err := newConn.Ping(pingCtx); err != nil {
		// Clean up failed connection with timeout
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer closeCancel()

		go func() {
			_ = newConn.Close() // Clean up failed connection
			close(make(chan struct{}))
		}()

		// Just wait for timeout - we don't care about the result
		<-closeCtx.Done()

		return fmt.Errorf("ping after reconnect failed: %w", err)
	}

	// Replace the connection
	c.conn = newConn
	success = true
	c.logger.Debug("reconnected to clickhouse")
	return nil
}

// GetTableInfo retrieves detailed metadata about a table, including handling
// for Distributed tables by inspecting the underlying local table.
func (c *Client) GetTableInfo(ctx context.Context, database, table string) (*TableInfo, error) {
	start := time.Now()
	defer func() {
		c.logger.Debug("table info query completed",
			"duration_ms", time.Since(start).Milliseconds(),
			"database", database,
			"table", table,
		)
	}()

	// First, get the base info (engine, columns, create statement) for the specified table.
	baseInfo, err := c.getBaseTableInfo(ctx, database, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get base table info: %w", err)
	}

	// If it's a Distributed engine table, fetch metadata from the underlying local table.
	if baseInfo.Engine == "Distributed" && len(baseInfo.EngineParams) >= 3 {
		return c.handleDistributedTable(ctx, baseInfo), nil
	}

	// If it's a MergeTree family table, attempt to get sorting keys.
	if strings.Contains(baseInfo.Engine, "MergeTree") {
		sortKeys, err := c.getSortKeys(ctx, database, table)
		if err != nil {
			// Log failure but don't fail the entire operation.
			c.logger.Warn("failed to get sort keys", "error", err, "database", database, "table", table)
		} else {
			baseInfo.SortKeys = sortKeys
		}
	}

	return baseInfo, nil
}

// getBaseTableInfo retrieves the fundamental metadata for a table from system tables.
func (c *Client) getBaseTableInfo(ctx context.Context, database, table string) (*TableInfo, error) {
	engine, params, createQuery, err := c.getTableEngine(ctx, database, table)
	if err != nil {
		return nil, err // Error getting engine details is fatal here.
	}

	columns, err := c.getColumns(ctx, database, table)
	if err != nil {
		return nil, err // Error getting basic columns is fatal here.
	}

	// Extended column info is optional; log errors but don't fail.
	// Try to get extended columns, but handle version compatibility gracefully.
	extColumns, err := c.getExtendedColumns(ctx, database, table)
	if err != nil {
		c.logger.Warn("failed to get extended column info",
			"error", err,
			"database", database,
			"table", table,
		)
		// Set to nil to indicate extended columns are not available
		extColumns = nil
	}

	return &TableInfo{
		Database:     database,
		Name:         table,
		Engine:       engine,
		EngineParams: params,
		CreateQuery:  createQuery,
		Columns:      columns,
		ExtColumns:   extColumns,
		// SortKeys added later if applicable.
	}, nil
}

// getExtendedColumns retrieves detailed column metadata from system.columns.
// This function handles version compatibility by checking available columns.
func (c *Client) getExtendedColumns(ctx context.Context, database, table string) ([]ExtendedColumnInfo, error) {
	// Use a simpler query that works across more ClickHouse versions
	// The is_nullable column is not available in all versions
	query := `
		SELECT
			name, type,
			is_in_primary_key,
			default_expression,
			comment
		FROM system.columns
		WHERE database = ? AND table = ?
		ORDER BY position
	`
	var rows driver.Rows
	var err error

	// Use hook wrapper for consistency, though less critical for metadata queries.
	err = c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		rows, err = c.conn.Query(hookCtx, query, database, table)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("failed to query extended columns: %w", err)
	}
	defer rows.Close()

	var columns []ExtendedColumnInfo
	for rows.Next() {
		var col ExtendedColumnInfo
		err := rows.Scan(
			&col.Name, &col.Type,
			&col.IsPrimaryKey,
			&col.DefaultExpression,
			&col.Comment,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan extended column: %w", err)
		}
		// Determine nullability from the type string since is_nullable column may not be available
		col.IsNullable = strings.HasPrefix(col.Type, "Nullable(")
		columns = append(columns, col)
	}
	return columns, rows.Err() // Return any error encountered during iteration.
}

// getTableEngine retrieves the table engine, full engine string, and CREATE statement.
func (c *Client) getTableEngine(ctx context.Context, database, table string) (engine string, engineParams []string, createQuery string, err error) {
	query := `
		SELECT engine, engine_full, create_table_query
		FROM system.tables
		WHERE database = ? AND name = ?
	`
	var rows driver.Rows

	err = c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		rows, err = c.conn.Query(hookCtx, query, database, table)
		return err
	})

	if err != nil {
		return "", nil, "", fmt.Errorf("failed to query table engine: %w", err)
	}
	defer rows.Close()

	var engineFull string
	if rows.Next() {
		if scanErr := rows.Scan(&engine, &engineFull, &createQuery); scanErr != nil {
			return "", nil, "", fmt.Errorf("failed to scan table engine: %w", scanErr)
		}
	} else {
		return "", nil, "", fmt.Errorf("table %s.%s not found in system.tables", database, table)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return "", nil, "", fmt.Errorf("error iterating table engine results: %w", rowsErr)
	}

	if strings.HasPrefix(engine, "Distributed") {
		engineParams = parseEngineParams(engineFull)
	}
	return engine, engineParams, createQuery, nil
}

// getColumns retrieves basic column name and type information.
func (c *Client) getColumns(ctx context.Context, database, table string) ([]models.ColumnInfo, error) {
	query := `
		SELECT name, type
		FROM system.columns
		WHERE database = ? AND table = ?
		ORDER BY position
	`
	var rows driver.Rows
	var err error

	err = c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		rows, err = c.conn.Query(hookCtx, query, database, table)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	var columns []models.ColumnInfo
	for rows.Next() {
		var col models.ColumnInfo
		if err := rows.Scan(&col.Name, &col.Type); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

// getSortKeys retrieves the sorting key expression for MergeTree family tables.
func (c *Client) getSortKeys(ctx context.Context, database, table string) ([]string, error) {
	// This query assumes the table engine is MergeTree compatible.
	query := `
		SELECT sorting_key
		FROM system.tables
		WHERE database = ? AND name = ?
	`
	var rows driver.Rows
	var err error

	err = c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		rows, err = c.conn.Query(hookCtx, query, database, table)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("failed to query sort keys: %w", err)
	}
	defer rows.Close()

	var sortKeys string
	if rows.Next() {
		if err := rows.Scan(&sortKeys); err != nil {
			return nil, fmt.Errorf("failed to scan sort keys: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sort key results: %w", err)
	}

	// Parse the potentially complex sorting_key string into individual column names.
	return parseSortKeys(sortKeys), nil
}

// handleDistributedTable fetches metadata from the underlying local table
// referenced by a Distributed table engine.
func (c *Client) handleDistributedTable(ctx context.Context, base *TableInfo) *TableInfo {
	if len(base.EngineParams) < 3 {
		c.logger.Warn("distributed table has insufficient engine parameters", "params", base.EngineParams)
		return base
	}

	// Extract cluster, local database, and local table names from engine parameters.
	cluster := base.EngineParams[0]
	localDB := base.EngineParams[1]
	localTable := base.EngineParams[2]

	c.logger.Debug("resolving distributed table metadata",
		"distributed_table", fmt.Sprintf("%s.%s", base.Database, base.Name),
		"cluster", cluster,
		"local_db", localDB,
		"local_table", localTable,
	)

	// Recursively get info for the underlying local table.
	underlyingInfo, err := c.GetTableInfo(ctx, localDB, localTable)
	if err != nil {
		// If fetching underlying info fails, log a warning and return the original distributed table info.
		c.logger.Warn("failed to get underlying table info for distributed table",
			"error", err,
			"cluster", cluster,
			"local_db", localDB,
			"local_table", localTable,
		)
		return base
	}

	// Construct the final TableInfo, merging distributed table identity
	// with the structure (columns, sort keys) of the underlying local table.
	return &TableInfo{
		Database:     base.Database,     // Keep original DB name.
		Name:         base.Name,         // Keep original table name.
		Engine:       base.Engine,       // Keep "Distributed" engine type.
		EngineParams: base.EngineParams, // Keep distributed engine parameters.
		CreateQuery:  base.CreateQuery,  // Keep distributed CREATE statement.
		Columns:      underlyingInfo.Columns,
		ExtColumns:   underlyingInfo.ExtColumns,
		SortKeys:     underlyingInfo.SortKeys,
	}
}

// parseEngineParams extracts parameters from engine constructor string.
func parseEngineParams(engineFull string) []string {
	start := strings.Index(engineFull, "(")
	if start == -1 {
		return nil
	}

	end := findMatchingParen(engineFull, start)
	if end == -1 || start >= end {
		return nil
	}

	return splitEngineParams(engineFull[start+1 : end])
}

func findMatchingParen(s string, start int) int {
	parenCount := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(':
			parenCount++
		case ')':
			parenCount--
			if parenCount == 0 {
				return i
			}
		}
	}
	return -1
}

func splitEngineParams(paramsStr string) []string {
	params := make([]string, 0)
	var currentParam strings.Builder
	inQuote := false
	nestedLevel := 0

	for _, char := range paramsStr {
		switch {
		case char == '\'':
			inQuote = !inQuote
			currentParam.WriteRune(char)
		case char == '(' && !inQuote:
			nestedLevel++
			currentParam.WriteRune(char)
		case char == ')' && !inQuote:
			nestedLevel--
			currentParam.WriteRune(char)
		case char == ',' && !inQuote && nestedLevel == 0:
			params = append(params, stripQuotes(strings.TrimSpace(currentParam.String())))
			currentParam.Reset()
		default:
			currentParam.WriteRune(char)
		}
	}

	if currentParam.Len() > 0 {
		params = append(params, stripQuotes(strings.TrimSpace(currentParam.String())))
	}

	return params
}

func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseSortKeys attempts to extract individual column names from the sorting_key string.
// It handles simple cases and tuple() but might fail on complex expressions.
func parseSortKeys(sortingKey string) []string {
	if sortingKey == "" {
		return nil
	}

	// Basic handling: remove tuple() if present.
	trimmedKey := strings.TrimSpace(sortingKey)
	if strings.HasPrefix(trimmedKey, "tuple(") && strings.HasSuffix(trimmedKey, ")") {
		trimmedKey = trimmedKey[6 : len(trimmedKey)-1]
	} else if strings.HasPrefix(trimmedKey, "(") && strings.HasSuffix(trimmedKey, ")") {
		// Handle cases like ORDER BY (col1, col2)
		trimmedKey = trimmedKey[1 : len(trimmedKey)-1]
	}

	// Split by comma, then trim spaces and quotes.
	// This won't handle commas inside function calls correctly.
	rawKeys := strings.Split(trimmedKey, ",")
	keys := make([]string, 0, len(rawKeys))
	for _, key := range rawKeys {
		trimmed := strings.TrimSpace(key)
		// Further strip potential backticks or quotes if needed, though identifiers
		// usually don't contain them after parsing functions like tuple().
		// Basic identifier extraction:
		re := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`) // Simple identifier regex
		match := re.FindString(trimmed)
		if match != "" && !isKeyword(match) { // Check if it's not a keyword
			keys = append(keys, match)
		}
	}

	return keys
}

// isKeyword checks if a string is a common ClickHouse keyword
// to avoid misinterpreting them as column names in sort keys.
func isKeyword(s string) bool {
	// Case-insensitive check.
	lowerS := strings.ToLower(s)
	// Add more keywords if needed based on common sort key expressions.
	keywords := map[string]bool{
		"tuple": true, "array": true, "map": true,
		"as": true, "by": true, "in": true, "is": true,
		"not": true, "null": true, "or": true, "and": true,
		// Potentially date/time functions if used without args:
		// "now": true, "today": true,
	}
	return keywords[lowerS]
}

// FieldValueInfo represents a distinct value with its count for a field.
type FieldValueInfo struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// FieldValuesResult holds the distinct values for a field along with metadata.
type FieldValuesResult struct {
	FieldName     string           `json:"field_name"`
	FieldType     string           `json:"field_type"`
	IsLowCard     bool             `json:"is_low_cardinality"`
	Values        []FieldValueInfo `json:"values"`
	TotalDistinct int64            `json:"total_distinct"`
}

// FieldValuesParams holds parameters for fetching field distinct values.
type FieldValuesParams struct {
	FieldName      string
	FieldType      string
	TimestampField string    // Required: timestamp column name for time range filter
	StartTime      time.Time // Required: start of time range
	EndTime        time.Time // Required: end of time range
	Timezone       string    // Optional: timezone for time conversion (defaults to UTC)
	Limit          int       // Optional: max values to return (default 10, max 100)
	Timeout        *int      // Optional: query timeout in seconds
	LogchefQL      string    // Optional: LogchefQL query string - parsed on backend for proper SQL generation
}

// buildLogchefQLConditionsSQL parses a LogchefQL query and returns the SQL WHERE clause fragment.
// This uses the proper LogchefQL parser which handles nested fields, Map columns, JSON extraction, etc.
// Returns empty string if query is empty or invalid.
func buildLogchefQLConditionsSQL(query string) string {
	if query == "" || strings.TrimSpace(query) == "" {
		return ""
	}

	result := logchefql.Translate(query, nil)
	if !result.Valid || result.SQL == "" {
		return ""
	}

	// Return the SQL wrapped as " AND (...)" to be appended to WHERE clause
	return " AND (" + result.SQL + ")"
}

// GetFieldDistinctValues retrieves the top N distinct values for a field within a time range.
func (c *Client) GetFieldDistinctValues(ctx context.Context, database, table string, params FieldValuesParams) (*FieldValuesResult, error) {
	limit, timeoutSeconds, timezone := normalizeFieldValuesParams(params)

	c.logger.Debug("fetching distinct values for field",
		"database", database, "table", table, "field", params.FieldName,
		"field_type", params.FieldType, "limit", limit)

	isLowCard := strings.Contains(params.FieldType, "LowCardinality")
	startTimeStr := params.StartTime.UTC().Format("2006-01-02 15:04:05")
	endTimeStr := params.EndTime.UTC().Format("2006-01-02 15:04:05")
	additionalConditions := buildLogchefQLConditionsSQL(params.LogchefQL)

	query := fmt.Sprintf(`
		SELECT %s AS value, count() AS cnt
		FROM %s.%s
		PREWHERE %s BETWEEN toDateTime('%s', '%s') AND toDateTime('%s', '%s')
		WHERE %s != ''%s
		GROUP BY value ORDER BY cnt DESC LIMIT %d
	`, params.FieldName, database, table,
		params.TimestampField, startTimeStr, timezone, endTimeStr, timezone,
		params.FieldName, additionalConditions, limit)

	result, err := c.QueryWithTimeout(ctx, query, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to query distinct values for %s: %w", params.FieldName, err)
	}

	values := extractFieldValues(result)

	totalDistinct := c.queryTotalDistinct(ctx, database, table, params, startTimeStr, endTimeStr, timezone, additionalConditions, timeoutSeconds)

	return &FieldValuesResult{
		FieldName:     params.FieldName,
		FieldType:     params.FieldType,
		IsLowCard:     isLowCard,
		Values:        values,
		TotalDistinct: totalDistinct,
	}, nil
}

func normalizeFieldValuesParams(params FieldValuesParams) (limit int, timeout *int, timezone string) {
	limit = params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	timeout = params.Timeout
	if timeout == nil {
		defaultTimeout := 10
		timeout = &defaultTimeout
	}

	timezone = params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	return
}

func extractFieldValues(result *models.QueryResult) []FieldValueInfo {
	values := make([]FieldValueInfo, 0, len(result.Logs))
	for _, row := range result.Logs {
		val, ok := extractStringFromRow(row, "value")
		if !ok || val == "" {
			continue
		}

		count, ok := extractInt64FromRow(row, "cnt")
		if !ok {
			continue
		}

		values = append(values, FieldValueInfo{Value: val, Count: count})
	}
	return values
}

func extractStringFromRow(row map[string]any, key string) (string, bool) {
	rawVal, exists := row[key]
	if !exists {
		return "", false
	}

	switch v := rawVal.(type) {
	case string:
		return v, true
	case *string:
		if v == nil {
			return "", false
		}
		return *v, true
	case []byte:
		return string(v), true
	case *[]byte:
		if v == nil {
			return "", false
		}
		return string(*v), true
	case nil:
		return "", false
	default:
		return fmt.Sprintf("%v", v), true
	}
}

func extractInt64FromRow(row map[string]any, key string) (int64, bool) {
	rawVal, exists := row[key]
	if !exists {
		return 0, false
	}

	switch v := rawVal.(type) {
	case uint64:
		// #nosec G115 -- count values from DB are bounded by actual row counts
		return int64(min(v, uint64(math.MaxInt64))), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

func (c *Client) queryTotalDistinct(ctx context.Context, database, table string, params FieldValuesParams, startTimeStr, endTimeStr, timezone, additionalConditions string, timeoutSeconds *int) int64 {
	query := fmt.Sprintf(`
		SELECT uniq(%s) AS total
		FROM %s.%s
		PREWHERE %s BETWEEN toDateTime('%s', '%s') AND toDateTime('%s', '%s')
		WHERE %s != ''%s
	`, params.FieldName, database, table,
		params.TimestampField, startTimeStr, timezone, endTimeStr, timezone,
		params.FieldName, additionalConditions)

	result, err := c.QueryWithTimeout(ctx, query, timeoutSeconds)
	if err != nil || len(result.Logs) == 0 {
		return 0
	}

	if total, ok := result.Logs[0]["total"]; ok {
		switch v := total.(type) {
		case uint64:
			// #nosec G115 -- distinct count values are bounded by actual row counts
			return int64(min(v, uint64(math.MaxInt64)))
		case int64:
			return v
		}
	}
	return 0
}

// AllFieldValuesParams holds parameters for fetching field values for filterable columns.
type AllFieldValuesParams struct {
	TimestampField string    // Required: timestamp column name for time range filter
	StartTime      time.Time // Required: start of time range
	EndTime        time.Time // Required: end of time range
	Timezone       string    // Optional: timezone for time conversion (defaults to UTC)
	Limit          int       // Optional: max values per field (default 10, max 100)
	Timeout        *int      // Optional: query timeout in seconds (default 5s for String fields)
	LogchefQL      string    // Optional: LogchefQL query string - parsed on backend for proper SQL generation
}

// isFilterableColumnType returns true if the column type is suitable for distinct value queries.
// LowCardinality fields are always fast. String fields are included with timeout protection.
func isFilterableColumnType(colType string) bool {
	// Exclude complex types that can't be compared to empty string
	// Map, Array, Tuple, JSON types are not suitable for simple distinct value queries
	lowerType := strings.ToLower(colType)
	if strings.HasPrefix(lowerType, "map(") ||
		strings.HasPrefix(lowerType, "array(") ||
		strings.HasPrefix(lowerType, "tuple(") ||
		lowerType == "json" ||
		strings.HasPrefix(lowerType, "json(") {
		return false
	}

	// LowCardinality fields - always fast due to dictionary
	if strings.Contains(colType, "LowCardinality") {
		return true
	}

	// Regular String fields - may be slow for high cardinality, but we use timeout
	if colType == "String" || colType == "Nullable(String)" {
		return true
	}

	// Enum types - always fast, finite set of values
	if strings.HasPrefix(colType, "Enum") {
		return true
	}

	return false
}

// GetAllFilterableFieldValues retrieves distinct values for all filterable fields within a time range.
// Filterable fields include: LowCardinality, String, Nullable(String), and Enum types.
// This is useful for populating a field sidebar with filterable values.
// For String fields, a shorter timeout is used to gracefully handle high cardinality columns.
// IMPORTANT: Time range is required to avoid scanning entire tables.
func (c *Client) GetAllFilterableFieldValues(ctx context.Context, database, table string, params AllFieldValuesParams) (map[string]*FieldValuesResult, error) {
	// Reuse existing getColumns function to get column metadata
	columns, err := c.getColumns(ctx, database, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	results := make(map[string]*FieldValuesResult)

	// Default timeout for String fields (shorter to fail fast on high cardinality)
	stringFieldTimeout := 5
	lowCardTimeout := 10

	for _, col := range columns {
		// Check if context was cancelled (e.g., client disconnected or request timed out)
		// This allows early termination when the caller no longer needs the results
		if ctx.Err() != nil {
			c.logger.Debug("context cancelled, stopping field value queries",
				"error", ctx.Err(),
				"fields_completed", len(results))
			break
		}

		// Check if this column type is suitable for distinct value queries
		if !isFilterableColumnType(col.Type) {
			continue
		}

		// Use shorter timeout for regular String fields (may be high cardinality)
		timeout := params.Timeout
		if timeout == nil {
			if strings.Contains(col.Type, "LowCardinality") {
				timeout = &lowCardTimeout
			} else {
				timeout = &stringFieldTimeout
			}
		}

		// Build params for individual field query
		fieldParams := FieldValuesParams{
			FieldName:      col.Name,
			FieldType:      col.Type,
			TimestampField: params.TimestampField,
			StartTime:      params.StartTime,
			EndTime:        params.EndTime,
			Timezone:       params.Timezone,
			Limit:          params.Limit,
			Timeout:        timeout,
			LogchefQL:      params.LogchefQL, // Pass through user's LogchefQL query
		}

		fieldResult, err := c.GetFieldDistinctValues(ctx, database, table, fieldParams)
		if err != nil {
			// Log but don't fail - this field just won't have values shown
			// Common for high cardinality String fields that timeout
			c.logger.Debug("skipping field values (likely timeout or high cardinality)",
				"field", col.Name,
				"type", col.Type,
				"error", err)
			continue
		}
		results[col.Name] = fieldResult
	}

	return results, nil
}

// GetAllLowCardinalityFieldValues is deprecated, use GetAllFilterableFieldValues instead.
// Kept for backwards compatibility.
func (c *Client) GetAllLowCardinalityFieldValues(ctx context.Context, database, table string, params AllFieldValuesParams) (map[string]*FieldValuesResult, error) {
	return c.GetAllFilterableFieldValues(ctx, database, table, params)
}

// Ping checks the connectivity to the ClickHouse server and optionally verifies a table exists.
// It uses short timeouts internally. Returns nil on success, or an error indicating the failure reason.
func (c *Client) Ping(ctx context.Context, database, table string) error {
	if c.conn == nil {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(false)
		}
		return errors.New("clickhouse connection is nil")
	}

	// 1. Check basic connection with a short timeout.
	pingCtx, pingCancel := context.WithTimeout(ctx, 1*time.Second)
	defer pingCancel()

	if err := c.conn.Ping(pingCtx); err != nil {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(false)
			c.metrics.UpdateConnectionStatus(false)
		}

		// Check if the error is due to the context deadline exceeding
		if errors.Is(err, context.DeadlineExceeded) {
			c.logger.Debug("ping timed out after 1 second")
			return fmt.Errorf("ping timed out: %w", err)
		}
		return fmt.Errorf("ping failed: %w", err)
	}

	// 2. If database and table are provided, check table existence.
	if database == "" || table == "" {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(true)
			c.metrics.UpdateConnectionStatus(true)
		}
		return nil // Basic ping successful, no table check needed.
	}

	tableCtx, tableCancel := context.WithTimeout(ctx, 1*time.Second)
	defer tableCancel()

	// Query system.tables to check if the table exists. Using QueryRow and Scan.
	// If the table doesn't exist, QueryRow will return an error (sql.ErrNoRows or similar).
	query := `SELECT 1 FROM system.tables WHERE database = ? AND name = ? LIMIT 1`
	// Use uint8 as the target type for scanning SELECT 1, as recommended by the driver error.
	var exists uint8

	// No need for executeQueryWithHooks here, it's a simple metadata check.
	err := c.conn.QueryRow(tableCtx, query, database, table).Scan(&exists)
	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordConnectionValidation(false)
			c.metrics.UpdateConnectionStatus(false)
		}

		if errors.Is(err, context.DeadlineExceeded) {
			c.logger.Debug("table check timed out", "database", database, "table", table, "timeout", "1s")
			return fmt.Errorf("table check timed out for %s.%s: %w", database, table, err)
		}
		// Check specifically for sql.ErrNoRows which indicates the table doesn't exist.
		// The clickhouse-go driver might wrap this, so checking the string might be necessary
		// if errors.Is(err, sql.ErrNoRows) doesn't work reliably across versions.
		// For now, we rely on the error message in the log.
		if strings.Contains(err.Error(), "no rows in result set") {
			c.logger.Debug("table not found in system.tables", "database", database, "table", table)
			return fmt.Errorf("table '%s.%s' not found: %w", database, table, err)
		} else {
			// Log other scan/query errors.
			c.logger.Debug("table existence check query failed", "database", database, "table", table, "error", err)
			return fmt.Errorf("checking table '%s.%s' failed: %w", database, table, err)
		}
	}

	// If Scan succeeds without error, the table exists.
	if c.metrics != nil {
		c.metrics.RecordConnectionValidation(true)
		c.metrics.UpdateConnectionStatus(true)
	}
	return nil
}
