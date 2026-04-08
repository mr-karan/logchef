---
title: Declarative Provisioning
description: Manage teams, sources, and access control via config files
---

Logchef supports declarative provisioning — define your teams, data sources, and access control in a TOML config file instead of (or alongside) the web UI. This enables GitOps workflows where infrastructure config is version-controlled and deployed automatically.

## How It Works

Provisioning uses a **managed vs unmanaged** strategy:

- **Managed resources** — Declared in your provisioning config. Logchef creates, updates, and optionally deletes them on startup. The API rejects manual edits to managed resources.
- **Unmanaged resources** — Created via the UI. Provisioning ignores them entirely.

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
| `source_type` | No | `clickhouse` | Datasource backend: `clickhouse` or `victorialogs` |
| `connection` | Yes for new configs | — | Provider-specific connection block |
| `secret_ref` | No | — | Environment variable name for the provider secret |
| `meta_ts_field` | No | `timestamp` for ClickHouse, `_time` for VictoriaLogs | Timestamp field name |
| `meta_severity_field` | No | — | Severity/level column name |
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

For VictoriaLogs bearer auth, `secret_ref` fills `connection.auth.token`. For VictoriaLogs basic auth, it fills `connection.auth.password`.

For **Nomad** deployments, use Nomad variables with template blocks:
```toml
password = "{{ "{{.ch_prod_password}}" }}"
```

## Adopt Existing Resources

When you first enable provisioning on an existing Logchef instance, resources declared in config are matched against existing database records **by name**:

- A source named "Production Logs" in config matches an existing source named "Production Logs" in the DB
- Matched resources are adopted (marked as managed) and updated to match config
- Unmatched config entries create new resources

No special migration flag is needed — adoption happens automatically on name match.

## Pruning

With `prune = false` (default), managed resources removed from config are **kept** but logged as warnings:

```
WARN managed source not in config (prune=false, keeping)  name="Old Source"
```

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

This returns a JSON representation of all sources, teams, and memberships — useful as a starting point for writing your `provisioning.toml`.

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
