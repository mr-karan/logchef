package clickhouse

// Table/schema introspection: table metadata, columns, engine parsing, sort keys.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

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
	columns = withColumnDescriptions(columns, extColumns)

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

func withColumnDescriptions(columns []models.ColumnInfo, extColumns []ExtendedColumnInfo) []models.ColumnInfo {
	if len(columns) == 0 || len(extColumns) == 0 {
		return columns
	}

	commentsByName := make(map[string]string, len(extColumns))
	for _, col := range extColumns {
		if col.Comment != "" {
			commentsByName[col.Name] = col.Comment
		}
	}

	if len(commentsByName) == 0 {
		return columns
	}

	for i := range columns {
		columns[i].Description = commentsByName[columns[i].Name]
	}
	return columns
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
// sortKeyIdentifierRe extracts a leading identifier from a sort-key expression.
var sortKeyIdentifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`)

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
		match := sortKeyIdentifierRe.FindString(trimmed)
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
