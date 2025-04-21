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

## Environment Variables

All configuration options can also be set using environment variables. The format is:
`LOGCHEF_SECTION_KEY=value`

Examples:

```bash
# Set server port
LOGCHEF_SERVER_PORT=8125

# Set OIDC provider URL
LOGCHEF_OIDC_PROVIDER_URL=http://dex:5556/dex

# Set admin emails
LOGCHEF_AUTH_ADMIN_EMAILS=admin@example.com,another@example.com
```

## Production Configuration

For production deployments, ensure you:

1. Set appropriate `host` and `port` values
2. Configure a secure `client_secret` for OIDC
3. Set the correct `redirect_url` matching your domain
4. Configure admin emails for initial access
5. Adjust session duration based on your security requirements
6. Set logging level to "info" or "warn"

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
```
