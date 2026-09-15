# ClickHouse query compatibility

LogChef uses the native `clickhouse-go` API. The driver is pinned to v2.48.0.
Upstream's supported server range starts at 25.8. LogChef also tests its query
paths against 23.7.4.5 and 24.1.2.5 for existing installations. These tests cover
LogChef behavior. They do not extend upstream's support policy.

## Result values

Both buffered queries and streaming queries use the same result normalization:

- Native JSON becomes a nested object. The driver's `chcol.JSON` marshaler has
  a pointer receiver, so serializing a dereferenced scan value loses its data.
- JSON sent in native string mode becomes `json.RawMessage`. Ordinary String
  and Nullable(String) columns remain strings, even if their text contains JSON.
- Dynamic and Variant values expose their underlying values.
- UInt8 arrays become numeric arrays, including nested arrays. Go's default
  encoding would treat them as base64-encoded bytes.
- NaN and infinite floats become null, matching ClickHouse's default JSON
  output policy. Decimal values retain the driver's string representation.
- Maps become JSON objects with string keys. Integer values retain their Go
  precision during encoding. Browser consumers still have JavaScript's numeric
  precision limits.

Scan targets are allocated after the first data block is decoded. JSON's scan
type can differ between the result header and the first data block. Nullable
targets are cleared before each scan because the driver does not clear all
nullable Go pointer types when it reads NULL.

The client detects support for
`output_format_native_use_flattened_dynamic_and_json_serialization`, caches the result, then
enables it for result queries on supported servers. The driver's JSON/Dynamic
decoder needs this format to read shared data. Older servers never receive the
setting. A failed capability query is not cached and does not block ordinary
queries. The probe has its own two-second limit and does not inherit source
row limits. Reconnecting clears the cached capability result.

## Query behavior

- Result limits and writer errors cancel the query before closing its rows.
  Closing rows alone drains the driver's remaining result stream.
- Source settings apply to metadata and DDL as well as result queries. Request
  context deadlines remain an independent upper bound on elapsed time, even
  when a source configures a longer server-side execution limit.
- Field-value queries preserve absolute UTC instants and nanosecond precision.
  Display timezone does not reinterpret the requested time range. Filters use
  the table schema, and invalid filters return an error.
- LogchefQL filters and projections use subcolumns for native JSON. Legacy
  string-backed JSON continues to use JSON extraction functions.
- Daily histograms use calendar-day intervals. Nullable grouping keys use
  tuple-wrapped join keys for compatibility with older servers.
- Histogram projection changes use the existing SQL parser. The client does
  not inject missing columns into DISTINCT, grouped, or set-operation queries.
  Such queries must expose the required columns themselves. SQL syntax that
  the parser cannot understand returns a histogram error.
- Distributed sources retain their declared columns and comments. Local table
  inspection supplies sorting metadata and detects cyclic references.
- Dashboard cache execution errors return without repeating the query. A
  ClickHouse cache-buffer overflow can fall back to uncached streaming.

## Query statistics and schema reuse

Buffered and streaming query responses collect scanned rows and bytes from
ClickHouse progress packets. `rows_returned` counts the result rows separately.
An aggregate can return one row after scanning many rows. An empty result can
also report scan work.

Canceled or truncated queries report only progress received before cancellation.
The driver may not deliver a final progress packet. Zero counters therefore do
not distinguish an empty scan from unavailable progress. JSON capability probes
do not contribute to the result query's scan statistics.
Counters saturate at the platform's maximum integer instead of wrapping on overflow.

LogChef owns the progress callback for result queries. Other driver callbacks
and query IDs remain available. Execution metrics count each query once across
buffered, streaming, and DDL paths.

LogchefQL compilation reuses the source inspection cache when column metadata
is missing. The cache expires after one minute and checks the source revision.
Concurrent requests share a cache fill. Compilation uses a source copy rather
than changing shared source columns. If inspection fails, compilation continues
without schema metadata.

## Histogram limits

Histogram execution shares preview concurrency limits. A shared dashboard cache
fill uses one execution slot. Cache hits do not need an execution slot.

ClickHouse and VictoriaLogs reject results with more than 5,000 distinct time
buckets or an approximate response size above 16 MiB. VictoriaLogs also limits
the raw response body to 16 MiB before decoding it. Grouped results retain the
top ten series plus Other, within the same bucket and response budgets.

An oversized histogram returns a validation error with guidance. It does not
return partial counts or change the requested interval. The bucket budget
counts returned buckets, not empty intervals in a sparse time range. Queries
with time filters inside SQL can still omit a separate request time range.

## Running the tests

The `clickhouse-integration` CI job runs real-server tests on 23.7.4.5,
24.1.2.5, and 26.8.2.7. Native JSON cases run on the modern server. Legacy
strings, nullable values, arrays, maps, decimals, timestamps, field discovery,
histograms, schema inspection, cancellation, and reconnect tests run on all
three versions.

Run the same tests against a disposable local server:

```sh
LOGCHEF_TEST_CLICKHOUSE_ADDR=127.0.0.1:9000 \
  go test -race ./internal/clickhouse ./internal/datasource -count=1
```

The integration suite creates and drops uniquely named tables. Use a test
server. Without the environment variable, real-server cases skip.

Measure scalar row conversion with:

```sh
go test ./internal/clickhouse -run '^$' -bench '^BenchmarkScanRowMap$' -benchmem
```

## Release verification

Before and after deploying a new backend, use the CLI against representative
sources to check server versions, schemas, and small bounded queries. Include:

1. Nullable non-null/NULL transitions.
2. UInt8 arrays and legacy JSON stored as strings.
3. A native JSON column alongside `toJSONString(column)`.
4. A field-value query with a non-UTC display timezone and fractional bounds.
5. A daily histogram with a nullable grouping field.

Local integration tests do not establish that production is running the new
backend. Record the deployed version and repeat the production checks after
rollout.

References:

- [Driver v2.48.0 release](https://github.com/ClickHouse/clickhouse-go/releases/tag/v2.48.0)
- [Driver JSON examples](https://github.com/ClickHouse/clickhouse-go/blob/v2.48.0/examples/clickhouse_api/json_paths.go)
- [ClickHouse JSON type](https://clickhouse.com/docs/sql-reference/data-types/newjson)
