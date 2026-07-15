use anyhow::{Context, Result};
use chrono::{DateTime, NaiveDateTime, TimeZone, Utc};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{HistogramBucket, HistogramRequest, TranslateRequest};
use logchef_core::cache::Cache;
use logchef_core::timerange::{TimeInput, resolve_time_range, resolve_timezone};

use crate::cli::GlobalArgs;
use crate::commands::{parse_lookback, resolve_source, resolve_team};
use crate::session;
use crate::ui;

const WALL_CLOCK_FORMAT: &str = "%Y-%m-%d %H:%M:%S";
const BAR_WIDTH: usize = 40;

/// Bucket sizes the histogram endpoint accepts, smallest first. Used to snap
/// an `auto` interval to a supported window.
const WINDOWS: &[(&str, i64)] = &[
    ("1s", 1),
    ("5s", 5),
    ("10s", 10),
    ("15s", 15),
    ("30s", 30),
    ("1m", 60),
    ("5m", 300),
    ("10m", 600),
    ("15m", 900),
    ("30m", 1800),
    ("1h", 3600),
    ("2h", 7200),
    ("3h", 10800),
    ("6h", 21600),
    ("12h", 43200),
    ("24h", 86400),
];

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Error rate over the last 24h, auto-sized buckets
  logchef histogram 'level=\"error\"' --since 24h -t platform -S app-logs

  # Total volume in 5-minute buckets, broken down by service (top 10)
  logchef histogram --since 6h --interval 5m --group-by service

  # Machine-readable buckets for plotting elsewhere
  logchef histogram 'status>=500' --since 1h --output jsonl")]
pub struct HistogramArgs {
    /// LogchefQL query to bucket (e.g. `level="error"`)
    query: Option<String>,

    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Relative lookback window (e.g. 15m, 1h, 24h)
    #[arg(long, short = 's')]
    since: Option<String>,

    /// Absolute start time (YYYY-MM-DD HH:MM:SS) in the effective timezone. Requires --to.
    #[arg(long)]
    from: Option<String>,

    /// Absolute end time (YYYY-MM-DD HH:MM:SS) in the effective timezone. Requires --from.
    #[arg(long)]
    to: Option<String>,

    /// Bucket size (e.g. 1m, 5m, 1h). `auto` picks a size from the time range.
    #[arg(long, default_value = "auto")]
    interval: String,

    /// Optional field to break each bucket down by (top 10 series)
    #[arg(long)]
    group_by: Option<String>,

    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,

    /// Query timeout in seconds
    #[arg(long, default_value = "30")]
    timeout: u32,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    Table,
}

pub async fn run(args: HistogramArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let team = args.team.clone().or_else(|| ctx.defaults.team_with_env());
    let source = args
        .source
        .clone()
        .or_else(|| ctx.defaults.source_with_env());

    let team_id = resolve_team(client, &mut cache, team).await?;
    let source_id = resolve_source(client, &mut cache, team_id, source).await?;

    let since = args
        .since
        .clone()
        .unwrap_or_else(|| ctx.defaults.since.clone());
    let (start_utc, end_utc) = resolve_instants(
        &since,
        args.from.as_deref(),
        args.to.as_deref(),
        ctx.defaults.timezone.as_deref(),
    )?;

    // Wall-clock strings (in the effective timezone) for translation; the
    // ClickHouse translator bakes these into the generated SQL.
    let wall = resolve_time_range(
        TimeInput::Instant {
            start: start_utc,
            end: end_utc,
        },
        ctx.defaults.timezone.as_deref(),
    );

    // The histogram endpoint expects a source-native query_text (full SQL for
    // ClickHouse, LogsQL for VictoriaLogs), so translate the LogchefQL first —
    // exactly what the web explorer sends. The time range is baked into the
    // ClickHouse SQL and passed alongside (below) for VictoriaLogs.
    let query = args.query.clone().unwrap_or_default();
    let translate = client
        .translate_logchefql(
            team_id,
            source_id,
            &TranslateRequest {
                query,
                start_time: Some(wall.start.clone()),
                end_time: Some(wall.end.clone()),
                timezone: Some(wall.timezone.clone()),
                limit: None,
            },
        )
        .await
        .context("Failed to translate query")?;

    if !translate.valid {
        let message = translate
            .error
            .map(|e| e.message)
            .unwrap_or_else(|| "invalid LogchefQL query".to_string());
        anyhow::bail!("{}", message);
    }

    let window = resolve_window(&args.interval, end_utc - start_utc);

    let request = HistogramRequest {
        query_text: translate.generated_query().to_string(),
        start_timestamp: Some(start_utc.timestamp_millis()),
        end_timestamp: Some(end_utc.timestamp_millis()),
        window: Some(window),
        group_by: args.group_by.clone(),
        timezone: Some(wall.timezone.clone()),
        limit: Some(100),
        query_timeout: Some(args.timeout),
    };

    let response = client
        .get_histogram(team_id, source_id, &request)
        .await
        .context("Histogram query failed")?;

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&response)?);
        }
        OutputFormat::Jsonl => {
            for bucket in &response.data {
                println!("{}", serde_json::to_string(bucket)?);
            }
        }
        OutputFormat::Table => {
            print_table(&response.data, args.group_by.is_some());
        }
        OutputFormat::Text => {
            print_chart(&response, args.group_by.is_some(), global.quiet);
        }
    }

    Ok(())
}

/// Resolves the query window to a pair of UTC instants. `--from/--to` are
/// wall-clock times in the effective timezone; otherwise `now - since`.
fn resolve_instants(
    since: &str,
    from: Option<&str>,
    to: Option<&str>,
    configured_tz: Option<&str>,
) -> Result<(DateTime<Utc>, DateTime<Utc>)> {
    match (from, to) {
        (Some(from), Some(to)) => {
            let tz = resolve_timezone(configured_tz);
            let start = parse_wall_clock(from, tz).context("Invalid --from time")?;
            let end = parse_wall_clock(to, tz).context("Invalid --to time")?;
            Ok((start, end))
        }
        (Some(_), None) => anyhow::bail!("--from requires --to to be specified"),
        (None, Some(_)) => anyhow::bail!("--to requires --from to be specified"),
        (None, None) => {
            let end = Utc::now();
            Ok((end - parse_lookback(since)?, end))
        }
    }
}

fn parse_wall_clock<Tz: TimeZone>(value: &str, tz: Tz) -> Result<DateTime<Utc>> {
    let naive = NaiveDateTime::parse_from_str(value.trim(), WALL_CLOCK_FORMAT)
        .context("expected format YYYY-MM-DD HH:MM:SS")?;
    let local = tz
        .from_local_datetime(&naive)
        .single()
        .ok_or_else(|| anyhow::anyhow!("ambiguous or invalid local time '{}'", value))?;
    Ok(local.with_timezone(&Utc))
}

/// Returns the explicit interval, or an auto-selected window sized so the range
/// yields roughly 60 buckets, snapped up to a supported window.
fn resolve_window(interval: &str, span: chrono::Duration) -> String {
    if !interval.eq_ignore_ascii_case("auto") {
        return interval.to_string();
    }

    let span_secs = span.num_seconds().max(1);
    let ideal = (span_secs / 60).max(1);
    WINDOWS
        .iter()
        .find(|(_, secs)| *secs >= ideal)
        .or_else(|| WINDOWS.last())
        .map(|(label, _)| label.to_string())
        .unwrap_or_else(|| "1m".to_string())
}

fn format_bucket(raw: &str) -> String {
    DateTime::parse_from_rfc3339(raw)
        .map(|dt| dt.format("%m-%d %H:%M:%S").to_string())
        .unwrap_or_else(|_| raw.to_string())
}

fn bar(count: i64, max: i64) -> String {
    if max <= 0 || count <= 0 {
        return String::new();
    }
    const EIGHTHS: [char; 8] = [' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'];
    let units = (count as f64 / max as f64) * BAR_WIDTH as f64 * 8.0;
    let units = units.round() as usize;
    let full = units / 8;
    let rem = units % 8;
    let mut out = "█".repeat(full);
    if rem > 0 {
        out.push(EIGHTHS[rem]);
    }
    out
}

fn print_chart(response: &logchef_core::api::HistogramResponse, has_group_by: bool, quiet: bool) {
    if response.data.is_empty() {
        println!("No data in the selected time range.");
        return;
    }

    let max = response.data.iter().map(|b| b.log_count).max().unwrap_or(0);
    let total: i64 = response.data.iter().map(|b| b.log_count).sum();
    let color = ui::human(quiet);

    // Time-axis header: the span the chart covers and the bucket size.
    let start = format_bucket(
        &response
            .data
            .first()
            .map(|b| b.bucket.clone())
            .unwrap_or_default(),
    );
    let end = format_bucket(
        &response
            .data
            .last()
            .map(|b| b.bucket.clone())
            .unwrap_or_default(),
    );
    let header = format!("span {} → {} · bucket {}", start, end, response.granularity);
    if color {
        println!("\x1b[2m{}\x1b[0m", header);
    } else {
        println!("{}", header);
    }

    for HistogramBucket {
        bucket,
        log_count,
        group_value,
    } in &response.data
    {
        let time = format_bucket(bucket);
        let label = if has_group_by {
            format!("{} [{}]", time, group_value.as_deref().unwrap_or(""))
        } else {
            time
        };
        // Pad the (plain) bar to a fixed width first, then colorize — so the
        // ANSI escapes never throw off the column alignment.
        let padded = format!("{:<width$}", bar(*log_count, max), width = BAR_WIDTH + 1);
        let bar_out = if color {
            format!("\x1b[36m{}\x1b[0m", padded)
        } else {
            padded
        };
        println!("{:<28} │{} {:>8}", label, bar_out, ui::compact(*log_count));
    }

    println!(
        "\n{} buckets · {} logs · peak {}",
        ui::thousands(response.data.len() as i64),
        ui::thousands(total),
        ui::thousands(max)
    );
    if let Some(notice) = &response.notice
        && !notice.is_empty()
    {
        eprintln!("note: {}", notice);
    }
}

fn print_table(buckets: &[HistogramBucket], has_group_by: bool) {
    if buckets.is_empty() {
        println!("No data in the selected time range.");
        return;
    }

    if has_group_by {
        println!("{:<28} {:<24} {:>12}", "BUCKET", "GROUP", "COUNT");
    } else {
        println!("{:<28} {:>12}", "BUCKET", "COUNT");
    }
    println!("{}", "-".repeat(70));

    for bucket in buckets {
        let time = format_bucket(&bucket.bucket);
        if has_group_by {
            println!(
                "{:<28} {:<24} {:>12}",
                time,
                bucket.group_value.as_deref().unwrap_or(""),
                ui::thousands(bucket.log_count)
            );
        } else {
            println!("{:<28} {:>12}", time, ui::thousands(bucket.log_count));
        }
    }
}
