---
name: logchef
description: Query application logs via LogChef CLI. Use when investigating production incidents, debugging errors, analyzing log patterns, or correlating events across services.
argument-hint: "[service name] [time range]"
allowed-tools: Bash(logchef *), Read, Grep
---

# LogChef Log Analysis

Query production logs using the LogChef CLI for incident investigation.

## Quick Reference

| Command | Use Case |
|---------|----------|
| `logchef sql "..."` | SQL queries (aggregations, counts, time series) |
| `logchef query '...'` | Filter queries (sample logs, grep-style) |

## Required Parameters

Always include these flags (get values from `logchef config show`):
- `-t <team>` - Team name or ID
- `-S <source>` - Source name, `database.table_name`, or ID

Or set defaults:
```bash
logchef config set team "my-team"
logchef config set source "my-source"
```

## Time Formats

```bash
# Relative time (recommended)
--since 1h
--since 15m
--since 24h

# Absolute time (ISO 8601 or YYYY-MM-DD HH:MM:SS)
--from "2026-01-22 09:15:00" --to "2026-01-22 10:00:00"
--from "2026-01-22T09:15:00Z" --to "2026-01-22T10:00:00Z"
```

## LogChefQL Syntax

LogChefQL is a simple query language for filtering logs.

### Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `=` | Exact match | `level="error"` |
| `!=` | Not equal | `status!=200` |
| `~` | Contains/regex (case-insensitive) | `msg~"timeout"` |
| `!~` | Does not contain | `msg!~"expected"` |
| `>` | Greater than | `status>400` |
| `<` | Less than | `response_time<100` |
| `>=` | Greater or equal | `severity>=3` |
| `<=` | Less or equal | `count<=10` |

### Boolean Operators

- `and` - Both conditions must match
- `or` - Either condition matches
- `()` - Grouping for precedence

### Examples

```bash
# Exact match (quoted value)
level="error"

# Exact match (unquoted value)
level=error

# Contains/regex match
msg~"timeout"

# Negation
msg!~"noise pattern"

# Combined conditions
level="error" and service="api"

# OR conditions
level="error" or level="warn"

# Grouping
(level="error" or level="warn") and service="api"

# Field selection with pipe
level="error" | timestamp msg service
```

## Common Patterns

### 1. Log Volume Over Time

```bash
logchef sql "SELECT toStartOfMinute(_timestamp) as ts, count() as logs 
FROM DATABASE.TABLE 
WHERE _timestamp >= 'YYYY-MM-DD HH:MM:SS' 
  AND _timestamp <= 'YYYY-MM-DD HH:MM:SS' 
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE
```

### 2. Error Count by Minute

```bash
logchef sql "SELECT toStartOfMinute(_timestamp) as ts, count() as errors 
FROM DATABASE.TABLE 
WHERE _timestamp >= 'YYYY-MM-DD HH:MM:SS' 
  AND _timestamp <= 'YYYY-MM-DD HH:MM:SS' 
  AND msg ILIKE '%error%' 
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE
```

### 3. Sample Actual Logs

```bash
logchef query 'service="my-service" and msg~"pattern"' \
  -t TEAM -S SOURCE \
  --from "YYYY-MM-DD HH:MM:SS" \
  --to "YYYY-MM-DD HH:MM:SS" \
  --limit 10
```

### 4. List Distinct Values

```bash
logchef sql "SELECT DISTINCT service FROM DATABASE.TABLE 
WHERE _timestamp >= now() - INTERVAL 1 HOUR
LIMIT 50" -t TEAM -S SOURCE
```

### 5. High Resolution (30-Second Granularity)

```bash
logchef sql "SELECT toStartOfInterval(_timestamp, INTERVAL 30 SECOND) as ts, 
  count() as logs 
FROM DATABASE.TABLE 
WHERE _timestamp >= 'YYYY-MM-DD HH:MM:SS' 
  AND _timestamp <= 'YYYY-MM-DD HH:MM:SS' 
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE
```

### 6. Multiple Conditions with countIf

```bash
logchef sql "SELECT toStartOfMinute(_timestamp) as ts,
  countIf(msg ILIKE '%error%') as errors,
  countIf(msg ILIKE '%timeout%') as timeouts,
  countIf(msg ILIKE '%connection%refused%') as conn_refused
FROM DATABASE.TABLE 
WHERE _timestamp >= 'YYYY-MM-DD HH:MM:SS' 
  AND _timestamp <= 'YYYY-MM-DD HH:MM:SS' 
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE
```

### 7. Log Level Distribution

```bash
logchef sql "SELECT level, count() as cnt 
FROM DATABASE.TABLE 
WHERE _timestamp >= now() - INTERVAL 1 HOUR
GROUP BY level ORDER BY cnt DESC" -t TEAM -S SOURCE
```

### 8. Error Messages Breakdown

```bash
logchef sql "SELECT 
  extractAll(msg, 'error[: ]([^,\n]+)')[1] as error_type,
  count() as cnt 
FROM DATABASE.TABLE 
WHERE _timestamp >= now() - INTERVAL 1 HOUR
  AND msg ILIKE '%error%'
GROUP BY error_type 
ORDER BY cnt DESC 
LIMIT 20" -t TEAM -S SOURCE
```

## Investigation Workflows

### 1. Initial Triage

```bash
# Get log volume pattern
logchef sql "SELECT toStartOfMinute(_timestamp) as ts, count() as logs 
FROM DATABASE.TABLE 
WHERE _timestamp >= 'START_TIME' 
  AND _timestamp <= 'END_TIME' 
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE

# Look for cliff (sudden drop) or spike (sudden increase)
```

### 2. Error Analysis

```bash
# Count errors by minute
logchef sql "SELECT toStartOfMinute(_timestamp) as ts, count() as errors 
FROM DATABASE.TABLE 
WHERE _timestamp >= 'START_TIME' 
  AND _timestamp <= 'END_TIME' 
  AND msg ILIKE '%error%' 
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE

# Sample actual errors
logchef query 'msg~"error"' \
  -t TEAM -S SOURCE \
  --from "START_TIME" \
  --to "END_TIME" \
  --limit 20
```

### 3. Cross-Service Correlation

```bash
# Check multiple services at once
logchef sql "SELECT toStartOfMinute(_timestamp) as ts,
  countIf(service='api') as api,
  countIf(service='web') as web,
  countIf(service='worker') as worker
FROM DATABASE.TABLE 
WHERE service IN ('api', 'web', 'worker')
  AND _timestamp >= 'START_TIME' 
  AND _timestamp <= 'END_TIME' 
  AND msg ILIKE '%error%'
GROUP BY ts ORDER BY ts" -t TEAM -S SOURCE
```

### 4. Host-Level Analysis

```bash
# Errors by host
logchef sql "SELECT host, count() as errors 
FROM DATABASE.TABLE 
WHERE _timestamp >= 'START_TIME' 
  AND _timestamp <= 'END_TIME' 
  AND msg ILIKE '%error%'
GROUP BY host ORDER BY errors DESC" -t TEAM -S SOURCE
```

## Common Gotchas

| Issue | Solution |
|-------|----------|
| Query timeout | Narrow time window, add more filters |
| No results | Check field names, verify time range |
| Wrong timestamp | Use `_timestamp` (check your schema) |
| Regex not working | Use `ILIKE '%pattern%'` in SQL, `msg~"pattern"` in query |
| Case sensitive | Use `ILIKE` (case-insensitive) instead of `LIKE` |
| Performance | Always include time filter first |
| Syntax error | Use `=` not `:` for field matching |

## SQL Functions Reference

```sql
-- Time bucketing
toStartOfMinute(_timestamp)      -- 1-minute buckets
toStartOfFiveMinutes(_timestamp) -- 5-minute buckets
toStartOfHour(_timestamp)        -- Hourly buckets
toStartOfInterval(_timestamp, INTERVAL 30 SECOND)  -- Custom interval

-- Conditional counting
countIf(condition)
sumIf(column, condition)

-- String matching
msg ILIKE '%pattern%'            -- Case-insensitive contains
msg LIKE '%pattern%'             -- Case-sensitive contains
match(msg, 'regex')              -- Regex match

-- Extraction
extractAll(msg, 'pattern')[1]    -- Extract regex group
substring(msg, 1, 100)           -- First 100 chars
```

## Output Formats

```bash
# Default text output with highlighting
logchef query 'level="error"'

# JSON output (for jq processing)
logchef query 'level="error"' --output json | jq '.logs[] | .msg'

# JSON Lines (one object per line)
logchef query 'level="error"' --output jsonl | jq '.msg'

# Disable highlighting for piping
logchef query 'level="error"' --no-highlight | grep "pattern"

# Show generated SQL
logchef query 'level="error"' --show-sql
```

## Performance Tips

1. **Always filter by time first** - LogChef uses time-based partitioning
2. **Narrow time windows** - Start with 15 minutes, expand if needed
3. **Filter early** - Add service/level filters to reduce scan scope
4. **Use LIMIT for sampling** - Don't pull all logs
5. **Aggregate before retrieving** - Use SQL aggregations
