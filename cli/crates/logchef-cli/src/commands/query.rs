use anyhow::{Context, Result};
use chrono::{Duration, Utc};
use clap::Args;
use inquire::{Select, Text};
use logchef_core::Config;
use logchef_core::api::{Client, Column, QueryRequest, QueryStats};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};
use logchef_core::timerange::{TimeInput, resolve_time_range};
use serde::Serialize;
use std::io::IsTerminal;

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Errors from the api service in the last hour (LogchefQL)
  logchef query 'level=\"error\" and service=\"api\"' --since 1h

  # All logs in an absolute window, newest 500, as JSON lines for jq
  logchef query --from '2026-07-14 09:00:00' --to '2026-07-14 10:00:00' \\
    --limit 500 --output jsonl | jq 'select(.status >= 500)'

  # See the ClickHouse SQL / LogsQL a query compiles to, then run it
  logchef query 'status>=500' --since 15m --show-sql")]
pub struct QueryArgs {
    query: Option<String>,

    /// Relative lookback window (e.g. 15m, 1h, 24h) evaluated against now,
    /// in the effective timezone: `defaults.timezone` if configured,
    /// otherwise the system's local timezone (see `logchef config show`).
    #[arg(long, short = 's')]
    since: Option<String>,

    /// Absolute start time (YYYY-MM-DD HH:MM:SS), interpreted as wall-clock
    /// in the effective timezone. Requires --to.
    #[arg(long)]
    from: Option<String>,

    /// Absolute end time (YYYY-MM-DD HH:MM:SS), interpreted as wall-clock
    /// in the effective timezone. Requires --from.
    #[arg(long)]
    to: Option<String>,

    #[arg(long, short = 't')]
    team: Option<String>,

    #[arg(long, short = 'S')]
    source: Option<String>,

    #[arg(long, short = 'l')]
    limit: Option<u32>,

    #[arg(long, default_value = "text")]
    output: OutputFormat,

    #[arg(long)]
    no_highlight: bool,

    #[arg(long)]
    no_timestamp: bool,

    /// Trace the server-generated query on stderr after executing. Use
    /// `--dry-run` to print the query and exit without keeping the results.
    #[arg(
        long,
        visible_alias = "explain",
        help = "Show the generated backend query (SQL for ClickHouse, LogsQL for VictoriaLogs)"
    )]
    show_sql: bool,

    /// Print the server-generated SQL to stdout and exit. (The server is
    /// still called once to translate LogChefQL.)
    #[arg(long)]
    dry_run: bool,

    #[arg(long = "highlight", value_name = "COLOR:WORDS")]
    highlights: Vec<String>,

    #[arg(long = "disable-highlight", value_name = "GROUP")]
    disable_highlights: Vec<String>,

    #[arg(long, default_value = "30")]
    timeout: u32,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    JsonFlat,
    Table,
    Msg,
}

#[derive(Serialize)]
struct JsonOutput<'a> {
    logs: &'a [logchef_core::api::LogEntry],
    count: usize,
    stats: &'a QueryStats,
    #[serde(skip_serializing_if = "Option::is_none")]
    query_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    generated_sql: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    generated_query: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    generated_query_language: Option<&'a str>,
    columns: &'a [Column],
}

pub async fn run(args: QueryArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();

    // Detect interactive mode: no query provided, no team/source args, and running in a TTY
    let is_interactive = args.query.is_none()
        && args.team.is_none()
        && args.source.is_none()
        && default_team.is_none()
        && default_source.is_none()
        && std::io::stdin().is_terminal();

    // Resolve team
    let team_id = if is_interactive {
        prompt_team_interactive(client, &mut cache).await?
    } else {
        let team_input = args.team.or(default_team).ok_or_else(|| {
            anyhow::anyhow!(
                "Team not specified. Use --team or set defaults.team. List teams with 'logchef teams'."
            )
        })?;

        match parse_identifier(&team_input) {
            Identifier::Id(id) => id,
            Identifier::Name(name) => {
                if let Some(id) = cache.get_team_id(&name) {
                    id
                } else {
                    let teams = client.list_teams().await.context("Failed to list teams")?;
                    cache.set_teams(
                        &teams
                            .iter()
                            .map(|t| (t.name.clone(), t.id))
                            .collect::<Vec<_>>(),
                    );
                    teams
                        .iter()
                        .find(|t| t.name.eq_ignore_ascii_case(&name))
                        .map(|t| t.id)
                        .ok_or_else(|| anyhow::anyhow!("Team '{}' not found", name))?
                }
            }
        }
    };

    // Resolve source
    let source_id = if is_interactive {
        prompt_source_interactive(client, team_id, &mut cache).await?
    } else {
        let source_input = args.source.or(default_source).ok_or_else(|| {
            anyhow::anyhow!(
                "Source not specified. Use --source or set defaults.source. List sources with 'logchef sources --team <team>'."
            )
        })?;

        match parse_identifier(&source_input) {
            Identifier::Id(id) => id,
            Identifier::Name(name) => {
                if let Some(id) = cache.get_source_id(team_id, &name) {
                    id
                } else {
                    let sources = client
                        .list_sources(team_id)
                        .await
                        .context("Failed to list sources")?;

                    let mut cache_entries: Vec<(String, i64)> =
                        sources.iter().map(|s| (s.name.clone(), s.id)).collect();
                    for s in &sources {
                        if let Some(target_ref) = s.target_ref() {
                            cache_entries.push((target_ref, s.id));
                        }
                    }
                    cache.set_sources(team_id, &cache_entries);

                    sources
                        .iter()
                        .find(|s| s.name.eq_ignore_ascii_case(&name))
                        .or_else(|| {
                            sources.iter().find(|s| {
                                s.target_ref()
                                    .map(|r| r.eq_ignore_ascii_case(&name))
                                    .unwrap_or(false)
                            })
                        })
                        .map(|s| s.id)
                        .ok_or_else(|| anyhow::anyhow!("Source '{}' not found", name))?
                }
            }
        }
    };

    let since = args.since.unwrap_or_else(|| ctx.defaults.since.clone());
    let limit = args.limit.unwrap_or(ctx.defaults.limit);

    let time_range = parse_time_range(
        &since,
        args.from.as_deref(),
        args.to.as_deref(),
        ctx.defaults.timezone.as_deref(),
    )?;

    // Resolve query (prompt in interactive mode if not provided)
    let query = if is_interactive && args.query.is_none() {
        prompt_query_interactive()?
    } else {
        args.query.unwrap_or_default()
    };

    let request = QueryRequest {
        query,
        start_time: time_range.start,
        end_time: time_range.end,
        timezone: Some(time_range.timezone),
        limit: Some(limit),
        query_timeout: Some(args.timeout),
    };

    let spinner = ui::Spinner::start(global.quiet, "querying");
    let result = client.query_logchefql(team_id, source_id, &request).await;
    spinner.finish();
    let response = result.context("Query failed")?;

    if args.dry_run {
        // Print the generated backend query to stdout (clean, pipeable) and
        // exit. `generated_query()` falls back to `generated_sql`, so this
        // works for both engines: ClickHouse returns `generated_sql`, while
        // VictoriaLogs returns `generated_query` + `generated_query_language:
        // "logsql"`. Never error just because the source is VictoriaLogs.
        match response.generated_query() {
            Some(query) => println!("{}", query),
            None => anyhow::bail!("Server did not return a generated query; cannot --dry-run."),
        }
        return Ok(());
    }

    if args.show_sql
        && let Some(query) = response.generated_query()
    {
        let label = match response.generated_query_language() {
            Some("logsql") => "Generated LogsQL",
            Some("clickhouse-sql") => "Generated SQL",
            _ => "Generated query",
        };
        let rendered = ui::highlight_query(
            query,
            response.generated_query_language(),
            ui::stderr_human(global.quiet),
        );
        eprintln!("{}: {}\n", label, rendered);
    }

    let entries = response.entries();

    match args.output {
        OutputFormat::Json => {
            let output = JsonOutput {
                logs: entries,
                count: entries.len(),
                stats: &response.stats,
                query_id: response.query_id.as_deref(),
                generated_sql: response.generated_sql.as_deref(),
                generated_query: response.generated_query(),
                generated_query_language: response.generated_query_language(),
                columns: &response.columns,
            };
            println!("{}", serde_json::to_string_pretty(&output)?);
        }
        OutputFormat::Jsonl => {
            for entry in entries {
                println!("{}", serde_json::to_string(entry)?);
            }
            ui::print_stats(
                global.quiet,
                entries.len(),
                response.stats.execution_time_ms,
                response.stats.rows_read,
            );
        }
        OutputFormat::JsonFlat => {
            print_json_flat(entries)?;
        }
        OutputFormat::Table => {
            print_table(entries, &response.columns);
            ui::print_stats(
                global.quiet,
                entries.len(),
                response.stats.execution_time_ms,
                response.stats.rows_read,
            );
        }
        OutputFormat::Msg => {
            print_msg(entries, &response.columns, false);
        }
        OutputFormat::Text => {
            let highlighter = if args.no_highlight || !ui::human(global.quiet) {
                None
            } else {
                let hl_options = HighlightOptions {
                    adhoc_highlights: parse_highlight_args(&args.highlights),
                    disabled_groups: args.disable_highlights.clone(),
                };
                Highlighter::with_options(&config.highlights, &hl_options).ok()
            };

            let fmt_options = FormatOptions {
                show_timestamp: !args.no_timestamp,
            };

            for entry in entries {
                let line = format_log_entry_with_options(entry, &response.columns, &fmt_options);
                if let Some(ref h) = highlighter {
                    println!("{}", h.highlight(&line));
                } else {
                    println!("{}", line);
                }
            }
            ui::print_stats(
                global.quiet,
                entries.len(),
                response.stats.execution_time_ms,
                response.stats.rows_read,
            );
        }
    }

    Ok(())
}

fn parse_time_range(
    since: &str,
    from: Option<&str>,
    to: Option<&str>,
    configured_tz: Option<&str>,
) -> Result<logchef_core::timerange::ResolvedTimeRange> {
    let input = match (from, to) {
        (Some(from), Some(to)) => TimeInput::WallClock {
            start: from,
            end: to,
        },
        (Some(_), None) => anyhow::bail!("--from requires --to to be specified"),
        (None, Some(_)) => anyhow::bail!("--to requires --from to be specified"),
        (None, None) => {
            let end = Utc::now();
            let start = end - parse_duration(since)?;
            TimeInput::Instant { start, end }
        }
    };
    Ok(resolve_time_range(input, configured_tz))
}

fn parse_duration(s: &str) -> Result<Duration> {
    let s = s.trim();
    if s.is_empty() {
        return Ok(Duration::minutes(15));
    }

    let (num, unit) = if s.ends_with('m') {
        (s.trim_end_matches('m'), "m")
    } else if s.ends_with('h') {
        (s.trim_end_matches('h'), "h")
    } else if s.ends_with('d') {
        (s.trim_end_matches('d'), "d")
    } else if s.ends_with('w') {
        (s.trim_end_matches('w'), "w")
    } else {
        (s, "m")
    };

    let num: i64 = num.parse().context("Invalid duration number")?;

    match unit {
        "m" => Ok(Duration::minutes(num)),
        "h" => Ok(Duration::hours(num)),
        "d" => Ok(Duration::days(num)),
        "w" => Ok(Duration::weeks(num)),
        _ => Ok(Duration::minutes(num)),
    }
}

fn parse_highlight_args(args: &[String]) -> Vec<(String, Vec<String>)> {
    args.iter()
        .filter_map(|arg| {
            let parts: Vec<&str> = arg.splitn(2, ':').collect();
            if parts.len() == 2 {
                let color = parts[0].to_string();
                let words: Vec<String> =
                    parts[1].split(',').map(|s| s.trim().to_string()).collect();
                Some((color, words))
            } else {
                None
            }
        })
        .collect()
}

fn print_json_flat(entries: &[logchef_core::api::LogEntry]) -> Result<()> {
    for entry in entries {
        println!("{}", serde_json::to_string(&flatten_msg(entry))?);
    }
    Ok(())
}

fn flatten_msg(entry: &logchef_core::api::LogEntry) -> logchef_core::api::LogEntry {
    let mut out = entry.clone();
    if let Some(msg) = entry.get("msg").and_then(|value| value.as_str())
        && let Ok(serde_json::Value::Object(obj)) = serde_json::from_str::<serde_json::Value>(msg)
    {
        for (key, value) in obj {
            out.entry(key).or_insert(value);
        }
    }
    out
}

fn print_msg(
    entries: &[logchef_core::api::LogEntry],
    columns: &[logchef_core::api::Column],
    fallback_to_first_column: bool,
) {
    let field = if entries.iter().any(|entry| entry.contains_key("msg")) {
        Some("msg")
    } else if fallback_to_first_column {
        columns.first().map(|column| column.name.as_str())
    } else {
        None
    };

    let Some(field) = field else {
        return;
    };

    for entry in entries {
        println!(
            "{}",
            entry.get(field).map(json_value_to_line).unwrap_or_default()
        );
    }
}

fn json_value_to_line(value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Null => String::new(),
        _ => value.to_string(),
    }
}

fn print_table(entries: &[logchef_core::api::LogEntry], columns: &[logchef_core::api::Column]) {
    if entries.is_empty() {
        println!("No results");
        return;
    }

    let display_cols: Vec<_> = columns
        .iter()
        .filter(|c| !c.name.starts_with('_') || c.name == "_timestamp")
        .take(6)
        .collect();

    let header: Vec<_> = display_cols.iter().map(|c| c.name.as_str()).collect();
    println!("{}", header.join(" | "));
    println!("{}", "-".repeat(80));

    for entry in entries {
        let row: Vec<_> = display_cols
            .iter()
            .map(|c| {
                entry
                    .get(&c.name)
                    .map(|v| match v {
                        serde_json::Value::String(s) => s.clone(),
                        _ => v.to_string(),
                    })
                    .unwrap_or_default()
            })
            .collect();
        println!("{}", row.join(" | "));
    }
}

async fn prompt_team_interactive(client: &Client, cache: &mut Cache) -> Result<i64> {
    let teams = client.list_teams().await.context("Failed to list teams")?;
    if teams.is_empty() {
        anyhow::bail!("No teams available");
    }

    let options: Vec<String> = teams
        .iter()
        .map(|t| format!("{} (ID: {})", t.name, t.id))
        .collect();
    let selection = Select::new("Select team:", options)
        .prompt()
        .context("Failed to select team")?;

    // Parse team ID from selection
    let team = teams
        .iter()
        .find(|t| selection.starts_with(&t.name))
        .ok_or_else(|| anyhow::anyhow!("Team not found"))?;
    cache.set_teams(
        &teams
            .iter()
            .map(|t| (t.name.clone(), t.id))
            .collect::<Vec<_>>(),
    );
    Ok(team.id)
}

async fn prompt_source_interactive(
    client: &Client,
    team_id: i64,
    cache: &mut Cache,
) -> Result<i64> {
    let sources = client
        .list_sources(team_id)
        .await
        .context("Failed to list sources")?;
    if sources.is_empty() {
        anyhow::bail!("No sources available for this team");
    }

    let options: Vec<String> = sources.iter().map(|s| s.display_name()).collect();
    let selection = Select::new("Select source:", options)
        .prompt()
        .context("Failed to select source")?;

    let source = sources
        .iter()
        .find(|s| selection.starts_with(&s.name))
        .ok_or_else(|| anyhow::anyhow!("Source not found"))?;

    let mut cache_entries: Vec<(String, i64)> =
        sources.iter().map(|s| (s.name.clone(), s.id)).collect();
    for s in &sources {
        if let Some(target_ref) = s.target_ref() {
            cache_entries.push((target_ref, s.id));
        }
    }
    cache.set_sources(team_id, &cache_entries);

    Ok(source.id)
}

fn prompt_query_interactive() -> Result<String> {
    let query = Text::new("LogChefQL query:")
        .with_help_message(r#"e.g., level="error" and service="api" (leave empty for all logs)"#)
        .prompt()
        .context("Failed to read query")?;
    Ok(query)
}
