---
title: "Let Your AI Agent Query Your Logs"
description: "Logchef serves an MCP endpoint at /mcp. Connect Claude Code, Codex, Cursor, Claude.ai or ChatGPT with OAuth and let the agent read schemas, run LogchefQL and investigate errors in ClickHouse and VictoriaLogs."
pubDate: 2026-11-18
tags: ["mcp", "ai", "agents", "clickhouse", "victorialogs", "logs"]
author: "Logchef Team"
---

It's 2 a.m. and the checkout error alert fired. You already have Claude Code open in the repo, because the fix will land there. The agent can read the code, the recent commits and the deploy config. It can't read the one thing that tells you what broke: the logs. So you switch to the log UI, write a filter, copy 40 lines of stack traces, paste them into the chat, and ask "what changed?" Then the agent asks for the previous hour so it can compare, and you do it again.

The copy-and-paste step is the slow part, and it's also where context gets lost. You paste the lines you think matter, not the ones the agent would have looked for.

[Logchef](https://github.com/mr-karan/logchef) now serves a [Model Context Protocol](https://modelcontextprotocol.io/) endpoint, so the agent can query the logs itself. It's built into the server at `<public_url>/mcp`. There's no separate process to install, and there's no API token to paste into a config file.

## Turn it on

The endpoint exists only when OAuth is enabled on the server. Two settings:

```toml
[server]
public_url = "https://logchef.example.com"

[auth.oauth]
enabled = true
```

Or as environment variables: `LOGCHEF_SERVER__PUBLIC_URL` and `LOGCHEF_AUTH__OAUTH__ENABLED=true`. `public_url` must be an `https` origin (plain `http` is allowed for `localhost`, `127.0.0.1` and `::1`), and it must match the address your MCP client uses, because it's the OAuth issuer. Behind a reverse proxy, forward `/oauth/*`, `/.well-known/oauth-*` and `/mcp` to Logchef. With OAuth off, `/mcp` returns `404`.

## Connect an agent

Local agents (Claude Code, Codex, Cursor desktop) use the built-in client ID `logchef-mcp`. It needs no server config. For Claude Code:

```bash
claude mcp add --transport http --client-id logchef-mcp logchef https://logchef.example.com/mcp
```

Run `/mcp` in Claude Code, choose `logchef`, and sign in. Your browser opens the Logchef consent page. You sign in the way you normally do (SSO or local login), check the client name, the return address and the scopes, and choose **Approve**. The client gets a 10-minute access token and a 30-day refresh token.

Codex is two commands:

```bash
codex mcp add logchef --url https://logchef.example.com/mcp --oauth-client-id logchef-mcp
codex mcp login logchef
```

Hosted agents (Claude.ai, Claude Desktop connectors, ChatGPT) run in a vendor's cloud, so the operator registers them with their exact callback URL:

```toml
[[auth.oauth.clients]]
id = "claude"
name = "Claude"
redirect_uris = ["https://claude.ai/api/mcp/auth_callback"]

[[auth.oauth.clients]]
id = "chatgpt"
name = "ChatGPT"
redirect_uris = ["https://chatgpt.com/connector_platform_oauth_redirect"]
```

Then add a custom connector with the URL `https://logchef.example.com/mcp` and the client ID `claude` or `chatgpt`, with no client secret. These clients are public and use PKCE. For hosted agents, Logchef must be reachable from the internet over HTTPS. The [Agent Setup](/integration/agent-setup) page has the steps for each client, including Cursor's `mcp.json`.

To check the connection, ask the agent to call `get_teams`. It returns the teams you belong to.

## What the agent gets

The endpoint exposes 21 tools, all read-only. They fall into a few groups:

- **Discovery:** `get_profile`, `get_teams`, `get_meta`, `get_sources`, `get_team_sources`, `get_source_schema`. The agent finds the right source and reads its columns before it writes a query, so it doesn't guess field names.
- **Querying:** `query_logs` runs SQL on ClickHouse sources or LogsQL on VictoriaLogs sources. `get_log_histogram` returns volume over time, and `get_log_context` returns the lines before and after a timestamp.
- **LogchefQL:** `query_logchefql`, `translate_logchefql` and `validate_logchefql`. The agent can write one filter language for both backends and see the generated SQL or LogsQL before it runs anything.
- **Investigation:** `get_field_values`, `get_all_field_dimensions`, `top_values`, `compare_windows` and `open_investigation`.
- **Saved work and alerts:** `list_saved_queries`, `get_saved_query`, `list_saved_views`, `list_alerts`, `get_alert_history`.

Back to the 2 a.m. page. You type "checkout errors started around 01:40, find out why". A reasonable agent first calls `get_sources` and `get_source_schema` to find the field names. Then it counts errors with an aggregation pipe through `query_logchefql`:

```
severity_text="ERROR" | count() by service_name
```

That tells it whether the problem is checkout alone or spread across services. Next it buckets by time to find the start:

```
severity_text="ERROR" and service_name="checkout" | count() by time(1m)
```

Then `compare_windows` runs the same filter over 01:00 to 01:40 and 01:40 to 02:20, and `top_values` shows which hosts, error messages or status codes changed between them. The agent looks at the actual lines with `get_log_context` around the first failure. What comes back is real rows from your logs, not a guess based on the stack trace you chose to paste.

The same aggregation text compiles to ClickHouse SQL on a ClickHouse source and to LogsQL on a VictoriaLogs source, so the agent doesn't need to know which backend holds a given team's logs. For SQL that a filter can't express, `query_logs` takes `raw_sql`, and it accepts the same [time macros](/guide/time-macros) and `{{name}}` variables that dashboard panels use.

Two prompts are included for clients that surface them. `investigate_error_spike` takes a `team_id`, a `source_id` and an optional `time_range` (last hour by default), and walks through schema discovery, error volume, patterns, a timeline and root cause. `investigate_alert` takes an `alert_id`, reads the alert's configuration and history, and pulls the matching logs.

## The investigation panel

Rows of JSON in a chat are hard to scan. `open_investigation` opens an interactive panel inside the conversation, using [MCP Apps](https://modelcontextprotocol.io/extensions/apps/overview). In the panel you can browse sources, run bounded LogchefQL queries, select a histogram interval or specific rows, compare two time windows, and attach the evidence you selected back to the conversation. So you can say "these 12 rows, not those" without typing them out.

The panel is one self-contained page with no access to external networks. It calls the same read-only tools with the same token. A client that doesn't support MCP Apps gets the tool result as text.

## What it can't do

The agent acts as you, and with less reach than you have in the web UI.

- **Read-only.** No tool creates, changes or deletes teams, sources, users, tokens, saved queries, saved views, collections or alerts.
- **Team membership only.** An OAuth token sees the teams you belong to and the sources linked to them. This applies to admins too: an admin's web session can open any team, but an admin's agent token can't.
- **Scoped.** The consent page requests eight read scopes (`profile:read`, `teams:read`, `sources:read`, `logs:read`, `saved_queries:read`, `collections:read`, `alerts:read`, `saved_views:read`). A tool that needs a scope the token doesn't hold doesn't appear in the tool list.
- **Bounded.** One tool call ends after `query.mcp_call_timeout_seconds` (default 60, capped by `query.max_timeout_seconds`), and the per-user and global concurrency limits from the web UI apply. `query_logs` returns at most 100 rows per call, and `stats.truncation` tells the agent whether rows were cut, so it knows when it's looking at a sample.
- **Separate credentials.** `/mcp` accepts only Logchef OAuth tokens issued for it. API tokens, service tokens, browser cookies and CLI tokens get `401`, and an MCP token gets `401` at `/api`.

To cut an agent off, open **Settings → Connected apps** and remove it. Its next request fails.

There's also a risk the server can't solve for you: log lines are untrusted text. A log line can contain instructions aimed at an assistant. Read what the agent does with what it found, and connect only the agents you need.

Some things are out of scope. There are no admin tools, no write tools, and no tool for ClickHouse `system.query_log`. Logchef's own AI SQL assistant stays in the web UI; over MCP, the agent you connect writes the queries. Logchef doesn't support dynamic client registration, so every client uses `logchef-mcp` or a client the operator registered.

## Try it

Logchef is a single binary with SQLite for metadata. Your logs stay in ClickHouse or VictoriaLogs. Enable `[auth.oauth]`, add the server to your agent, and ask it what broke.

- Source and releases: [github.com/mr-karan/logchef](https://github.com/mr-karan/logchef)
- MCP reference: [logchef.app/integration/mcp-server](https://logchef.app/integration/mcp-server)
- Client setup: [logchef.app/integration/agent-setup](https://logchef.app/integration/agent-setup)
