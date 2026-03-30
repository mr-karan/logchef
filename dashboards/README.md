# Logchef Grafana Dashboards

This directory contains the official Grafana dashboards for Logchef monitoring and observability.

## Available Dashboards

### Main Dashboard
- **File**: `logchef-monitoring.json`
- **Description**: Primary Logchef monitoring dashboard with comprehensive metrics
- **Features**:
  - HTTP request rates and latency percentiles
  - ClickHouse connection status monitoring
  - Query performance metrics by source
  - Error tracking and success/failure rates
  - Resource utilization (goroutines, memory, GC)
  - Session operations by user

## Dashboard Structure

The dashboard includes the following key panels:

### HTTP Metrics
- **HTTP Request Rate by Endpoint**: Shows request rates per endpoint with method and status code breakdown
- **HTTP Request Duration Percentiles**: 50th and 95th percentile response times
- **HTTP Error Rate**: Error rates by error type and status code
- **Active HTTP Requests**: Current number of active requests

### ClickHouse Metrics
- **ClickHouse Connection Status**: Health status of ClickHouse connections per source
- **Query Duration by Source**: Query performance percentiles per data source
- **Query Success/Failure Rate**: Success vs failure rates for queries
- **Query Rows Returned**: Number of rows returned by queries
- **Query Errors by Type**: Breakdown of query errors by type and source

### System Metrics
- **Go Goroutines**: Number of active goroutines
- **Memory Heap Allocated**: Current heap memory usage
- **GC CPU Usage**: Garbage collection CPU overhead

### User Activity
- **Session Operations by User**: User activity tracking with operation types

## Usage

### Importing Dashboard
1. Copy the JSON content from `logchef-monitoring.json`
2. In Grafana, go to **Dashboards** > **Import**
3. Paste the JSON content
4. Replace `${PROMETHEUS_DATASOURCE_UID}` with your actual Prometheus datasource UID
5. Configure the Prometheus datasource as needed

### Customization
- Update the datasource UID placeholder to match your Prometheus instance
- Adjust refresh intervals and time ranges as needed
- Modify panel queries based on your specific Logchef deployment

## Metrics Dependencies

The dashboard expects the following Prometheus metrics to be available:

### HTTP Metrics
- `logchef_http_requests_total`
- `logchef_http_request_duration_seconds_bucket`
- `logchef_http_errors_total`
- `logchef_http_active_requests`

### ClickHouse Metrics
- `logchef_clickhouse_connection_status`
- `logchef_query_duration_seconds_bucket`
- `logchef_query_total`
- `logchef_query_rows_returned_bucket`
- `logchef_query_errors_total`
- `logchef_query_timeouts_total`

### System Metrics
- `go_goroutines`
- `go_memstats_heap_alloc_bytes`
- `go_memstats_gc_cpu_fraction`

### Session Metrics
- `logchef_session_operations_total`

## Dashboard Sync

To sync dashboard changes:

1. Export the updated dashboard from Grafana as JSON
2. Replace the content in `logchef-monitoring.json`
3. Commit the changes to version control
4. Use `just dashboards-sync` (if available) to automate the sync process

## Security

The dashboard JSON uses a placeholder `${PROMETHEUS_DATASOURCE_UID}` for the Prometheus datasource. Replace this with your actual datasource UID when importing to Grafana.

## Tags

The dashboard is tagged with:
- `logchef`
- `monitoring`
- `clickhouse`
- `logs`

This helps in dashboard discovery and organization within Grafana.