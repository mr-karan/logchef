---
title: Query Examples
description: Practical examples for common log analytics scenarios
---

This guide provides practical examples for common log analytics scenarios using LogChef. Each example includes both the LogchefQL syntax and the equivalent SQL query.

## Error Analysis

### Finding All Errors

Find all errors across all services to get an overview of system health.

```
level="error"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Service-specific Errors

Narrow down errors to a specific service when troubleshooting issues in that component.

```
level="error" and service="payment-api"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
  AND service = 'payment-api'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Errors Excluding Debug Noise

Find errors while excluding specific patterns that aren't relevant.

```
level="error" and message!~"health check"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
  AND positionCaseInsensitive(message, 'health check') = 0
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Critical vs Warning Analysis

Compare different severity levels.

```
severity_number>=4 or level="critical"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE severity_number >= 4
   OR level = 'critical'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

## HTTP Logs Analysis

### Server Errors (5xx Status Codes)

Identify all server-side errors to find potential backend issues.

```
status>=500
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE status >= 500
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Slow API Requests

Find API requests that took longer than 1 second to complete, which may indicate performance bottlenecks.

```
request_path~"/api/" and response_time>1000
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE positionCaseInsensitive(request_path, '/api/') > 0
  AND response_time > 1000
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Client Errors for a Specific Endpoint

Find client errors (4xx) for a specific API endpoint to identify potential client integration issues.

```
status>=400 and status<500 and request_path~"/api/payments"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE status >= 400
  AND status < 500
  AND positionCaseInsensitive(request_path, '/api/payments') > 0
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Request Latency Analysis

Find requests within specific latency ranges.

```
# Very slow requests (over 5 seconds)
response_time>5000

# Fast requests (under 100ms)
response_time<100

# Requests in a specific range
response_time>=100 and response_time<=500
```

## Nested Field Queries

LogchefQL supports querying nested fields in Map and JSON columns using dot notation.

### Map Column Access

Query logs by attributes stored in Map columns (common in OpenTelemetry logs).

```
# Filter by user ID in attributes
log_attributes.user_id="user-12345"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE log_attributes['user_id'] = 'user-12345'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Multi-level Nesting

Access deeply nested fields.

```
# Query nested request attributes
log_attributes.http.request.method="POST"

# Query nested error details
log_attributes.error.code="CONNECTION_REFUSED"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE log_attributes['http.request.method'] = 'POST'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Pattern Matching in Nested Fields

Use contains operator on nested values.

```
log_attributes.request.url~"/api/v2/"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE positionCaseInsensitive(log_attributes['request.url'], '/api/v2/') > 0
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### JSON Column Extraction

For JSON or String columns containing JSON, LogchefQL uses `JSONExtractString`.

```
body.request.user_agent~"Mozilla"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE positionCaseInsensitive(JSONExtractString(body, 'request', 'user_agent'), 'Mozilla') > 0
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Quoted Field Names

For field names containing dots or special characters.

```
# Field name literally contains a dot
log_attributes."service.name"="payment-api"

# Mixed quoted and unquoted
log_attributes."nested.key".subfield="value"
```

## Using the Pipe Operator

The pipe operator (`|`) lets you select specific columns instead of `SELECT *`.

### Basic Column Selection

Select only the fields you need.

```
level="error" | timestamp service level message
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT timestamp, service, level, message
FROM logs.app
WHERE level = 'error'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Extracting Nested Values

Pull specific values from nested structures.

```
namespace="prod" | timestamp log_attributes.user_id log_attributes.request_id body
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT 
  timestamp, 
  log_attributes['user_id'] AS log_attributes_user_id, 
  log_attributes['request_id'] AS log_attributes_request_id, 
  body
FROM logs.app
WHERE namespace = 'prod'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Minimal Output for Scanning

When you just need to scan for specific patterns.

```
message~"error" | timestamp message
```

### Service Overview

Get a quick view of service activity.

```
namespace="production" | timestamp service_name level
```

## Security Analysis

### Failed Authentication Attempts

Identify potential brute force attacks by finding multiple failed login attempts.

```
event="login_failed" and ip_address~"192.168."
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE event = 'login_failed'
  AND positionCaseInsensitive(ip_address, '192.168.') > 0
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Suspicious Activity Detection

Find logs that might indicate suspicious activities based on warning messages.

```
level="warn" and (message~"suspicious" or message~"unauthorized")
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE level = 'warn'
  AND (
    positionCaseInsensitive(message, 'suspicious') > 0
    OR positionCaseInsensitive(message, 'unauthorized') > 0
  )
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Access Pattern Analysis

Track access to sensitive endpoints.

```
request_path~"/admin" or request_path~"/api/internal"
```

## System Monitoring

### High Resource Usage

Detect potential resource bottlenecks by finding instances of high CPU or memory usage.

```
type="system_metrics" and (cpu_usage>90 or memory_usage>85)
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE type = 'system_metrics'
  AND (cpu_usage > 90 OR memory_usage > 85)
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Failed Service Health Checks

Monitor service health by finding instances where health checks have failed.

```
event="health_check" and status!="ok"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE event = 'health_check'
  AND status != 'ok'
ORDER BY timestamp DESC
LIMIT 100
```

</details>

### Disk Space Warnings

Identify servers that are running low on disk space and might need attention.

```
type="system_metrics" and disk_free_percent<15
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE type = 'system_metrics'
  AND disk_free_percent < 15
ORDER BY timestamp DESC
LIMIT 100
```

</details>

## Distributed Tracing

### Complete Request Trace

Trace a complete request flow across multiple services using a trace ID.

```
trace_id="abc123def456"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE trace_id = 'abc123def456'
ORDER BY timestamp ASC
LIMIT 1000
```

</details>

### Service Dependency Analysis

Find all the services involved in a specific transaction to understand service dependencies.

```
trace_id="abc123def456" and level="info" and event="service_call"
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT service, remote_service, timestamp
FROM logs.app
WHERE trace_id = 'abc123def456'
  AND level = 'info'
  AND event = 'service_call'
ORDER BY timestamp ASC
LIMIT 100
```

</details>

### Trace with Specific Fields

Get a focused view of a trace with only relevant fields.

```
trace_id="abc123def456" | timestamp service_name span_id body
```

## OpenTelemetry Log Queries

LogchefQL works great with OpenTelemetry log data.

### Filter by Resource Attributes

```
log_attributes.service.name="frontend" and severity_text="ERROR"
```

### Kubernetes Context

```
log_attributes.k8s.namespace.name="production" and log_attributes.k8s.pod.name~"api-"
```

### Span Correlation

```
trace_id!="" and span_id!="" and level="error"
```

## Effective Query Tips

1. **Start Specific, Then Broaden**

   - Begin with specific conditions that target your issue
   - Add or remove filters to adjust the result set size

2. **Use Comparison Operators for Metrics**

   - `response_time>1000` is cleaner than text matching
   - Works well with numeric fields like status codes, durations, counts

3. **Leverage Nested Field Access**

   - Query Map and JSON columns directly: `log_attributes.user_id="123"`
   - No need to flatten your log schema

4. **Use the Pipe Operator for Focus**

   - `level="error" | timestamp service message` reduces noise
   - Faster queries when you don't need all columns

5. **Combine Multiple Conditions**

   - Use `and` to narrow results
   - Use `or` to broaden results
   - Use parentheses for complex conditions: `(condition1 or condition2) and condition3`

6. **Filter by Context First**
   - Start with service, component, or environment
   - Then add conditions for errors, warnings, or specific events
   - Finally, add free-text search terms with the `~` operator

7. **Switch to SQL Mode for Aggregations**
   - LogchefQL is for filtering; use SQL mode for `COUNT`, `GROUP BY`, etc.
