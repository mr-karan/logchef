---
title: Declarative Provisioning
description: Manage teams, sources, and access control via config files
---

LogChef supports declarative provisioning — define your teams, data sources, and access control in a TOML config file instead of (or alongside) the web UI. This enables GitOps workflows where infrastructure config is version-controlled and deployed automatically.

## How It Works

Provisioning uses a **managed vs unmanaged** strategy:

- **Managed resources** — Declared in your provisioning config. LogChef creates, updates, and optionally deletes them on startup. The API rejects manual edits to managed resources.
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
host = "clickhouse.internal:9000"
username = "logchef"
password = "secret"
database = "logs"
table_name = "otel_logs"
meta_ts_field = "timestamp"
description = "Production application logs"
ttl_days = 30

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

### 3. Start LogChef

On startup, LogChef reconciles the declared state with the database. With `dry_run = true`, it logs what it *would* do without making changes:

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
| `host` | Yes | — | ClickHouse host:port |
| `username` | Yes | — | ClickHouse username |
| `password` | Yes | — | ClickHouse password (or use `secret_ref`) |
| `secret_ref` | No | — | Environment variable name containing the password |
| `database` | Yes | — | ClickHouse database |
| `table_name` | Yes | — | ClickHouse table |
| `meta_ts_field` | No | `timestamp` | Timestamp column name |
| `meta_severity_field` | No | — | Severity/level column name |
| `description` | No | — | Human-readable description |
| `ttl_days` | No | `0` | Data retention in days |

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
host = "clickhouse:9000"
username = "logchef"
secret_ref = "LOGCHEF_CH_PROD_PASSWORD"
database = "logs"
table_name = "otel_logs"
```

Set the environment variable before starting LogChef:
```bash
export LOGCHEF_CH_PROD_PASSWORD="actual-password"
```

For **Nomad** deployments, use Nomad variables with template blocks:
```toml
password = "{{ "{{.ch_prod_password}}" }}"
```

## Adopt Existing Resources

When you first enable provisioning on an existing LogChef instance, resources declared in config are matched against existing database records **by name**:

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
# ...
```

The separate file approach keeps secrets isolated and makes the provisioning config independently deployable.
