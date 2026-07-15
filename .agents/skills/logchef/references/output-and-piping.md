# Output and piping reference

## Formats

Set with `--output <fmt>` on `query`, `sql`, `tail`, `collections`,
`saved-queries`, `find`, `schema`, `sources` (support varies per command).

| Format | Shape | Best for |
|---|---|---|
| `text` (default) | one formatted, syntax-highlighted line per row | humans at a TTY |
| `jsonl` | one JSON object per line (raw columns) | **agents / piping to `jq`** |
| `json` | single pretty-printed object: `logs`, `count`, `stats`, `columns`, generated query | one-shot structured capture |
| `json-flat` | like `jsonl`, but a JSON `msg` field is merged up into top-level keys | logs whose `msg` is itself JSON |
| `table` | aligned columns (first ~6 non-internal + `_timestamp`) | quick eyeball |
| `msg` | just the `msg` field (or first column) per row | grepping message text |
| `csv` | server-side CSV export, streamed | **`sql` only**; spreadsheets, large pulls |

`tail` supports `text`, `jsonl`, `msg` only. `find` supports `text`, `json`,
`jsonl`. Listing collections/saved-queries rejects `msg`/`json-flat`.

## Clean output for machines

- Prefer `--output jsonl` — newline-delimited, no pretty-printing, streams.
- Row stats (`N logs | Xms | Y rows read`) print to **stderr** and only when
  stdout is a TTY, so a pipe gets pure data on stdout.
- Add `--no-highlight` to guarantee no ANSI color codes leak into text output
  (highlighting is auto-disabled when stdout isn't a TTY, but be explicit in
  scripts).
- `--no-timestamp` hides the timestamp column in `text` output.

```bash
logchef query 'status>=500' -s 15m --output jsonl --no-highlight | jq -r '.msg'
logchef query 'level="error"' -s 1h --output jsonl | jq -r 'select(.service=="api") | .msg'
logchef query 'level="error"' -s 1h --output json  | jq '.count, .stats.rows_read'
logchef query 'level="error"' -s 5m --output msg | grep -i timeout
```

## jq recipes

```bash
# Unique services seen in the window
logchef query 'level="error"' -s 1h --output jsonl | jq -r '.service' | sort | uniq -c | sort -rn

# Pull one field out of a JSON msg (use json-flat so nested keys hoist up)
logchef query 'level="error"' -s 1h --output json-flat | jq -r '.trace_id'

# Count rows client-side
logchef query 'status>=500' -s 15m --output jsonl | wc -l
```

## Highlighting (text output only)

```bash
logchef query 'level="error"' --highlight 'red:timeout,refused' --highlight 'yellow:retry'
logchef query 'level="error"' --disable-highlight <group>       # turn off a configured group
logchef query 'level="error"' --no-highlight                    # all off
```

`--highlight COLOR:word1,word2` adds ad-hoc rules on top of configured ones.

## Stdin (`sql`)

Read a query from stdin with `-`:

```bash
cat query.sql | logchef sql -
echo "SELECT count() FROM logs.app WHERE level='error'" | logchef sql - -s 1h
```

## Streaming large result sets

`sql --stream --output jsonl` streams rows directly from the server (only
`jsonl` is valid with `--stream`); `sql --output csv` runs an export job and
streams the finished file. Both bump the timeout floor to 120s. See
`clickhouse-sql.md`.
