use anyhow::{Context, Result};
use chrono::{Duration, TimeZone, Utc};
use clap::Args;
use inquire::Select;
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

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
pub struct CollectionsArgs {
    /// Collection name to run (optional - lists collections if not provided)
    name: Option<String>,

    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Override time range with relative time (e.g., 15m, 1h, 24h)
    #[arg(long, short = 's')]
    since: Option<String>,

    /// Override limit
    #[arg(long, short = 'l')]
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

    /// Variable overrides (format: name=value)
    #[arg(long = "var", short = 'V', value_name = "NAME=VALUE")]
    variables: Vec<String>,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    JsonFlat,
    Table,
    List,
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

pub async fn run(args: CollectionsArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();

    // Clone args values we need before any potential moves
    let arg_team = args.team.clone();
    let arg_source = args.source.clone();
    let arg_name = args.name.clone();

    // Detect interactive mode
    let is_interactive = arg_name.is_none()
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

    // Fetch collections
    let collections = client
        .list_collections(team_id, source_id)
        .await
        .context("Failed to list collections")?;

    // If no name provided (or list output), show the list
    if arg_name.is_none() && !is_interactive {
        return list_collections(&collections, &args);
    }

    // Get the collection to run
    let collection = if is_interactive {
        prompt_collection_interactive(&collections)?
    } else {
        let name = arg_name.as_ref().unwrap();
        collections
            .iter()
            .find(|c| c.name.eq_ignore_ascii_case(name))
            .ok_or_else(|| anyhow::anyhow!("Collection '{}' not found", name))?
            .clone()
    };

    // Run the collection
    run_collection(&config, client, team_id, source_id, &collection, &args, ctx).await
}

fn list_collections(collections: &[Collection], args: &CollectionsArgs) -> Result<()> {
    if collections.is_empty() {
        println!("No collections found for this source.");
        return Ok(());
    }

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(collections)?);
        }
        OutputFormat::Jsonl => {
            for c in collections {
                println!("{}", serde_json::to_string(c)?);
            }
        }
        OutputFormat::Msg => {
            anyhow::bail!(
                "--output msg is for running collections, not listing. Use --output text|json|jsonl|table."
            );
        }
        OutputFormat::JsonFlat => {
            anyhow::bail!(
                "--output json-flat is for running collections, not listing. Use --output json or jsonl."
            );
        }
        OutputFormat::List | OutputFormat::Text | OutputFormat::Table => {
            println!("{:<4} {:<30} {:<12} DESCRIPTION", "ID", "NAME", "TYPE");
            println!("{}", "-".repeat(70));
            for c in collections {
                let desc = c.description.as_deref().unwrap_or("");
                let desc_truncated = if desc.len() > 30 {
                    format!("{}...", &desc[..27])
                } else {
                    desc.to_string()
                };
                println!(
                    "{:<4} {:<30} {:<12} {}",
                    c.id,
                    truncate_str(&c.name, 28),
                    c.query_type,
                    desc_truncated
                );
            }
            println!("\n{} collections", collections.len());
        }
    }

    Ok(())
}

fn truncate_str(s: &str, max_len: usize) -> String {
    if s.len() > max_len {
        format!("{}...", &s[..max_len.saturating_sub(3)])
    } else {
        s.to_string()
    }
}

async fn run_collection(
    config: &Config,
    client: &Client,
    team_id: i64,
    source_id: i64,
    collection: &Collection,
    args: &CollectionsArgs,
    ctx: &logchef_core::config::Context,
) -> Result<()> {
    // Parse the query content
    let content: CollectionQueryContent =
        serde_json::from_str(&collection.query_content).context("Failed to parse query content")?;

    let query_str = content.content.unwrap_or_default();

    // Apply variable overrides
    let mut final_query = query_str.clone();
    let var_overrides = parse_variable_overrides(&args.variables);

    // Replace variables from collection
    if let Some(vars) = &content.variables {
        for var in vars {
            let value = var_overrides
                .get(&var.name)
                .cloned()
                .or_else(|| var.value.as_ref().map(json_value_to_string))
                .unwrap_or_default();
            // Replace {{name}} with value
            final_query = final_query.replace(&format!("{{{{{}}}}}", var.name), &value);
        }
    }

    // Determine time range
    let (start_time, end_time) = if let Some(since) = &args.since {
        // Use override
        let end = Utc::now();
        let start = end - parse_duration(since)?;
        let format = "%Y-%m-%d %H:%M:%S";
        (
            start.format(format).to_string(),
            end.format(format).to_string(),
        )
    } else if let Some(tr) = &content.time_range {
        if let Some(rel) = &tr.relative {
            let end = Utc::now();
            let start = end - parse_duration(rel)?;
            let format = "%Y-%m-%d %H:%M:%S";
            (
                start.format(format).to_string(),
                end.format(format).to_string(),
            )
        } else if let Some(abs) = &tr.absolute {
            let format = "%Y-%m-%d %H:%M:%S";
            let start = Utc
                .timestamp_millis_opt(abs.start)
                .single()
                .ok_or_else(|| anyhow::anyhow!("Invalid start timestamp"))?;
            let end = Utc
                .timestamp_millis_opt(abs.end)
                .single()
                .ok_or_else(|| anyhow::anyhow!("Invalid end timestamp"))?;
            (
                start.format(format).to_string(),
                end.format(format).to_string(),
            )
        } else {
            // Default to last 15 minutes
            let end = Utc::now();
            let start = end - Duration::minutes(15);
            let format = "%Y-%m-%d %H:%M:%S";
            (
                start.format(format).to_string(),
                end.format(format).to_string(),
            )
        }
    } else {
        // Default to last 15 minutes
        let end = Utc::now();
        let start = end - Duration::minutes(15);
        let format = "%Y-%m-%d %H:%M:%S";
        (
            start.format(format).to_string(),
            end.format(format).to_string(),
        )
    };

    let limit = args.limit.or(content.limit).unwrap_or(100);

    eprintln!(
        "Running collection: {} ({})",
        collection.name, collection.query_type
    );

    let response = if collection.query_type == "sql" {
        let request = SqlQueryRequest {
            raw_sql: final_query,
            limit: None, // SQL queries control their own limit
            timezone: ctx.defaults.timezone.clone(),
            start_time: None,
            end_time: None,
            query_timeout: Some(30),
        };
        client
            .query_sql(team_id, source_id, &request)
            .await
            .context("SQL query failed")?
    } else {
        // logchefql
        let request = QueryRequest {
            query: final_query,
            start_time,
            end_time,
            timezone: ctx.defaults.timezone.clone(),
            limit: Some(limit),
            query_timeout: None,
        };
        client
            .query_logchefql(team_id, source_id, &request)
            .await
            .context("Query failed")?
    };

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
            if is_tty {
                eprintln!(
                    "\n{} logs | {}ms | {} rows read",
                    entries.len(),
                    response.stats.execution_time_ms,
                    response.stats.rows_read
                );
            }
        }
        OutputFormat::JsonFlat => {
            print_json_flat(entries)?;
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
        OutputFormat::Msg => {
            print_msg(entries, &response.columns, collection.query_type == "sql");
        }
        OutputFormat::Text | OutputFormat::List => {
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

fn json_value_to_string(value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Null => String::new(),
        _ => value.to_string(),
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

fn prompt_collection_interactive(collections: &[Collection]) -> Result<Collection> {
    if collections.is_empty() {
        anyhow::bail!("No collections available for this source");
    }

    let options: Vec<String> = collections
        .iter()
        .map(|c| format!("{} [{}]", c.name, c.query_type))
        .collect();
    let selection = Select::new("Select collection:", options)
        .prompt()
        .context("Failed to select collection")?;

    let collection = collections
        .iter()
        .find(|c| selection.starts_with(&c.name))
        .ok_or_else(|| anyhow::anyhow!("Collection not found"))?;

    Ok(collection.clone())
}
