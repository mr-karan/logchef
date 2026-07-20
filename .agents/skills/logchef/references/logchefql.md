# LogchefQL reference

LogchefQL is Logchef's filter language. You write it once; the server translates
it to ClickHouse SQL (for ClickHouse sources) or LogsQL (for VictoriaLogs
sources). Used by `query`, `tail`, `histogram`, and LogchefQL collections.

Always wrap a query in **single quotes** so the shell leaves `"`, `!`, `|`, `()`
alone.

## Shape

Every clause is `field operator value`. Clauses combine with `and` / `or`
(case-insensitive) and parentheses. **There is no bare full-text search** — a
lone word like `'timeout'` is a syntax error. Search text by filtering a field:

```bash
# WRONG:  logchef query 'timeout'
# RIGHT:
logchef query 'msg~"timeout"' -s 15m
```

An empty query (or just a `| field-list`) matches everything in the window —
useful for "show me anything from this source".

## Operators

| Operator | Meaning | Example |
|---|---|---|
| `=`  | equals | `level="error"` |
| `!=` | not equals | `level!="debug"` |
| `~`  | contains, case-insensitive substring | `msg~"connection refused"` |
| `!~` | does **not** contain | `path!~"/health"` |
| `>` `<` `>=` `<=` | numeric comparison | `status>=500`, `duration_ms>1000` |

`!=` and `!~` are fully supported by the server lexer (its operator pattern is
`!=|!~|>=|<=|[=~><]`). Do not avoid them.

### What the operators actually do

- **ClickHouse sources:** `~` → `positionCaseInsensitive(col, val) > 0` and `!~`
  → `... = 0`. Despite the internal name, `~` is a **case-insensitive substring**
  match, **not** a regular expression. `=`/`!=` are exact equality.
- **VictoriaLogs sources:** `=`→`field:=value`, `!=`→`NOT field:=value`,
  `~`→`field:~<regex>` (your substring is regex-escaped, so it stays a literal
  substring match), `!~`→`NOT field:~…`, comparisons→`field:>value` etc.

Because `~` is substring (not regex/tokenized), `msg~"time"` also matches
`"timeout"` and `"lifetime"`. Narrow with a more specific substring or combine
with `=` on another field.

## Values

- Bare values need no quotes: `status=200`, `level=error`.
- **Quote** anything with spaces or punctuation: `msg~"connection refused"`,
  `service="payment-api"`.
- Numbers stay unquoted for comparison operators: `status>=500`.
- Quote the *value*, using double quotes inside the single-quoted shell arg.

## Nested / Map / JSON fields

Use dot notation to reach into Map columns, JSON columns, or nested JSON inside
a string column:

```bash
logchef query 'log_attributes.user_id="12345"' -s 15m
logchef query 'log_attributes.request.method="POST" and msg~"error"' -s 1h
```

If a field *name itself* contains a dot, quote that segment:

```bash
logchef query 'log_attributes."user.name"="alice"' -s 15m
```

Discover what's available first with `logchef schema` (columns) and
`logchef fields <field>` (observed values) rather than guessing.

## Selecting columns — the `|` pipe

End a query with `| field1 field2 …` to return only those columns (space-
separated). A leading `|` with no filter selects columns across all rows:

```bash
logchef query 'level="error" | _timestamp service msg' -s 15m
logchef query '| service_name msg' -s 5m          # no filter, just these columns
```

The timestamp column is included by the server as needed for ordering; list it
explicitly if you want it shown.

## Combining conditions

```bash
logchef query 'level="error" and service="api"' -s 15m
logchef query '(service="auth" or service="users") and level="error"' -s 1h
logchef query 'status>=500 and path!~"/health" and msg~"timeout"' -s 30m
```

`and` binds tighter than `or`; parenthesize when mixing them.

## Previewing the translation

Before running something expensive, see exactly what the server will execute:

```bash
logchef explain 'level="error" and status>=500'    # print generated SQL/LogsQL, no scan
logchef query   'level="error"' --explain -s 15m    # trace SQL to stderr AND run
logchef query   'level="error"' --dry-run           # print generated SQL, then exit
```

`explain` / `--dry-run` are the fastest way to debug "why did this match
nothing?" — you see whether your nested-field path or substring landed where you
expected.

## When LogchefQL isn't enough

LogchefQL filters and selects columns; it does not aggregate, join, or dedupe.
For `count()`, `GROUP BY`, `DISTINCT`, subqueries, or window functions, drop to
`logchef sql` (see `clickhouse-sql.md`) or, on VictoriaLogs, LogsQL `| stats`
(see `logsql-victorialogs.md`). For counts-over-time specifically, try
`logchef histogram` first — it stays in LogchefQL and is cheap.
