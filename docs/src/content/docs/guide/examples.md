---
title: Query Examples
description: Practical examples for common log analytics scenarios
---

This guide provides practical examples for common log analytics scenarios. Each example shows both the simple search syntax and the equivalent SQL query.

## Error Analysis

### Finding Errors

Simple search:

```
level="error"
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
ORDER BY _timestamp DESC
LIMIT 100
```

### Service-specific Errors

Simple search:

```
level="error" AND service="payment-api"
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
  AND service = 'payment-api'
ORDER BY _timestamp DESC
LIMIT 100
```

## HTTP Logs

### Failed Requests

Simple search:

```
type="access_log" AND status>=500
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE type = 'access_log'
  AND status >= 500
ORDER BY _timestamp DESC
LIMIT 100
```

### Slow Requests

Simple search:

```
type="access_log" AND response_time>1000
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE type = 'access_log'
  AND response_time > 1000
ORDER BY _timestamp DESC
LIMIT 100
```

## Security Analysis

### Failed Login Attempts

Simple search:

```
event="login_failed" AND ip_address~"192.168."
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE event = 'login_failed'
  AND positionCaseInsensitive(ip_address, '192.168.') > 0
ORDER BY _timestamp DESC
LIMIT 100
```

### Suspicious Activities

Simple search:

```
level="warn" AND (message~"suspicious" OR message~"unauthorized")
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE level = 'warn'
  AND (
    positionCaseInsensitive(message, 'suspicious') > 0
    OR positionCaseInsensitive(message, 'unauthorized') > 0
  )
ORDER BY _timestamp DESC
LIMIT 100
```

## System Health

### Resource Usage

Simple search:

```
type="system_metrics" AND (cpu_usage>90 OR memory_usage>85)
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE type = 'system_metrics'
  AND (cpu_usage > 90 OR memory_usage > 85)
ORDER BY _timestamp DESC
LIMIT 100
```

### Service Health Checks

Simple search:

```
event="health_check" AND status!="ok"
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE event = 'health_check'
  AND status != 'ok'
ORDER BY _timestamp DESC
LIMIT 100
```

## Debugging

### Request Tracing

Simple search:

```
trace_id="abc123" OR parent_trace_id="abc123"
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE trace_id = 'abc123'
   OR parent_trace_id = 'abc123'
ORDER BY _timestamp DESC
LIMIT 100
```

### Error Context

Simple search:

```
service="payment-api" AND (level="error" OR level="warn")
```

SQL query:

```sql
SELECT *
FROM logs.app
WHERE service = 'payment-api'
  AND level IN ('error', 'warn')
ORDER BY _timestamp DESC
LIMIT 100
```

## Tips for Effective Querying

1. **Use Specific Filters**

   - Start with the most specific conditions
   - Add conditions gradually to narrow down results

2. **Combine Conditions**

   - Use `AND` to narrow down results
   - Use `OR` to broaden the search
   - Use parentheses for complex combinations

3. **Text Search**

   - Use `~` for partial matches
   - Use `!~` to exclude matches
   - Remember searches are case-insensitive

4. **Common Patterns**
   - Filter by `level` for error investigation
   - Filter by `service` for service-specific issues
   - Use `type` to focus on specific log types
