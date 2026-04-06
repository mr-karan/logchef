---
name: logchef
description: "LogChef CLI and LogchefQL query syntax for log analytics. Use this skill when querying logs via the logchef CLI, writing LogchefQL search filters, running ClickHouse SQL against log sources, managing saved collections, troubleshooting query errors, or learning LogchefQL syntax. Also use when the user mentions logchef, LogchefQL, log search, or wants to analyze logs from ClickHouse-backed sources."
---

# LogChef CLI & LogchefQL

LogChef is a log analytics platform backed by ClickHouse. The CLI lets you query logs, run SQL, and manage saved collections from the terminal.

## Quick Start

```bash
logchef auth --server https://logs.example.com   # authenticate via OIDC
logchef config set team "my-team"                 # set default team
logchef config set source "app-logs"              # set default source
logchef query 'level="error"' --since 15m         # search logs
```

## Commands

| Command | Purpose |
|---------|---------|
| `logchef auth --server <url>` | Authenticate via browser OIDC |
| `logchef auth --status` | Check auth status |
| `logchef query '<logchefql>'` | Search with LogchefQL |
| `logchef sql '<sql>'` | Run raw ClickHouse SQL |
| `logchef collections` | List/run saved queries |
| `logchef teams` | List available teams |
| `logchef sources -t <team>` | List sources for a team |
| `logchef schema -t <team> -S <source>` | Show table columns and types |
| `logchef config list` | View configured contexts |
| `logchef config set <key> <value>` | Set defaults (team, source, limit, timezone) |

Every command needs `--team` (`-t`) and `--source` (`-S`) unless defaults are set. Values can be names, numeric IDs, or `database.table`.

## Time Range

### Relative (--since)

Use `--since` / `-s` with `m` (minutes), `h` (hours), `d` (days), or `w` (weeks). Default: `15m`.

```bash
logchef query 'level="error"' -s 1h
logchef query 'status>=500' -s 7d
```

Seconds (`s`) and fractional values are **not supported**.

### Absolute (--from / --to)

Both flags are required together. Format: `'YYYY-MM-DD HH:MM:SS'` (space-separated, no `T` or `Z`).

```bash
logchef query 'level="error"' --from '2026-01-20 09:00:00' --to '2026-01-20 09:30:00'
```

Set timezone: `logchef config set timezone "Asia/Kolkata"`

### Limit

Default is `100` for `logchef query`. Override with `--limit` / `-l`. For SQL mode, use `LIMIT` in the query itself.

## LogchefQL Syntax

Wrap queries in **single quotes** to prevent shell expansion.

### Operators

| Operator | Meaning | Example |
|----------|---------|---------|
| `=` | Exact match | `level="error"` |
| `!=` | Not equals | `level!="debug"` |
| `~` | Contains (case-insensitive) | `msg~"timeout"` |
| `!~` | Does not contain | `path!~"health"` |
| `>` `<` `>=` `<=` | Numeric comparison | `status>=500` |

The `~` and `!~` operators use ClickHouse's `positionCaseInsensitive` for efficient substring matching — they are **not** regex.

### Values

- Simple values need no quotes: `status=200`, `level=error`
- Values with spaces **must** be quoted: `msg~"connection refused"`
- Field names with dots use quotes: `log_attributes."user.name"="alice"`

### Combining Conditions

Use `and` / `or` (case-insensitive) with parentheses for grouping:

```bash
logchef query 'level="error" and service="api"' -s 15m
logchef query '(service="auth" or service="users") and level="error"' -s 1h
```

**Standalone text search does not work** — every expression needs `field operator value`:
```bash
# WRONG: logchef query 'timeout'
# RIGHT:
logchef query 'msg~"timeout"' -s 15m
```

### Nested Field Access

Access Map columns, JSON fields, or nested JSON in string columns with dot notation:

```bash
logchef query 'log_attributes.user_id="12345"' -s 15m
logchef query 'log_attributes.request.method="POST" and msg~"error"' -s 1h
```

### Field Selection (Pipe Operator)

Select specific columns with `|` at the end:

```bash
logchef query 'level="error" | _timestamp level msg' -s 15m
logchef query '| service_name msg' -s 5m   # no filter, just select fields
```

## SQL Mode

Use `logchef sql` for aggregations, joins, negation, or anything beyond LogchefQL. Always include a time filter and `LIMIT`.

```bash
# Top error messages
logchef sql "SELECT msg, count() AS cnt FROM logs.app WHERE _timestamp > now() - INTERVAL 1 HOUR AND level='error' GROUP BY msg ORDER BY cnt DESC LIMIT 10"

# Read from stdin
cat query.sql | logchef sql -
```

## Collections (Saved Queries)

```bash
logchef collections                              # list saved queries
logchef collections "Error Dashboard"            # run by name
logchef collections "Error Dashboard" -s 1h      # override time range
logchef collections "Error Dashboard" --var env=prod  # override variables
```

## Output Formats

Use `--output text|json|jsonl|table` on `query`, `sql`, and `collections`.

```bash
logchef query 'status=500' --output json | jq '.msg'
logchef query 'level="error"' --output jsonl --no-highlight | grep "timeout"
```

Useful flags: `--no-highlight`, `--no-timestamp`, `--show-sql` (shows generated SQL for LogchefQL queries).

## Investigation Workflow

1. **Check schema** to know available columns: `logchef schema -t <team> -S <source>`
2. **Count volume** with SQL in a narrow window
3. **Group by dimension** to find dominant patterns
4. **Sample representative logs** with `--limit 10`
5. **Pivot on trace/request ID** for specific issues
6. **Expand time range** only after counts look reasonable

```bash
# 1. Schema
logchef schema -t prod -S app-logs

# 2. Count errors
logchef sql "SELECT count() FROM logs.app WHERE _timestamp > now() - INTERVAL 15 MINUTE AND level='error'"

# 3. Group by service
logchef sql "SELECT service, count() AS cnt FROM logs.app WHERE _timestamp > now() - INTERVAL 15 MINUTE AND level='error' GROUP BY service ORDER BY cnt DESC LIMIT 10"

# 4. Sample
logchef query 'service="payment-api" and level="error"' -s 15m -l 10

# 5. Trace
logchef query 'log_attributes.trace_id="abc123"' -s 1h
```

## Common Errors

| Error | Fix |
|-------|-----|
| `No context configured` | Run `logchef auth --server <url>` |
| `Team not specified` | Use `-t` or `logchef config set team <id>` |
| `Source not specified` | Use `-S` or `logchef config set source <id>` |
| `--from requires --to` | Both flags must be provided together |
| `invalid time format` | Use `'YYYY-MM-DD HH:MM:SS'` (no T, no Z) |
| `Invalid duration number` | Use integer + unit: `1m`, `1h`, `1d`, `1w` (no seconds) |
| `unexpected token "<EOF>"` | Queries need `field op value`, not bare text |
| `SQL query required` | Pass SQL as arg or pipe via `-` |
| Shell expansion issues | Wrap LogchefQL in single quotes |

## Safety

- Start with short time ranges (`15m`) and expand gradually
- Always include at least one filter beyond time
- Keep raw log samples small (≤20 rows) and redact secrets/PII
- Prefer SQL aggregations before pulling raw logs
- Use `--show-sql` to verify LogchefQL generates what you expect
