package models

// FilterOperator represents a filter operation
type FilterOperator string

const (
	FilterOperatorEquals      FilterOperator = "="
	FilterOperatorNotEquals   FilterOperator = "!="
	FilterOperatorContains    FilterOperator = "contains"
	FilterOperatorNotContains FilterOperator = "not_contains"
	FilterOperatorIContains   FilterOperator = "icontains"
	FilterOperatorStartsWith  FilterOperator = "startswith"
	FilterOperatorEndsWith    FilterOperator = "endswith"
	FilterOperatorIn          FilterOperator = "in"
	FilterOperatorNotIn       FilterOperator = "not_in"
	FilterOperatorIsNull      FilterOperator = "is_null"
	FilterOperatorIsNotNull   FilterOperator = "is_not_null"
)

// SortOrder represents sort direction
type SortOrder string

const (
	SortOrderAsc  SortOrder = "ASC"
	SortOrderDesc SortOrder = "DESC"
)

// GroupOperator represents a group operator
type GroupOperator string

const (
	GroupOperatorAnd GroupOperator = "AND"
	GroupOperatorOr  GroupOperator = "OR"
)

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Value    interface{}    `json:"value"`
}

// SortOptions represents sorting configuration
type SortOptions struct {
	Field string    `json:"field"`
	Order SortOrder `json:"order"`
}

// QueryMode represents different query modes
type QueryMode string

const (
	QueryModeFilters   QueryMode = "filters"
	QueryModeRawSQL    QueryMode = "raw_sql"
	QueryModeLogChefQL QueryMode = "logchefql"
)

// ColumnInfo represents column metadata from ClickHouse
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// QueryStats represents statistics about the query execution
type QueryStats struct {
	ExecutionTimeMs float64 `json:"execution_time_ms"`
	RowsRead        int     `json:"rows_read"`
	BytesRead       int     `json:"bytes_read"`
}

// LogQueryRequest represents the request for querying logs
type LogQueryRequest struct {
	StartTimestamp int64             `json:"start_timestamp"`
	EndTimestamp   int64             `json:"end_timestamp"`
	Limit          int               `json:"limit"`
	Conditions     []FilterCondition `json:"conditions,omitempty"`
	Sort           *SortOptions      `json:"sort,omitempty"`
	Mode           QueryMode         `json:"mode,omitempty"`
	RawSQL         string            `json:"raw_sql,omitempty"`
	LogChefQL      string            `json:"logchefql,omitempty"`
}

// LogQueryResponse represents the response for querying logs
type LogQueryResponse struct {
	Data    []map[string]interface{} `json:"data"`
	Stats   QueryStats               `json:"stats"`
	Columns []ColumnInfo             `json:"columns"`
}

// TableSchema represents a table's schema information
type TableSchema struct {
	Columns []ColumnInfo `json:"columns"`
}

const (
	// OTELLogsTableSchema is the schema for OpenTelemetry logs
	OTELLogsTableSchema = `CREATE TABLE IF NOT EXISTS "{{database_name}}"."{{table_name}}"
	(
		timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
		trace_id String CODEC(ZSTD(1)),
		span_id String CODEC(ZSTD(1)),
		trace_flags UInt32 CODEC(ZSTD(1)),
		severity_text LowCardinality(String) CODEC(ZSTD(1)),
		severity_number Int32 CODEC(ZSTD(1)),
		service_name LowCardinality(String) CODEC(ZSTD(1)),
		namespace LowCardinality(String) CODEC(ZSTD(1)),
		body String CODEC(ZSTD(1)),
		log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),

		INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
		INDEX idx_severity_text severity_text TYPE set(100) GRANULARITY 4,
		INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
		INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
		INDEX idx_body body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
	)
	ENGINE = MergeTree()
	PARTITION BY toDate(timestamp)
	ORDER BY (namespace, service_name, timestamp)
	TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY
	SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;`

	// HTTPLogsTableSchema is the schema for HTTP logs table
	HTTPLogsTableSchema = `
	CREATE TABLE IF NOT EXISTS {{database_name}}.{{table_name}} (
		timestamp DateTime,
		remote_addr String,
		request_method String,
		request_uri String,
		status UInt16,
		body_bytes_sent UInt64,
		http_referer String,
		http_user_agent String,
		http_x_forwarded_for String,
		http_host String,
		request_time Float64,
		upstream_response_time Float64,
		upstream_addr String,
		upstream_status UInt16,
		request_id String,
		request_length UInt64,
		request_completion String,
		ssl_protocol String,
		ssl_cipher String,
		scheme String,
		gzip_ratio Float64,
		http_cf_ray String,
		http_cf_connecting_ip String,
		http_true_client_ip String,
		http_cf_ipcountry String,
		http_cf_visitor String,
		http_cdn_loop String,
		http_cf_worker String
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMM(timestamp)
	ORDER BY (timestamp, request_id)
	TTL timestamp + INTERVAL {{ttl_day}} DAY`
)
