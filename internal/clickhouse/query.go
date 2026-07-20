package clickhouse

// Query execution: SELECT/DDL execution, streaming, row scanning helpers, and
// timeout classification.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"reflect"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// QueryOptions controls ClickHouse execution and LogChef-side result handling.
type QueryOptions struct {
	TimeoutSeconds   *int
	Settings         map[string]any
	LimitApplied     int
	MaxRows          int
	MaxResponseBytes int
	Warnings         []models.QueryWarning
}

// RowStreamWriter receives rows as they are read from ClickHouse.
type RowStreamWriter interface {
	Begin(columns []models.ColumnInfo) error
	WriteRow(row map[string]any) error
	Finish(stats models.QueryStats) error
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
	return c.QueryWithOptions(ctx, query, QueryOptions{TimeoutSeconds: timeoutSeconds})
}

// QueryWithOptions executes a SELECT query and buffers a bounded result for
// browser preview style responses.
func (c *Client) QueryWithOptions(ctx context.Context, query string, opts QueryOptions) (*models.QueryResult, error) {
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
	if opts.TimeoutSeconds == nil {
		defaultTimeout := DefaultQueryTimeout
		opts.TimeoutSeconds = &defaultTimeout
	}

	// Bound the Go context by the timeout too — max_execution_time only limits
	// ClickHouse-side execution, not a stalled network read or driver hang.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*opts.TimeoutSeconds)*time.Second+queryTimeoutGrace)
	defer cancel()

	defer func() {
		c.logger.Debug("query processing complete",
			"duration_ms", time.Since(start).Milliseconds(),
			"query", query,
			"timeout_seconds", *opts.TimeoutSeconds,
		)
	}()

	// Delegate DDL statements (CREATE, ALTER, DROP, etc.) to execDDL.
	if isDDLStatement(query) {
		return c.execDDLWithTimeout(ctx, query, opts.TimeoutSeconds)
	}

	var rows driver.Rows
	var resultData []map[string]any
	var columnsInfo []models.ColumnInfo
	var bytesReturned int
	truncatedReason := ""

	// Execute the core query logic within the hook wrapper.
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		var queryErr error
		queryStartTime = time.Now() // Reset timer before execution

		hookCtx = c.contextWithQuerySettings(hookCtx, opts)

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

		var scanDest []any
		var scanPtrs []reflect.Value
		// Assign (not :=) so the outer columnsInfo makes it into the result —
		// a := here would shadow it and the response would carry no columns.
		columnsInfo, scanDest, scanPtrs = prepareRowScan(rows)

		// Preallocate to the applied row bound (capped) to avoid repeated slice
		// regrowth on large result sets, without over-committing on huge limits.
		resultData = make([]map[string]any, 0, boundedRowCap(opts))
		for rows.Next() {
			if opts.MaxRows > 0 && len(resultData) >= opts.MaxRows {
				truncatedReason = "row_limit"
				break
			}

			if err := rows.Scan(scanDest...); err != nil {
				return fmt.Errorf("scanning row: %w", err)
			}

			rowMap := scanRowMap(scanPtrs, columnsInfo)
			if opts.MaxResponseBytes > 0 {
				// Approximate size for the soft byte budget instead of marshaling
				// every row (the full result is JSON-encoded once for the response).
				rowSize := approxJSONSize(rowMap)
				if bytesReturned+rowSize > opts.MaxResponseBytes {
					truncatedReason = "byte_limit"
					break
				}
				bytesReturned += rowSize
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
		timedOut := isTimeoutError(err)
		queryHelper.Finish(success, rowsReturned, errorType, timedOut)
	}

	// Handle errors from either query execution or row processing.
	if err != nil {
		return nil, fmt.Errorf("executing query or processing results: %w", err)
	}

	// Construct the final result.
	queryResult := &models.QueryResult{
		Logs:     resultData,
		Columns:  columnsInfo,
		Warnings: opts.Warnings,
		Stats: models.QueryStats{
			RowsRead:        len(resultData), // Use length of returned data as approximation
			BytesRead:       0,               // Cannot reliably get BytesRead currently
			RowsReturned:    len(resultData),
			BytesReturned:   bytesReturned,
			LimitApplied:    opts.LimitApplied,
			Truncated:       truncatedReason != "",
			TruncatedReason: truncatedReason,
			ExecutionTimeMs: float64(queryDuration.Milliseconds()),
		},
	}

	return queryResult, nil
}

// QueryStream executes a SELECT query and streams rows into writer without
// retaining the full result set in memory.
func (c *Client) QueryStream(ctx context.Context, query string, opts QueryOptions, writer RowStreamWriter) (models.QueryStats, error) {
	start := time.Now()
	if opts.TimeoutSeconds == nil {
		defaultTimeout := DefaultQueryTimeout
		opts.TimeoutSeconds = &defaultTimeout
	}

	// Bound the Go context by the timeout (backstop for network/driver stalls
	// beyond ClickHouse's max_execution_time). Safe to cancel on return: rows
	// are fully consumed within this call.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*opts.TimeoutSeconds)*time.Second+queryTimeoutGrace)
	defer cancel()

	if isDDLStatement(query) {
		return models.QueryStats{}, fmt.Errorf("streaming DDL statements is not supported")
	}

	var stats models.QueryStats
	var rowsReturned int
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		hookCtx = c.contextWithQuerySettings(hookCtx, opts)

		rows, err := c.conn.Query(hookCtx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		columnsInfo, scanDest, scanPtrs := prepareRowScan(rows)
		if err := writer.Begin(columnsInfo); err != nil {
			return err
		}

		for rows.Next() {
			if opts.MaxRows > 0 && rowsReturned >= opts.MaxRows {
				stats.Truncated = true
				stats.TruncatedReason = "row_limit"
				break
			}

			if err := rows.Scan(scanDest...); err != nil {
				return fmt.Errorf("scanning row: %w", err)
			}
			rowMap := scanRowMap(scanPtrs, columnsInfo)
			if err := writer.WriteRow(rowMap); err != nil {
				return err
			}
			rowsReturned++
		}
		if err := rows.Err(); err != nil {
			return err
		}

		stats.RowsRead = rowsReturned
		stats.RowsReturned = rowsReturned
		stats.LimitApplied = opts.LimitApplied
		stats.ExecutionTimeMs = float64(time.Since(start).Milliseconds())
		return writer.Finish(stats)
	})
	if err != nil {
		return stats, fmt.Errorf("streaming query results: %w", err)
	}

	stats.ExecutionTimeMs = float64(time.Since(start).Milliseconds())
	return stats, nil
}

func (c *Client) contextWithQuerySettings(ctx context.Context, opts QueryOptions) context.Context {
	settings := buildQuerySettings(*opts.TimeoutSeconds, opts.Settings, c.querySettings)
	return clickhouse.Context(ctx, clickhouse.WithSettings(settings))
}

// buildQuerySettings merges, in increasing precedence: the request timeout,
// LogChef's per-query settings (perQuery), and the per-source operator settings
// (source). Source settings are applied last so per-source caps, timeouts, and
// read-only mode override LogChef's automatic defaults. Only settings present in
// each map are applied.
func buildQuerySettings(timeoutSeconds int, perQuery, source clickhouse.Settings) clickhouse.Settings {
	settings := clickhouse.Settings{
		"max_execution_time": timeoutSeconds,
	}
	maps.Copy(settings, perQuery)
	maps.Copy(settings, source)
	return settings
}

// ClickHouse exception codes (see clickhouse-go's lib/proto.Exception.Code)
// that indicate the query was aborted due to a timeout rather than some
// other server-side failure.
const (
	chExceptionTimeoutExceeded int32 = 159 // TIMEOUT_EXCEEDED: max_execution_time exceeded server-side.
	chExceptionSocketTimeout   int32 = 209 // SOCKET_TIMEOUT: the connection's socket timed out mid-query.
)

// isTimeoutError reports whether err represents a query timeout, so the
// query metrics can distinguish timeouts from other kinds of failures. It
// checks, in order:
//   - the Go context deadline (queryTimeoutGrace backstop) expiring, surfaced
//     as context.DeadlineExceeded anywhere in the error chain;
//   - ClickHouse itself reporting the query was aborted for taking too long
//     (*clickhouse.Exception with code 159 TIMEOUT_EXCEEDED or 209
//     SOCKET_TIMEOUT);
//   - the underlying net.Conn reporting a read/write timeout.
//
// A plain context.Canceled (e.g. the caller/request going away) is
// deliberately not treated as a timeout — that's a cancellation, not a
// deadline being hit.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var exception *clickhouse.Exception
	if errors.As(err, &exception) {
		switch exception.Code {
		case chExceptionTimeoutExceeded, chExceptionSocketTimeout:
			return true
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	return false
}

// boundedRowCap returns a preallocation hint for the result slice: the applied
// limit / MaxRows, capped so a huge configured limit doesn't over-commit memory
// for what may be a small result set.
func boundedRowCap(opts QueryOptions) int {
	hint := opts.LimitApplied
	if opts.MaxRows > 0 && (hint <= 0 || opts.MaxRows < hint) {
		hint = opts.MaxRows
	}
	if hint < 0 {
		return 0
	}
	if hint > 4096 {
		return 4096
	}
	return hint
}

// prepareRowScan returns column metadata, the []any scan targets for rows.Scan,
// and the addressable reflect.Values backing them (kept so scanRowMap can deref
// without a fresh reflect.ValueOf per cell per row). All three are allocated
// once per query and reused across every row.
func prepareRowScan(rows driver.Rows) (columns []models.ColumnInfo, dests []any, ptrs []reflect.Value) {
	columnTypes := rows.ColumnTypes()
	columnsInfo := make([]models.ColumnInfo, len(columnTypes))
	scanDest := make([]any, len(columnTypes))
	ptrValues := make([]reflect.Value, len(columnTypes))
	for i, ct := range columnTypes {
		columnsInfo[i] = models.ColumnInfo{
			Name: ct.Name(),
			Type: ct.DatabaseTypeName(),
		}
		p := reflect.New(ct.ScanType()) // *T, never nil
		ptrValues[i] = p
		scanDest[i] = p.Interface()
	}
	return columnsInfo, scanDest, ptrValues
}

func scanRowMap(ptrs []reflect.Value, columnsInfo []models.ColumnInfo) map[string]any {
	rowMap := make(map[string]any, len(columnsInfo))
	for i, col := range columnsInfo {
		// ptrs[i] is the *T from reflect.New (always non-nil), so Elem() is valid;
		// Interface() yields the scanned value exactly as before.
		rowMap[col.Name] = ptrs[i].Elem().Interface()
	}
	return rowMap
}

// approxJSONSize returns a fast approximation of a scanned row's JSON-encoded
// size, used only for the soft response-byte budget. Scalars (the overwhelming
// majority of log columns) are estimated arithmetically; only non-scalar values
// fall back to json.Marshal — avoiding a full per-row marshal on the scan path.
func approxJSONSize(row map[string]any) int {
	size := 2 // {}
	for k, v := range row {
		size += len(k) + 4 // "k": plus separators
		size += approxValueSize(v)
	}
	return size
}

// jsonStringSize returns the JSON-encoded byte size of s (including surrounding
// quotes) without allocating, accounting for escaping so the response byte
// budget can't be materially under-counted by escape-heavy payloads. It counts
// conservatively (>= the real encoded size for standard/HTML-escaping encoders).
func jsonStringSize(s string) int {
	n := 2 // surrounding quotes
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"', c == '\\', c == '\n', c == '\r', c == '\t', c == '\b', c == '\f':
			n += 2 // short escape, e.g. \n
		case c < 0x20, c == '<', c == '>', c == '&':
			n += 6 // \u00XX (control) or HTML-escaped form
		default:
			n++
		}
	}
	return n
}

func approxValueSize(v any) int {
	switch val := v.(type) {
	case nil:
		return 4 // null
	case string:
		return jsonStringSize(val)
	case []byte:
		return jsonStringSize(string(val))
	case bool:
		return 5
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return 20
	case float32, float64:
		return 24
	case time.Time:
		return 32
	default:
		if b, err := json.Marshal(v); err == nil {
			return len(b)
		}
		return 16
	}
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
		Logs:    []map[string]any{},
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
