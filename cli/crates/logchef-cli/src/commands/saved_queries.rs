use anyhow::{Context, Result};
use chrono::{Duration, TimeZone, Utc};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{
    Client, Collection, CollectionQueryContent, Column, QueryRequest, QueryStats, SqlQueryRequest,
};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};
use serde::Serialize;
use std::io::IsTerminal;
use url::Url;

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
pub struct SavedQueriesArgs {
    /// Saved-query name, numeric ID, or explorer URL. Lists saved queries if omitted.
    query: Option<String>,

    /// Team ID or name. Defaults to the query's resolved team.
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name. Defaults to the query's source.
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Override row limit
    #[arg(long, short = 'l')]
    limit: Option<u32>,

    /// Variable overrides (format: name=value)
    #[arg(long = "var", short = 'V', value_name = "NAME=VALUE")]
    variables: Vec<String>,

    /// Print the resolved query without running it
    #[arg(long)]
    show_sql: bool,

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

    /// Query timeout in seconds
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
    columns: &'a [Column],
}

#[derive(Debug)]
struct QuerySelector {
    id: Option<i64>,
    name: Option<String>,
    url_team_id: Option<i64>,
    url_source_id: Option<i64>,
}

pub async fn run(args: SavedQueriesArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);

    if args.query.is_none() {
        let source_filter = resolve_optional_source_filter(client, &mut cache, ctx, &args).await?;
        let queries = client
            .list_saved_queries(source_filter)
            .await
            .context("Failed to list saved queries")?;
        return list_saved_queries(&queries, &args);
    }

    let selector = parse_query_selector(args.query.as_deref().unwrap())?;
    let preferred_team_id =
        resolve_preferred_team(client, &mut cache, ctx, &args, &selector).await?;
    let mut resolved = resolve_query(client, &selector, preferred_team_id).await?;

    let team_id = resolve_execution_team(
        client,
        &mut cache,
        ctx,
        args.team.as_deref(),
        preferred_team_id,
        resolved.resolved_team_id,
    )
    .await?;
    let source_id = resolve_execution_source(
        client,
        &mut cache,
        ctx,
        team_id,
        args.source.as_deref(),
        selector.url_source_id,
        resolved.query.source_id,
    )
    .await?;

    if source_id != resolved.query.source_id {
        resolved.query.source_id = source_id;
    }

    run_saved_query(
        &config,
        client,
        team_id,
        source_id,
        &resolved.query,
        &args,
        ctx,
    )
    .await
}

fn list_saved_queries(queries: &[Collection], args: &SavedQueriesArgs) -> Result<()> {
    if queries.is_empty() {
        println!("No saved queries found.");
        return Ok(());
    }

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(queries)?);
        }
        OutputFormat::Jsonl => {
            for q in queries {
                println!("{}", serde_json::to_string(q)?);
            }
        }
        OutputFormat::Msg => {
            anyhow::bail!(
                "--output msg is for running a saved query, not listing. Use --output text|json|jsonl|table."
            );
        }
        OutputFormat::JsonFlat => {
            anyhow::bail!(
                "--output json-flat is for running a saved query, not listing. Use --output json or jsonl."
            );
        }
        OutputFormat::Text | OutputFormat::Table => {
            println!(
                "{:<4} {:<30} {:<12} {:<24} DESCRIPTION",
                "ID", "NAME", "TYPE", "SOURCE"
            );
            println!("{}", "-".repeat(95));
            for q in queries {
                let desc = q.description.as_deref().unwrap_or("");
                println!(
                    "{:<4} {:<30} {:<12} {:<24} {}",
                    q.id,
                    truncate_str(&q.name, 28),
                    q.query_type,
                    truncate_str(q.source_name.as_deref().unwrap_or(""), 22),
                    truncate_str(desc, 28)
                );
            }
            println!("\n{} saved queries", queries.len());
        }
    }

    Ok(())
}

async fn run_saved_query(
    config: &Config,
    client: &Client,
    team_id: i64,
    source_id: i64,
    query: &Collection,
    args: &SavedQueriesArgs,
    ctx: &logchef_core::config::Context,
) -> Result<()> {
    let content: CollectionQueryContent =
        serde_json::from_str(&query.query_content).context("Failed to parse query content")?;

    let mut final_query = content.content.clone().unwrap_or_default();
    let var_overrides = parse_variable_overrides(&args.variables);
    if let Some(vars) = &content.variables {
        for var in vars {
            let value = var_overrides
                .get(&var.name)
                .cloned()
                .or_else(|| var.value.as_ref().map(json_value_to_string))
                .unwrap_or_default();
            final_query = final_query.replace(&format!("{{{{{}}}}}", var.name), &value);
        }
    }

    let (start_time, end_time) = parse_time_range(&content)?;
    let limit = args.limit.or(content.limit).unwrap_or(ctx.defaults.limit);

    if args.show_sql {
        println!("{}", final_query);
        return Ok(());
    }

    eprintln!("Running saved query: {} ({})", query.name, query.query_type);

    let response = if query.query_type == "sql" {
        let request = SqlQueryRequest {
            raw_sql: final_query,
            limit: args.limit,
            timezone: ctx.defaults.timezone.clone(),
            start_time: None,
            end_time: None,
            query_timeout: Some(args.timeout),
        };
        client
            .query_sql(team_id, source_id, &request)
            .await
            .context("SQL query failed")?
    } else {
        let request = QueryRequest {
            query: final_query,
            start_time,
            end_time,
            timezone: ctx.defaults.timezone.clone(),
            limit: Some(limit),
            query_timeout: Some(args.timeout),
        };
        client
            .query_logchefql(team_id, source_id, &request)
            .await
            .context("Query failed")?
    };

    print_query_response(config, query, args, &response)
}

fn print_query_response(
    config: &Config,
    _query: &Collection,
    args: &SavedQueriesArgs,
    response: &logchef_core::api::QueryResponse,
) -> Result<()> {
    let entries = response.entries();
    let is_tty = std::io::stdout().is_terminal();

    match args.output {
        OutputFormat::Json => {
            let output = JsonOutput {
                logs: entries,
                count: entries.len(),
                stats: &response.stats,
                query_id: response.query_id.as_deref(),
                generated_sql: response.generated_sql.as_deref(),
                columns: &response.columns,
            };
            println!("{}", serde_json::to_string_pretty(&output)?);
        }
        OutputFormat::Jsonl => {
            for entry in entries {
                println!("{}", serde_json::to_string(entry)?);
            }
            print_stats_if_tty(is_tty, entries.len(), &response.stats);
        }
        OutputFormat::JsonFlat => {
            print_json_flat(entries)?;
        }
        OutputFormat::Table => {
            print_table(entries, &response.columns);
            print_stats_if_tty(is_tty, entries.len(), &response.stats);
        }
        OutputFormat::Msg => {
            print_msg(entries, &response.columns, _query.query_type == "sql");
        }
        OutputFormat::Text => {
            let highlighter = if args.no_highlight || !is_tty {
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
            print_stats_if_tty(is_tty, entries.len(), &response.stats);
        }
    }

    Ok(())
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

fn print_stats_if_tty(is_tty: bool, count: usize, stats: &QueryStats) {
    if is_tty {
        eprintln!(
            "\n{} logs | {}ms | {} rows read",
            count, stats.execution_time_ms, stats.rows_read
        );
    }
}

async fn resolve_query(
    client: &Client,
    selector: &QuerySelector,
    preferred_team_id: Option<i64>,
) -> Result<logchef_core::api::ResolvedSavedQuery> {
    if let Some(id) = selector.id {
        return client
            .resolve_saved_query(id, preferred_team_id)
            .await
            .with_context(|| format!("Failed to resolve saved query {}", id));
    }

    let name = selector
        .name
        .as_ref()
        .ok_or_else(|| anyhow::anyhow!("Saved query selector is empty"))?;
    let queries = client
        .list_saved_queries(None)
        .await
        .context("Failed to list saved queries")?;
    let matches: Vec<_> = queries
        .iter()
        .filter(|q| q.name.eq_ignore_ascii_case(name))
        .collect();

    match matches.as_slice() {
        [] => anyhow::bail!("Saved query '{}' not found", name),
        [query] => client
            .resolve_saved_query(query.id, preferred_team_id)
            .await
            .with_context(|| format!("Failed to resolve saved query {}", query.id)),
        many => {
            let ids = many
                .iter()
                .map(|q| q.id.to_string())
                .collect::<Vec<_>>()
                .join(", ");
            anyhow::bail!(
                "Saved query name '{}' is ambiguous. Matching IDs: {}",
                name,
                ids
            )
        }
    }
}

async fn resolve_optional_source_filter(
    client: &Client,
    cache: &mut Cache,
    ctx: &logchef_core::config::Context,
    args: &SavedQueriesArgs,
) -> Result<Option<i64>> {
    let Some(source) = args.source.as_deref() else {
        return Ok(None);
    };
    if let Identifier::Id(id) = parse_identifier(source) {
        return Ok(Some(id));
    }
    let team_id = resolve_execution_team(client, cache, ctx, args.team.as_deref(), None, 0).await?;
    Ok(Some(
        resolve_source_id(client, cache, team_id, source).await?,
    ))
}

async fn resolve_preferred_team(
    client: &Client,
    cache: &mut Cache,
    ctx: &logchef_core::config::Context,
    args: &SavedQueriesArgs,
    selector: &QuerySelector,
) -> Result<Option<i64>> {
    if let Some(team) = args.team.as_deref() {
        return Ok(Some(resolve_team_id(client, cache, ctx, team).await?));
    }
    Ok(selector.url_team_id)
}

async fn resolve_execution_team(
    client: &Client,
    cache: &mut Cache,
    ctx: &logchef_core::config::Context,
    team_arg: Option<&str>,
    preferred_team_id: Option<i64>,
    resolved_team_id: i64,
) -> Result<i64> {
    if let Some(team) = team_arg {
        return resolve_team_id(client, cache, ctx, team).await;
    }
    if let Some(id) = preferred_team_id {
        return Ok(id);
    }
    if resolved_team_id != 0 {
        return Ok(resolved_team_id);
    }
    let default_team = ctx.defaults.team_with_env();
    if let Some(team) = default_team.as_deref() {
        return resolve_team_id(client, cache, ctx, team).await;
    }
    anyhow::bail!("Team not specified. Use --team or set defaults.team.")
}

async fn resolve_execution_source(
    client: &Client,
    cache: &mut Cache,
    ctx: &logchef_core::config::Context,
    team_id: i64,
    source_arg: Option<&str>,
    url_source_id: Option<i64>,
    query_source_id: i64,
) -> Result<i64> {
    if let Some(source) = source_arg {
        return resolve_source_id(client, cache, team_id, source).await;
    }
    if let Some(id) = url_source_id {
        return Ok(id);
    }
    if query_source_id != 0 {
        return Ok(query_source_id);
    }
    let default_source = ctx.defaults.source_with_env();
    if let Some(source) = default_source.as_deref() {
        return resolve_source_id(client, cache, team_id, source).await;
    }
    anyhow::bail!("Source not specified. Use --source or set defaults.source.")
}

async fn resolve_team_id(
    client: &Client,
    cache: &mut Cache,
    _ctx: &logchef_core::config::Context,
    team: &str,
) -> Result<i64> {
    match parse_identifier(team) {
        Identifier::Id(id) => Ok(id),
        Identifier::Name(name) => {
            if let Some(id) = cache.get_team_id(&name) {
                return Ok(id);
            }
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
                .ok_or_else(|| anyhow::anyhow!("Team '{}' not found", name))
        }
    }
}

async fn resolve_source_id(
    client: &Client,
    cache: &mut Cache,
    team_id: i64,
    source: &str,
) -> Result<i64> {
    match parse_identifier(source) {
        Identifier::Id(id) => Ok(id),
        Identifier::Name(name) => {
            if let Some(id) = cache.get_source_id(team_id, &name) {
                return Ok(id);
            }

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
                .ok_or_else(|| anyhow::anyhow!("Source '{}' not found", name))
        }
    }
}

fn parse_query_selector(input: &str) -> Result<QuerySelector> {
    if let Ok(url) = Url::parse(input)
        && url.path().contains("/logs/explore")
    {
        let id = parse_query_param_i64(&url, "id")
            .ok_or_else(|| anyhow::anyhow!("Explorer URL is missing saved-query id parameter"))?;
        return Ok(QuerySelector {
            id: Some(id),
            name: None,
            url_team_id: parse_query_param_i64(&url, "team"),
            url_source_id: parse_query_param_i64(&url, "source"),
        });
    }

    if let Ok(id) = input.parse::<i64>() {
        if id <= 0 {
            anyhow::bail!("Saved query ID must be positive");
        }
        return Ok(QuerySelector {
            id: Some(id),
            name: None,
            url_team_id: None,
            url_source_id: None,
        });
    }

    Ok(QuerySelector {
        id: None,
        name: Some(input.to_string()),
        url_team_id: None,
        url_source_id: None,
    })
}

fn parse_query_param_i64(url: &Url, key: &str) -> Option<i64> {
    url.query_pairs()
        .find(|(k, _)| k == key)
        .and_then(|(_, v)| v.parse::<i64>().ok())
}

fn parse_time_range(content: &CollectionQueryContent) -> Result<(String, String)> {
    let format = "%Y-%m-%d %H:%M:%S";

    if let Some(tr) = &content.time_range {
        if let Some(rel) = &tr.relative {
            let end = Utc::now();
            let start = end - parse_duration(rel)?;
            return Ok((
                start.format(format).to_string(),
                end.format(format).to_string(),
            ));
        }

        if let Some(abs) = &tr.absolute {
            let start = Utc
                .timestamp_millis_opt(abs.start)
                .single()
                .ok_or_else(|| anyhow::anyhow!("Invalid start timestamp"))?;
            let end = Utc
                .timestamp_millis_opt(abs.end)
                .single()
                .ok_or_else(|| anyhow::anyhow!("Invalid end timestamp"))?;
            return Ok((
                start.format(format).to_string(),
                end.format(format).to_string(),
            ));
        }
    }

    let end = Utc::now();
    let start = end - Duration::minutes(15);
    Ok((
        start.format(format).to_string(),
        end.format(format).to_string(),
    ))
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

fn parse_variable_overrides(vars: &[String]) -> std::collections::HashMap<String, String> {
    vars.iter()
        .filter_map(|v| {
            let parts: Vec<&str> = v.splitn(2, '=').collect();
            if parts.len() == 2 {
                Some((parts[0].to_string(), parts[1].to_string()))
            } else {
                None
            }
        })
        .collect()
}

fn json_value_to_string(value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Null => String::new(),
        _ => value.to_string(),
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

fn truncate_str(s: &str, max_len: usize) -> String {
    if s.len() > max_len {
        format!("{}...", &s[..max_len.saturating_sub(3)])
    } else {
        s.to_string()
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_explorer_url_selector() {
        let selector =
            parse_query_selector("https://logs.example.com/logs/explore?team=8&source=11&id=14")
                .unwrap();
        assert_eq!(selector.id, Some(14));
        assert_eq!(selector.url_team_id, Some(8));
        assert_eq!(selector.url_source_id, Some(11));
    }

    #[test]
    fn parses_numeric_selector() {
        let selector = parse_query_selector("42").unwrap();
        assert_eq!(selector.id, Some(42));
        assert!(selector.name.is_none());
    }

    #[test]
    fn parses_name_selector() {
        let selector = parse_query_selector("Error Dashboard").unwrap();
        assert_eq!(selector.name.as_deref(), Some("Error Dashboard"));
    }
}
