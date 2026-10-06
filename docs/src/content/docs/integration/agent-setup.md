---
title: Agent Setup
description: Connect Claude Code, Codex, Cursor, Claude.ai, and ChatGPT to Logchef. Use the CLI for a read-only setup preview, or the built-in MCP endpoint with OAuth.
---

An agent can use Logchef in two ways:

- **The CLI.** The agent runs `logchef` commands in a terminal. Refer to [Run the preview](#run-the-preview).
- **The MCP endpoint.** The agent calls tools over the [Model Context Protocol](/integration/mcp-server). Refer to [Connect over MCP](#connect-over-mcp).

## Run the preview

`logchef agent setup` prints the steps to use Logchef from a coding agent such as Claude Code, Codex, or Cursor. It is a preview. It does not change your agent configuration, sign you in, or open a browser.

## Run the preview

```bash
logchef agent setup
```

The command prints:

- The instance it resolved (from `--context`, `--server`, or your current context). With no server configured, commands show the placeholder `<your-logchef-url>`.
- The results of [`logchef doctor`](/integration/cli/#doctor): config, connectivity, token, and defaults.
- The steps to run: sign in with `logchef auth`, verify with `logchef doctor --json`, and read the version-matched guide with `logchef skills get core`. If the server does not offer [Logchef OAuth](/getting-started/configuration#logchef-oauth), the sign-in step tells you to set `LOGCHEF_AUTH_TOKEN` to an API token instead.

The server URL must be a plain `http` or `https` URL. The command rejects URLs that contain a username, password, query string, or fragment.

## Structured output

Use `--output json` (or `jsonl`) when an agent or script runs the command. Data goes to stdout. Notes go to stderr.

```bash
logchef agent setup --host claude-code --output json | jq '.steps[].command'
```

```json
{
  "mode": "preview",
  "mutates": false,
  "instance": { "context": "prod", "server_url": "https://logs.example.com" },
  "checks": [{ "check": "Server reachable", "status": "ok", "detail": "Logchef 2.0.0" }],
  "steps": [{ "id": "auth", "description": "...", "command": "logchef auth --server https://logs.example.com" }]
}
```

The command exits `0` even when a check fails, because a preview reports state. Read `checks[].status` (`ok`, `warn`, `fail`) to decide what to fix.

## Hosts

`--host` accepts `claude-code`, `codex`, `cursor`, or `chatgpt`. The steps are the same for the first three. ChatGPT connects through its remote app, so this command prints no steps for `chatgpt`. Refer to [ChatGPT](#chatgpt).

## Use the bundled guide

Point your agent at the CLI instead of copying syntax into prompts. `logchef skills get core` prints usage, LogchefQL, SQL, and LogsQL guidance that matches the installed binary. See [Skills](/integration/cli/#skills).

## Connect over MCP

Logchef serves MCP at `https://logchef.example.com/mcp`. Replace the host with your instance. The agent signs in with OAuth. There is no API token to copy, and no extra process to run.

Before you start:

- The Logchef operator must enable OAuth. Refer to [Logchef OAuth](/getting-started/configuration#logchef-oauth).
- The instance must be reachable from the machine that runs the agent. For Claude.ai and ChatGPT, it must be reachable from the internet over HTTPS.

Two kinds of client exist:

- **Local agents** run on your machine and receive the OAuth callback on a local port. Claude Code, Codex, and Cursor desktop use the built-in client ID `logchef-mcp`. You need no server configuration for them.
- **Hosted agents** run in a vendor's cloud. Claude.ai, Claude Desktop connectors, and ChatGPT need a client that the operator adds to the server config with the exact callback URL of the host.

### Claude Code

```bash
claude mcp add --transport http --client-id logchef-mcp logchef https://logchef.example.com/mcp
```

Then run `/mcp` in Claude Code, choose `logchef`, and sign in. The browser opens the Logchef consent page.

### Codex

```bash
codex mcp add logchef --url https://logchef.example.com/mcp --oauth-client-id logchef-mcp
codex mcp login logchef
```

### Cursor desktop

Add the server to `mcp.json`:

```json
{
  "mcpServers": {
    "logchef": {
      "url": "https://logchef.example.com/mcp",
      "auth": {
        "CLIENT_ID": "logchef-mcp"
      }
    }
  }
}
```

Cursor receives the callback at `http://localhost:8787/callback`. The `logchef-mcp` client accepts that address.

### Claude.ai and Claude Desktop

These use a custom connector. The operator adds a web client to the Logchef config:

```toml
[auth.oauth]
enabled = true

[[auth.oauth.clients]]
id = "claude"
name = "Claude"
redirect_uris = ["https://claude.ai/api/mcp/auth_callback"]
```

Then, in Claude, add a custom connector:

1. Set the server URL to `https://logchef.example.com/mcp`.
2. Open the advanced settings and set **OAuth Client ID** to `claude`.
3. Leave the client secret empty. The client is public and uses PKCE.
4. Connect, and approve the request on the Logchef consent page.

### ChatGPT

The operator adds a web client with the ChatGPT callback:

```toml
[auth.oauth]
enabled = true

[[auth.oauth.clients]]
id = "chatgpt"
name = "ChatGPT"
redirect_uris = ["https://chatgpt.com/connector_platform_oauth_redirect"]
```

In ChatGPT, add an app that uses your MCP URL, `https://logchef.example.com/mcp`. Choose OAuth, enter the client ID `chatgpt`, and leave the client secret empty.

### Other hosts

Any MCP client that supports OAuth with a pre-registered client ID can connect:

- A client that receives the callback on a loopback address (`localhost`, `127.0.0.1`, or `[::1]`, any port, path `/callback`) uses `logchef-mcp`.
- A client that runs in the cloud needs a `[[auth.oauth.clients]]` entry with its exact callback URL. Matching is exact. Copy the URL from the host's connector settings.

Logchef does not support dynamic client registration. Each client needs a client ID.

### Check the connection

Ask the agent to call `get_teams`. It returns the teams you belong to. To remove access, open **Settings → Connected apps** in Logchef.

The agent can use the tools that the [MCP Server](/integration/mcp-server#available-tools) page lists.
