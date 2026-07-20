# LogsQL (VictoriaLogs sources) reference

When a source's `TYPE` is **VictoriaLogs** (see `logchef sources`), two things
are true:

1. `logchef query '<logchefql>'` still works — the server translates LogchefQL
   into LogsQL for you. Prefer this.
2. `logchef sql '<logsql>'` sends your text to VictoriaLogs **verbatim as
   LogsQL** (not SQL). This is the escape hatch for LogsQL features LogchefQL
   can't express — chiefly `| stats` aggregation.

`logchef explain '<logchefql>'` on a VictoriaLogs source prints the generated
**LogsQL**, so you can learn the translation and then extend it by hand.

## LogsQL essentials

LogsQL is a filter followed by optional `|`-separated pipes.

### Field filters

```
level:=error                 exact match on a field
NOT level:=debug             negation
app:="payment-api"           quote values with spaces/punctuation
msg:~"connection refused"    regex match (substring-style)
status:>=500                 numeric comparison  (:> :< :>= :<=)
level:=error AND app:=api    combine (AND / OR / NOT, parentheses)
```

Bare words are full-text over the default message field (VictoriaLogs *does*
allow this, unlike LogchefQL): `error` matches messages containing `error`.

### Time ranges

Logchef injects the time window for LogchefQL queries automatically. When you
write raw LogsQL via `logchef sql`, either:

- pass `--since` / `--from`+`--to` and let Logchef scope it, or
- write a LogsQL `_time:` filter yourself:

```
_time:5m                         last 5 minutes
_time:[2026-07-14 09:00, 2026-07-14 09:30]
error AND _time:1h
```

### Pipes

```
error | stats count() logs                          total matching
error | stats by (level) count() logs               group by level
* | stats by (service) count() c | sort by (c) desc top services
error | fields _time, service, msg                  project columns
error | sort by (_time) desc | limit 20             newest 20
```

`| stats`, `| sort`, `| limit`, `| fields`, `| uniq`, `| top` are the common
ones. This is how you aggregate on VictoriaLogs — there is no `GROUP BY`.

## Examples via the CLI

```bash
# LogchefQL (portable — works here and on ClickHouse)
logchef query 'level="error" and app="checkout"' -t platform -S vl-app -s 1h

# Raw LogsQL for aggregation LogchefQL can't do
logchef sql '_time:1h error | stats by (app) count() c | sort by (c) desc' \
  -t platform -S vl-app

# Preview what a LogchefQL query becomes on this backend
logchef explain 'status>=500 and path!~"/health"' -t platform -S vl-app
```

## Field names and discovery

VictoriaLogs sources typically use `_time` as the timestamp field and `_msg` /
`msg` for the message. Reserved words (pipe names, stats functions like `count`,
`sum`, `min`, `max`, `avg`, `uniq`) can't be used as bare field names — quote or
qualify them. Use `logchef schema` and `logchef fields <field>` to see the real
field set and values before writing filters.

## Gotchas

- `logchef sql` on a VictoriaLogs source is **LogsQL, not SQL**. Sending
  `SELECT …` there will fail — use `logchef query` or LogsQL syntax.
- LogchefQL `~` becomes a regex-escaped substring match (`field:~…`), so it stays
  literal even if your value contains regex metacharacters.
- `tail` on a VictoriaLogs source keys dedup/cursor off the source's configured
  timestamp field (e.g. `_time`), not a hardcoded `_timestamp`.
