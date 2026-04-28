use anyhow::{Context, Result};
use clap::Args;
use inquire::{Select, Text};
use logchef_core::Config;
use logchef_core::api::{Client, Column, ExportSqlRequest, QueryStats, SqlQueryRequest};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};
use serde::Serialize;
use std::io::{IsTerminal, Read, Write};
use tokio::time::{Duration, sleep};

use crate::cli::GlobalArgs;

const STREAMING_SQL_MIN_TIMEOUT_SECS: u32 = 120;
const SQL_HTTP_TIMEOUT_HEADROOM_SECS: u64 = 60;

#[derive(Args)]
pub struct SqlArgs {
    /// Raw SQL query to execute. Use '-' to read from stdin.
    sql: Option<String>,

    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

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
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    Csv,
    Table,
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

    let resolved = resolve_context(&config, &global)?;

    let (ctx, ctx_name, is_ephemeral): (&logchef_core::config::Context, String, bool) =
        match &resolved {
            ResolvedContext::Saved(ctx, name) => (*ctx, name.clone(), false),
            ResolvedContext::Ephemeral(ctx) => (ctx, "(ephemeral)".to_string(), true),
        };

    let effective_query_timeout_secs =
        effective_query_timeout_secs(args.timeout, &args.output, args.stream);
    let client_timeout_secs =
        sql_transport_timeout_secs(ctx.timeout_secs, effective_query_timeout_secs);

    let client = if let Some(token) = &global.token {
        Client::from_context_with_timeout(ctx, client_timeout_secs)?.with_token(token.clone())
    } else {
        Client::from_context_with_timeout(ctx, client_timeout_secs)?
    };

    if !ctx.is_authenticated() && global.token.is_none() {
        if is_ephemeral {
            anyhow::bail!(
                "Token required for server '{}'. Use --token or run 'logchef auth --server {}'.",
                ctx.server_url,
                ctx.server_url
            );
        } else {
            anyhow::bail!(
                "Not authenticated for context '{}'. Run 'logchef auth' first.",
                ctx_name
            );
        }
    }

    let mut cache = Cache::new(&ctx.server_url);

    // Detect interactive mode: no sql provided, no team/source args, and running in a TTY
    let is_interactive = args.sql.is_none()
        && args.team.is_none()
        && args.source.is_none()
        && ctx.defaults.team.is_none()
        && ctx.defaults.source.is_none()
        && std::io::stdin().is_terminal();

    // Resolve team
    let team_id = if is_interactive {
        prompt_team_interactive(&client, &mut cache).await?
    } else {
        let team_input = args.team.or(ctx.defaults.team.clone()).ok_or_else(|| {
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
        prompt_source_interactive(&client, team_id, &mut cache).await?
    } else {
        let source_input = args.source.or(ctx.defaults.source.clone()).ok_or_else(|| {
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
                        if let Some(table_ref) = s.table_ref() {
                            cache_entries.push((table_ref, s.id));
                        }
                    }
                    cache.set_sources(team_id, &cache_entries);

                    sources
                        .iter()
                        .find(|s| s.name.eq_ignore_ascii_case(&name))
                        .or_else(|| {
                            sources.iter().find(|s| {
                                s.table_ref()
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
        match args.sql {
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
                    "SQL query required. Provide as argument or use '-' to read from stdin."
                )
            }
        }
    };

    if sql.is_empty() {
        anyhow::bail!("SQL query cannot be empty");
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

    let request = SqlQueryRequest {
        raw_sql: sql,
        limit: args.limit,
        timezone: ctx.defaults.timezone.clone(),
        start_time: None,
        end_time: None,
        query_timeout: Some(args.timeout),
    };

    let response = client
        .query_sql(team_id, source_id, &request)
        .await
        .context("SQL query failed")?;

    let entries = response.entries();
    let is_tty = std::io::stdout().is_terminal();

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
            if is_tty {
                eprintln!(
                    "\n{} logs | {}ms | {} rows read",
                    entries.len(),
                    response.stats.execution_time_ms,
                    response.stats.rows_read
                );
            }
        }
        OutputFormat::Table => {
            print_table(entries, &response.columns);
            if is_tty {
                eprintln!(
                    "\n{} logs | {}ms | {} rows read",
                    entries.len(),
                    response.stats.execution_time_ms,
                    response.stats.rows_read
                );
            }
        }
        OutputFormat::Csv => {
            anyhow::bail!("Use --stream --output csv for CSV output");
        }
        OutputFormat::Text => {
            let highlighter = if args.no_highlight {
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
            if is_tty {
                eprintln!(
                    "\n{} logs | {}ms | {} rows read",
                    entries.len(),
                    response.stats.execution_time_ms,
                    response.stats.rows_read
                );
            }
        }
    }

    Ok(())
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
}

enum ResolvedContext<'a> {
    Saved(&'a logchef_core::config::Context, String),
    Ephemeral(logchef_core::config::Context),
}

fn resolve_context<'a>(config: &'a Config, global: &GlobalArgs) -> Result<ResolvedContext<'a>> {
    if let Some(name) = &global.context {
        let ctx = config
            .get_context(name)
            .ok_or_else(|| anyhow::anyhow!("Context '{}' not found", name))?;
        return Ok(ResolvedContext::Saved(ctx, name.clone()));
    }

    if let Some(url) = &global.server {
        if let Some((name, ctx)) = config.find_context_by_url(url) {
            return Ok(ResolvedContext::Saved(ctx, name.to_string()));
        }
        let ephemeral = logchef_core::config::Context::new(url.clone());
        return Ok(ResolvedContext::Ephemeral(ephemeral));
    }

    let name = config
        .current_context_name()
        .ok_or_else(|| anyhow::anyhow!("No context configured. Run 'logchef auth' first."))?;
    let ctx = config
        .current_context()
        .ok_or_else(|| anyhow::anyhow!("Current context '{}' not found", name))?;

    Ok(ResolvedContext::Saved(ctx, name.to_string()))
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

    let options: Vec<String> = sources
        .iter()
        .map(|s| format!("{} ({})", s.name, s.table_ref().unwrap_or_default()))
        .collect();
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
        if let Some(table_ref) = s.table_ref() {
            cache_entries.push((table_ref, s.id));
        }
    }
    cache.set_sources(team_id, &cache_entries);

    Ok(source.id)
}

fn prompt_sql_interactive() -> Result<String> {
    let sql = Text::new("SQL query:")
        .with_help_message("Full ClickHouse SQL including time filters")
        .prompt()
        .context("Failed to read SQL query")?;

    if sql.trim().is_empty() {
        anyhow::bail!("SQL query cannot be empty");
    }
    Ok(sql)
}
