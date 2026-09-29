# Troubleshooting

## Error → fix

| Symptom / message | Cause and fix |
|---|---|
| `No context configured` / `No current context` | Not logged in. `logchef auth --server <url>`. |
| `Team not specified` | Pass `-t <team>` or `logchef config set team <name|id>`. |
| `Source not specified` | Pass `-S <source>` or `logchef config set source <name|id>`. |
| `Team '…' not found` / `Source '…' not found` | Wrong name/id. List with `logchef teams` / `logchef sources -t <team>`. Source accepts name, id, or `database.table`. |
| `--from requires --to` (or vice-versa) | Absolute time needs **both** flags. |
| `invalid time format` | Use `'YYYY-MM-DD HH:MM:SS'` — a space, no `T`, no `Z`. |
| `Invalid timezone: 'IST'` | Use an IANA zone name: `logchef config set timezone Asia/Kolkata`. `doctor` warns when a hand-edited config holds an invalid zone. |
| `Invalid duration number` | `--since` is integer + `m`/`h`/`d`/`w` (`15m`, `2h`). No seconds/fractions (`tail` alone allows `s`). |
| `unexpected token "<EOF>"` / parse error | LogchefQL needs `field op value`; bare words aren't valid. `msg~"timeout"`, not `timeout`. |
| Shell ate your `!`, `|`, `"`, or `()` | Wrap the whole query in **single quotes**. |
| `Raw query required` / `cannot be empty` | Give `sql` a query as an arg or pipe via `-`. |
| `--stream does not support --output <x>` | `--stream` only allows `jsonl`. Use `--output csv` (no `--stream`) for CSV, or drop `--stream`. |
| `SELECT …` fails on a VictoriaLogs source | `sql` sends **LogsQL** there, not SQL. Use `logchef query` or LogsQL syntax. |
| `Token may be invalid or expired` | Re-authenticate: `logchef auth`. Check with `logchef auth --status` / `auth current`. |
| `CLI authentication not configured on this server` | Server admin must set `oidc.cli_client_id`. |
| Query returns nothing unexpectedly | See "Empty results" below. |

## Empty results — debug order

1. `logchef explain '<query>'` (or `query --dry-run`) — is the generated
   SQL/LogsQL what you meant? Did a nested-field path or substring land wrong?
2. Widen time a little (`-s 1h`) — maybe nothing happened in the last 15m.
3. Check field names with `logchef schema` and values with `logchef fields <field>`
   — the column may be named differently than you assumed.
4. Remember `~` is a **case-insensitive substring**, not tokenized or regex:
   `msg~"err"` matches `ERR`, `error`, and `terror`, but `msg~"error"` does not
   match a line that only says `ERR`. Broaden or narrow the substring.
5. Confirm timezone: `logchef config show`. `--from` / `--to` are wall-clock
   in the effective zone, so a wrong zone shifts your window. Row timestamps
   keep the backend's offset (often UTC), so a row at `03:45:00Z` (09:15 IST) is inside an
   `Asia/Kolkata` window of `09:00`–`09:30`.

## Auth and contexts

Logchef uses kubectl-style **contexts** — one per server. The token is stored in
`~/.config/logchef/logchef.json` (created `0600`; exact dir follows the OS config
convention / `$XDG_CONFIG_HOME`).

```bash
logchef auth --server https://logs.example.com   # OIDC PKCE browser login → creates/updates a context
logchef auth --status                             # authenticated? who?
logchef auth current                              # active context, server, token source (offline, no network)
logchef auth --logout                             # clear the token for the active context
logchef whoami                                    # user + accessible teams

logchef config list                               # all contexts (* = current)
logchef config use <name>                         # switch context
logchef config show                               # current context: server, defaults, effective timezone
logchef config path                               # where the config file lives
logchef config rename <old> <new>
logchef config delete <name>
```

Overrides (highest precedence first): `--context` / `--server` / `--token`
flags and their env vars (`LOGCHEF_CONTEXT`, `LOGCHEF_SERVER_URL`,
`LOGCHEF_AUTH_TOKEN`), then `LOGCHEF_DEFAULT_TEAM` / `LOGCHEF_DEFAULT_SOURCE`,
then the saved context and its defaults. `--debug` on any command prints
verbose logs to stderr.

Never paste a token back to the user; if one is exposed, recommend rotating it
(re-run `logchef auth`).

## Config defaults

```bash
logchef config set team platform
logchef config set source app-logs
logchef config set limit 200
logchef config set since 1h
logchef config set timezone Asia/Kolkata
logchef config set timeout 60
```

Valid keys: `team`, `source`, `limit`, `since`, `timezone`, `timeout`. With
`team` and `source` set, you can omit `-t`/`-S` everywhere.

## Making sure the skill matches the binary

If CLI behavior doesn't match this doc, your skill copy may be stale. The binary
serves its own version-matched copy:

```bash
logchef skills get core          # current instructions
logchef skills get core --full   # + all references
logchef --version                # confirm the binary version
```
