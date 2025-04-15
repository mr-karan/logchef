package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Client represents a connection to a ClickHouse database using the native protocol.
// It provides methods for executing queries and retrieving metadata.
type Client struct {
	conn       driver.Conn // Underlying ClickHouse native connection.
	logger     *slog.Logger
	queryHooks []QueryHook // Hooks to execute before/after queries.
}

// ClientOptions holds configuration for establishing a new ClickHouse client connection.
type ClientOptions struct {
	Host     string                 // Hostname or IP address.
	Database string                 // Target database name.
	Username string                 // Username for authentication.
	Password string                 // Password for authentication.
	Settings map[string]interface{} // Additional ClickHouse settings (e.g., max_execution_time).
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
// It takes connection options and a logger, tests the connection, and returns a Client instance.
func NewClient(opts ClientOptions, logger *slog.Logger) (*Client, error) {
	// Ensure host includes the native protocol port (default 9000) if not specified.
	host := opts.Host
	if !strings.Contains(host, ":") {
		host = host + ":9000"
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

	// Verify the connection is active by pinging the server.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		// Attempt to close the potentially problematic connection before returning error.
		_ = conn.Close()
		return nil, fmt.Errorf("pinging clickhouse failed: %w", err)
	}

	client := &Client{
		conn:       conn,
		logger:     logger,
		queryHooks: []QueryHook{}, // Initialize hooks slice.
	}

	// Apply a default hook for basic query logging.
	client.AddQueryHook(NewLogQueryHook(logger, false)) // Verbose logging disabled by default.

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
func (c *Client) Query(ctx context.Context, query string) (*models.QueryResult, error) {
	start := time.Now() // Used for calculating total duration including hook overhead.
	defer func() {
		c.logger.Debug("query processing complete",
			"duration_ms", time.Since(start).Milliseconds(),
			"query_length", len(query),
		)
	}()

	// Delegate DDL statements (CREATE, ALTER, DROP, etc.) to execDDL.
	if isDDLStatement(query) {
		return c.execDDL(ctx, query)
	}

	var rows driver.Rows
	var resultData []map[string]interface{}
	var columns []models.ColumnInfo

	// Execute the core query logic within the hook wrapper.
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		var queryErr error
		rows, queryErr = c.conn.Query(hookCtx, query)
		if queryErr != nil {
			return queryErr // Return error to be logged by AfterQuery hook.
		}
		// Defer rows.Close() inside the function passed to executeQueryWithHooks
		// to ensure it closes even if hook processing fails later.
		defer rows.Close()

		// Get column names and types.
		columnTypes := rows.ColumnTypes()
		columns = make([]models.ColumnInfo, len(columnTypes))
		scanDest := make([]interface{}, len(columnTypes)) // Prepare scan destinations.
		for i, ct := range columnTypes {
			columns[i] = models.ColumnInfo{
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
			for i, col := range columns {
				// Dereference the pointer to get the actual scanned value.
				rowMap[col.Name] = reflect.ValueOf(scanDest[i]).Elem().Interface()
			}
			resultData = append(resultData, rowMap)
		}

		// Check for errors during row iteration.
		return rows.Err()
	})

	// Handle errors from either query execution or row processing.
	if err != nil {
		return nil, fmt.Errorf("executing query or processing results: %w", err)
	}

	// Construct the final result.
	queryResult := &models.QueryResult{
		Logs:    resultData,
		Columns: columns,
		Stats: models.QueryStats{
			RowsRead:        len(resultData),
			ExecutionTimeMs: float64(time.Since(start).Milliseconds()), // Total time including hooks.
		},
	}

	return queryResult, nil
}

// execDDL executes a DDL statement (e.g., CREATE, ALTER, DROP) using hooks.
// It returns an empty QueryResult on success.
func (c *Client) execDDL(ctx context.Context, query string) (*models.QueryResult, error) {
	start := time.Now()
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
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

// CheckHealth performs a simple ping to verify the database connection is alive.
func (c *Client) CheckHealth(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// Close terminates the underlying database connection.
func (c *Client) Close() error {
	c.logger.Info("closing clickhouse client connection")
	return c.conn.Close()
}

// GetTableSchema retrieves comprehensive schema information for a ClickHouse table.
// This is an alias for GetTableInfo.
func (c *Client) GetTableSchema(ctx context.Context, database, table string) (*TableInfo, error) {
	return c.GetTableInfo(ctx, database, table)
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
		return c.handleDistributedTable(ctx, baseInfo)
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
	extColumns, err := c.getExtendedColumns(ctx, database, table)
	if err != nil {
		c.logger.Warn("failed to get extended column info",
			"error", err,
			"database", database,
			"table", table,
		)
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
func (c *Client) getExtendedColumns(ctx context.Context, database, table string) ([]ExtendedColumnInfo, error) {
	query := `
		SELECT
			name, type,
			is_in_primary_key,
			default_expression,
			comment,
			is_nullable
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
			&col.IsNullable,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan extended column: %w", err)
		}
		columns = append(columns, col)
	}
	return columns, rows.Err() // Return any error encountered during iteration.
}

// getTableEngine retrieves the table engine, full engine string, and CREATE statement.
func (c *Client) getTableEngine(ctx context.Context, database, table string) (string, []string, string, error) {
	query := `
		SELECT engine, engine_full, create_table_query
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
		return "", nil, "", fmt.Errorf("failed to query table engine: %w", err)
	}
	defer rows.Close()

	var engine, engineFull, createQuery string
	if rows.Next() {
		if err := rows.Scan(&engine, &engineFull, &createQuery); err != nil {
			return "", nil, "", fmt.Errorf("failed to scan table engine: %w", err)
		}
	} else {
		// If no rows returned, the table likely doesn't exist.
		return "", nil, "", fmt.Errorf("table %s.%s not found in system.tables", database, table)
	}
	if err := rows.Err(); err != nil {
		return "", nil, "", fmt.Errorf("error iterating table engine results: %w", err)
	}

	// Parse engine parameters from the engine_full string.
	params := parseEngineParams(engineFull)
	return engine, params, createQuery, nil
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
func (c *Client) handleDistributedTable(ctx context.Context, base *TableInfo) (*TableInfo, error) {
	if len(base.EngineParams) < 3 {
		// Distributed engine string is malformed or unexpected.
		c.logger.Warn("distributed table has insufficient engine parameters", "params", base.EngineParams)
		return base, nil // Return base info as fallback.
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
		return base, nil
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
	}, nil
}

// parseEngineParams attempts to parse the parameters from a full engine string
// (e.g., "MergeTree() PARTITION BY toYYYYMM(date) ORDER BY (date, id)").
// This is a best-effort parser and might not handle all edge cases perfectly.
func parseEngineParams(engineFull string) []string {
	// Extract content between the first '(' and the last ')'.
	start := strings.Index(engineFull, "(")
	end := strings.LastIndex(engineFull, ")")
	if start == -1 || end == -1 || start >= end {
		return nil // No parameters found or malformed.
	}
	paramsStr := engineFull[start+1 : end]

	params := make([]string, 0)
	var currentParam strings.Builder
	inQuote := false
	nestedLevel := 0 // Handle nested parentheses within parameters.

	for _, char := range paramsStr {
		switch char {
		case '\'':
			inQuote = !inQuote
			currentParam.WriteRune(char)
		case '(':
			if !inQuote {
				nestedLevel++
			}
			currentParam.WriteRune(char)
		case ')':
			if !inQuote {
				nestedLevel--
			}
			currentParam.WriteRune(char)
		case ',':
			// Split only if not inside quotes and not inside nested parentheses.
			if !inQuote && nestedLevel == 0 {
				param := strings.TrimSpace(currentParam.String())
				// Optionally remove surrounding quotes from the parameter itself.
				if len(param) >= 2 && param[0] == '\'' && param[len(param)-1] == '\'' {
					param = param[1 : len(param)-1]
				}
				params = append(params, param)
				currentParam.Reset() // Start next parameter.
			} else {
				currentParam.WriteRune(char)
			}
		default:
			currentParam.WriteRune(char)
		}
	}

	// Add the last parameter accumulated.
	if currentParam.Len() > 0 {
		param := strings.TrimSpace(currentParam.String())
		if len(param) >= 2 && param[0] == '\'' && param[len(param)-1] == '\'' {
			param = param[1 : len(param)-1]
		}
		params = append(params, param)
	}

	return params
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
