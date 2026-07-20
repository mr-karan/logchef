use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::Client;
use logchef_core::config::Context as CtxConfig;
use logchef_core::timerange::resolve_timezone;
use serde::Serialize;
use std::io::IsTerminal;

use crate::cli::GlobalArgs;

const CLI_VERSION: &str = env!("CARGO_PKG_VERSION");

#[derive(Args)]
#[command(
    long_about = "Run a one-shot health check of your Logchef setup: config file, \
current context, server reachability (GET /api/v1/meta), CLI auth availability, \
token validity, and whether your default team/source actually resolve.\n\n\
Each line is ✓ (ok), ⚠ (warning), or ✗ (problem); every warning/problem prints \
an actionable `→` fix. Exits 0 when there are no ✗ (warnings are fine), 1 otherwise. \
Network checks degrade gracefully when there's no server or token configured.",
    after_help = "EXAMPLES:
  # Human-readable health report
  logchef doctor

  # Machine-readable checks for scripts/CI
  logchef doctor --json | jq '.[] | select(.status == \"fail\")'

  # Diagnose a specific server without switching contexts
  logchef doctor --server https://logs.example.com"
)]
pub struct DoctorArgs {
    /// Emit the checks as a JSON array of {check, status, detail, hint}.
    #[arg(long)]
    json: bool,
}

#[derive(Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
enum Status {
    Ok,
    Warn,
    Fail,
}

impl Status {
    fn glyph(self) -> &'static str {
        match self {
            Status::Ok => "✓",
            Status::Warn => "⚠",
            Status::Fail => "✗",
        }
    }

    /// ANSI color for the glyph, applied only on a TTY.
    fn color(self) -> &'static str {
        match self {
            Status::Ok => "\x1b[32m",   // green
            Status::Warn => "\x1b[33m", // yellow
            Status::Fail => "\x1b[31m", // red
        }
    }
}

#[derive(Serialize)]
struct Check {
    check: String,
    status: Status,
    detail: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    hint: Option<String>,
}

impl Check {
    fn ok(check: &str, detail: impl Into<String>) -> Self {
        Self {
            check: check.into(),
            status: Status::Ok,
            detail: detail.into(),
            hint: None,
        }
    }

    fn warn(check: &str, detail: impl Into<String>, hint: impl Into<String>) -> Self {
        Self {
            check: check.into(),
            status: Status::Warn,
            detail: detail.into(),
            hint: Some(hint.into()),
        }
    }

    fn fail(check: &str, detail: impl Into<String>, hint: impl Into<String>) -> Self {
        Self {
            check: check.into(),
            status: Status::Fail,
            detail: detail.into(),
            hint: Some(hint.into()),
        }
    }
}

pub async fn run(args: DoctorArgs, global: GlobalArgs) -> Result<()> {
    let mut checks = Vec::new();

    let config = Config::load();

    // ---- Config file & context -------------------------------------------
    let config = match config {
        Ok(config) => {
            let path = Config::config_path()
                .map(|p| p.display().to_string())
                .unwrap_or_else(|_| "(unknown)".to_string());
            let exists = Config::config_path().map(|p| p.exists()).unwrap_or(false);
            if exists {
                checks.push(Check::ok("Config file", path));
            } else {
                checks.push(Check::warn(
                    "Config file",
                    format!("{} (not created yet)", path),
                    "run `logchef auth --server <url>` to create it",
                ));
            }
            config
        }
        Err(err) => {
            checks.push(Check::fail(
                "Config file",
                format!("failed to load: {}", err),
                "check the file is valid JSON, or remove it to start fresh",
            ));
            return finish(checks, args.json);
        }
    };

    // Resolve the context to inspect: --context, then --server, then current.
    let resolved = resolve_context(&config, &global);

    match &resolved {
        Some((name, _)) => checks.push(Check::ok("Context", name.clone())),
        None => checks.push(Check::fail(
            "Context",
            "no context configured",
            "run `logchef auth --server <url>` to sign in",
        )),
    }

    let effective_tz = resolve_timezone(
        resolved
            .as_ref()
            .and_then(|(_, c)| c.defaults.timezone.as_deref()),
    );
    let tz_detail = match resolved
        .as_ref()
        .and_then(|(_, c)| c.defaults.timezone.clone())
    {
        Some(tz) => tz,
        None => format!("{} (detected, not set)", effective_tz),
    };
    checks.push(Check::ok("Timezone", tz_detail));

    // ---- Server ----------------------------------------------------------
    let server_url = resolved.as_ref().map(|(_, c)| c.server_url.clone());
    let token = global
        .token
        .clone()
        .or_else(|| resolved.as_ref().and_then(|(_, c)| c.token.clone()));

    let Some(server_url) = server_url else {
        checks.push(Check::fail(
            "Server URL",
            "not configured",
            "run `logchef auth --server <url>`",
        ));
        // No server → the remaining network checks can't run.
        checks.push(Check::warn(
            "Server reachable",
            "skipped (no server configured)",
            "configure a server first",
        ));
        checks.push(Check::warn(
            "Auth token",
            "skipped (no server configured)",
            "configure a server, then run `logchef auth`",
        ));
        return finish(checks, args.json);
    };
    checks.push(Check::ok("Server URL", server_url.clone()));

    let client = match Client::new(&server_url, 15) {
        Ok(client) => match &token {
            Some(t) => client.with_token(t.clone()),
            None => client,
        },
        Err(err) => {
            checks.push(Check::fail(
                "Server reachable",
                format!("could not build HTTP client: {}", err),
                "check the server URL is a valid http(s) URL",
            ));
            return finish(checks, args.json);
        }
    };

    // Reachability + auth availability via GET /api/v1/meta.
    let meta = client.get_meta().await;
    match &meta {
        Ok(meta) => {
            checks.push(Check::ok(
                "Server reachable",
                format!("Logchef {}", meta.data.version),
            ));
            if meta.data.oidc_enabled() {
                checks.push(Check::ok("CLI auth", "OIDC configured on server"));
            } else {
                checks.push(Check::warn(
                    "CLI auth",
                    "OIDC/CLI login not configured on server",
                    "ask your admin to set oidc.cli_client_id, or use --token",
                ));
            }
            if meta.data.version != CLI_VERSION {
                checks.push(Check::warn(
                    "Version",
                    format!("CLI {} vs server {}", CLI_VERSION, meta.data.version),
                    "consider updating the CLI to match the server",
                ));
            } else {
                checks.push(Check::ok(
                    "Version",
                    format!("CLI and server on {}", CLI_VERSION),
                ));
            }
        }
        Err(err) => {
            checks.push(Check::fail(
                "Server reachable",
                format!("GET /api/v1/meta failed: {}", err),
                "check connectivity and the server URL",
            ));
        }
    }

    // ---- Auth ------------------------------------------------------------
    let token_present = token.is_some();
    let expiry = resolved.as_ref().and_then(|(_, c)| c.token_expires_at);
    if !token_present {
        checks.push(Check::fail(
            "Auth token",
            "not set",
            "run `logchef auth` (or pass --token)",
        ));
    } else if let Some(expiry) = expiry {
        if expiry < chrono::Utc::now() {
            checks.push(Check::fail(
                "Auth token",
                format!(
                    "expired {}",
                    expiry.to_rfc3339_opts(chrono::SecondsFormat::Secs, true)
                ),
                "run `logchef auth` to sign in again",
            ));
        } else {
            checks.push(Check::ok(
                "Auth token",
                format!(
                    "valid until {}",
                    expiry.to_rfc3339_opts(chrono::SecondsFormat::Secs, true)
                ),
            ));
        }
    } else {
        checks.push(Check::ok("Auth token", "set"));
    }

    // Validate identity via GET /api/v1/me when we have a token and the server
    // answered meta (avoids a guaranteed-failing call on an unreachable host).
    if token_present && meta.is_ok() {
        match client.get_current_user().await {
            Ok(user) => checks.push(Check::ok(
                "Identity",
                format!("{} ({})", user.email, user.role),
            )),
            Err(err) => checks.push(Check::fail(
                "Identity",
                format!("GET /api/v1/me failed: {}", err),
                "token may be invalid — run `logchef auth`",
            )),
        }
    }

    // ---- Defaults --------------------------------------------------------
    // Only meaningful when authenticated and the server is up.
    let can_resolve = token_present && meta.is_ok();
    check_defaults(
        &client,
        resolved.as_ref().map(|(_, c)| c),
        can_resolve,
        &mut checks,
    )
    .await;

    finish(checks, args.json)
}

/// Resolves which context doctor should inspect, honoring --context and
/// --server overrides, then the current context. Returns None when nothing is
/// configured (so the network checks degrade gracefully).
fn resolve_context(config: &Config, global: &GlobalArgs) -> Option<(String, CtxConfig)> {
    if let Some(name) = &global.context {
        return config.get_context(name).map(|c| (name.clone(), c.clone()));
    }
    if let Some(url) = &global.server {
        if let Some((name, ctx)) = config.find_context_by_url(url) {
            return Some((name.to_string(), ctx.clone()));
        }
        return Some(("(ephemeral)".to_string(), CtxConfig::new(url.clone())));
    }
    let name = config.current_context_name()?.to_string();
    config.current_context().map(|c| (name, c.clone()))
}

async fn check_defaults(
    client: &Client,
    ctx: Option<&CtxConfig>,
    can_resolve: bool,
    checks: &mut Vec<Check>,
) {
    let Some(ctx) = ctx else {
        return;
    };
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();

    let Some(team) = default_team else {
        checks.push(Check::warn(
            "Default team",
            "not set",
            "set one with `logchef config set team <id|name>` to drop -t",
        ));
        // Source without a team can't be resolved; note it.
        if default_source.is_some() {
            checks.push(Check::warn(
                "Default source",
                "set, but no default team to resolve it against",
                "also set a default team",
            ));
        } else {
            checks.push(Check::warn(
                "Default source",
                "not set",
                "set one with `logchef config set source <id|name>` to drop -S",
            ));
        }
        return;
    };

    if !can_resolve {
        checks.push(Check::warn(
            "Default team",
            format!("{} (not verified — server/auth unavailable)", team),
            "resolve once the server is reachable and you're signed in",
        ));
        return;
    }

    // Resolve the team against the live list.
    let teams = match client.list_teams().await {
        Ok(teams) => teams,
        Err(err) => {
            checks.push(Check::fail(
                "Default team",
                format!("could not list teams: {}", err),
                "check auth/connectivity",
            ));
            return;
        }
    };
    let team_id = match parse_id(&team) {
        Some(id) => teams.iter().find(|t| t.id == id).map(|t| t.id),
        None => teams
            .iter()
            .find(|t| t.name.eq_ignore_ascii_case(&team))
            .map(|t| t.id),
    };
    let Some(team_id) = team_id else {
        checks.push(Check::fail(
            "Default team",
            format!("'{}' not found among your teams", team),
            "pick one from `logchef teams`, then `logchef config set team <id|name>`",
        ));
        return;
    };
    checks.push(Check::ok(
        "Default team",
        format!("{} (id {})", team, team_id),
    ));

    let Some(source) = default_source else {
        checks.push(Check::warn(
            "Default source",
            "not set",
            "set one with `logchef config set source <id|name>` to drop -S",
        ));
        return;
    };

    let sources = match client.list_sources(team_id).await {
        Ok(sources) => sources,
        Err(err) => {
            checks.push(Check::fail(
                "Default source",
                format!("could not list sources: {}", err),
                "check the team is correct and you have access",
            ));
            return;
        }
    };
    let matched = match parse_id(&source) {
        Some(id) => sources.iter().find(|s| s.id == id),
        None => sources
            .iter()
            .find(|s| s.name.eq_ignore_ascii_case(&source))
            .or_else(|| {
                sources.iter().find(|s| {
                    s.target_ref()
                        .map(|r| r.eq_ignore_ascii_case(&source))
                        .unwrap_or(false)
                })
            }),
    };
    match matched {
        Some(s) => checks.push(Check::ok(
            "Default source",
            format!("{} (id {}, {})", source, s.id, s.source_type_label()),
        )),
        None => checks.push(Check::fail(
            "Default source",
            format!("'{}' not found in team {}", source, team_id),
            "pick one from `logchef sources -t <team>`, then `logchef config set source <id|name>`",
        )),
    }
}

fn parse_id(value: &str) -> Option<i64> {
    value.trim().parse::<i64>().ok()
}

/// Prints the report (text or JSON) and returns after setting the process exit
/// code: 1 if any check failed, 0 otherwise (warnings are OK).
fn finish(checks: Vec<Check>, json: bool) -> Result<()> {
    let any_fail = checks.iter().any(|c| c.status == Status::Fail);
    let any_warn = checks.iter().any(|c| c.status == Status::Warn);

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&checks).context("Failed to serialize checks")?
        );
    } else {
        let color = std::io::stdout().is_terminal();
        let width = checks.iter().map(|c| c.check.len()).max().unwrap_or(0);
        for c in &checks {
            let glyph = if color {
                format!("{}{}\x1b[0m", c.status.color(), c.status.glyph())
            } else {
                c.status.glyph().to_string()
            };
            println!("{} {:<width$}  {}", glyph, c.check, c.detail, width = width);
            if let Some(hint) = &c.hint {
                println!("  {:<width$}  → {}", "", hint, width = width);
            }
        }

        println!();
        if any_fail {
            println!("Found problems. Fix the ✗ items above and re-run `logchef doctor`.");
        } else if any_warn {
            println!("All good, with some warnings.");
        } else {
            println!("All checks passed. You're ready to query.");
        }
    }

    if any_fail {
        std::process::exit(1);
    }
    Ok(())
}
