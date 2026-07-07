use anyhow::{Context, Result};
use chrono::{DateTime, Duration as ChronoDuration, NaiveDateTime, Utc};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Client, Column, LogEntry, QueryRequest};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};
use serde::Serialize;
use std::collections::HashMap;
use std::io::IsTerminal;
use tokio::time::{Duration, sleep};

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
pub struct TailArgs {
    /// LogChefQL query to follow.
    query: String,

    /// Team ID or name.
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name.
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Initial lookback window.
    #[arg(long, short = 's', default_value = "30s")]
    since: String,

    /// Poll interval in seconds.
    #[arg(long, default_value = "2")]
    interval: u64,

    /// Maximum rows to fetch per poll.
    #[arg(long, default_value = "100")]
    limit: u32,

    /// Stop after printing this many rows.
    #[arg(long)]
    max_lines: Option<usize>,

    /// Output format.
    #[arg(long, default_value = "text")]
    output: OutputFormat,

    /// Disable syntax highlighting.
    #[arg(long)]
    no_highlight: bool,

    /// Hide timestamp column in text output.
    #[arg(long)]
    no_timestamp: bool,

    /// Custom highlight rules (format: COLOR:word1,word2).
    #[arg(long = "highlight", value_name = "COLOR:WORDS")]
    highlights: Vec<String>,

    /// Disable specific highlight groups.
    #[arg(long = "disable-highlight", value_name = "GROUP")]
    disable_highlights: Vec<String>,

    /// Query timeout in seconds per poll.
    #[arg(long, default_value = "30")]
    timeout: u32,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Jsonl,
    Msg,
}

#[derive(Serialize)]
struct JsonlOutput<'a> {
    #[serde(flatten)]
    entry: &'a LogEntry,
}

pub async fn run(args: TailArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();
    let team_id = resolve_team_id(client, &mut cache, args.team.or(default_team)).await?;
    let source_id =
        resolve_source_id(client, &mut cache, team_id, args.source.or(default_source)).await?;

    let is_tty = std::io::stdout().is_terminal();
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

    let mut start = Utc::now() - parse_duration(&args.since)?;
    let mut seen: HashMap<DedupKey, ()> = HashMap::new();
    let mut printed = 0usize;
    let mut backpressure_warned = false;

    loop {
        let end = Utc::now();
        let request = QueryRequest {
            query: args.query.clone(),
            start_time: format_time(start),
            end_time: format_time(end),
            timezone: ctx.defaults.timezone.clone(),
            limit: Some(args.limit),
            query_timeout: Some(args.timeout),
        };

        let response = client
            .query_logchefql(team_id, source_id, &request)
            .await
            .context("Tail query failed")?;

        let returned = response.entries().len();
        let mut entries = response.entries().iter().collect::<Vec<_>>();
        entries.sort_by_key(|entry| parse_entry_timestamp(entry));

        let mut newest = None;
        for entry in entries {
            let ts = parse_entry_timestamp(entry);
            let key = dedup_key(entry, ts);
            if seen.insert(key, ()).is_some() {
                continue;
            }
            newest = newest.max(ts);
            print_entry(
                &args.output,
                entry,
                &response.columns,
                &fmt_options,
                highlighter.as_ref(),
            )?;
            printed += 1;
            if let Some(max_lines) = args.max_lines
                && printed >= max_lines
            {
                return Ok(());
            }
        }

        if let Some(ts) = newest {
            start = ts;
        }
        // Evict dedup entries older than the current window start; bounded by
        // the BETWEEN range the API filters with, so anything older cannot
        // reappear in a future poll.
        seen.retain(|key, _| key.ts.map(|t| t >= start).unwrap_or(true));

        if returned as u32 >= args.limit && !backpressure_warned {
            eprintln!(
                "tail: poll returned at --limit ({}); rows may have been dropped. Increase --limit or shrink --interval.",
                args.limit
            );
            backpressure_warned = true;
        }

        tokio::select! {
            _ = tokio::signal::ctrl_c() => return Ok(()),
            _ = sleep(Duration::from_secs(args.interval)) => {}
        }
    }
}

#[derive(Hash, Eq, PartialEq, Clone, Debug)]
struct DedupKey {
    ts: Option<DateTime<Utc>>,
    fingerprint: u64,
}

fn dedup_key(entry: &LogEntry, ts: Option<DateTime<Utc>>) -> DedupKey {
    use std::hash::{Hash, Hasher};
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    let mut keys: Vec<&String> = entry.keys().collect();
    keys.sort();
    for k in keys {
        k.hash(&mut hasher);
        if let Some(v) = entry.get(k) {
            v.to_string().hash(&mut hasher);
        }
    }
    DedupKey {
        ts,
        fingerprint: hasher.finish(),
    }
}

fn print_entry(
    output: &OutputFormat,
    entry: &LogEntry,
    columns: &[Column],
    fmt_options: &FormatOptions,
    highlighter: Option<&Highlighter>,
) -> Result<()> {
    match output {
        OutputFormat::Jsonl => println!("{}", serde_json::to_string(&JsonlOutput { entry })?),
        OutputFormat::Msg => {
            println!(
                "{}",
                entry.get("msg").map(json_value_to_line).unwrap_or_default()
            );
        }
        OutputFormat::Text => {
            let line = format_log_entry_with_options(entry, columns, fmt_options);
            if let Some(highlighter) = highlighter {
                println!("{}", highlighter.highlight(&line));
            } else {
                println!("{}", line);
            }
        }
    }
    Ok(())
}

fn parse_entry_timestamp(entry: &LogEntry) -> Option<DateTime<Utc>> {
    let value = entry.get("_timestamp").or_else(|| entry.get("timestamp"))?;
    let s = value.as_str()?;
    DateTime::parse_from_rfc3339(s)
        .map(|dt| dt.with_timezone(&Utc))
        .or_else(|_| NaiveDateTime::parse_from_str(s, "%Y-%m-%d %H:%M:%S").map(|dt| dt.and_utc()))
        .ok()
}

fn format_time(value: DateTime<Utc>) -> String {
    value.format("%Y-%m-%d %H:%M:%S").to_string()
}

fn parse_duration(s: &str) -> Result<ChronoDuration> {
    let s = s.trim();
    if s.is_empty() {
        return Ok(ChronoDuration::seconds(30));
    }

    let (num, unit) = if s.ends_with('s') {
        (s.trim_end_matches('s'), "s")
    } else if s.ends_with('m') {
        (s.trim_end_matches('m'), "m")
    } else if s.ends_with('h') {
        (s.trim_end_matches('h'), "h")
    } else if s.ends_with('d') {
        (s.trim_end_matches('d'), "d")
    } else {
        (s, "s")
    };

    let num: i64 = num.parse().context("Invalid duration number")?;

    match unit {
        "s" => Ok(ChronoDuration::seconds(num)),
        "m" => Ok(ChronoDuration::minutes(num)),
        "h" => Ok(ChronoDuration::hours(num)),
        "d" => Ok(ChronoDuration::days(num)),
        _ => Ok(ChronoDuration::seconds(num)),
    }
}

async fn resolve_team_id(client: &Client, cache: &mut Cache, team: Option<String>) -> Result<i64> {
    let team = team.ok_or_else(|| {
        anyhow::anyhow!(
            "Team not specified. Use --team, LOGCHEF_DEFAULT_TEAM, or config defaults.team."
        )
    })?;

    match parse_identifier(&team) {
        Identifier::Id(id) => Ok(id),
        Identifier::Name(name) => {
            if let Some(id) = cache.get_team_id(&name) {
                return Ok(id);
            }
            let teams = client.list_teams().await.context("Failed to list teams")?;
            cache.set_teams(
                &teams
                    .iter()
                    .map(|team| (team.name.clone(), team.id))
                    .collect::<Vec<_>>(),
            );
            teams
                .iter()
                .find(|team| team.name.eq_ignore_ascii_case(&name))
                .map(|team| team.id)
                .ok_or_else(|| anyhow::anyhow!("Team '{}' not found", name))
        }
    }
}

async fn resolve_source_id(
    client: &Client,
    cache: &mut Cache,
    team_id: i64,
    source: Option<String>,
) -> Result<i64> {
    let source = source.ok_or_else(|| {
        anyhow::anyhow!(
            "Source not specified. Use --source, LOGCHEF_DEFAULT_SOURCE, or config defaults.source."
        )
    })?;

    match parse_identifier(&source) {
        Identifier::Id(id) => Ok(id),
        Identifier::Name(name) => {
            if let Some(id) = cache.get_source_id(team_id, &name) {
                return Ok(id);
            }
            let sources = client
                .list_sources(team_id)
                .await
                .context("Failed to list sources")?;
            let mut cache_entries = sources
                .iter()
                .map(|source| (source.name.clone(), source.id))
                .collect::<Vec<_>>();
            for source in &sources {
                if let Some(table_ref) = source.table_ref() {
                    cache_entries.push((table_ref, source.id));
                }
            }
            cache.set_sources(team_id, &cache_entries);
            sources
                .iter()
                .find(|source| source.name.eq_ignore_ascii_case(&name))
                .or_else(|| {
                    sources.iter().find(|source| {
                        source
                            .table_ref()
                            .map(|table| table.eq_ignore_ascii_case(&name))
                            .unwrap_or(false)
                    })
                })
                .map(|source| source.id)
                .ok_or_else(|| anyhow::anyhow!("Source '{}' not found", name))
        }
    }
}

fn json_value_to_line(value: &serde_json::Value) -> String {
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_tail_timestamp() {
        let mut entry = LogEntry::new();
        entry.insert(
            "_timestamp".to_string(),
            serde_json::Value::String("2026-05-19T09:15:00Z".to_string()),
        );
        assert!(parse_entry_timestamp(&entry).is_some());
    }

    fn entry_from(pairs: &[(&str, &str)]) -> LogEntry {
        let mut e = LogEntry::new();
        for (k, v) in pairs {
            e.insert(k.to_string(), serde_json::Value::String(v.to_string()));
        }
        e
    }

    #[test]
    fn dedup_key_is_stable_across_insertion_order() {
        let a = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "hi")]);
        let b = entry_from(&[("msg", "hi"), ("_timestamp", "2026-05-19T09:15:00Z")]);
        let ts = parse_entry_timestamp(&a);
        assert_eq!(dedup_key(&a, ts), dedup_key(&b, ts));
    }

    #[test]
    fn dedup_key_differs_when_value_differs() {
        let a = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "hi")]);
        let b = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "ho")]);
        let ts = parse_entry_timestamp(&a);
        assert_ne!(dedup_key(&a, ts), dedup_key(&b, ts));
    }

    #[test]
    fn dedup_key_differs_when_timestamp_differs() {
        let a = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "hi")]);
        let b = entry_from(&[("_timestamp", "2026-05-19T09:15:01Z"), ("msg", "hi")]);
        let ts_a = parse_entry_timestamp(&a);
        let ts_b = parse_entry_timestamp(&b);
        assert_ne!(dedup_key(&a, ts_a), dedup_key(&b, ts_b));
    }

    #[test]
    fn parse_duration_handles_seconds_default() {
        assert_eq!(parse_duration("30").unwrap(), ChronoDuration::seconds(30));
        assert_eq!(parse_duration("30s").unwrap(), ChronoDuration::seconds(30));
        assert_eq!(parse_duration("5m").unwrap(), ChronoDuration::minutes(5));
    }
}
