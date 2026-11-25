use anyhow::{Context, Result, anyhow, bail};
use chrono::{DateTime, Utc};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::{AuditEvent, AuditEventFilter};

use crate::cli::GlobalArgs;
use crate::commands::parse_lookback;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Every source change in the last 24 hours
  logchef audit list --action source. --since 24h

  # One source's history as JSON lines
  logchef audit list --target source:7 --output jsonl

  # Failed sign-ins over the last week
  logchef audit list --action session.login --outcome failure --since 7d

The audit log is for global admins. Use an API token with the audit:read scope
(pass it with --token or LOGCHEF_AUTH_TOKEN). A `logchef auth` sign-in cannot
read it.")]
pub struct AuditArgs {
    #[command(subcommand)]
    command: AuditCommand,
}

#[derive(Subcommand)]
enum AuditCommand {
    /// List audit events, newest first.
    List(ListArgs),
}

#[derive(Args)]
struct ListArgs {
    /// Start of the range: a lookback such as 24h or 7d, or an RFC 3339
    /// time. Default: 7 days before --until.
    #[arg(long)]
    since: Option<String>,

    /// End of the range, in the same forms as --since. Default: now.
    #[arg(long)]
    until: Option<String>,

    /// An exact action such as source.update, or a resource with a trailing
    /// dot such as source.
    #[arg(long)]
    action: Option<String>,

    /// User ID of the actor.
    #[arg(long)]
    actor: Option<i64>,

    /// Target type, or type:id such as source:7.
    #[arg(long)]
    target: Option<String>,

    /// Outcome.
    #[arg(long)]
    outcome: Option<Outcome>,

    /// Maximum number of events. The CLI pages through the results.
    #[arg(long, short = 'l', default_value_t = 100)]
    limit: usize,

    /// Output format.
    #[arg(long, default_value = "table")]
    output: OutputFormat,
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum Outcome {
    Success,
    Failure,
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum OutputFormat {
    Table,
    Json,
    Jsonl,
}

pub async fn run(args: AuditArgs, global: GlobalArgs) -> Result<()> {
    match args.command {
        AuditCommand::List(args) => list(args, global).await,
    }
}

async fn list(args: ListArgs, global: GlobalArgs) -> Result<()> {
    if args.limit == 0 {
        bail!("--limit must be at least 1");
    }
    let filter = build_filter(&args, Utc::now())?;
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;

    let events = s
        .client
        .list_audit_events(&filter, args.limit)
        .await
        .map_err(explain_forbidden)?;

    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&events)?),
        OutputFormat::Jsonl => {
            for event in &events {
                println!("{}", serde_json::to_string(event)?);
            }
        }
        OutputFormat::Table => print!("{}", render_table(&events)),
    }
    Ok(())
}

fn build_filter(args: &ListArgs, now: DateTime<Utc>) -> Result<AuditEventFilter> {
    let (target_type, target_id) = match args.target.as_deref() {
        None => (None, None),
        Some(target) => parse_target(target)?,
    };
    Ok(AuditEventFilter {
        from: args
            .since
            .as_deref()
            .map(|s| parse_time(s, now))
            .transpose()?,
        to: args
            .until
            .as_deref()
            .map(|s| parse_time(s, now))
            .transpose()?,
        action: args.action.clone(),
        actor_id: args.actor,
        target_type,
        target_id,
        outcome: args.outcome.map(|outcome| match outcome {
            Outcome::Success => "success".to_owned(),
            Outcome::Failure => "failure".to_owned(),
        }),
    })
}

/// Reads an RFC 3339 time, or a lookback such as 24h counted back from now.
fn parse_time(value: &str, now: DateTime<Utc>) -> Result<DateTime<Utc>> {
    if let Ok(time) = DateTime::parse_from_rfc3339(value) {
        return Ok(time.with_timezone(&Utc));
    }
    let lookback = parse_lookback(value).map_err(|_| {
        anyhow!("'{value}' is neither a lookback such as 24h or 7d nor an RFC 3339 time")
    })?;
    Ok(now - lookback)
}

fn parse_target(value: &str) -> Result<(Option<String>, Option<String>)> {
    match value.split_once(':') {
        None if !value.is_empty() => Ok((Some(value.to_owned()), None)),
        Some((kind, id)) if !kind.is_empty() && !id.is_empty() => {
            Ok((Some(kind.to_owned()), Some(id.to_owned())))
        }
        _ => bail!("--target takes a type such as source, or type:id such as source:7"),
    }
}

/// The server refuses the audit log with a 403 to anyone but a global admin,
/// to an API token without audit:read, and to every OAuth sign-in. The
/// generic 403 hint (team/source access, sign in again) would mislead, so the
/// error is replaced.
fn explain_forbidden(err: logchef_core::Error) -> anyhow::Error {
    match err {
        logchef_core::Error::Api {
            status: Some(403),
            message,
            ..
        } => anyhow!(
            "The server refused the audit log: {message}\n\
             Reading it needs a global admin's API token with the audit:read scope (or *). \
             A `logchef auth` sign-in cannot read it. Create one on your Profile page, \
             then pass it with --token or LOGCHEF_AUTH_TOKEN."
        ),
        other => anyhow::Error::new(other).context("Failed to list audit events"),
    }
}

fn render_table(events: &[AuditEvent]) -> String {
    if events.is_empty() {
        return "No audit events found.\n".to_owned();
    }
    let mut out = format!(
        "{:<24} {:<30} {:<28} {:<30} {:<8} IP\n{}\n",
        "TIME (UTC)",
        "ACTOR",
        "ACTION",
        "TARGET",
        "OUTCOME",
        "-".repeat(130)
    );
    for event in events {
        out.push_str(&format!(
            "{:<24} {:<30} {:<28} {:<30} {:<8} {}\n",
            event.occurred_at.format("%Y-%m-%d %H:%M:%S%.3f"),
            ui::truncate(&actor(event), 30),
            ui::truncate(&event.action, 28),
            ui::truncate(&target(event), 30),
            event.outcome,
            event.ip,
        ));
    }
    out.push_str(&format!("\n{} events\n", events.len()));
    out
}

/// The actor label, with the auth method when it is not a browser session.
fn actor(event: &AuditEvent) -> String {
    let label = if event.actor_label.is_empty() {
        &event.actor_type
    } else {
        &event.actor_label
    };
    if event.auth_method == "session" {
        label.clone()
    } else {
        format!("{label} ({})", event.auth_method)
    }
}

fn target(event: &AuditEvent) -> String {
    let name = if event.target_label.is_empty() {
        &event.target_id
    } else {
        &event.target_label
    };
    format!("{}:{}", event.target_type, name)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn now() -> DateTime<Utc> {
        "2026-11-16T12:00:00Z".parse().expect("parse now")
    }

    fn args(since: Option<&str>, target: Option<&str>) -> ListArgs {
        ListArgs {
            since: since.map(str::to_owned),
            until: None,
            action: Some("source.".to_owned()),
            actor: Some(3),
            target: target.map(str::to_owned),
            outcome: Some(Outcome::Failure),
            limit: 100,
            output: OutputFormat::Table,
        }
    }

    #[test]
    fn since_takes_a_lookback_or_an_rfc3339_time() {
        let lookback = build_filter(&args(Some("24h"), None), now()).expect("lookback");
        assert_eq!(
            lookback.from,
            Some("2026-11-15T12:00:00Z".parse().expect("parse"))
        );
        let absolute =
            build_filter(&args(Some("2026-11-10T09:00:00+05:30"), None), now()).expect("rfc3339");
        assert_eq!(
            absolute.from,
            Some("2026-11-10T03:30:00Z".parse().expect("parse"))
        );
        assert!(build_filter(&args(Some("yesterday"), None), now()).is_err());
    }

    #[test]
    fn target_takes_a_type_or_type_and_id() {
        let filter = build_filter(&args(None, Some("source:7")), now()).expect("type:id");
        assert_eq!(filter.target_type.as_deref(), Some("source"));
        assert_eq!(filter.target_id.as_deref(), Some("7"));
        assert_eq!(filter.outcome.as_deref(), Some("failure"));
        assert_eq!(filter.actor_id, Some(3));
        assert_eq!(filter.from, None);

        let kind = build_filter(&args(None, Some("team")), now()).expect("type");
        assert_eq!(kind.target_type.as_deref(), Some("team"));
        assert_eq!(kind.target_id, None);

        for bad in [":7", "source:", ""] {
            assert!(
                build_filter(&args(None, Some(bad)), now()).is_err(),
                "{bad}"
            );
        }
    }

    #[test]
    fn a_403_names_the_audit_read_token() {
        let err = explain_forbidden(logchef_core::Error::api(Some(403), "Admin access required"));
        let text = format!("{err:#}");
        assert!(text.contains("audit:read"), "{text}");
        assert!(text.contains("Admin access required"), "{text}");
        assert_eq!(crate::ui::hint_for_error(&err), None);
    }
}
