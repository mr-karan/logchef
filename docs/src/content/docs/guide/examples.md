---
title: Query Examples
description: Practical examples for common log analytics scenarios
---

This guide provides practical examples for common log analytics scenarios using LogChef. Each example includes both the simple search syntax and the equivalent SQL query.

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

### Error Spikes in the Last Hour

Find if there's been a sudden increase in errors in the past hour, which might indicate a service degradation.

```
level="error" and timestamp > now() - INTERVAL 1 HOUR
```

<details>
<summary>SQL Equivalent</summary>

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
  AND timestamp > now() - INTERVAL 1 HOUR
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

## Effective Query Tips

1. **Start Specific, Then Broaden**

   - Begin with specific conditions that target your issue
   - Add or remove filters to adjust the result set size

2. **Use Time Windows Effectively**

   - Focus on relevant time periods (e.g., `timestamp > now() - INTERVAL 15 MINUTE`)
   - Compare similar time windows when analyzing patterns

3. **Combine Multiple Conditions**

   - Use `and` to narrow results
   - Use `or` to broaden results
   - Use parentheses for complex conditions: `(condition1 or condition2) and condition3`

4. **Filter by Context First**
   - Start with service, component, or environment
   - Then add conditions for errors, warnings, or specific events
   - Finally, add free-text search terms with the `~` operator
