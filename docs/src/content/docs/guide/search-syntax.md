---
title: Search Syntax
description: Learn how to use LogChef's simple yet powerful search syntax
---

LogChef provides a simple yet powerful search syntax called **LogchefQL** that makes it easy to find exactly what you're looking for in your logs.

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

LogchefQL supports a comprehensive set of operators for different use cases:

### Equality Operators

| Operator | Description      | Example             |
| -------- | ---------------- | ------------------- |
| `=`      | Equals           | `status=200`        |
| `!=`     | Not equals       | `level!="debug"`    |

### Pattern Matching Operators

| Operator | Description                    | Example             |
| -------- | ------------------------------ | ------------------- |
| `~`      | Contains (case-insensitive)    | `message~"timeout"` |
| `!~`     | Does not contain               | `path!~"health"`    |

### Comparison Operators

| Operator | Description              | Example                |
| -------- | ------------------------ | ---------------------- |
| `>`      | Greater than             | `status>400`           |
| `<`      | Less than                | `response_time<1000`   |
| `>=`     | Greater than or equal to | `severity_number>=3`   |
| `<=`     | Less than or equal to    | `duration<=5000`       |

## Combining Conditions

You can combine multiple conditions using `and` and `or` operators (case-insensitive):

```
# Find errors in payment service
level="error" and service="payment-api"

# Find successful or redirected responses
status=200 or status=301

# Complex combinations with parentheses
(service="auth" or service="users") and level="error"

# Find slow requests with errors
response_time>1000 and status>=500
```

## Nested Field Access

LogchefQL supports accessing nested fields using **dot notation**. This works seamlessly with:

- **Map columns** (e.g., `Map(String, String)`)
- **JSON columns**
- **String columns containing JSON**

### Dot Notation

Access nested keys using dots:

```
# Access nested field in a Map column
log_attributes.user_id="12345"

# Multi-level nesting
log_attributes.request.method="POST"

# Pattern matching on nested fields
log_attributes.error.message~"connection refused"
```

### Quoted Field Names

For field names containing special characters (like dots), use quotes:

```
# Field name contains a literal dot
log_attributes."user.name"="alice"

# Mixed notation
log_attributes."nested.key".subfield="value"
```

## Pipe Operator (Custom SELECT)

The pipe operator (`|`) allows you to select specific columns instead of the default `SELECT *`. This is useful for:

- Reducing data transfer
- Focusing on relevant fields
- Extracting specific nested values

### Syntax

```
<filter conditions> | <field1> <field2> ...
```

### Examples

```
# Select only service_name from syslog namespace
namespace="syslog" | service_name

# Select multiple fields
namespace="prod" | namespace service_name body

# Select nested fields
level="error" | timestamp log_attributes.request_id body

# Extract specific nested values
service="api" | timestamp log_attributes.user_id log_attributes.endpoint
```

The selected fields appear as columns in the result set, making it easy to focus on the data you need.

## Examples

### Finding Errors

```
level="error"
```

### HTTP Status Codes

```
# Server errors
status>=500

# Client errors
status>=400 and status<500

# Successful requests
status=200
```

### Service-specific Logs

```
service="payment-api" and level="error"
```

### Partial Text Search

```
# Find logs containing "timeout"
message~"timeout"

# Find paths not containing "internal"
path!~"internal"
```

### Performance Analysis

```
# Slow requests (over 1 second)
response_time>1000

# Very fast requests (under 10ms)
response_time<10
```

### Nested Field Queries

```
# Filter by nested attribute
log_attributes.user_id="user-123"

# Pattern match in nested JSON
body~"error" and log_attributes.request.method="POST"
```

## Under the Hood

When you use LogchefQL, LogChef converts it to optimized ClickHouse SQL queries:

- The `~` and `!~` operators use ClickHouse's `positionCaseInsensitive` function for efficient partial matches
- Nested field access on Map columns uses subscript notation: `column['key']`
- Nested field access on JSON/String columns uses `JSONExtractString`
- A default time range and limit is automatically applied
- Results are ordered by timestamp in descending order

For example, this search:

```
level="error" and log_attributes.user_id="12345"
```

Gets converted to (for a table with `log_attributes` as a Map column):

```sql
SELECT *
FROM logs.app
WHERE (`level` = 'error') AND (`log_attributes`['user_id'] = '12345')
  AND timestamp BETWEEN toDateTime('2025-04-07 14:20:42', 'UTC') AND toDateTime('2025-04-07 14:25:42', 'UTC')
ORDER BY timestamp DESC
LIMIT 100
```

And with the pipe operator:

```
level="error" | timestamp level body
```

Generates:

```sql
SELECT `timestamp`, `level`, `body`
FROM logs.app
WHERE `level` = 'error'
  AND timestamp BETWEEN toDateTime('2025-04-07 14:20:42', 'UTC') AND toDateTime('2025-04-07 14:25:42', 'UTC')
ORDER BY timestamp DESC
LIMIT 100
```

## SQL Mode

For advanced queries that go beyond LogchefQL's capabilities, you can switch to **SQL Mode** in the query editor. This gives you full access to ClickHouse SQL, including:

- Aggregations (`COUNT`, `SUM`, `AVG`, etc.)
- Subqueries
- Joins
- Custom functions
- Complex expressions

In SQL mode, your query is executed exactly as written—time range and limit controls are disabled since you have full control over the SQL.

## Tips for Effective Queries

1. **Start specific, then broaden**: Begin with specific conditions and remove filters to expand results
2. **Use parentheses for complex logic**: `(a or b) and c` is clearer than relying on operator precedence
3. **Leverage nested field access**: Don't flatten your logs—query them directly
4. **Use the pipe operator**: Select only the fields you need for faster queries
5. **Switch to SQL mode**: For aggregations and advanced analysis
