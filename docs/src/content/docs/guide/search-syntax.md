---
title: Search Syntax
description: Learn how to use LogChef's simple yet powerful search syntax
---

LogChef provides a simple yet powerful search syntax that makes it easy to find exactly what you're looking for in your logs.

## Basic Syntax

The basic syntax follows a simple key-value pattern:

```
key="value"
```

For example:

```
level="error"
service="payment-api"
```

## Operators

LogChef supports the following operators:

| Operator | Description      | Example             |
| -------- | ---------------- | ------------------- |
| `=`      | Equals           | `status=200`        |
| `!=`     | Not equals       | `level!="debug"`    |
| `~`      | Contains         | `message~"timeout"` |
| `!~`     | Does not contain | `path!~"health"`    |

## Combining Conditions

You can combine multiple conditions using `AND` and `OR` operators:

```
# Find errors in payment service
level="error" AND service="payment-api"

# Find successful or redirected responses
status=200 OR status=301

# Complex combinations
(service="auth" OR service="users") AND level="error"
```

## Examples

### Finding Errors

```
level="error"
```

### HTTP Status Codes

```
status=500
status!=200
```

### Service-specific Logs

```
service="payment-api" AND level="error"
```

### Partial Text Search

```
# Find logs containing "timeout"
message~"timeout"

# Find paths not containing "internal"
path!~"internal"
```

## Under the Hood

When you use the search syntax, LogChef converts it to optimized Clickhouse SQL queries:

- The `~` and `!~` operators use Clickhouse's `positionCaseInsensitive` function for partial matches
- A default time range and limit is automatically applied
- Results are ordered by timestamp in descending order

For example, this search:

```
level="error" AND service="payment-api"
```

Gets converted to:

```sql
SELECT *
FROM logs.app
WHERE level = 'error'
  AND service = 'payment-api'
  AND _timestamp BETWEEN toDateTime('2025-04-07 14:20:42') AND toDateTime('2025-04-07 14:25:42')
ORDER BY _timestamp DESC
LIMIT 100
```

## Coming Soon

- **JSON Operations**: Support for querying nested JSON fields
- **Advanced Operators**: Additional comparison operators for numeric fields
- **Field Suggestions**: Autocomplete for field names and common values
