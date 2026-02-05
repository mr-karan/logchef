---
title: Configuration
description: Configure LogChef to match your environment
---

LogChef uses a minimal TOML configuration file for bootstrap settings, with runtime configuration managed through the Admin Settings UI. This guide explains the essential configuration options and how to manage non-essential settings through the web interface.

## Configuration Architecture

LogChef separates configuration into two categories:

**Essential (Bootstrap) Settings** - Required in `config.toml`:
- Server connection details (port, host)
- SQLite database path
- OIDC authentication credentials
- Admin user emails and API token secrets
- Logging configuration

**Runtime Settings** - Managed via Admin Settings UI:
- Alerting configuration (SMTP settings, intervals, timeouts)
- AI/LLM settings (API keys, models, endpoints)
- Session management (duration, concurrency)
- Frontend URL for CORS

On first boot, LogChef seeds the database with values from `config.toml`. After that, runtime settings are stored in the database and managed through the Admin Settings UI at **Administration → System Settings**.

## Essential Configuration

These settings must be present in `config.toml` for LogChef to start:

## Server Settings

Configure the HTTP server and frontend settings:

```toml
[server]
# Port for the HTTP server (default: 8125)
port = 8125

# Host address to bind to (default: "0.0.0.0")
host = "0.0.0.0"

# URL of the frontend application
# Leave empty in production, used only in development
frontend_url = ""

# HTTP server timeout for requests (default: 30s)
http_server_timeout = "30s"
```

## Database Configuration

SQLite database settings for storing metadata:

```toml
[sqlite]
# Path to the SQLite database file
path = "logchef.db"
```

## Authentication

### OpenID Connect (OIDC)

Configure your SSO provider (example using Dex):

```toml
[oidc]
# URL of your OIDC provider
provider_url = "http://dex:5556/dex"

# Authentication endpoint URL (Optional: often discovered via provider_url)
auth_url = "http://dex:5556/dex/auth"

# Token endpoint URL (Optional: often discovered via provider_url)
token_url = "http://dex:5556/dex/token"

# OIDC client credentials
client_id = "logchef"
client_secret = "logchef-secret"

# CLI client ID for CLI authentication (public OIDC client, PKCE flow)
cli_client_id = "logchef-cli"

# Callback URL for OIDC authentication
# Must match the URL configured in your OIDC provider
redirect_url = "http://localhost:8125/api/v1/auth/callback"

# Required OIDC scopes
scopes = ["openid", "email", "profile"]
```

If you plan to use the CLI, create a public OIDC client with loopback redirect URIs
(`http://127.0.0.1:19876/callback` through `http://127.0.0.1:19878/callback`) and set
`oidc.cli_client_id` to that client ID.

### Auth Settings

Configure authentication behavior:

```toml
[auth]
# List of email addresses that have admin privileges (required)
admin_emails = ["admin@corp.internal"]

# Secret key for API token hashing (required, min 32 characters)
# Generate with: openssl rand -hex 32
api_token_secret = "your-secret-key-minimum-32-characters-long"
```

**Note:** Session duration, concurrent session limits, and default token expiry are managed via the Admin Settings UI under Authentication settings.

## Logging

Configure application logging:

```toml
[logging]
# Log level: "debug", "info", "warn", "error"
level = "info"
```

### Query Settings

Control query execution limits. Useful for environments where large data exports are needed.

```toml
[query]
# Maximum rows per query result. Default: 1000000 (1 million).
# Increase if your ClickHouse cluster can handle larger exports.
max_limit = 1000000
```

The UI will show limit options up to this value (100, 500, 1K, 2K, 5K, 10K, 50K, 100K, 200K, 500K, 1M).

**Environment variable:** `LOGCHEF_QUERY__MAX_LIMIT=500000`

## Runtime Configuration (Admin Settings UI)

The following settings are managed through the web interface at **Administration → System Settings** after first boot. You can optionally set initial values in `config.toml` which will be seeded to the database on first boot.

![Admin Settings UI](/screenshots/settings.gif)

### AI SQL Generation

Configure AI-powered SQL generation through the Admin Settings UI:

**Settings available:**
- **Enabled**: Enable/disable AI features
- **API Key**: OpenAI API key (marked as sensitive, hidden in UI)
- **Base URL**: OpenAI-compatible API endpoint (default: https://api.openai.com/v1)
- **Model**: Model name (e.g., "gpt-4o", "gpt-4o-mini")
- **Max Tokens**: Maximum tokens to generate (default: 1024)
- **Temperature**: Generation temperature 0.0-1.0 (default: 0.1)

**Supported Providers:**
- **OpenAI**: Use default base URL (https://api.openai.com/v1)
- **OpenRouter**: Set base URL to "https://openrouter.ai/api/v1"
- **Azure OpenAI**: Configure your Azure endpoint
- **Local Models**: Point to your local OpenAI-compatible server

**Optional `config.toml` seeding (first boot only):**
```toml
[ai]
enabled = false
base_url = "https://api.openai.com/v1"
api_key = ""  # Set via Admin UI after first boot
model = "gpt-4o"
max_tokens = 1024
temperature = 0.1
```

**Note:** After first boot, changes to `[ai]` section in `config.toml` are ignored. Manage settings via the UI.

### Alerting

Configure real-time log monitoring with email and webhook notifications through the Admin Settings UI. Per-alert recipients and webhook URLs are managed in the alert form.

**Settings available:**
- **Enabled**: Enable/disable alert evaluation and delivery
- **SMTP Host**: Email server hostname
- **SMTP Port**: Email server port
- **SMTP Username**: SMTP auth username (optional)
- **SMTP Password**: SMTP auth password (optional)
- **SMTP From**: From address for alert emails
- **SMTP Reply-To**: Reply-To address (optional)
- **SMTP Security**: `none`, `starttls`, or `tls`
- **Evaluation Interval**: How often to check all active alerts (e.g., "1m")
- **Default Lookback**: Default time range for alert queries (e.g., "5m")
- **History Limit**: Number of historical events to keep per alert (default: 50)
- **External URL**: Backend URL for API access
- **Frontend URL**: Frontend URL for web UI links in notifications
- **Request Timeout**: Alert notification request timeout (default: "5s")
- **TLS Insecure Skip Verify**: Skip TLS cert verification (dev only)

**Optional `config.toml` seeding (first boot only):**
```toml
[alerts]
enabled = false
evaluation_interval = "1m"
default_lookback = "5m"
history_limit = 50
smtp_host = ""
smtp_port = 587
smtp_username = ""
smtp_password = ""
smtp_from = "alerts@example.com"
smtp_reply_to = ""
smtp_security = "starttls"
external_url = ""
frontend_url = ""
request_timeout = "5s"
tls_insecure_skip_verify = false
```

**Note:** After first boot, manage all alert settings via **Administration → System Settings → Alerts**.

For alert configuration examples, notification setup, and best practices, see the [alerting feature guide](/features/alerting).

## Environment Variables

All configuration options set in the TOML file can be overridden or supplied via environment variables. This is particularly useful for sensitive information like API keys or for containerized deployments.

Environment variables are prefixed with `LOGCHEF_`. For nested keys in the TOML structure, use a double underscore `__` to represent the nesting.

**Format:** `LOGCHEF_SECTION__KEY=value`

**Examples:**

- Set server port:
  ```bash
  export LOGCHEF_SERVER__PORT=8125
  ```
- Set OIDC provider URL:
  ```bash
  export LOGCHEF_OIDC__PROVIDER_URL="http://dex.example.com/dex"
  ```
- Set admin emails (comma-separated for arrays):
  ```bash
  export LOGCHEF_AUTH__ADMIN_EMAILS="admin@example.com,ops@example.com"
  ```
- Set AI API Key:
  ```bash
  export LOGCHEF_AI__API_KEY="sk-your_actual_api_key_here"
  ```
- Enable AI features and set the model:
  ```bash
  export LOGCHEF_AI__ENABLED=true
  export LOGCHEF_AI__MODEL="gpt-4o"
  ```
- Configure alerting:
  ```bash
  export LOGCHEF_ALERTS__ENABLED=true
  export LOGCHEF_ALERTS__SMTP_HOST="smtp.example.com"
  export LOGCHEF_ALERTS__SMTP_PORT=587
  export LOGCHEF_ALERTS__SMTP_FROM="alerts@example.com"
  export LOGCHEF_ALERTS__FRONTEND_URL="https://logchef.example.com"
  ```

Environment variables take precedence over values defined in the TOML configuration file.

## Production Configuration

For production deployments, ensure you:

1. Set appropriate `host` and `port` values
2. Configure a secure `client_secret` for OIDC
3. Set the correct `redirect_url` matching your domain
4. Configure admin emails for initial access
5. Adjust session duration based on your security requirements
6. Set logging level to "info" or "warn"
7. If using AI features, ensure `LOGCHEF_AI__API_KEY` is set securely
8. If using alerting, configure SMTP settings and set `frontend_url` for correct generator links
9. Use `smtp_security` set to `tls` or `starttls` in production

## Minimal Production Configuration

This example shows the **essential configuration** required to run LogChef. All other settings (AI, alerting, sessions) are managed via the Admin Settings UI.

```toml
[server]
port = 8125
host = "0.0.0.0"
http_server_timeout = "30s"

[sqlite]
path = "/data/logchef.db"

[oidc]
provider_url = "https://dex.example.com"
auth_url = "https://dex.example.com/auth"
token_url = "https://dex.example.com/token"
client_id = "logchef"
client_secret = "your-secure-secret"
cli_client_id = "logchef-cli"
redirect_url = "https://logchef.example.com/api/v1/auth/callback"
scopes = ["openid", "email", "profile"]

[auth]
admin_emails = ["admin@example.com"]
api_token_secret = "your-secret-key-minimum-32-characters-long"

```

**After deployment:**
1. Login as admin user
2. Navigate to **Administration → System Settings**
3. Configure:
   - **AI** tab: Enable AI features and add API key
   - **Alerts** tab: Configure SMTP and notification settings
   - **Authentication** tab: Set session duration and limits
   - **Server** tab: Set frontend URL if needed

If your frontend is served from a different origin before first login, set
`LOGCHEF_SERVER__FRONTEND_URL` in the environment to ensure auth redirects return to the UI.

See [`config.toml`](https://github.com/mr-karan/logchef/blob/main/config.toml) for a fully commented configuration example.
