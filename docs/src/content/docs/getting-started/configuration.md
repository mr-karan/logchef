---
title: Configuration
description: Configure LogChef to match your environment
---

LogChef uses a TOML configuration file to manage its settings. This guide explains all available configuration options.

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

# Callback URL for OIDC authentication
# Must match the URL configured in your OIDC provider
redirect_url = "http://localhost:8125/api/v1/auth/callback"

# Required OIDC scopes
scopes = ["openid", "email", "profile"]
```

### Auth Settings

Configure authentication behavior:

```toml
[auth]
# List of email addresses that have admin privileges
admin_emails = ["admin@corp.internal"]

# Duration of user sessions (e.g., "8h", "24h", "7d")
session_duration = "8h"

# Maximum number of concurrent sessions per user
max_concurrent_sessions = 1
```

## Logging

Configure application logging:

```toml
[logging]
# Log level: "debug", "info", "warn", "error"
level = "info"
```

## AI SQL Generation (OpenAI)

Configure settings for the AI-powered SQL generation feature using OpenAI.

```toml
[ai]
# Enable or disable AI features (default: false)
enabled = true

# --- API Endpoint Configuration ---
# Optional: Base URL for the OpenAI API compatible endpoint.
# Useful for proxying or using services like Azure OpenAI, OpenRouter.
# (default: uses the standard OpenAI API endpoint)
# base_url = "https://your-proxy.com/v1"

# OpenAI API Key. Required if AI features are enabled.
# Can also be set via LOGCHEF_AI__API_KEY environment variable.
api_key = "sk-your_openai_api_key"

# --- Model Parameters ---
# Model to use for SQL generation (default: "gpt-4o")
# Examples: "gpt-4o", "gpt-3.5-turbo"
model = "gpt-4o"

# Maximum number of tokens to generate in the SQL query (default: 1024)
max_tokens = 1024

# Temperature for generation (0.0-1.0). Lower is more deterministic (default: 0.1)
temperature = 0.1
```

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

Environment variables take precedence over values defined in the TOML configuration file.

## Production Configuration

For production deployments, ensure you:

1. Set appropriate `host` and `port` values
2. Configure a secure `client_secret` for OIDC
3. Set the correct `redirect_url` matching your domain
4. Configure admin emails for initial access
5. Adjust session duration based on your security requirements
6. Set logging level to "info" or "warn"
7. If using AI features, ensure `LOGCHEF_AI__API_KEY` is set securely.

## Example Production Configuration

```toml
[server]
port = 8125
host = "0.0.0.0"

[sqlite]
path = "/data/logchef.db"

[oidc]
provider_url = "https://dex.example.com"
client_id = "logchef"
client_secret = "your-secure-secret"
redirect_url = "https://logchef.example.com/api/v1/auth/callback"
scopes = ["openid", "email", "profile"]

[auth]
admin_emails = ["admin@example.com"]
session_duration = "8h"
max_concurrent_sessions = 1

[logging]
level = "info"

# Example for AI features in production (API key should be set via env var)
[ai]
enabled = true

# --- API Endpoint Configuration ---
# base_url = ""
# api_key = "" # Prefer LOGCHEF_AI__API_KEY for sensitive data

# --- Model Parameters ---
model = "gpt-4o"
max_tokens = 1024
temperature = 0.1
```
