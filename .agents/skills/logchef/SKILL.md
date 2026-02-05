---
name: logchef
description: Query and analyze LogChef logs from the terminal using the LogChef CLI. Use for incident triage, debugging errors, exploring LogChefQL filters, running ClickHouse SQL, and executing saved collections.
---

# LogChef CLI

Use the LogChef CLI to query logs safely and accurately.

**Quick Start**
1. Authenticate with OIDC: `logchef auth --server https://logs.example.com`
2. Set defaults: `logchef config set team "my-team"` and `logchef config set source "nginx-logs"`
3. Query logs: `logchef query 'level="error"' --since 15m --limit 20`

**Command Cheat Sheet**
| Command | Use |
| --- | --- |
| `logchef auth` | Authenticate (browser-based OIDC) |
| `logchef query '...'` | Run LogChefQL filters with time flags |
| `logchef sql '...'` | Run raw ClickHouse SQL (use `LIMIT` in SQL) |
| `logchef collections` | List or run saved collections |
| `logchef teams` | List teams available to you |
| `logchef sources` | List sources for a team |
| `logchef schema` | Show columns/types for a source |
| `logchef config ...` | Manage contexts and defaults |

**Auth And Context**
Use `logchef auth --server <url>` to create a context and store a token.
Use `logchef auth --status` and `logchef auth --logout` to check or clear auth.
Use `--context <name>` or `LOGCHEF_CONTEXT` to target a specific context.
Use `--server <url>` and `--token <token>` (or `LOGCHEF_SERVER_URL` and `LOGCHEF_AUTH_TOKEN`) for one-off, ephemeral use.
Use `logchef config list` to view contexts and `logchef config use <name>` to switch.
Avoid `logchef config set auth.token` because it is not supported by the CLI.

**Required Context**
Provide `--team` and `--source` on every command unless defaults are set.
Set defaults with `logchef config set team <name_or_id>` and `logchef config set source <name_or_id>`.
Use interactive mode by running `logchef query` or `logchef sql` with no args in a TTY and no defaults.
Use case-insensitive names, numeric IDs, or `database.table` as the `--source` value.
Discover names and IDs with `logchef teams` and `logchef sources --team <name_or_id>`.

**Time Range And Limit**
Use `--since` on `logchef query` and `logchef collections` for relative ranges with `m`, `h`, `d`, or `w` (default is `15m` for `query`).
Use `--from` and `--to` together for absolute ranges on `logchef query` only.
Use the exact format `YYYY-MM-DD HH:MM:SS` for `--from` and `--to`.
Set timezone with `logchef config set timezone "UTC"` (or IANA name like `America/Los_Angeles`) and provide absolute times in that timezone.
Expect `--limit` default to `100` for `logchef query` unless overridden via `logchef config set limit <n>`.
Use `LIMIT` in SQL because `logchef sql` does not accept `--limit` or `--since`.
Expect collections to use their saved time range and limit unless overridden.

**LogChefQL Basics**
Quote the entire query with single quotes to avoid shell piping and expansion.
Use `=` `!=` `>` `<` `>=` `<=` for comparisons.
Use `~` and `!~` for case-insensitive substring match (not regex).
Use `and` / `or` with parentheses for grouping.
Use `| field1 field2` to select output columns.
Quote values with spaces or special characters.
Quote field names that contain dots or special characters.

```bash
# Exact match
logchef query 'level="error" and service="api"' --since 15m

# Substring match (case-insensitive)
logchef query 'msg~"timeout"' --since 1h --limit 20

# Field selection with pipe (note the single quotes)
logchef query 'status>=500 and path~"/v1/" | _timestamp status path msg' --since 30m

# Field name with dots
logchef query 'k8s.labels."app.kubernetes.io/name"="api"' --since 1h
```

**SQL Usage**
Use `logchef sql` for aggregations, joins, or complex logic.
Always include a time filter on the timestamp column (often `_timestamp`).
Use `LIMIT` in SQL to control result size.
Use `-` to read SQL from stdin for longer queries.

```bash
logchef sql "SELECT level, count() AS cnt FROM logs.app WHERE _timestamp > now() - INTERVAL 1 HOUR GROUP BY level ORDER BY cnt DESC LIMIT 10"

cat query.sql | logchef sql -
```

**Collections**
List collections: `logchef collections --team "production" --source "nginx-logs"`.
Run a collection by name: `logchef collections "Error Dashboard" --team "production" --source "nginx-logs"`.
Override time range: `--since 1h`.
Override limit: `--limit 50`.
Override variables: `--var name=value` (repeatable).

**Teams, Sources, Schema**
List teams: `logchef teams`.
List sources for a team: `logchef sources --team "production"`.
Show columns/types: `logchef schema --team "production" --source "nginx-logs"`.

**Output And Highlighting**
Use `--output text|json|jsonl|table` on `query`, `sql`, and `collections`.
Use `--output list` on `collections` when listing.
Use `--no-highlight` and `--no-timestamp` for clean piping.
Use `--highlight color:WORD1,WORD2` and `--disable-highlight GROUP` for ad-hoc formatting.
Use `--show-sql` on `logchef query` to display the generated SQL.

```bash
logchef query 'status=500' --output json | jq '.count'
logchef query 'status=500' --output jsonl | jq '.msg'
logchef query 'level="error"' --no-highlight | grep -i "timeout"
```

**Common Errors And Fixes**
`No context configured` → run `logchef auth --server <url>` or set `LOGCHEF_SERVER_URL` and `LOGCHEF_AUTH_TOKEN`.
`Team not specified` → pass `--team` or run `logchef config set team <name_or_id>`.
`Source not specified` → pass `--source` or run `logchef config set source <name_or_id>`.
`--from requires --to` → always set both `--from` and `--to`.
`invalid time format` → use `YYYY-MM-DD HH:MM:SS` (no `T`, no `Z`, no offset).
`SQL query required` → pass SQL as an argument or use `-` to read from stdin.
Shell pipe errors → wrap LogChefQL in single quotes.

**Safety Rules**
Always bound time (start with `15m`, expand gradually).
Always scope with at least one filter besides time (service, level, host, trace_id).
Always use `--limit` for `query` and `collections`.
Keep raw samples small (≤20 lines) and redact secrets or PII.
Prefer aggregations before pulling raw logs.

**Investigation Workflow**
1. Count volume with a narrow window.
2. Group by a small dimension to find dominant errors.
3. Sample a few representative logs.
4. Pivot on `trace_id` or `request_id`.
5. Expand time range only after counts look reasonable.

```bash
# Count errors (SQL)
logchef sql "SELECT count() AS cnt FROM logs.app WHERE _timestamp > now() - INTERVAL 15 MINUTE AND level='error'"

# Top error messages (SQL)
logchef sql "SELECT msg, count() AS cnt FROM logs.app WHERE _timestamp > now() - INTERVAL 15 MINUTE AND msg ILIKE '%error%' GROUP BY msg ORDER BY cnt DESC LIMIT 10"

# Sample specific error
logchef query 'msg~"connection refused"' --since 15m --limit 10
```
