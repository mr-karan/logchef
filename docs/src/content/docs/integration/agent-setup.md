---
title: Agent Setup
description: Preview how to connect a coding agent to your Logchef instance with the CLI. Read-only, no config changes.
---

`logchef agent setup` prints the steps to use Logchef from a coding agent such as Claude Code, Codex, or Cursor. It is a preview. It does not change your agent configuration, sign you in, or open a browser.

## Run the preview

```bash
logchef agent setup
```

The command prints:

- The instance it resolved (from `--context`, `--server`, or your current context). With no server configured, commands show the placeholder `<your-logchef-url>`.
- The results of [`logchef doctor`](/integration/cli/#doctor): config, connectivity, token, and defaults.
- The steps to run: sign in with `logchef auth`, verify with `logchef doctor --json`, and read the version-matched guide with `logchef skills get core`.

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

`--host` accepts `claude-code`, `codex`, `cursor`, or `chatgpt`. The steps are the same for the first three. ChatGPT connects through its remote app, so this command prints no steps for `chatgpt`.

## Use the bundled guide

Point your agent at the CLI instead of copying syntax into prompts. `logchef skills get core` prints usage, LogchefQL, SQL, and LogsQL guidance that matches the installed binary. See [Skills](/integration/cli/#skills).
