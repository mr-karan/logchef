use anyhow::{Context, Result, anyhow, bail};
use chrono::{DateTime, Utc};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::{Report, ReportRun, ReportSchedule};
use logchef_core::auth::API_TOKENS_PAGE;

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = format!("EXAMPLES:
  # Your reports, with their next and last run
  logchef reports list

  # Reports other people send to you
  logchef reports list --scope received

  # The last 20 runs of report 4, with panel and delivery counts
  logchef reports runs 4

  # Send report 4 to yourself now, then to every recipient
  logchef reports run 4
  logchef reports run 4 --all

Reports need an API token with the reports:read scope (reports:write for
`run`), or *. Pass it with --token or LOGCHEF_AUTH_TOKEN. Create one under
{API_TOKENS_PAGE}. A `logchef auth`
sign-in cannot reach reports. Create and edit reports in the web UI."))]
pub struct ReportsArgs {
    #[command(subcommand)]
    command: ReportsCommand,
}

#[derive(Subcommand)]
enum ReportsCommand {
    /// List reports: yours, the ones sent to you, or all (global admins).
    List(ListArgs),
    /// List the runs of a report, newest first. Owner or global admin.
    Runs(RunsArgs),
    /// Start a manual run of a report. Owner or global admin.
    Run(RunArgs),
}

#[derive(Args)]
struct ListArgs {
    /// Which reports to list.
    #[arg(long, default_value = "mine")]
    scope: Scope,

    /// Output format.
    #[arg(long, default_value = "table")]
    output: OutputFormat,
}

#[derive(Args)]
struct RunsArgs {
    /// Report ID.
    id: i64,

    /// Maximum number of runs, 1 to 100.
    #[arg(long, short = 'l', default_value_t = 20)]
    limit: u32,

    /// Output format.
    #[arg(long, default_value = "table")]
    output: OutputFormat,
}

#[derive(Args)]
struct RunArgs {
    /// Report ID.
    id: i64,

    /// Send to every recipient instead of only to you.
    #[arg(long)]
    all: bool,
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum Scope {
    /// Reports you own.
    Mine,
    /// Reports sent to you that you do not own.
    Received,
    /// Every report. Global admins only.
    All,
}

impl Scope {
    fn as_str(self) -> &'static str {
        match self {
            Scope::Mine => "mine",
            Scope::Received => "received",
            Scope::All => "all",
        }
    }
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum OutputFormat {
    Table,
    Json,
}

pub async fn run(args: ReportsArgs, global: GlobalArgs) -> Result<()> {
    match args.command {
        ReportsCommand::List(args) => list(args, global).await,
        ReportsCommand::Runs(args) => runs(args, global).await,
        ReportsCommand::Run(args) => start(args, global).await,
    }
}

async fn list(args: ListArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let reports = s
        .client
        .list_reports(args.scope.as_str())
        .await
        .map_err(|err| explain(err, "list reports", None, "reports:read"))?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&reports)?),
        OutputFormat::Table => print!("{}", render_reports(&reports)),
    }
    Ok(())
}

async fn runs(args: RunsArgs, global: GlobalArgs) -> Result<()> {
    if !(1..=100).contains(&args.limit) {
        bail!("--limit must be 1 to 100");
    }
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let runs = s
        .client
        .list_report_runs(args.id, args.limit)
        .await
        .map_err(|err| explain(err, "read the run history", Some(args.id), "reports:read"))?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&runs)?),
        OutputFormat::Table => print!("{}", render_runs(&runs)),
    }
    Ok(())
}

async fn start(args: RunArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let run = s
        .client
        .run_report(args.id, args.all)
        .await
        .map_err(|err| explain(err, "start the run", Some(args.id), "reports:write"))?;
    let to = if args.all { "every recipient" } else { "you" };
    println!(
        "Started run {} of report {}, sending to {to}. It finishes in the background; \
         check it with `logchef reports runs {}`.",
        run.id, run.report_id, run.report_id
    );
    Ok(())
}

/// A 403 on the report routes means the token lacks the scope, the
/// credential is an OAuth sign-in (which holds no report scope), or a
/// non-admin asked for `--scope all`. A 404 on one report also hides a
/// report the caller may not edit. The generic hints (sign in again,
/// team/source access) would mislead, so both errors are replaced, and a
/// missing scope's message too, since the advice here says it. `action`
/// completes "Failed to ...", and `report` is None for the list.
fn explain(
    err: logchef_core::Error,
    action: &str,
    report: Option<i64>,
    scope: &str,
) -> anyhow::Error {
    let scope_denied = err.is_scope_denied();
    match (err, report) {
        (
            logchef_core::Error::Api {
                status: Some(403),
                message,
                ..
            },
            _,
        ) => {
            let admin = if report.is_none() {
                " `--scope all` also needs a global admin."
            } else {
                ""
            };
            let reason = if scope_denied {
                ".".to_owned()
            } else {
                format!(": {message}")
            };
            anyhow!(
                "The server refused to {action}{reason}\n\
                 It needs an API token with the {scope} scope (or *). A `logchef auth` sign-in \
                 cannot reach reports. Create one under {API_TOKENS_PAGE}, then pass it with \
                 --token or LOGCHEF_AUTH_TOKEN.{admin}"
            )
        }
        (
            logchef_core::Error::Api {
                status: Some(404), ..
            },
            Some(id),
        ) => anyhow!(
            "Report {id} was not found, or you are not its owner or a global admin. \
             Only they read its runs or start one. List yours with `logchef reports list`."
        ),
        (other, _) => anyhow::Error::new(other).context(format!("Failed to {action}")),
    }
}

fn render_reports(reports: &[Report]) -> String {
    if reports.is_empty() {
        return "No reports found.\n".to_owned();
    }
    let mut out = format!(
        "{:<6} {:<28} {:<24} {:<36} {:<18} {:<30} OWNER\n{}\n",
        "ID",
        "NAME",
        "SOURCE",
        "SCHEDULE",
        "NEXT RUN (UTC)",
        "LAST RUN",
        "-".repeat(160)
    );
    for r in reports {
        let next = match (&r.next_run_at, r.state.as_str()) {
            (Some(at), _) => minute(at),
            (None, "paused") => format!("paused ({})", r.pause_reason),
            (None, _) => "-".to_owned(),
        };
        let last = r
            .last_run
            .as_ref()
            .map(|run| match run.reason.as_str() {
                "" => run.status.clone(),
                reason => format!("{}/{reason}", run.status),
            })
            .unwrap_or_else(|| "-".to_owned());
        let owner = r
            .owner
            .as_ref()
            .map(|o| {
                if o.name.is_empty() {
                    o.email.clone()
                } else {
                    o.name.clone()
                }
            })
            .unwrap_or_else(|| "no owner".to_owned());
        out.push_str(&format!(
            "{:<6} {:<28} {:<24} {:<36} {:<18} {:<30} {}\n",
            r.id,
            ui::truncate(&r.name, 28),
            ui::truncate(&r.source.name, 24),
            ui::truncate(&format!("{} {}", schedule(&r.schedule), r.timezone), 36),
            next,
            ui::truncate(&last, 30),
            owner,
        ));
    }
    out.push_str(&format!("\n{} reports\n", reports.len()));
    out
}

fn render_runs(runs: &[ReportRun]) -> String {
    if runs.is_empty() {
        return "No runs yet.\n".to_owned();
    }
    let mut out = format!(
        "{:<8} {:<18} {:<9} {:<12} {:<22} {:<12} DELIVERIES\n{}\n",
        "ID",
        "STARTED (UTC)",
        "TRIGGER",
        "STATUS",
        "REASON",
        "PANELS",
        "-".repeat(100)
    );
    for run in runs {
        let panels_ok = run
            .panels
            .iter()
            .filter(|p| matches!(p.status.as_str(), "ok" | "empty"))
            .count();
        let sent = run.deliveries.iter().filter(|d| d.status == "sent").count();
        let panels = format!("{panels_ok}/{} ok", run.panels.len());
        let deliveries = format!("{sent}/{} sent", run.deliveries.len());
        let reason = if run.reason.is_empty() {
            "-"
        } else {
            &run.reason
        };
        out.push_str(&format!(
            "{:<8} {:<18} {:<9} {:<12} {:<22} {:<12} {deliveries}\n",
            run.id,
            minute(&run.started_at),
            run.trigger,
            run.status,
            ui::truncate(reason, 22),
            panels,
        ));
    }
    out
}

fn minute(at: &DateTime<Utc>) -> String {
    at.format("%Y-%m-%d %H:%M").to_string()
}

/// The schedule in the words of the web editor's presets, without the
/// timezone.
fn schedule(s: &ReportSchedule) -> String {
    const DAYS: [&str; 7] = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
    let at = format!("{:02}:{:02}", s.hour.unwrap_or(0), s.minute);
    match s.frequency.as_str() {
        "hourly" => format!("hourly at :{:02}", s.minute),
        "daily" => format!("daily {at}"),
        "weekly" if s.weekdays == [1, 2, 3, 4, 5] => format!("weekdays {at}"),
        "weekly" => {
            let days: Vec<&str> = s
                .weekdays
                .iter()
                .filter_map(|d| DAYS.get(*d as usize).copied())
                .collect();
            format!("weekly {} {at}", days.join(","))
        }
        "monthly" => format!("monthly day {} {at}", s.day_of_month.unwrap_or(1)),
        other => other.to_owned(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn schedule_of(value: serde_json::Value) -> String {
        schedule(&serde_json::from_value(value).expect("parse schedule"))
    }

    #[test]
    fn schedules_read_as_the_editor_presets() {
        assert_eq!(
            schedule_of(serde_json::json!({"frequency": "hourly", "minute": 5})),
            "hourly at :05"
        );
        assert_eq!(
            schedule_of(serde_json::json!({"frequency": "daily", "minute": 0, "hour": 9})),
            "daily 09:00"
        );
        assert_eq!(
            schedule_of(
                serde_json::json!({"frequency": "weekly", "minute": 30, "hour": 8, "weekdays": [1, 2, 3, 4, 5]})
            ),
            "weekdays 08:30"
        );
        assert_eq!(
            schedule_of(
                serde_json::json!({"frequency": "weekly", "minute": 0, "hour": 18, "weekdays": [5, 0]})
            ),
            "weekly Fri,Sun 18:00"
        );
        assert_eq!(
            schedule_of(
                serde_json::json!({"frequency": "monthly", "minute": 0, "hour": 6, "day_of_month": 1})
            ),
            "monthly day 1 06:00"
        );
    }

    #[test]
    fn a_403_names_the_scope_the_command_needs() {
        for (scope, report) in [("reports:read", None), ("reports:write", Some(4))] {
            let err = explain(
                logchef_core::Error::api(Some(403), "API token does not have the required scope"),
                "start the run",
                report,
                scope,
            );
            let text = format!("{err:#}");
            assert!(text.contains(scope), "{text}");
            assert!(text.contains("logchef auth"), "{text}");
            assert!(text.contains("does not have the required scope"), "{text}");
            assert_eq!(crate::ui::hint_for_error(&err), None, "{text}");
        }
    }

    #[test]
    fn a_404_on_one_report_says_who_may_see_it() {
        let err = explain(
            logchef_core::Error::api(Some(404), "Report not found"),
            "read the run history",
            Some(4),
            "reports:read",
        );
        let text = format!("{err:#}");
        assert!(
            text.contains("Report 4") && text.contains("owner"),
            "{text}"
        );
        assert_eq!(crate::ui::hint_for_error(&err), None, "{text}");
    }

    #[test]
    fn other_errors_keep_their_cause() {
        let err = explain(
            logchef_core::Error::api(Some(409), "A run of this report is in flight"),
            "start the run",
            Some(4),
            "reports:write",
        );
        assert!(format!("{err:#}").contains("in flight"));
    }
}
