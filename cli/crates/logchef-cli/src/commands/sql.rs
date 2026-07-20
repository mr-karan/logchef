use anyhow::{Context as _, Result};
use chrono::{DateTime, Duration as ChronoDuration, NaiveDateTime, SecondsFormat, Utc};
use clap::Args;
use inquire::{Select, Text};
use logchef_core::Config;
use logchef_core::api::{Client, Column, ExportSqlRequest, QueryStats, Source, SqlQueryRequest};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::config::Context;
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};
use logchef_core::timerange::{TimeInput, resolve_time_range, resolve_timezone};
use serde::Serialize;
use std::io::{IsTerminal, Read, Write};
use tokio::time::{Duration, sleep};

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

const STREAMING_SQL_MIN_TIMEOUT_SECS: u32 = 120;
const SQL_HTTP_TIMEOUT_HEADROOM_SECS: u64 = 60;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Top services by volume (ClickHouse SQL); time window auto-spliced
  logchef sql 'SELECT service, count() c FROM logs.app GROUP BY service ORDER BY c DESC' \\
    --since 24h -t platform -S app-logs

  # LogsQL against a VictoriaLogs source
  logchef sql '_time:1h error | stats by (service) count()' -S vl-logs

  # Stream a large result straight to a file (NDJSON), no memory spike
  logchef sql 'SELECT * FROM logs.app' --since 6h --stream --output jsonl > out.ndjson

  # Read the query from stdin, export as CSV
  echo 'SELECT * FROM logs.app LIMIT 1000' | logchef sql - --output csv > rows.csv")]
pub struct SqlArgs {
    /// Raw native query to execute. Use SQL for ClickHouse and LogsQL for VictoriaLogs. Use '-' to read from stdin.
    sql: Option<String>,

    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Apply a relative time range to SQL (e.g., 15m, 1h, 24h), evaluated
    /// against now in the effective timezone: `defaults.timezone` if
    /// configured, otherwise the system's local timezone (see `logchef
    /// config show`).
    #[arg(long, short = 's')]
    since: Option<String>,

    /// Apply an absolute start time (YYYY-MM-DD HH:MM:SS), interpreted as
    /// wall-clock in the effective timezone.
    #[arg(long)]
    from: Option<String>,

    /// Apply an absolute end time (YYYY-MM-DD HH:MM:SS), interpreted as
    /// wall-clock in the effective timezone.
    #[arg(long)]
    to: Option<String>,

    /// Query timeout in seconds
    #[arg(long, default_value = "30")]
    timeout: u32,

    /// Stream results directly from the server instead of buffering a preview response
    #[arg(long)]
    stream: bool,

    /// Result row limit. In stream mode this caps the download; otherwise it caps the preview.
    #[arg(long)]
    limit: Option<u32>,

    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,

    /// Disable syntax highlighting
    #[arg(long)]
    no_highlight: bool,

    /// Hide timestamp column in text output
    #[arg(long)]
    no_timestamp: bool,

    /// Custom highlight rules (format: COLOR:word1,word2)
    #[arg(long = "highlight", value_name = "COLOR:WORDS")]
    highlights: Vec<String>,

    /// Disable specific highlight groups
    #[arg(long = "disable-highlight", value_name = "GROUP")]
    disable_highlights: Vec<String>,

    /// Trace the resolved query on stderr before executing it. Use `--dry-run`
    /// instead to print the query and exit without running it.
    #[arg(long, visible_alias = "explain")]
    show_sql: bool,

    /// Print the resolved query (after --since/--from/--to handling) to stdout
    /// and exit without executing it. Pipes cleanly to other tools.
    #[arg(long)]
    dry_run: bool,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    JsonFlat,
    Csv,
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
    columns: &'a [Column],
}

pub async fn run(args: SqlArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;

    let effective_query_timeout_secs =
        effective_query_timeout_secs(args.timeout, &args.output, args.stream);

    let s = session::authed_with_timeout(&config, &global, |ctx| {
        sql_transport_timeout_secs(ctx.timeout_secs, effective_query_timeout_secs)
    })?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();
    let arg_team = args.team.clone();
    let arg_source = args.source.clone();
    let arg_sql = args.sql.clone();

    // Detect interactive mode: no sql provided, no team/source args, and running in a TTY
    let is_interactive = arg_sql.is_none()
        && arg_team.is_none()
        && arg_source.is_none()
        && default_team.is_none()
        && default_source.is_none()
        && std::io::stdin().is_terminal();

    // Resolve team
    let team_id = if is_interactive {
        prompt_team_interactive(client, &mut cache).await?
    } else {
        let team_input = arg_team.or(default_team).ok_or_else(|| {
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
        let source_input = arg_source.or(default_source).ok_or_else(|| {
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

    // Read SQL from argument, stdin, or interactive prompt
    let sql = if is_interactive {
        prompt_sql_interactive()?
    } else {
        match arg_sql {
            Some(s) if s == "-" => {
                let mut buffer = String::new();
                std::io::stdin()
                    .read_to_string(&mut buffer)
                    .context("Failed to read SQL from stdin")?;
                buffer.trim().to_string()
            }
            Some(s) => s,
            None => {
                anyhow::bail!(
                    "Raw query required. Provide as argument or use '-' to read from stdin."
                )
            }
        }
    };

    if sql.is_empty() {
        anyhow::bail!("Raw query cannot be empty");
    }

    // Fetch the source once: we need its engine (to pick the time-range
    // strategy) and its timestamp field (for the ClickHouse injection).
    let source = client
        .get_source(team_id, source_id)
        .await
        .context("Failed to fetch source")?;
    let is_victorialogs = source.source_type.eq_ignore_ascii_case("victorialogs");

    // Time-range handling differs by engine:
    //   ClickHouse   — splice a `toDateTime(...) BETWEEN` condition into the
    //                  SQL string (or fill __START__/__END__ placeholders).
    //   VictoriaLogs — leave the LogsQL untouched and pass an absolute RFC3339
    //                  window via the request's start_time/end_time, which the
    //                  VL provider applies as the query's `start`/`end` bounds
    //                  (see internal/victorialogs/querying.go QueryLogs).
    //                  Splicing ClickHouse SQL into LogsQL would be invalid.
    let (sql, vl_window) = if is_victorialogs {
        (sql, vl_time_window(&args, ctx)?)
    } else {
        (apply_clickhouse_time_range(&source, sql, &args, ctx)?, None)
    };

    // --dry-run: print resolved query to stdout (clean for piping) and exit.
    // For VictoriaLogs this is the raw LogsQL (the time window rides in the
    // request fields, never spliced into the query) — never ClickHouse SQL.
    if args.dry_run {
        println!("{}", sql);
        return Ok(());
    }

    // --explain / --show-sql: trace to stderr with an engine-accurate label,
    // then continue executing (matches the LogChefQL `query` command).
    if args.show_sql {
        let (label, lang) = if is_victorialogs {
            ("Generated LogsQL", Some("logsql"))
        } else {
            ("Generated ClickHouse SQL", Some("clickhouse-sql"))
        };
        let rendered = ui::highlight_query(&sql, lang, ui::stderr_human(global.quiet));
        eprintln!("{}: {}\n", label, rendered);
    }

    if matches!(args.output, OutputFormat::Csv) {
        let request = ExportSqlRequest {
            raw_sql: sql,
            format: "csv".to_string(),
            limit: args.limit,
            query_timeout: Some(effective_query_timeout_secs),
        };

        let job = client
            .create_export_job(team_id, source_id, &request)
            .await
            .context("Failed to create CSV export")?;
        let export_id = job.id.clone();

        let deadline = std::time::Instant::now()
            + Duration::from_secs(u64::from(effective_query_timeout_secs) + 60);
        loop {
            let current = client
                .get_export_job(team_id, source_id, &export_id)
                .await
                .context("Failed to check CSV export status")?;

            match current.status.as_str() {
                "complete" => {
                    let mut response = client
                        .download_export_job(team_id, source_id, &export_id)
                        .await
                        .context("Failed to download CSV export")?;

                    let mut stdout = std::io::stdout().lock();
                    while let Some(chunk) = response
                        .chunk()
                        .await
                        .context("Failed to read CSV export")?
                    {
                        stdout
                            .write_all(&chunk)
                            .context("Failed to write CSV export to stdout")?;
                    }
                    stdout.flush().context("Failed to flush stdout")?;
                    return Ok(());
                }
                "failed" => {
                    anyhow::bail!(
                        "{}",
                        current
                            .error_message
                            .unwrap_or_else(|| "CSV export failed".to_string())
                    );
                }
                "pending" | "running" => {
                    if std::time::Instant::now() >= deadline {
                        anyhow::bail!("CSV export is taking longer than expected");
                    }
                    sleep(Duration::from_secs(1)).await;
                }
                other => anyhow::bail!("CSV export entered unknown state '{}'", other),
            }
        }
    }

    if args.stream {
        let format = match args.output {
            OutputFormat::Jsonl => "ndjson",
            OutputFormat::Json => {
                anyhow::bail!(
                    "--stream does not support --output json. Use --output jsonl for streamed JSON or drop --stream for buffered JSON output."
                );
            }
            OutputFormat::Text => {
                anyhow::bail!(
                    "--stream does not support --output text. Use --stream --output jsonl for live streaming or --output csv for a completed-file export."
                );
            }
            OutputFormat::Table => {
                anyhow::bail!(
                    "--stream does not support --output table. Use --stream --output jsonl for live streaming or --output csv for a completed-file export."
                );
            }
            OutputFormat::JsonFlat => {
                anyhow::bail!(
                    "--stream does not support --output json-flat. Use --output json-flat without --stream for buffered flattened JSON output."
                );
            }
            OutputFormat::Msg => {
                anyhow::bail!(
                    "--stream does not support --output msg. Use --output msg without --stream for buffered message output."
                );
            }
            OutputFormat::Csv => unreachable!("CSV output is handled by export jobs"),
        };

        let request = ExportSqlRequest {
            raw_sql: sql,
            format: format.to_string(),
            limit: args.limit,
            query_timeout: Some(effective_query_timeout_secs),
        };

        let mut response = client
            .export_sql(team_id, source_id, &request)
            .await
            .context("SQL stream failed")?;

        let mut stdout = std::io::stdout().lock();
        while let Some(chunk) = response.chunk().await.context("Failed to read stream")? {
            stdout
                .write_all(&chunk)
                .context("Failed to write stream to stdout")?;
        }
        stdout.flush().context("Failed to flush stdout")?;
        return Ok(());
    }

    let (start_time, end_time) = match &vl_window {
        Some((start, end)) => (Some(start.clone()), Some(end.clone())),
        None => (None, None),
    };
    let request = SqlQueryRequest {
        query_text: sql,
        limit: args.limit,
        // For ClickHouse, any --since/--from/--to was baked into `sql` as a
        // literal `toDateTime(..., tz)` condition above, so no window rides
        // here. For VictoriaLogs, the resolved RFC3339 window is passed
        // through and the server bounds the raw LogsQL query with it.
        timezone: Some(resolve_timezone(ctx.defaults.timezone.as_deref()).to_string()),
        start_time,
        end_time,
        query_timeout: Some(args.timeout),
    };

    let spinner = ui::Spinner::start(global.quiet, "querying");
    let result = client.query_sql(team_id, source_id, &request).await;
    spinner.finish();
    let response = result.context("Raw query failed")?;

    let entries = response.entries();

    match args.output {
        OutputFormat::Json => {
            let output = JsonOutput {
                logs: entries,
                count: entries.len(),
                stats: &response.stats,
                query_id: response.query_id.as_deref(),
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
        OutputFormat::Csv => {
            anyhow::bail!("Use --stream --output csv for CSV output");
        }
        OutputFormat::Msg => {
            print_msg(entries, &response.columns, true);
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

/// Resolves an absolute RFC3339 UTC time window for a VictoriaLogs `sql` query
/// from --since/--from/--to. Returns None when no time flag is set.
///
/// VictoriaLogs bounds a raw LogsQL query by the request's start/end params
/// (see internal/victorialogs/querying.go QueryLogs), so — unlike ClickHouse
/// — the window must NOT be spliced into the query string. We resolve it to
/// absolute instants and let the server pass them to VL as `start`/`end`.
fn vl_time_window(args: &SqlArgs, ctx: &Context) -> Result<Option<(String, String)>> {
    if args.since.is_none() && args.from.is_none() && args.to.is_none() {
        return Ok(None);
    }

    let (start, end) = match (args.from.as_deref(), args.to.as_deref()) {
        (Some(from), Some(to)) => {
            let tz = resolve_timezone(ctx.defaults.timezone.as_deref());
            (wall_clock_to_utc(from, tz)?, wall_clock_to_utc(to, tz)?)
        }
        (Some(_), None) => anyhow::bail!("--from requires --to to be specified"),
        (None, Some(_)) => anyhow::bail!("--to requires --from to be specified"),
        (None, None) => {
            let end = Utc::now();
            let start = end - parse_duration(args.since.as_deref().unwrap_or("15m"))?;
            (start, end)
        }
    };

    Ok(Some((
        start.to_rfc3339_opts(SecondsFormat::Secs, true),
        end.to_rfc3339_opts(SecondsFormat::Secs, true),
    )))
}

/// Parses a `YYYY-MM-DD HH:MM:SS` wall-clock string, interpreted in `tz`, into
/// a UTC instant. Generic over the timezone so this file needs no direct
/// chrono-tz dependency (the concrete zone comes from `resolve_timezone`).
fn wall_clock_to_utc<Tz: chrono::TimeZone>(value: &str, tz: Tz) -> Result<DateTime<Utc>> {
    let naive = NaiveDateTime::parse_from_str(value.trim(), "%Y-%m-%d %H:%M:%S")
        .context("Invalid time format (expected YYYY-MM-DD HH:MM:SS)")?;
    naive
        .and_local_timezone(tz)
        .single()
        .map(|dt| dt.with_timezone(&Utc))
        .ok_or_else(|| anyhow::anyhow!("Ambiguous or invalid local time '{}'", value))
}

/// ClickHouse time-range injection: splices a `toDateTime(...) BETWEEN` filter
/// into the SQL string, or fills __START__/__END__ placeholders. This path is
/// ClickHouse-only — VictoriaLogs uses [`vl_time_window`] instead.
fn apply_clickhouse_time_range(
    source: &Source,
    sql: String,
    args: &SqlArgs,
    ctx: &Context,
) -> Result<String> {
    if args.since.is_none() && args.from.is_none() && args.to.is_none() {
        return Ok(sql);
    }

    let time_range = parse_time_range(
        args.since.as_deref(),
        args.from.as_deref(),
        args.to.as_deref(),
        ctx.defaults.timezone.as_deref(),
    )?;
    let timestamp_field = source
        .meta_ts_field
        .as_deref()
        .filter(|field| !field.trim().is_empty())
        .unwrap_or("_timestamp");
    let condition = sql_time_condition(
        timestamp_field,
        &time_range.start,
        &time_range.end,
        &time_range.timezone,
    );

    if sql.contains("__START__") || sql.contains("__END__") {
        if !(sql.contains("__START__") && sql.contains("__END__")) {
            anyhow::bail!("SQL time placeholders must include both __START__ and __END__");
        }
        let start_expr = sql_datetime_expr(&time_range.start, &time_range.timezone);
        let end_expr = sql_datetime_expr(&time_range.end, &time_range.timezone);
        return Ok(sql
            .replace("__START__", &start_expr)
            .replace("__END__", &end_expr));
    }

    Ok(inject_sql_condition(&sql, &condition))
}

fn parse_time_range(
    since: Option<&str>,
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
            let start = end - parse_duration(since.unwrap_or("15m"))?;
            TimeInput::Instant { start, end }
        }
    };
    Ok(resolve_time_range(input, configured_tz))
}

fn parse_duration(s: &str) -> Result<ChronoDuration> {
    let s = s.trim();
    if s.is_empty() {
        return Ok(ChronoDuration::minutes(15));
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
        "m" => Ok(ChronoDuration::minutes(num)),
        "h" => Ok(ChronoDuration::hours(num)),
        "d" => Ok(ChronoDuration::days(num)),
        "w" => Ok(ChronoDuration::weeks(num)),
        _ => Ok(ChronoDuration::minutes(num)),
    }
}

fn sql_time_condition(
    timestamp_field: &str,
    start_time: &str,
    end_time: &str,
    timezone: &str,
) -> String {
    format!(
        "{} BETWEEN {} AND {}",
        sql_identifier(timestamp_field),
        sql_datetime_expr(start_time, timezone),
        sql_datetime_expr(end_time, timezone)
    )
}

fn sql_datetime_expr(value: &str, timezone: &str) -> String {
    format!(
        "toDateTime('{}', '{}')",
        sql_string(value),
        sql_string(timezone)
    )
}

fn sql_identifier(value: &str) -> String {
    format!("`{}`", value.trim_matches('`').replace('`', "``"))
}

fn sql_string(value: &str) -> String {
    value.replace('\\', "\\\\").replace('\'', "\\'")
}

fn inject_sql_condition(sql: &str, condition: &str) -> String {
    let trimmed = sql.trim();
    let has_semicolon = trimmed.ends_with(';');
    let body = trimmed.trim_end_matches(';').trim_end();
    let (where_pos, clause_pos) = scan_top_level_clauses(body);
    let insert_at = clause_pos.unwrap_or(body.len());

    let connector = if where_pos.map(|w| w < insert_at).unwrap_or(false) {
        "AND"
    } else {
        "WHERE"
    };

    let (head, tail) = body.split_at(insert_at);
    let separator = if tail.is_empty() { "" } else { " " };
    format!(
        "{} {} {}{}{}{}",
        head.trim_end(),
        connector,
        condition,
        separator,
        tail.trim_start(),
        if has_semicolon { ";" } else { "" }
    )
}

/// Walks the SQL skipping string literals, backtick identifiers, and
/// parenthesized groups (subqueries). Returns the byte offset of the first
/// top-level WHERE keyword and the first top-level clause boundary among
/// GROUP/ORDER/LIMIT/HAVING/SETTINGS/FORMAT.
fn scan_top_level_clauses(sql: &str) -> (Option<usize>, Option<usize>) {
    let bytes = sql.as_bytes();
    let mut i = 0;
    let mut paren_depth = 0i32;
    let mut where_pos: Option<usize> = None;
    let mut clause_pos: Option<usize> = None;

    while i < bytes.len() {
        let b = bytes[i];

        // Block comment /* ... */
        if b == b'/' && i + 1 < bytes.len() && bytes[i + 1] == b'*' {
            i += 2;
            while i + 1 < bytes.len() && !(bytes[i] == b'*' && bytes[i + 1] == b'/') {
                i += 1;
            }
            i = (i + 2).min(bytes.len());
            continue;
        }
        // Line comment -- ... \n
        if b == b'-' && i + 1 < bytes.len() && bytes[i + 1] == b'-' {
            i += 2;
            while i < bytes.len() && bytes[i] != b'\n' {
                i += 1;
            }
            continue;
        }
        // String literal '...'
        if b == b'\'' {
            i += 1;
            while i < bytes.len() {
                if bytes[i] == b'\\' && i + 1 < bytes.len() {
                    i += 2;
                    continue;
                }
                if bytes[i] == b'\'' {
                    i += 1;
                    break;
                }
                i += 1;
            }
            continue;
        }
        // Backtick identifier `...`
        if b == b'`' {
            i += 1;
            while i < bytes.len() && bytes[i] != b'`' {
                i += 1;
            }
            i = (i + 1).min(bytes.len());
            continue;
        }
        // Double-quoted identifier "..."
        if b == b'"' {
            i += 1;
            while i < bytes.len() && bytes[i] != b'"' {
                i += 1;
            }
            i = (i + 1).min(bytes.len());
            continue;
        }
        if b == b'(' {
            paren_depth += 1;
            i += 1;
            continue;
        }
        if b == b')' {
            paren_depth = (paren_depth - 1).max(0);
            i += 1;
            continue;
        }

        if paren_depth == 0 && is_keyword_boundary(bytes, i) {
            if where_pos.is_none() && matches_kw(bytes, i, b"WHERE") {
                where_pos = Some(i);
                i += 5;
                continue;
            }
            if clause_pos.is_none() {
                for kw in [
                    &b"GROUP"[..],
                    &b"ORDER"[..],
                    &b"LIMIT"[..],
                    &b"HAVING"[..],
                    &b"SETTINGS"[..],
                    &b"FORMAT"[..],
                ] {
                    if matches_kw(bytes, i, kw) {
                        // Ensure GROUP/ORDER are followed by BY (with whitespace)
                        let needs_by = kw == b"GROUP" || kw == b"ORDER";
                        if needs_by {
                            let after = i + kw.len();
                            if !followed_by(bytes, after, b"BY") {
                                continue;
                            }
                        }
                        clause_pos = Some(i);
                        break;
                    }
                }
                if clause_pos.is_some() {
                    return (where_pos, clause_pos);
                }
            }
        }
        i += 1;
    }
    (where_pos, clause_pos)
}

fn is_keyword_boundary(bytes: &[u8], pos: usize) -> bool {
    if pos == 0 {
        return true;
    }
    let prev = bytes[pos - 1];
    !prev.is_ascii_alphanumeric() && prev != b'_'
}

fn matches_kw(bytes: &[u8], pos: usize, kw: &[u8]) -> bool {
    if pos + kw.len() > bytes.len() {
        return false;
    }
    for (i, &k) in kw.iter().enumerate() {
        if bytes[pos + i].to_ascii_uppercase() != k {
            return false;
        }
    }
    let after = pos + kw.len();
    if after == bytes.len() {
        return true;
    }
    let next = bytes[after];
    !next.is_ascii_alphanumeric() && next != b'_'
}

fn followed_by(bytes: &[u8], from: usize, kw: &[u8]) -> bool {
    let mut i = from;
    while i < bytes.len() && (bytes[i] == b' ' || bytes[i] == b'\t' || bytes[i] == b'\n') {
        i += 1;
    }
    matches_kw(bytes, i, kw)
}

fn effective_query_timeout_secs(
    requested_timeout_secs: u32,
    output: &OutputFormat,
    stream: bool,
) -> u32 {
    if stream || matches!(output, OutputFormat::Csv) {
        requested_timeout_secs.max(STREAMING_SQL_MIN_TIMEOUT_SECS)
    } else {
        requested_timeout_secs
    }
}

fn sql_transport_timeout_secs(context_timeout_secs: u64, query_timeout_secs: u32) -> u64 {
    context_timeout_secs.max(u64::from(query_timeout_secs) + SQL_HTTP_TIMEOUT_HEADROOM_SECS)
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

fn prompt_sql_interactive() -> Result<String> {
    let sql = Text::new("Raw query:")
        .with_help_message(
            "Enter a source-native query (SQL for ClickHouse, LogsQL for VictoriaLogs)",
        )
        .prompt()
        .context("Failed to read raw query")?;

    if sql.trim().is_empty() {
        anyhow::bail!("Raw query cannot be empty");
    }
    Ok(sql)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn effective_timeout_keeps_preview_timeout_for_buffered_queries() {
        assert_eq!(
            effective_query_timeout_secs(45, &OutputFormat::Json, false),
            45
        );
    }

    #[test]
    fn effective_timeout_enforces_streaming_minimum() {
        assert_eq!(
            effective_query_timeout_secs(30, &OutputFormat::Jsonl, true),
            120
        );
    }

    #[test]
    fn effective_timeout_enforces_csv_export_minimum() {
        assert_eq!(
            effective_query_timeout_secs(30, &OutputFormat::Csv, false),
            120
        );
    }

    #[test]
    fn transport_timeout_never_undercuts_query_timeout() {
        assert_eq!(sql_transport_timeout_secs(30, 120), 180);
        assert_eq!(sql_transport_timeout_secs(300, 120), 300);
    }

    #[test]
    fn injects_time_condition_into_existing_where() {
        let sql = "SELECT count() FROM logs.app WHERE service = 'api' GROUP BY service";
        let out = inject_sql_condition(
            sql,
            "`_timestamp` BETWEEN toDateTime('a') AND toDateTime('b')",
        );
        assert_eq!(
            out,
            "SELECT count() FROM logs.app WHERE service = 'api' AND `_timestamp` BETWEEN toDateTime('a') AND toDateTime('b') GROUP BY service"
        );
    }

    #[test]
    fn injects_time_condition_without_where() {
        let sql = "SELECT count() FROM logs.app ORDER BY count() DESC";
        let out = inject_sql_condition(
            sql,
            "`_timestamp` BETWEEN toDateTime('a') AND toDateTime('b')",
        );
        assert_eq!(
            out,
            "SELECT count() FROM logs.app WHERE `_timestamp` BETWEEN toDateTime('a') AND toDateTime('b') ORDER BY count() DESC"
        );
    }

    #[test]
    fn formats_time_condition_with_timezone() {
        let condition = sql_time_condition(
            "_timestamp",
            "2026-05-19 09:15:00",
            "2026-05-19 09:30:00",
            "UTC",
        );
        assert_eq!(
            condition,
            "`_timestamp` BETWEEN toDateTime('2026-05-19 09:15:00', 'UTC') AND toDateTime('2026-05-19 09:30:00', 'UTC')"
        );
    }

    #[test]
    fn ignores_where_inside_string_literal() {
        let sql = "SELECT msg FROM logs.app WHERE msg = 'request WHERE matters' GROUP BY msg";
        let out = inject_sql_condition(
            sql,
            "`_timestamp` BETWEEN toDateTime('a') AND toDateTime('b')",
        );
        // Should detect the real WHERE (after msg =), not the WHERE inside the literal.
        assert_eq!(
            out,
            "SELECT msg FROM logs.app WHERE msg = 'request WHERE matters' AND `_timestamp` BETWEEN toDateTime('a') AND toDateTime('b') GROUP BY msg"
        );
    }

    #[test]
    fn ignores_limit_inside_string_literal() {
        let sql = "SELECT * FROM logs.app WHERE msg = 'LIMIT exceeded'";
        let out = inject_sql_condition(sql, "X");
        // The LIMIT inside the literal should not be treated as a clause boundary;
        // the AND should be appended at end of body.
        assert_eq!(
            out,
            "SELECT * FROM logs.app WHERE msg = 'LIMIT exceeded' AND X"
        );
    }

    #[test]
    fn ignores_where_inside_subquery() {
        let sql = "SELECT * FROM logs.app WHERE id IN (SELECT id FROM t WHERE x = 1) GROUP BY id";
        let out = inject_sql_condition(sql, "X");
        // Top-level WHERE found; inner WHERE inside the parenthesized subquery
        // is ignored, so we append "AND X" before GROUP BY.
        assert_eq!(
            out,
            "SELECT * FROM logs.app WHERE id IN (SELECT id FROM t WHERE x = 1) AND X GROUP BY id"
        );
    }

    #[test]
    fn ignores_clause_keywords_in_subquery() {
        let sql = "SELECT * FROM (SELECT * FROM logs.app LIMIT 5) AS s";
        let out = inject_sql_condition(sql, "X");
        // The inner LIMIT inside the subquery must not become the top-level
        // clause boundary; injection appends WHERE at end of body.
        assert_eq!(
            out,
            "SELECT * FROM (SELECT * FROM logs.app LIMIT 5) AS s WHERE X"
        );
    }

    #[test]
    fn skips_line_comment_when_scanning() {
        let sql = "SELECT * FROM logs.app -- WHERE never\n WHERE level='error'";
        let out = inject_sql_condition(sql, "X");
        assert_eq!(
            out,
            "SELECT * FROM logs.app -- WHERE never\n WHERE level='error' AND X"
        );
    }

    #[test]
    fn wall_clock_to_utc_converts_from_zone() {
        // Obtain the concrete zone via resolve_timezone so this test needs no
        // direct chrono-tz dependency.
        let tz = resolve_timezone(Some("Asia/Kolkata"));
        // 09:15 IST (UTC+5:30) == 03:45 UTC.
        let utc = wall_clock_to_utc("2026-05-19 09:15:00", tz).unwrap();
        assert_eq!(
            utc.to_rfc3339_opts(SecondsFormat::Secs, true),
            "2026-05-19T03:45:00Z"
        );
    }
}
