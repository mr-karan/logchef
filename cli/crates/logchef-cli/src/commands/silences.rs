use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::AlertSilence;
use logchef_core::cache::Cache;

use crate::cli::GlobalArgs;
use crate::commands::resolve_source_id;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # The silences of a source, and whether each mutes now
  logchef silences list -t platform -S app-logs

  # As JSON
  logchef silences list -S 2 --output json

Create and edit silences in the web UI.")]
pub struct SilencesArgs {
    #[command(subcommand)]
    command: SilencesCommand,
}

#[derive(Subcommand)]
enum SilencesCommand {
    /// List the alert silences of a source.
    List(ListArgs),
}

#[derive(Args)]
struct ListArgs {
    /// Team ID or name, to find a source by name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
}

pub async fn run(args: SilencesArgs, global: GlobalArgs) -> Result<()> {
    match args.command {
        SilencesCommand::List(args) => list(args, global).await,
    }
}

async fn list(args: ListArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let source = args
        .source
        .or_else(|| s.ctx.defaults.source_with_env())
        .ok_or_else(|| {
            anyhow::anyhow!(
                "Source not specified. Use --source or set defaults.source. List sources with 'logchef sources --team <team>'."
            )
        })?;
    let team = args.team.or_else(|| s.ctx.defaults.team_with_env());
    let mut cache = Cache::new(&s.ctx.server_url);
    let source_id = resolve_source_id(&s.client, &mut cache, team, source).await?;
    let silences = s
        .client
        .list_silences(source_id)
        .await
        .context("Failed to list silences")?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&silences.raw)?),
        OutputFormat::Text => print!("{}", render(&silences.parsed)),
    }
    Ok(())
}

fn render(silences: &[AlertSilence]) -> String {
    if silences.is_empty() {
        return "No silences found.\n".to_owned();
    }
    let mut out = format!(
        "{:<6} {:<10} {:<30} {:<58} {:<7} COMMENT\n{}\n",
        "ID",
        "RULE",
        "MATCHERS",
        "SCHEDULE",
        "ACTIVE",
        "-".repeat(142)
    );
    for silence in silences {
        let rule = silence
            .alert_id
            .map_or("all rules".to_owned(), |id| id.to_string());
        let matchers = if silence.matchers.is_empty() {
            "(every instance)".to_owned()
        } else {
            silence
                .matchers
                .iter()
                .map(|(k, v)| format!("{k}={v}"))
                .collect::<Vec<_>>()
                .join(", ")
        };
        out.push_str(&format!(
            "{:<6} {:<10} {:<30} {:<58} {:<7} {}\n",
            silence.id,
            rule,
            ui::truncate(&matchers, 30),
            ui::truncate(&schedule(silence), 58),
            if silence.active { "yes" } else { "no" },
            ui::truncate(&silence.comment, 40)
        ));
    }
    out
}

fn schedule(silence: &AlertSilence) -> String {
    const DAYS: [&str; 7] = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
    let at = |t: &Option<chrono::DateTime<chrono::Utc>>| {
        t.map_or("?".to_owned(), |t| t.format("%Y-%m-%d %H:%M").to_string())
    };
    match (&silence.weekly, silence.schedule_kind.as_str()) {
        (Some(w), "weekly") => {
            let days: Vec<&str> = w
                .weekdays
                .iter()
                .filter_map(|d| DAYS.get(usize::from(*d)).copied())
                .collect();
            format!(
                "weekly {} {} +{}m {}",
                days.join(","),
                w.start,
                w.duration_minutes,
                w.timezone
            )
        }
        (_, "once") => format!(
            "once {} to {} UTC",
            at(&silence.starts_at),
            at(&silence.ends_at)
        ),
        (_, other) => other.to_owned(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn schedules_read_as_once_or_weekly() {
        let silences: Vec<AlertSilence> = serde_json::from_value(serde_json::json!([
            {"id": 1, "matchers": {}, "comment": "", "schedule_kind": "once",
             "starts_at": "2026-11-25T08:00:00Z", "ends_at": "2026-11-25T10:30:00Z", "active": true},
            {"id": 2, "alert_id": 4, "matchers": {"service": "api"}, "comment": "deploys",
             "schedule_kind": "weekly",
             "weekly": {"weekdays": [1, 3], "start": "22:00", "duration_minutes": 90,
                        "timezone": "Asia/Kolkata"},
             "active": false}
        ]))
        .unwrap();
        assert_eq!(
            schedule(&silences[0]),
            "once 2026-11-25 08:00 to 2026-11-25 10:30 UTC"
        );
        assert_eq!(
            schedule(&silences[1]),
            "weekly Mon,Wed 22:00 +90m Asia/Kolkata"
        );
    }
}
