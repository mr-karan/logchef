---
title: Declarative Provisioning
description: Manage Logchef teams, ClickHouse, VictoriaLogs and Loki sources, and access control via version-controlled config files for GitOps workflows.
---

Logchef supports declarative provisioning: define your teams, data sources, and access control in a TOML config file instead of (or alongside) the web UI. This enables GitOps workflows where infrastructure config is version-controlled and deployed automatically.

## How It Works

Provisioning uses a **managed vs unmanaged** strategy:

- **Managed resources**: Declared in your provisioning config. Logchef creates, updates, and optionally deletes them on startup. The API rejects manual edits to managed resources.
- **Unmanaged resources**: Created via the UI. Provisioning ignores them entirely.

This means you can gradually adopt provisioning: start by declaring a few sources, and leave everything else UI-managed.

## Quick Start

### 1. Create `provisioning.toml`

```toml
manage_sources = true
manage_teams = true
prune = false
dry_run = true  # Start with dry-run to verify

[[sources]]
name = "Production Logs"
source_type = "clickhouse"
meta_ts_field = "timestamp"
description = "Production application logs"
ttl_days = 30

[sources.connection]
host = "clickhouse.internal:9000"
username = "logchef"
password = "secret"
database = "logs"
table_name = "otel_logs"

[[teams]]
name = "Platform"
description = "Platform engineering"
sources = ["Production Logs"]
members = [
  { email = "alice@example.com", role = "admin" },
  { email = "bob@example.com", role = "editor" },
  { email = "carol@example.com", role = "member" },
]
```

:::caution[Upgrading from a version before 2.0]
The source format changed in 2.0: each `[[sources]]` now needs a `source_type`
and a nested `[sources.connection]` block. If your `provisioning.toml` still puts
`host` / `database` / `table_name` at the top level of a `[[sources]]` entry, the
server refuses to start until you move those fields under `[sources.connection]`
and add `source_type = "clickhouse"` (as shown above). Teams, members, and the
non-connection fields (`description`, `ttl_days`, `meta_ts_field`) are unchanged.
:::

### 2. Point config.toml to it

```toml
[provisioning]
file = "provisioning.toml"
```

### 3. Start Logchef

On startup, Logchef reconciles the declared state with the database. With `dry_run = true`, it logs what it *would* do without making changes:

```
INFO provisioning dry-run complete, rolling back transaction
```

### 4. Apply for real

Once satisfied with the dry-run output, set `dry_run = false` and restart.

## Configuration Reference

### Top-Level Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `manage_sources` | bool | `false` | Enable declarative source management |
| `manage_teams` | bool | `false` | Enable declarative team management |
| `prune` | bool | `false` | Delete managed resources removed from config |
| `dry_run` | bool | `false` | Log changes without applying them |

### Source Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | Yes | — | Unique display name (used as identity key) |
| `source_type` | No | `clickhouse` | Datasource backend: `clickhouse`, `victorialogs` or `loki` |
| `connection` | Yes for new configs | — | Provider-specific connection block |
| `secret_ref` | No | — | Environment variable name for the provider secret |
| `header_secret_refs` | No | — | VictoriaLogs and Loki: map of custom header name to the environment variable that holds its value |
| `meta_ts_field` | No | `timestamp` for ClickHouse, `_time` for VictoriaLogs and Loki | Timestamp field name |
| `meta_severity_field` | No | — (`detected_level` for Loki) | Severity/level column name |
| `description` | No | — | Human-readable description |
| `ttl_days` | No | `0` | Data retention in days |

For **ClickHouse**, the connection block looks like:

```toml
[[sources]]
name = "Production Logs"
source_type = "clickhouse"
meta_ts_field = "timestamp"
secret_ref = "LOGCHEF_CH_PROD_PASSWORD"

[sources.connection]
host = "clickhouse.internal:9000"
username = "logchef"
database = "logs"
table_name = "otel_logs"
```

When a ClickHouse source targets an ordinary `View` (`ENGINE = View`) backed by timestamp-ordered `MergeTree` tables, you can opt in to **Recent activity** inspection via `[sources.connection.activity]`:

```toml
[[sources]]
name = "Visible Logs View"
source_type = "clickhouse"
meta_ts_field = "timestamp"
secret_ref = "LOGCHEF_CH_PROD_PASSWORD"

[sources.connection]
host = "clickhouse.internal:9000"
username = "logchef"
database = "logs"
table_name = "logs_visible"

[sources.connection.activity]
allow_view = true
max_rows_to_read = 50000000     # optional positive cap; defaults to 50,000,000
max_bytes_to_read = 1073741824  # optional positive cap; defaults to 1 GiB
```

Opted-in view activity queries run through the view itself with `max_execution_time` capped at 3 seconds and ClickHouse overflow modes forced to `throw`, so exceeding an execution or scan cap fails explicitly rather than returning partial counts. Timeouts and scan-cap failures return HTTP 422 with error type `ActivityLimitExceeded`. Other activity paths retain HTTP 504 for timeouts.

Activity caps must be positive integers, at most 10,000,000,000 rows and 1 TiB (1,099,511,627,776 bytes). Logchef takes the minimum of the activity caps and the explicit source query settings. It does not discover ClickHouse settings-profile caps, so activity defaults can override a stricter profile limit. Set `max_rows_to_read` and `max_bytes_to_read` explicitly under `[sources.connection.settings]` to stay within a profile limit.

For **VictoriaLogs**, use the native API connection shape:

```toml
[[sources]]
name = "Payments Logs"
source_type = "victorialogs"
meta_ts_field = "_time"
meta_severity_field = "level"
secret_ref = "LOGCHEF_VL_PROD_TOKEN"

[sources.connection]
base_url = "https://logs.example.com"

[sources.connection.auth]
mode = "bearer"

[sources.connection.tenant]
account_id = "12"
project_id = "34"

[sources.connection.scope]
query = "{app=\"payments\"} kubernetes.namespace:=prod"
```

The provisioning format is now nested and datasource-native. ClickHouse sources must use `source_type` plus the nested `connection` block rather than top-level `host` / `database` / `table_name` fields.

### Loki sources

For **Grafana Loki**, the connection block takes the base URL, the tenant, the
default stream selector and the parser:

```toml
[[sources]]
name = "Loki Demo"
source_type = "loki"
secret_ref = "LOGCHEF_SOURCE_LOKI_DEMO_TOKEN"

[sources.connection]
base_url = "http://127.0.0.1:3100"
tenant_id = "dev"
selector = '{service_name=~".+"}'
parser = "json"
principal = "payments-readonly"
auth = { mode = "bearer" }

[sources.connection.limits]
max_entries_per_query = 5000
sidebar_lookback_seconds = 900
schema_lookback_seconds = 3600
```

Logchef checks a Loki connection with the same rules as the API, and an error
names the field, such as `loki connection.selector: selector is required`.
`selector` is required. `auth.mode` is `none` (the default), `basic` or
`bearer`. `parser` is empty, `json` or `logfmt`. `limits` and `headers` are
optional, and `Authorization` and `X-Scope-OrgID` are reserved header names.
See [Using Grafana Loki with Logchef](/tutorials/loki/#add-the-source) for what
each field means.

`base_url`, `tenant_id`, `selector` and `principal` are the source's identity.
Provisioning refuses to change them on an existing Loki source, including one
it adopts by name, because that would repoint the source's team links and
saved queries at other data. Create a new source instead.

An export keeps the Loki source's principal and uses `secret_ref` for its
password or token. For each non-empty custom header, the export writes a
`header_secret_refs` entry with the declared or generated environment
variable name. See
[Export Current State](#export-current-state) for the naming rules.


### Source Views

A source can declare saved Explorer views. A declared view is a source view: every team that reaches the source sees it in the Explorer's view picker. Provisioning manages it, so the UI shows it without edit actions and the API refuses to change or delete it. Team and personal views stay in the UI.

```toml
[[sources]]
name = "nginx"
# ... connection ...

  [[sources.views]]
  name = "Access"
  description = "Requests that failed"
  default = true
  display_mode = "table"
  time_range = "1h"
  filter = 'status >= 400'
  sort = [{ column = "timestamp", desc = true }]
  columns = [{ name = "timestamp", width = 190 }, { name = "status" }, { name = "request_uri", width = 520 }]
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique per source, up to 200 characters. Provisioning matches a stored view by source and name |
| `description` | No | Up to 2,000 characters |
| `default` | No | Makes the view the source default. At most one view per source |
| `columns` | No | Visible columns in order, 1 to 200 entries. `name` is the column, `width` an optional width from 50 to 3,000 pixels. Columns not listed are hidden |
| `sort` | No | 1 to 3 entries, most significant first. `column` and an optional `desc` |
| `display_mode` | No | `table`, `compact` or `json` |
| `filter` | No | A LogchefQL query that fills an empty editor, up to 8,192 bytes |
| `time_range` | No | A relative range in minutes, hours, days or weeks: `15m`, `1h`, `7d`, `2w` |

A field you leave out leaves that part of the Explorer as it is. A source holds at most 50 declared views.

Logchef checks every view at startup with the rules the API uses and refuses to start on an error that names the source and the view. The `filter` must parse as LogchefQL. `time_range` must not use seconds: the Explorer's time picker has no seconds unit, so `30s` is an error.

The source default follows the file. A view with `default = true` takes the default from any other source view, including one an admin set in the UI. When no declared view has `default = true`, provisioning clears a default it set before and leaves a default set in the UI alone.

### Team Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique display name (used as identity key) |
| `description` | No | Human-readable description |
| `sources` | No | List of source names this team can access |
| `members` | No | List of team members |

### Member Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `email` | Yes | — | User's email (must match OIDC identity) |
| `role` | No | `member` | Team role: `admin`, `editor`, or `member` |

## Secret Management

Never commit passwords to version control. Use `secret_ref` to reference environment variables:

```toml
[[sources]]
name = "Production Logs"
source_type = "clickhouse"
secret_ref = "LOGCHEF_CH_PROD_PASSWORD"

[sources.connection]
host = "clickhouse:9000"
username = "logchef"
database = "logs"
table_name = "otel_logs"
```

Set the environment variable before starting Logchef:
```bash
export LOGCHEF_CH_PROD_PASSWORD="actual-password"
```

For VictoriaLogs and Loki bearer auth, `secret_ref` fills `connection.auth.token`. For basic auth, it fills `connection.auth.password`.

Custom headers on VictoriaLogs and Loki sources often carry a credential too, such as an API key for a proxy in front of the backend. Use `header_secret_refs` to read a header's value from an environment variable:

```toml
[[sources]]
name = "Payments Logs"
source_type = "victorialogs"

[sources.connection]
base_url = "https://logs.example.com"
headers = { X-Team = "payments" }

[sources.header_secret_refs]
X-Api-Key = "LOGCHEF_SOURCE_PAYMENTS_LOGS_HEADER_X_API_KEY"
```

Each entry sets the header to the variable's value unless
`connection.headers` already supplies a non-empty value for that header.
Header names match without regard to case. Declaring the same header twice in
`header_secret_refs` is an error. If a header needs a value from a variable
and the variable is empty or unset, Logchef refuses to start. The error
names the source, header, and variable.

For **Nomad** deployments, use Nomad variables with template blocks:
```toml
password = "{{ "{{.ch_prod_password}}" }}"
```

## Adopt Existing Resources

When you first enable provisioning on an existing Logchef instance, resources declared in config are matched against existing database records **by name**:

- A source named "Production Logs" in config matches an existing source named "Production Logs" in the DB
- Matched resources are adopted (marked as managed) and updated to match config
- A declared view matches an existing source view of the same source and name. It keeps its id, so links to it keep working
- Unmatched config entries create new resources

No special migration flag is needed. Adoption happens automatically on name match.

## Pruning

With `prune = false` (default), managed resources removed from config are **kept** but logged as warnings:

```
WARN managed source not in config (prune=false, keeping)  name="Old Source"
```

A managed view that its source no longer declares is kept and logged the same way. With `prune = true` it is deleted. A source view created in the UI is never pruned.

With `prune = true`, they are **deleted**. Be careful:

:::caution
Deleting a managed team cascades to its saved queries and alerts via database foreign key constraints. Always run with `dry_run = true` first when enabling pruning.
:::

## API Protection

Managed resources cannot be modified via the API or UI. Attempting to edit or delete a managed team/source/user returns:

```json
{
  "status": "error",
  "message": "This source is managed by provisioning config and cannot be modified via API",
  "error_type": "ManagedResourceError"
}
```

## Export Current State

Admins can export the current database state as a provisioning config via the API:

```bash
curl -s https://logchef.example.com/api/v1/admin/provisioning/export \
  -H "Authorization: Bearer <token>" | jq .
```

This returns a JSON representation of all sources with their source views, teams, and memberships, useful as a starting point for writing your `provisioning.toml`. Exported views import unchanged: no revision moves and no default changes.

The export replaces source passwords, tokens, and non-empty custom header
values with environment variable references. It keeps the declared
environment variable name when one exists.

- A password without a declared reference uses
  `LOGCHEF_SOURCE_<SOURCE>_PASSWORD` in `secret_ref`. A bearer token uses
  `LOGCHEF_SOURCE_<SOURCE>_TOKEN`.
- A non-empty custom header without a declared reference uses
  `LOGCHEF_SOURCE_<SOURCE>_HEADER_<HEADER>` in `header_secret_refs`. Empty
  headers stay in `connection.headers`.

Logchef uppercases the source and header names in generated references and
replaces each byte outside `A-Z`, `0-9`, and `_` with `_`. Several sources
can share a declared variable. If a generated name matches another generated
or declared name, the export returns HTTP 409 and identifies both uses.
Rename a source or declare a distinct name in `secret_ref` or
`header_secret_refs` to resolve the conflict.

Set each referenced variable to its password, token, or header value before
applying the export. Supplying the original values preserves the source
configuration on import.

## Inline vs Separate File

You can define provisioning inline in `config.toml` or as a separate file:

**Separate file (recommended):**
```toml
# config.toml
[provisioning]
file = "provisioning.toml"
```

**Inline:**
```toml
# config.toml
[provisioning]
manage_sources = true
manage_teams = true

[[provisioning.sources]]
name = "My Logs"
source_type = "clickhouse"

[provisioning.sources.connection]
# ...
```

The separate file approach keeps secrets isolated and makes the provisioning config independently deployable.
