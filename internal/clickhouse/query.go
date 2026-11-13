package clickhouse

// Query execution: SELECT/DDL execution, streaming, row scanning helpers, and
// timeout classification.

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/big"
	"net"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/chcol"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/shopspring/decimal"
)

// QueryOptions controls ClickHouse execution and LogChef-side result handling.
type QueryOptions struct {
	TimeoutSeconds   *int
	Settings         map[string]any
	LimitApplied     int
	MaxRows          int
	MaxResponseBytes int
	Warnings         []models.QueryWarning
	LimitSource      models.LimitSource
	// Probed reports that the query asked for one row past MaxRows, so a row
	// beyond the cap proves the result was cut.
	Probed bool
}

// truncation is the state of a result that stopped for reason after rows
// rows. A result that fills the cap without a probe may be incomplete, unless
// the statement's own LIMIT set the cap: then the author asked for that many.
func (opts QueryOptions) truncation(rows int, reason string, end queryEnd) models.Truncation {
	switch {
	case reason == "byte_limit":
		return models.TruncationBytes
	case reason == "row_limit":
		return models.TruncationRows
	case end.mayHaveBroken():
		return models.TruncationUnknown
	case !opts.Probed && opts.MaxRows > 0 && rows >= opts.MaxRows && opts.LimitSource != models.LimitSourceStatement:
		return models.TruncationUnknown
	}
	return models.TruncationNone
}

// queryEnd is what a query that ran to its end reported.
type queryEnd struct {
	// settings are the server's defaults overlaid with the settings the query sent.
	settings map[string]any
	// resultBytes sums the server's result profile, when profiled says one arrived.
	resultBytes uint64
	profiled    bool
	rowsRead    uint64
	bytesRead   uint64
	elapsed     time.Duration
}

// breakLimits are the limits at which ClickHouse ends a query without an
// error when their overflow mode is break. reached reports whether a query
// could have hit the limit; a counter below it proves the limit did not stop
// the query.
var breakLimits = []struct {
	limit, mode string
	reached     func(end queryEnd, limit uint64) bool
}{
	// The result profile counts allocated bytes, never fewer than max_result_bytes counts.
	{"max_result_bytes", "result_overflow_mode", func(end queryEnd, limit uint64) bool { return !end.profiled || end.resultBytes >= limit }},
	{"max_rows_to_read", "read_overflow_mode", func(end queryEnd, limit uint64) bool { return end.rowsRead >= limit }},
	{"max_bytes_to_read", "read_overflow_mode", func(end queryEnd, limit uint64) bool { return end.bytesRead >= limit }},
	// The server starts its clock after the client sends the query.
	{"max_execution_time", "timeout_overflow_mode", func(end queryEnd, limit uint64) bool { return end.elapsed.Seconds() >= float64(limit) }},
}

// mayHaveBroken reports whether a break limit could have ended the query, so
// a short result cannot prove that it is complete.
func (end queryEnd) mayHaveBroken() bool {
	for _, bound := range breakLimits {
		if end.settings[bound.mode] != "break" {
			continue
		}
		limit, err := strconv.ParseUint(fmt.Sprint(end.settings[bound.limit]), 10, 64)
		if err == nil && limit > 0 && bound.reached(end, limit) {
			return true
		}
	}
	return false
}

// overlaySettings applies the settings a query sends over the server's
// defaults, the order in which ClickHouse applies them.
func overlaySettings(defaults map[string]any, sent clickhouse.Settings) map[string]any {
	settings := maps.Clone(defaults)
	if settings == nil {
		settings = make(map[string]any, len(sent))
	}
	maps.Copy(settings, sent)
	return settings
}

// RowStreamWriter receives rows as they are read from ClickHouse.
type RowStreamWriter interface {
	Begin(columns []models.ColumnInfo) error
	WriteRow(row map[string]any) error
	Finish(stats models.QueryStats) error
}

type queryMetricsResultKey struct{}

type queryMetricsResult struct {
	rowsReturned int64
}

func recordRowsReturned(ctx context.Context, rows int) {
	if result, ok := ctx.Value(queryMetricsResultKey{}).(*queryMetricsResult); ok {
		result.rowsReturned = int64(rows)
	}
}

// Progress packets contain increments, not cumulative totals. The driver may
// deliver them, and the result profile, from its reader goroutine while the
// caller processes result rows. For canceled or truncated queries these are
// only the increments received.
type queryProgress struct {
	mu          sync.Mutex
	rows        uint64
	bytes       uint64
	resultBytes uint64
	profiled    bool
}

func (p *queryProgress) add(delta *clickhouse.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rows += min(delta.Rows, math.MaxUint64-p.rows)
	p.bytes += min(delta.Bytes, math.MaxUint64-p.bytes)
}

func (p *queryProgress) profile(info *clickhouse.ProfileInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resultBytes += min(info.Bytes, math.MaxUint64-p.resultBytes)
	p.profiled = true
}

func (p *queryProgress) end(settings map[string]any, elapsed time.Duration) queryEnd {
	p.mu.Lock()
	defer p.mu.Unlock()
	return queryEnd{settings: settings, resultBytes: p.resultBytes, profiled: p.profiled, rowsRead: p.rows, bytesRead: p.bytes, elapsed: elapsed}
}

func (p *queryProgress) apply(stats *models.QueryStats) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// QueryStats uses int. Saturate oversized counters rather than wrapping
	// them into negative or smaller scan totals.
	stats.RowsRead = math.MaxInt
	if p.rows <= math.MaxInt {
		stats.RowsRead = int(p.rows)
	}
	stats.BytesRead = math.MaxInt
	if p.bytes <= math.MaxInt {
		stats.BytesRead = int(p.bytes)
	}
}

// Query executes a SELECT query, processes the results, and applies query hooks.
// It automatically handles DDL statements by calling execDDL.
func (c *Client) Query(ctx context.Context, query string) (*models.QueryResult, error) {
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

	// Ensure timeout is provided (should always be the case now)
	if opts.TimeoutSeconds == nil {
		defaultTimeout := DefaultQueryTimeout
		opts.TimeoutSeconds = &defaultTimeout
	}

	opts.Settings = buildQuerySettings(*opts.TimeoutSeconds, opts.Settings, c.querySettings)

	// Bound the Go context by the timeout too — max_execution_time only limits
	// ClickHouse-side execution, not a stalled network read or driver hang.
	requestCtx := ctx
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
	var progress queryProgress

	// Execute the core query logic within the hook wrapper.
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		var queryErr error
		queryStartTime = time.Now() // Reset timer before execution

		hookCtx = c.contextWithQuerySettings(hookCtx, opts)

		rows, queryErr = c.queryResultRows(hookCtx, query, opts, &progress)
		if queryErr != nil {
			return queryErr
		}

		// Close rows when we're done processing them
		defer func() {
			if rows != nil {
				cancel()
				rows.Close()
			}
		}()

		var scanDest []any
		var scanPtrs []reflect.Value
		// JSON's scan type is known only after decoding the first data block.
		hasRow := rows.Next()
		// Assign (not :=) so the outer columnsInfo makes it into the result —
		// a := here would shadow it and the response would carry no columns.
		columnsInfo, scanDest, scanPtrs = prepareRowScan(rows)

		// Preallocate to the applied row bound (capped) to avoid repeated slice
		// regrowth on large result sets, without over-committing on huge limits.
		resultData = make([]map[string]any, 0, boundedRowCap(opts))
		for ; hasRow; hasRow = rows.Next() {
			if opts.MaxRows > 0 && len(resultData) >= opts.MaxRows {
				truncatedReason = "row_limit"
				break
			}

			resetNullableScanTargets(scanPtrs)
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
		recordRowsReturned(hookCtx, len(resultData))
		return rows.Err()
	})

	// Handle errors from either query execution or row processing.
	if err != nil {
		return nil, fmt.Errorf("executing query or processing results: %w", err)
	}
	settings := overlaySettings(c.serverDefaults(requestCtx), opts.Settings)

	// Construct the final result.
	queryResult := &models.QueryResult{
		Logs:     resultData,
		Columns:  columnsInfo,
		Warnings: opts.Warnings,
		Stats: models.QueryStats{
			RowsReturned:    len(resultData),
			BytesReturned:   bytesReturned,
			LimitApplied:    opts.LimitApplied,
			Truncated:       truncatedReason != "",
			TruncatedReason: truncatedReason,
			LimitSource:     opts.LimitSource,
			Truncation:      opts.truncation(len(resultData), truncatedReason, progress.end(settings, queryDuration)),
			ExecutionTimeMs: float64(queryDuration.Milliseconds()),
		},
	}
	progress.apply(&queryResult.Stats)

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

	opts.Settings = buildQuerySettings(*opts.TimeoutSeconds, opts.Settings, c.querySettings)

	// Bound the Go context by the timeout (backstop for network/driver stalls
	// beyond ClickHouse's max_execution_time). Safe to cancel on return: rows
	// are fully consumed within this call.
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*opts.TimeoutSeconds)*time.Second+queryTimeoutGrace)
	defer cancel()

	if isDDLStatement(query) {
		return models.QueryStats{}, &ValidationError{Message: "only SELECT queries are supported"}
	}

	var stats models.QueryStats
	var rowsReturned int
	var progress queryProgress
	err := c.executeQueryWithHooks(ctx, query, func(hookCtx context.Context) error {
		hookCtx = c.contextWithQuerySettings(hookCtx, opts)

		rows, err := c.queryResultRows(hookCtx, query, opts, &progress)
		if err != nil {
			return err
		}
		defer func() {
			cancel()
			rows.Close()
		}()

		hasRow := rows.Next()
		if err := rows.Err(); err != nil {
			return err
		}
		columnsInfo, scanDest, scanPtrs := prepareRowScan(rows)
		if err := writer.Begin(columnsInfo); err != nil {
			return err
		}

		for ; hasRow; hasRow = rows.Next() {
			if opts.MaxRows > 0 && rowsReturned >= opts.MaxRows {
				stats.Truncated = true
				stats.TruncatedReason = "row_limit"
				break
			}

			resetNullableScanTargets(scanPtrs)
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

		// Stop the reader before taking the final progress snapshot, including
		// when the client-side row budget stopped iteration early.
		cancel()
		rows.Close()
		progress.apply(&stats)
		recordRowsReturned(hookCtx, rowsReturned)
		stats.RowsReturned = rowsReturned
		stats.LimitApplied = opts.LimitApplied
		stats.LimitSource = opts.LimitSource
		elapsed := time.Since(start)
		settings := overlaySettings(c.serverDefaults(requestCtx), opts.Settings)
		stats.Truncation = opts.truncation(rowsReturned, stats.TruncatedReason, progress.end(settings, elapsed))
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

// ownQuery derives the context of a query that Logchef runs for itself. It
// keeps the caller's deadline and credentials, and leaves out the caller's
// query ID and driver callbacks.
func ownQuery(ctx context.Context, settings clickhouse.Settings) context.Context {
	return clickhouse.Context(ctx,
		clickhouse.WithSettings(settings),
		clickhouse.WithQueryID(""),
		clickhouse.WithLogs(nil),
		clickhouse.WithProgress(nil),
		clickhouse.WithProfileInfo(nil),
		clickhouse.WithProfileEvents(nil),
	)
}

// Detect support once per client; older servers must never receive this setting.
func (c *Client) contextWithNativeJSON(ctx context.Context, opts QueryOptions) context.Context {
	c.capabilitiesMu.Lock()
	flattenedJSON := c.flattenedJSON
	c.capabilitiesMu.Unlock()
	if flattenedJSON == nil {
		var supported uint64
		probeCtx := ownQuery(ctx, clickhouse.Settings{"max_execution_time": 2})
		err := c.conn.QueryRow(probeCtx, "SELECT count() FROM system.settings WHERE name = 'output_format_native_use_flattened_dynamic_and_json_serialization'").Scan(&supported)
		if err != nil {
			return ctx
		}
		enabled := supported > 0
		flattenedJSON = &enabled
		c.capabilitiesMu.Lock()
		c.flattenedJSON = flattenedJSON
		c.capabilitiesMu.Unlock()
	}
	if *flattenedJSON {
		settings := buildQuerySettings(*opts.TimeoutSeconds, opts.Settings, c.querySettings)
		settings["output_format_native_use_flattened_dynamic_and_json_serialization"] = 1
		ctx = clickhouse.Context(ctx, clickhouse.WithSettings(settings))
	}
	return ctx
}

// serverDefaultsUnseen are the settings that the read of the server's
// defaults sends itself: result_overflow_mode=break keeps a profile's result
// cap from failing the read, and the driver sends max_execution_time from the
// context deadline. The read cannot see their defaults, and every query that
// the LIMIT rule builds sends both.
var serverDefaultsUnseen = []string{"max_execution_time", "result_overflow_mode"}

// serverDefaultsQuery reads the break limits that a query gets from the
// user's settings profile and the connection unless it sends them.
var serverDefaultsQuery = func() string {
	var names []string
	for _, bound := range breakLimits {
		for _, name := range []string{bound.limit, bound.mode} {
			if !slices.Contains(serverDefaultsUnseen, name) && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return "SELECT name, value FROM system.settings WHERE changed AND name IN ('" + strings.Join(names, "', '") + "')"
}()

// serverDefaults returns the break limits that this client's queries get from
// the server unless they send them, read once per client after a query
// succeeds. A failed read is logged once and tried again after the next
// query; until it succeeds, results judge completeness by the settings they
// send.
func (c *Client) serverDefaults(ctx context.Context) map[string]any {
	c.capabilitiesMu.Lock()
	defaults := c.defaults
	c.capabilitiesMu.Unlock()
	if defaults != nil {
		return defaults
	}

	defaults, err := c.readServerDefaults(ctx)
	c.capabilitiesMu.Lock()
	defer c.capabilitiesMu.Unlock()
	if err != nil {
		if !c.defaultsFailed && ctx.Err() == nil {
			c.defaultsFailed = true
			c.logger.Warn("reading the ClickHouse profile limits failed; results report only the limits Logchef sends",
				"source_id", c.sourceID, "error", err)
		}
		return nil
	}
	c.defaults = defaults
	return defaults
}

func (c *Client) readServerDefaults(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := c.conn.Query(ownQuery(ctx, clickhouse.Settings{"result_overflow_mode": "break"}), serverDefaultsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	defaults := make(map[string]any)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		defaults[name] = value
	}
	return defaults, rows.Err()
}

func resetNullableScanTargets(ptrs []reflect.Value) {
	for _, ptr := range ptrs {
		if ptr.Elem().Kind() == reflect.Pointer {
			ptr.Elem().SetZero()
		}
	}
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

// IsQueryTimeoutError reports query deadlines separately from transport timeouts.
func IsQueryTimeoutError(err error) bool {
	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok {
		return exception.Code == chExceptionTimeoutExceeded || exception.Code == chExceptionSocketTimeout
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return false
	}
	// Client.Timeout also wraps DeadlineExceeded, but has its own transport budget.
	if requestErr, ok := errors.AsType[*url.Error](err); ok {
		return requestErr.Err == context.DeadlineExceeded //nolint:errorlint // Client.Timeout's wrapper must remain a transport failure.
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// IsTimeoutError reports whether a context, ClickHouse, or network error indicates a timeout.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if exception, ok := errors.AsType[*clickhouse.Exception](err); ok {
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
		if strings.HasPrefix(ct.DatabaseTypeName(), "Nullable(JSON") {
			p = reflect.ValueOf(&jsonScanValue{})
		}
		ptrValues[i] = p
		scanDest[i] = p.Interface()
	}
	return columnsInfo, scanDest, ptrValues
}

type jsonScanValue struct{ value any }

func (v *jsonScanValue) DeserializeClickHouseJSON(object *chcol.JSON) error {
	v.value = object.NestedMap()
	return nil
}

func (v *jsonScanValue) Scan(value any) error {
	switch value := value.(type) {
	case nil:
		v.value = nil
	case string:
		v.value = json.RawMessage(value)
	case []byte:
		v.value = json.RawMessage(append([]byte(nil), value...))
	default:
		return fmt.Errorf("unsupported JSON scan value %T", value)
	}
	return nil
}

func scanRowMap(ptrs []reflect.Value, columnsInfo []models.ColumnInfo) map[string]any {
	rowMap := make(map[string]any, len(columnsInfo))
	for i, col := range columnsInfo {
		value := ptrs[i].Elem().Interface()
		if text, ok := value.(string); ok && (col.Type == "JSON" || strings.HasPrefix(col.Type, "JSON(")) {
			value = json.RawMessage(text)
		}
		rowMap[col.Name] = normalizeResultValue(value)
	}
	return rowMap
}

const maxSafeJSONInteger = int64(1<<53 - 1)

// Normalize native driver values to JSON-compatible values before retaining rows.
// Values outside JavaScript's exact integer range become decimal strings. This
// keeps ordinary small counters numeric while preventing browsers from
// rounding identifiers and other wide ClickHouse integers.
func normalizeResultValue(value any) any {
	switch v := value.(type) {
	case jsonScanValue:
		return normalizeResultValue(v.value)
	case json.RawMessage:
		return normalizeRawJSON(v)
	case json.Number:
		return normalizeJSONNumber(v)
	case nil, string, bool:
		return value
	case int:
		return normalizeSignedInteger(int64(v), value)
	case int8:
		return normalizeSignedInteger(int64(v), value)
	case int16:
		return normalizeSignedInteger(int64(v), value)
	case int32:
		return normalizeSignedInteger(int64(v), value)
	case int64:
		return normalizeSignedInteger(v, value)
	case uint:
		return normalizeUnsignedInteger(uint64(v), value)
	case uint8:
		return normalizeUnsignedInteger(uint64(v), value)
	case uint16:
		return normalizeUnsignedInteger(uint64(v), value)
	case uint32:
		return normalizeUnsignedInteger(uint64(v), value)
	case uint64:
		return normalizeUnsignedInteger(v, value)
	case float64:
		return normalizeFloat(v)
	case float32:
		return normalizeFloat(v)
	}
	return normalizeDriverValue(value)
}

func normalizeFloat[T float32 | float64](value T) any {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return nil
	}
	return value
}

func normalizeDriverValue(value any) any {
	switch v := value.(type) {
	case chcol.JSON:
		return normalizeResultValue(v.NestedMap())
	case *chcol.JSON:
		if v == nil {
			return nil
		}
		return normalizeResultValue(v.NestedMap())
	case chcol.Variant:
		return normalizeResultValue(v.Any())
	case big.Int:
		return normalizeBigInteger(&v)
	case *big.Int:
		return normalizeBigInteger(v)
	case decimal.Decimal:
		return v.String()
	case *decimal.Decimal:
		if v == nil {
			return nil
		}
		return v.String()
	case decimal.NullDecimal:
		if !v.Valid {
			return nil
		}
		return v.Decimal.String()
	case *decimal.NullDecimal:
		if v == nil || !v.Valid {
			return nil
		}
		return v.Decimal.String()
	case json.Marshaler, encoding.TextMarshaler:
		return value
	}
	return normalizeReflectedValue(value)
}

func normalizeSignedInteger(value int64, original any) any {
	if value < -maxSafeJSONInteger || value > maxSafeJSONInteger {
		return strconv.FormatInt(value, 10)
	}
	return original
}

func normalizeUnsignedInteger(value uint64, original any) any {
	if value > uint64(maxSafeJSONInteger) {
		return strconv.FormatUint(value, 10)
	}
	return original
}

func normalizeBigInteger(value *big.Int) any {
	if value == nil {
		return nil
	}
	if value.IsInt64() {
		return normalizeSignedInteger(value.Int64(), value.Int64())
	}
	return value.String()
}

// normalizeRawJSON decodes native JSON strings with UseNumber. The default
// encoding/json decoder turns every number into float64, which would already
// lose wide integers before the result reaches the response encoder.
func normalizeRawJSON(raw json.RawMessage) any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return raw
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return raw
	}
	return normalizeResultValue(decoded)
}

func normalizeJSONNumber(value json.Number) any {
	text := value.String()
	if !strings.ContainsAny(text, ".eE") {
		integer, ok := new(big.Int).SetString(text, 10)
		if ok {
			if integer.IsInt64() {
				return normalizeSignedInteger(integer.Int64(), value)
			}
			return integer.String()
		}
	}

	// Keep JSON decimals that round-trip to the same mathematical value when
	// represented as a JavaScript Number. A decimal whose binary conversion
	// changes its value is emitted as a string instead.
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return text
	}
	original, originalOK := new(big.Rat).SetString(text)
	formatted := strconv.FormatFloat(parsed, 'g', -1, 64)
	converted, convertedOK := new(big.Rat).SetString(formatted)
	if !originalOK || !convertedOK || original.Cmp(converted) != 0 {
		return text
	}
	return value
}

func normalizeReflectedValue(value any) any {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return normalizeResultValue(v.Elem().Interface())
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		items := make([]any, v.Len())
		for i := range items {
			items[i] = normalizeResultValue(v.Index(i).Interface())
		}
		return items
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		object := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			object[fmt.Sprint(iter.Key().Interface())] = normalizeResultValue(iter.Value().Interface())
		}
		return object
	default:
		return value
	}
}

// approxJSONSize returns a fast approximation of a scanned row's JSON-encoded
// size, used only for the soft response-byte budget. Scalars (the overwhelming
// majority of log columns) are estimated arithmetically; only non-scalar values
// fall back to json.Marshal — avoiding a full per-row marshal on the scan path.
func approxJSONSize(row map[string]any) int {
	size := 2 // {}
	for k, v := range row {
		size += jsonStringSize(k) + 2 // colon and separator
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
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == '\u2028' || r == '\u2029' || r == utf8.RuneError && size == 1 {
				n += 6
			} else {
				n += size
			}
			i += size - 1
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
		return 37
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
		hookCtx = c.contextWithQuerySettings(hookCtx, QueryOptions{TimeoutSeconds: timeoutSeconds})
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
