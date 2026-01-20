use anyhow::{Context, Result};
use chrono::{Duration, Utc};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Client, QueryRequest};
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};

use crate::cli::GlobalArgs;

#[derive(Args)]
pub struct QueryArgs {
    query: Option<String>,

    #[arg(long, short = 's')]
    since: Option<String>,

    #[arg(long)]
    from: Option<String>,

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

    #[arg(long)]
    show_sql: bool,

    #[arg(long = "highlight", value_name = "COLOR:WORDS")]
    highlights: Vec<String>,

    #[arg(long = "disable-highlight", value_name = "GROUP")]
    disable_highlights: Vec<String>,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    Table,
}

pub async fn run(args: QueryArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;

    let resolved = resolve_context(&config, &global)?;

    let (ctx, ctx_name, is_ephemeral): (&logchef_core::config::Context, String, bool) =
        match &resolved {
            ResolvedContext::Saved(ctx, name) => (*ctx, name.clone(), false),
            ResolvedContext::Ephemeral(ctx) => (ctx, "(ephemeral)".to_string(), true),
        };

    let client = if let Some(token) = &global.token {
        Client::from_context(ctx)?.with_token(token.clone())
    } else {
        Client::from_context(ctx)?
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

    let team_name = args
        .team
        .or(ctx.defaults.team.clone())
        .ok_or_else(|| anyhow::anyhow!("Team not specified. Use --team or set defaults.team"))?;

    let source_name = args.source.or(ctx.defaults.source.clone()).ok_or_else(|| {
        anyhow::anyhow!("Source not specified. Use --source or set defaults.source")
    })?;

    let teams = client.list_teams().await.context("Failed to list teams")?;
    let team = teams
        .iter()
        .find(|t| t.name == team_name || t.id.to_string() == team_name)
        .ok_or_else(|| anyhow::anyhow!("Team '{}' not found", team_name))?;

    let sources = client
        .list_sources(team.id)
        .await
        .context("Failed to list sources")?;
    let source = sources
        .iter()
        .find(|s| s.name == source_name || s.id.to_string() == source_name)
        .ok_or_else(|| anyhow::anyhow!("Source '{}' not found", source_name))?;

    let since = args.since.unwrap_or_else(|| ctx.defaults.since.clone());
    let limit = args.limit.unwrap_or(ctx.defaults.limit);

    let (start_time, end_time) =
        parse_time_range(&since, args.from.as_deref(), args.to.as_deref())?;

    let request = QueryRequest {
        query: args.query.unwrap_or_default(),
        start_time,
        end_time,
        timezone: ctx.defaults.timezone.clone(),
        limit: Some(limit),
        query_timeout: None,
    };

    let response = client
        .query_logchefql(team.id, source.id, &request)
        .await
        .context("Query failed")?;

    if args.show_sql
        && let Some(sql) = &response.generated_sql
    {
        eprintln!("Generated SQL: {}\n", sql);
    }

    let entries = response.entries();

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&entries)?);
        }
        OutputFormat::Jsonl => {
            for entry in entries {
                println!("{}", serde_json::to_string(entry)?);
            }
        }
        OutputFormat::Table => {
            print_table(entries, &response.columns);
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
        }
    }

    eprintln!(
        "\n{} logs | {}ms | {} rows read",
        entries.len(),
        response.stats.execution_time_ms,
        response.stats.rows_read
    );

    Ok(())
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

fn parse_time_range(since: &str, from: Option<&str>, to: Option<&str>) -> Result<(String, String)> {
    let format = "%Y-%m-%d %H:%M:%S";

    match (from, to) {
        (Some(from), Some(to)) => Ok((from.to_string(), to.to_string())),
        (Some(_), None) => anyhow::bail!("--from requires --to to be specified"),
        (None, Some(_)) => anyhow::bail!("--to requires --from to be specified"),
        (None, None) => {
            let end = Utc::now();
            let start = end - parse_duration(since)?;
            Ok((
                start.format(format).to_string(),
                end.format(format).to_string(),
            ))
        }
    }
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
