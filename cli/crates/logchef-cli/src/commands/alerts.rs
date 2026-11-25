use anyhow::{Context, Result, bail};
use chrono::{DateTime, SecondsFormat, TimeZone, Utc};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::{Alert, AlertTestResult, BacktestPoint, BacktestResult};
use logchef_core::cache::Cache;
use logchef_core::timerange::resolve_timezone;
use std::collections::BTreeMap;
use std::fmt::Display;
use std::io::Read;

use crate::cli::GlobalArgs;
use crate::commands::resolve_source_id;
use crate::commands::sql::wall_clock_to_utc;
use crate::session;
use crate::ui;

/// The server stops a backtest after 60 seconds; the request outlives it.
const BACKTEST_TIMEOUT_SECS: u64 = 75;

/// The shortest and longest range the server replays.
const BACKTEST_MIN_RANGE_SECS: i64 = 3600;
const BACKTEST_MAX_RANGE_SECS: i64 = 7 * 24 * 3600;

/// The most episodes the text output lists.
const MAX_LISTED_EPISODES: usize = 20;

/// How the text output writes an instant: the wall-clock form `--from` and
/// `--to` read, so an episode can be replayed as given.
const WALL_CLOCK: &str = "%Y-%m-%d %H:%M:%S";

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # The rules you can see, of every source or of one
  logchef alerts list
  logchef alerts list -t platform -S app-logs

  # Evaluate a rule once without saving it
  logchef alerts test -f rule.json -S app-logs

  # Replay a rule over the past day, evaluating every 5 minutes, from stdin
  cat rule.json | logchef alerts backtest -f - --range 24h --frequency 300

  # Replay one past window, in the effective timezone
  logchef alerts backtest -f rule.json --from '2026-11-24 09:00:00' --to '2026-11-24 15:00:00'

A request file holds the JSON body of POST /api/v1/alerts/test, such as
{\"source_id\": 2, \"query_language\": \"logchefql\",
 \"query\": \"level=\\\"error\\\" | count() as value by service\",
 \"lookback_seconds\": 300, \"threshold_operator\": \"gt\", \"threshold_value\": 10}.
A native ClickHouse rule sets \"query_language\": \"clickhouse-sql\" and reads its
window from {{__time_filter}}.
A missing source_id comes from -S (or the default source). backtest also
needs frequency_seconds and range_seconds, from the request or from
--frequency and --range (or --from and --to). A backtest evaluates at most
120 times, so a long range reads only part of it when the step outgrows the
lookback; replay a shorter window to read all of it. test and backtest are
dry runs: they save nothing and notify no one.")]
pub struct AlertsArgs {
    #[command(subcommand)]
    command: AlertsCommand,
}

#[derive(Subcommand)]
enum AlertsCommand {
    /// List alert rules, of one source or of every source you can see.
    List(ListArgs),
    /// Evaluate an unsaved rule once and show the value and samples.
    Test(TestArgs),
    /// Replay an unsaved rule over a past range and show what it would have
    /// sent.
    Backtest(BacktestArgs),
}

#[derive(Args)]
struct SourceArgs {
    /// Team ID or name, to find a source by name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,
}

#[derive(Args)]
struct ListArgs {
    #[command(flatten)]
    scope: SourceArgs,

    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Args)]
struct TestArgs {
    #[command(flatten)]
    scope: SourceArgs,

    /// JSON request body, a file path or - for stdin
    #[arg(long, short = 'f')]
    file: String,

    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Args)]
struct BacktestArgs {
    #[command(flatten)]
    test: TestArgs,

    /// The past range to replay; sets range_seconds
    #[arg(long, conflicts_with = "from")]
    range: Option<Range>,

    /// When the replayed range ends (default now); sets end_time.
    /// YYYY-MM-DD HH:MM:SS is wall-clock in the effective timezone; an
    /// RFC3339 instant needs none.
    #[arg(long, conflicts_with = "from")]
    end: Option<String>,

    /// Start of the window to replay, in the same forms as --end. With --to,
    /// sets end_time and range_seconds (1h to 7d).
    #[arg(long, requires = "to")]
    from: Option<String>,

    /// End of the window to replay, in the same forms as --end.
    #[arg(long, requires = "from")]
    to: Option<String>,

    /// Seconds between evaluations; sets frequency_seconds
    #[arg(long)]
    frequency: Option<u32>,
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum Range {
    #[value(name = "1h")]
    Hour,
    #[value(name = "6h")]
    SixHours,
    #[value(name = "24h")]
    Day,
    #[value(name = "7d")]
    Week,
}

impl Range {
    fn seconds(self) -> u32 {
        match self {
            Range::Hour => 3600,
            Range::SixHours => 6 * 3600,
            Range::Day => 24 * 3600,
            Range::Week => 7 * 24 * 3600,
        }
    }
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
}

pub async fn run(args: AlertsArgs, global: GlobalArgs) -> Result<()> {
    match args.command {
        AlertsCommand::List(args) => list(args, global).await,
        AlertsCommand::Test(args) => test(args, global).await,
        AlertsCommand::Backtest(args) => backtest(args, global).await,
    }
}

async fn list(args: ListArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let source = args
        .scope
        .source
        .or_else(|| s.ctx.defaults.source_with_env());
    let source_id = match source {
        Some(source) => {
            let team = args.scope.team.or_else(|| s.ctx.defaults.team_with_env());
            let mut cache = Cache::new(&s.ctx.server_url);
            Some(resolve_source_id(&s.client, &mut cache, team, source).await?)
        }
        None => None,
    };
    let alerts = s
        .client
        .list_alerts(source_id)
        .await
        .context("Failed to list alert rules")?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&alerts.raw)?),
        OutputFormat::Text => print!("{}", render_alerts(&alerts.parse()?)),
    }
    Ok(())
}

async fn test(args: TestArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let request = read_request(&args, &s).await?;
    let result = s
        .client
        .test_alert(&request)
        .await
        .context("Failed to test the alert rule")?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&result.raw)?),
        OutputFormat::Text => {
            let result = result.parse()?;
            for warning in &result.warnings {
                eprintln!("Warning: {warning}");
            }
            print!("{}", render_test(&result));
        }
    }
    Ok(())
}

async fn backtest(args: BacktestArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed_with_timeout(&config, &global, |ctx| {
        ctx.timeout_secs.max(BACKTEST_TIMEOUT_SECS)
    })?;
    let mut request = read_request(&args.test, &s).await?;
    let zone = resolve_timezone(s.ctx.defaults.timezone.as_deref());
    let instant = |value: &str| {
        wall_clock_to_utc(value, zone).with_context(|| format!("Invalid time '{value}'"))
    };
    let window = match (&args.from, &args.to, &args.end) {
        (Some(from), Some(to), _) => {
            let (from, to) = (instant(from)?, instant(to)?);
            Some((to, Some(window_seconds(from, to)?)))
        }
        (_, _, Some(end)) => Some((instant(end)?, None)),
        _ => None,
    };
    if let Some((end, range_seconds)) = window {
        request["end_time"] = end.to_rfc3339_opts(SecondsFormat::Secs, true).into();
        if let Some(range_seconds) = range_seconds {
            request["range_seconds"] = range_seconds.into();
        }
    }
    if let Some(range) = args.range {
        request["range_seconds"] = range.seconds().into();
    }
    if let Some(frequency) = args.frequency {
        request["frequency_seconds"] = frequency.into();
    }
    let result = s
        .client
        .backtest_alert(&request)
        .await
        .context("Failed to backtest the alert rule")?;
    match args.test.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&result.raw)?),
        OutputFormat::Text => {
            let result = result.parse()?;
            for warning in backtest_warnings(&result) {
                eprintln!("warning: {warning}");
            }
            let rule = RuleShape {
                lookback_seconds: request["lookback_seconds"].as_i64(),
                lowest_matters: matches!(
                    request["threshold_operator"].as_str(),
                    Some("lt" | "lte")
                ),
            };
            print!("{}", render_backtest(&result, &rule, &zone));
        }
    }
    Ok(())
}

/// The span from `from` to `to` in seconds, when it is one the server
/// replays.
fn window_seconds(from: DateTime<Utc>, to: DateTime<Utc>) -> Result<i64> {
    let seconds = (to - from).num_seconds();
    if seconds <= 0 {
        bail!("--to must be after --from");
    }
    if !(BACKTEST_MIN_RANGE_SECS..=BACKTEST_MAX_RANGE_SECS).contains(&seconds) {
        bail!(
            "--from to --to spans {}; a backtest replays 1h to 7d. Pick a window in that span.",
            duration(seconds)
        );
    }
    Ok(seconds)
}

/// The server's warnings. An older server sends none, so a range the
/// evaluations read only part of gets one here.
fn backtest_warnings(result: &BacktestResult) -> Vec<String> {
    if !result.warnings.is_empty() || result.coverage >= 1.0 {
        return result.warnings.clone();
    }
    vec![format!(
        "the evaluations read {} of the range, so an episode shorter than the {} step can be \
         missed. Replay a shorter window with --from/--to to read all of it.",
        percent(result.coverage),
        duration(result.step_seconds)
    )]
}

/// What the backtest table needs from the request: the server echoes neither.
struct RuleShape {
    lookback_seconds: Option<i64>,
    /// A `lt` or `lte` rule breaches on its lowest value.
    lowest_matters: bool,
}

/// Reads the JSON request object from the file or stdin, and fills a missing
/// `source_id` from -S or the default source.
async fn read_request(args: &TestArgs, s: &session::AuthedSession) -> Result<serde_json::Value> {
    let text = if args.file == "-" {
        let mut text = String::new();
        std::io::stdin()
            .read_to_string(&mut text)
            .context("Failed to read the request from stdin")?;
        text
    } else {
        std::fs::read_to_string(&args.file)
            .with_context(|| format!("Failed to read the request file '{}'", args.file))?
    };
    let mut request: serde_json::Value = serde_json::from_str(&text)
        .with_context(|| format!("The request in '{}' is not valid JSON", args.file))?;
    if !request.is_object() {
        bail!(
            "The request in '{}' must be a JSON object, the body of POST /api/v1/alerts/test",
            args.file
        );
    }
    if request.get("source_id").is_none() {
        let Some(source) = args
            .scope
            .source
            .clone()
            .or_else(|| s.ctx.defaults.source_with_env())
        else {
            bail!(
                "The request has no source_id. Add it to the request, or pass -S <source> \
                 (or set a default with `logchef config set source <id|name>`)."
            );
        };
        let team = args
            .scope
            .team
            .clone()
            .or_else(|| s.ctx.defaults.team_with_env());
        let mut cache = Cache::new(&s.ctx.server_url);
        request["source_id"] = resolve_source_id(&s.client, &mut cache, team, source)
            .await?
            .into();
    }
    Ok(request)
}

fn render_alerts(alerts: &[Alert]) -> String {
    if alerts.is_empty() {
        return "No alert rules found.\n".to_owned();
    }
    let mut out = format!(
        "{:<6} {:<30} {:<9} {:<34} {:<7} {:<30} SOURCE\n{}\n",
        "ID",
        "NAME",
        "SEVERITY",
        "STATE",
        "ACTIVE",
        "THRESHOLD",
        "-".repeat(126)
    );
    for alert in alerts {
        let state = match &alert.instance_counts {
            Some(c) => format!(
                "{} ({} firing, {} no data, {} ok)",
                alert.last_state, c.firing, c.no_data, c.ok
            ),
            None => alert.last_state.clone(),
        };
        let threshold = format!(
            "{} {} over {} every {}",
            operator(&alert.threshold_operator),
            alert.threshold_value,
            duration(alert.lookback_seconds),
            duration(alert.frequency_seconds)
        );
        out.push_str(&format!(
            "{:<6} {:<30} {:<9} {:<34} {:<7} {:<30} {}\n",
            alert.id,
            ui::truncate(&alert.name, 30),
            alert.severity,
            ui::truncate(&state, 34),
            yes_no(alert.is_active),
            threshold,
            alert.source_id
        ));
    }
    out
}

fn render_test(result: &AlertTestResult) -> String {
    let mut out = format!(
        "Value:          {}\nThreshold met:  {}\nNo data:        {}\nRows:           {}\nExecution:      {} ms\n",
        result.value,
        yes_no(result.threshold_met),
        yes_no(result.no_data),
        result.rows_returned,
        result.execution_time_ms
    );
    if result.samples.is_empty() {
        return out;
    }
    out.push_str(&format!(
        "\nSamples ({} of {} groups):\n{:<50} {:<14} MET\n{}\n",
        result.samples.len(),
        result.groups_total.max(result.samples.len() as i64),
        "GROUP",
        "VALUE",
        "-".repeat(70)
    ));
    for sample in &result.samples {
        out.push_str(&format!(
            "{:<50} {:<14} {}\n",
            ui::truncate(&group(&sample.group), 50),
            sample.value,
            yes_no(sample.threshold_met)
        ));
    }
    out
}

/// Renders the replay with its instants in `zone`, the zone `--from` and
/// `--to` are read in.
fn render_backtest<Z>(result: &BacktestResult, rule: &RuleShape, zone: &Z) -> String
where
    Z: TimeZone + Display,
    Z::Offset: Display,
{
    let local = |t: DateTime<Utc>| t.with_timezone(zone).format(WALL_CLOCK).to_string();
    let summary = &result.summary;
    let lookback = rule
        .lookback_seconds
        .map_or(String::new(), |s| format!(" (lookback {})", duration(s)));
    let whole_result = match summary.empty_result_episodes {
        0 => String::new(),
        n => format!(" ({n} whole-result no-data episodes, not in the groups table)"),
    };
    let mut out = format!(
        "Range:            {} to {} {zone}\n\
         Evaluated {} times, every {}{lookback}: coverage {} of the range\n\
         Episodes:         {}{whole_result}\n\
         Notifications:    {}\n\
         Firing time:      {}\n\
         Breaching evals:  {}\n\
         No-data evals:    {}\n\
         Errors:           {}\n\
         Partial:          {}\n",
        local(result.start),
        local(result.end),
        result.evaluations,
        duration(result.step_seconds),
        percent(result.coverage),
        summary.episodes,
        summary.notifications,
        duration(summary.firing_seconds),
        summary.breaching_evaluations,
        summary.no_data_evaluations,
        summary.errors,
        if result.partial {
            "yes, the server stopped the replay at its deadline"
        } else {
            "no"
        },
    );
    if let Some(error) = &result.first_error {
        out.push_str(&format!("First error:      {error}\n"));
    }
    let episodes = episodes(&result.series);
    if !episodes.is_empty() {
        out.push_str(&format!("\nFiring or no-data episodes ({zone}):\n"));
        for episode in episodes.iter().take(MAX_LISTED_EPISODES) {
            let end = episode
                .end
                .map_or("ongoing at the end of the range".to_owned(), local);
            out.push_str(&format!("{} to {end}\n", local(episode.start)));
        }
        if episodes.len() > MAX_LISTED_EPISODES {
            out.push_str(&format!(
                "... and {} more\n",
                episodes.len() - MAX_LISTED_EPISODES
            ));
        }
    }
    if result.groups.is_empty() {
        return out;
    }
    let omitted = match result.groups_omitted {
        0 => String::new(),
        n => format!(", {n} more not listed"),
    };
    // An older server sends no min_value; its table keeps MAX.
    let lowest = rule.lowest_matters && result.groups.iter().any(|g| g.min_value.is_some());
    out.push_str(&format!(
        "\nTop groups ({}{omitted}):\n{:<50} {:<9} {:<10} {}\n{}\n",
        result.groups.len(),
        "GROUP",
        "EPISODES",
        "BREACHING",
        if lowest { "MIN" } else { "MAX" },
        "-".repeat(80)
    ));
    for g in &result.groups {
        let value = if lowest { g.min_value } else { g.max_value };
        out.push_str(&format!(
            "{:<50} {:<9} {:<10} {}\n",
            ui::truncate(&group(&g.group), 50),
            g.episodes,
            g.breaching_evaluations,
            value.map_or("-".to_owned(), |v| v.to_string())
        ));
    }
    out
}

/// A run of firing or no-data evaluations, from its first one to the first
/// later `ok` one. An error evaluation keeps the states it found, so it
/// neither starts nor ends a run. `end` is None for a run the range ends.
struct Episode {
    start: DateTime<Utc>,
    end: Option<DateTime<Utc>>,
}

fn episodes(series: &[BacktestPoint]) -> Vec<Episode> {
    let mut episodes: Vec<Episode> = Vec::new();
    for point in series {
        let open = episodes.last_mut().filter(|e| e.end.is_none());
        match (point.status.as_str(), open) {
            ("firing" | "no_data", None) => episodes.push(Episode {
                start: point.t,
                end: None,
            }),
            ("ok", Some(episode)) => episode.end = Some(point.t),
            _ => {}
        }
    }
    episodes
}

/// A 0 to 1 share as a whole percent, rounded as the server's warnings round
/// it (Go's `%.0f`), so the summary and a warning agree.
fn percent(share: f64) -> String {
    format!("{:.0}%", share * 100.0)
}

fn group(labels: &BTreeMap<String, String>) -> String {
    if labels.is_empty() {
        return "(all)".to_owned();
    }
    labels
        .iter()
        .map(|(k, v)| format!("{k}={v}"))
        .collect::<Vec<_>>()
        .join(", ")
}

fn operator(op: &str) -> &str {
    match op {
        "gt" => ">",
        "gte" => ">=",
        "lt" => "<",
        "lte" => "<=",
        "eq" => "=",
        "neq" => "!=",
        other => other,
    }
}

fn yes_no(value: bool) -> &'static str {
    if value { "yes" } else { "no" }
}

/// `90` → `1m30s`, `3600` → `1h`, `0` → `0s`.
fn duration(seconds: i64) -> String {
    let (d, h, m, s) = (
        seconds / 86_400,
        seconds % 86_400 / 3600,
        seconds % 3600 / 60,
        seconds % 60,
    );
    let out: String = [(d, "d"), (h, "h"), (m, "m"), (s, "s")]
        .iter()
        .filter(|(n, _)| *n > 0)
        .map(|(n, unit)| format!("{n}{unit}"))
        .collect();
    if out.is_empty() { "0s".to_owned() } else { out }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn durations_read_as_units() {
        assert_eq!(duration(0), "0s");
        assert_eq!(duration(90), "1m30s");
        assert_eq!(duration(3600), "1h");
        assert_eq!(duration(7 * 86_400 + 60), "7d1m");
    }

    #[test]
    fn coverage_rounds_as_the_server_warning_does() {
        // Go's %.0f of 100*coverage: nearest, an exact half to even.
        assert_eq!(percent(0.4167), "42%");
        assert_eq!(percent(0.0595), "6%");
        assert_eq!(percent(0.125), "12%");
    }

    #[test]
    fn a_rule_row_shows_state_counts_and_threshold() {
        let alerts: Vec<Alert> = serde_json::from_value(serde_json::json!([{
            "id": 4, "source_id": 2, "name": "5xx burst", "severity": "critical",
            "threshold_operator": "gte", "threshold_value": 2.5, "lookback_seconds": 300,
            "frequency_seconds": 60, "is_active": true, "last_state": "firing",
            "instance_counts": {"firing": 2, "no_data": 0, "ok": 5}
        }]))
        .unwrap();
        let text = render_alerts(&alerts);
        let row = text.lines().nth(2).unwrap();
        assert!(row.contains("firing (2 firing, 0 no data, 5 ok)"), "{row}");
        assert!(row.contains(">= 2.5 over 5m every 1m"), "{row}");
        assert!(row.ends_with(" 2"), "{row}");
    }
}
