use anyhow::{Context, Result};
use chrono::{DateTime, Duration as ChronoDuration, NaiveDateTime, Utc};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Client, Column, LogEntry, QueryRequest};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::highlight::{
    FormatOptions, HighlightOptions, Highlighter, format_log_entry_with_options,
};
use logchef_core::timerange::{TimeInput, resolve_time_range};
use serde::Serialize;
use std::collections::HashMap;
use tokio::time::{Duration, sleep};

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Follow errors from the api service live (native SSE stream)
  logchef tail 'level=\"error\" and service=\"api\"' -t platform -S app-logs

  # Follow as JSON lines, stop after 100 rows, pipe to jq
  logchef tail 'status>=500' --output jsonl --max-lines 100 | jq .

  # Fall back to client-side polling where SSE is unavailable
  logchef tail 'service=\"worker\"' --poll --interval 2")]
pub struct TailArgs {
    /// LogChefQL query to follow.
    query: String,

    /// Team ID or name.
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name.
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Initial lookback window, evaluated against now in the effective
    /// timezone: `defaults.timezone` if configured, otherwise the system's
    /// local timezone (see `logchef config show`). Only used by `--poll`; the
    /// native SSE stream always follows from now.
    #[arg(long, short = 's', default_value = "30s")]
    since: String,

    /// Poll interval in seconds. Only used with `--poll`; the native SSE
    /// stream is push-based and ignores it.
    #[arg(long, default_value = "2")]
    interval: u64,

    /// Use the legacy client-side polling loop instead of the server's native
    /// live-tail SSE stream. SSE is the default; `--poll` is a fallback for
    /// environments where the streaming endpoint is unavailable.
    #[arg(long)]
    poll: bool,

    /// Maximum rows to fetch per poll (only used with `--poll`).
    ///
    /// Each poll queries newest-first and keeps only the top --limit rows,
    /// then advances the cursor past the newest row returned. If a single
    /// poll has more matching rows than --limit, the OLDEST rows in that
    /// poll's window are silently dropped (not returned by the server) and
    /// are never re-fetched, since the cursor has already moved past them.
    /// A warning is printed the first time this happens; raise --limit or
    /// lower --interval to reduce the chance of a poll overflowing.
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

    /// Query timeout in seconds. With `--poll` this bounds each poll; with the
    /// SSE stream it is used as an idle read timeout (reconnect if no data,
    /// including heartbeats, arrives within this window).
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

/// Rolling lookback margin applied when advancing the poll cursor, mirroring
/// the server-side fix (#87 item 1): poll from `cursor - margin` rather than
/// `cursor` so late-arriving rows (ingestion lag/batching) aren't silently
/// missed. The existing dedup map absorbs the resulting overlap.
const LOOKBACK_MARGIN: ChronoDuration = ChronoDuration::seconds(5);

pub async fn run(args: TailArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();
    let team_id = resolve_team_id(client, &mut cache, args.team.clone().or(default_team)).await?;
    let source_id = resolve_source_id(
        client,
        &mut cache,
        team_id,
        args.source.clone().or(default_source),
    )
    .await?;

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

    if args.poll {
        run_poll(
            client,
            ctx,
            team_id,
            source_id,
            &args,
            highlighter.as_ref(),
            &fmt_options,
        )
        .await
    } else {
        run_sse(
            client,
            team_id,
            source_id,
            &args,
            highlighter.as_ref(),
            &fmt_options,
        )
        .await
    }
}

/// Follows the backend's native live-tail SSE stream (`GET .../logs/tail`).
/// The server handles ClickHouse polling and VictoriaLogs native streaming
/// internally, so the client just renders frames and reconnects on drop.
///
/// The tail endpoint has no resume cursor — it always follows from now — so a
/// reconnect resumes from the current instant. Clean session rollovers
/// (ttl_expired/completed) reconnect immediately; failures back off.
async fn run_sse(
    client: &Client,
    team_id: i64,
    source_id: i64,
    args: &TailArgs,
    highlighter: Option<&Highlighter>,
    fmt_options: &FormatOptions,
) -> Result<()> {
    let mut printed = 0usize;
    let mut backoff = Duration::from_millis(500);
    let max_backoff = Duration::from_secs(5);
    // Idle read timeout: the server heartbeats every 15s, so a longer silence
    // means a dead connection. Keep it comfortably above the heartbeat.
    let idle = Duration::from_secs(u64::from(args.timeout).max(20));

    // A single Ctrl-C future, reused across selects so tail exits cleanly (no
    // task leaks) whether it is blocked connecting, reading, or backing off.
    let ctrl_c = tokio::signal::ctrl_c();
    tokio::pin!(ctrl_c);

    loop {
        // LogchefQL is the tail language (empty query_language → server default);
        // the server compiles it per source (ClickHouse WHERE-fragment or VL
        // LogsQL) — see resolveTailQuery in tail_handlers.go.
        let connect = client.tail_stream(team_id, source_id, &args.query, "");
        tokio::pin!(connect);
        let mut resp = tokio::select! {
            _ = &mut ctrl_c => return Ok(()),
            r = &mut connect => match r {
                Ok(resp) => {
                    backoff = Duration::from_millis(500);
                    resp
                }
                Err(err) => {
                    eprintln!("tail: connection failed ({err}); reconnecting");
                    tokio::select! {
                        _ = &mut ctrl_c => return Ok(()),
                        _ = sleep(backoff) => {}
                    }
                    backoff = (backoff * 2).min(max_backoff);
                    continue;
                }
            },
        };

        let mut parser = SseParser::new();
        let mut backoff_needed = false;
        'read: loop {
            let chunk = tokio::select! {
                _ = &mut ctrl_c => return Ok(()),
                c = tokio::time::timeout(idle, resp.chunk()) => c,
            };
            let bytes = match chunk {
                Err(_elapsed) => {
                    eprintln!("tail: no data for {}s; reconnecting", idle.as_secs());
                    backoff_needed = true;
                    break 'read;
                }
                Ok(Ok(Some(bytes))) => bytes,
                Ok(Ok(None)) => break 'read, // server closed the stream cleanly
                Ok(Err(err)) => {
                    eprintln!("tail: stream error ({err}); reconnecting");
                    backoff_needed = true;
                    break 'read;
                }
            };
            for event in parser.feed(&bytes) {
                match event {
                    SseEvent::Rows(rows) => {
                        for entry in &rows {
                            let columns = columns_from_entry(entry);
                            print_entry(&args.output, entry, &columns, fmt_options, highlighter)?;
                            printed += 1;
                            if let Some(max_lines) = args.max_lines
                                && printed >= max_lines
                            {
                                return Ok(());
                            }
                        }
                    }
                    SseEvent::Notice(message) => {
                        eprintln!("tail: {message}");
                    }
                    SseEvent::End { reason, message } => {
                        // ttl_expired/completed are normal session rollovers —
                        // reconnect to keep following. An error end is surfaced
                        // and backed off before reconnecting.
                        if reason == "error" {
                            let detail = message.unwrap_or_else(|| "stream error".to_string());
                            eprintln!("tail: stream ended ({detail}); reconnecting");
                            backoff_needed = true;
                        }
                        break 'read;
                    }
                }
            }
        }

        // Backoff on failures; reconnect promptly after a clean end/close (with
        // a small floor to avoid a hot loop if the server ends immediately).
        let wait = if backoff_needed {
            let w = backoff;
            backoff = (backoff * 2).min(max_backoff);
            w
        } else {
            backoff = Duration::from_millis(500);
            Duration::from_millis(250)
        };
        tokio::select! {
            _ = &mut ctrl_c => return Ok(()),
            _ = sleep(wait) => {}
        }
    }
}

/// The SSE frames the tail endpoint emits (see tail_handlers.go). Heartbeat
/// comment lines (`: hb`) carry no event and are dropped by the parser.
#[derive(Debug, PartialEq)]
enum SseEvent {
    /// `event: rows` — a JSON array of log rows.
    Rows(Vec<LogEntry>),
    /// `event: notice` — a server notice (e.g. rate-limited); carries a message.
    Notice(String),
    /// `event: end` — the stream ended; carries a reason and optional message.
    End {
        reason: String,
        message: Option<String>,
    },
}

/// Incremental parser for the tail SSE wire format. Frames are separated by a
/// blank line (`\n\n`); each frame is `event: <name>` + `data: <json>`, or a
/// bare `: comment` heartbeat. `feed` buffers partial frames across chunk
/// boundaries and returns whatever complete events are available.
struct SseParser {
    buf: Vec<u8>,
}

impl SseParser {
    fn new() -> Self {
        Self { buf: Vec::new() }
    }

    fn feed(&mut self, chunk: &[u8]) -> Vec<SseEvent> {
        self.buf.extend_from_slice(chunk);
        let mut events = Vec::new();
        while let Some(end) = self
            .buf
            .windows(2)
            .position(|w| w == b"\n\n")
            .map(|i| i + 2)
        {
            let block: Vec<u8> = self.buf.drain(..end).collect();
            if let Some(event) = parse_sse_block(&block) {
                events.push(event);
            }
        }
        events
    }
}

fn parse_sse_block(block: &[u8]) -> Option<SseEvent> {
    let text = String::from_utf8_lossy(block);
    let mut event_type: Option<String> = None;
    let mut data = String::new();

    for raw_line in text.split('\n') {
        let line = raw_line.trim_end_matches('\r');
        if line.is_empty() || line.starts_with(':') {
            // Blank line (frame padding) or comment/heartbeat — ignore.
            continue;
        }
        if let Some(rest) = line.strip_prefix("event:") {
            event_type = Some(rest.trim().to_string());
        } else if let Some(rest) = line.strip_prefix("data:") {
            // SSE allows multiple data: lines, joined by newline. The server
            // emits one per frame, but handle multiples defensively.
            if !data.is_empty() {
                data.push('\n');
            }
            data.push_str(rest.strip_prefix(' ').unwrap_or(rest));
        }
    }

    match event_type?.as_str() {
        "rows" => serde_json::from_str::<Vec<LogEntry>>(&data)
            .ok()
            .map(SseEvent::Rows),
        "notice" => {
            let value: serde_json::Value = serde_json::from_str(&data).ok()?;
            let message = value
                .get("message")
                .and_then(|m| m.as_str())
                .unwrap_or("rate limited")
                .to_string();
            Some(SseEvent::Notice(message))
        }
        "end" => {
            let value: serde_json::Value =
                serde_json::from_str(&data).unwrap_or(serde_json::Value::Null);
            let reason = value
                .get("reason")
                .and_then(|r| r.as_str())
                .unwrap_or("completed")
                .to_string();
            let message = value
                .get("message")
                .and_then(|m| m.as_str())
                .map(|s| s.to_string());
            Some(SseEvent::End { reason, message })
        }
        _ => None,
    }
}

/// Synthesizes column metadata from a streamed row's keys (sorted for a stable
/// field order). The SSE stream sends rows without schema, and the text
/// formatter needs columns to render non-priority fields.
fn columns_from_entry(entry: &LogEntry) -> Vec<Column> {
    let mut names: Vec<&String> = entry.keys().collect();
    names.sort();
    names
        .into_iter()
        .map(|name| Column {
            name: name.clone(),
            column_type: "String".to_string(),
            description: None,
        })
        .collect()
}

/// The legacy client-side polling loop (used with `--poll`). Repeatedly queries
/// LogchefQL newest-first, dedups against a rolling window, and advances a
/// cursor. Preserves VictoriaLogs `_meta_ts_field` awareness for the
/// dedup/cursor key via `fetch_ts_field`.
async fn run_poll(
    client: &Client,
    ctx: &logchef_core::config::Context,
    team_id: i64,
    source_id: i64,
    args: &TailArgs,
    highlighter: Option<&Highlighter>,
    fmt_options: &FormatOptions,
) -> Result<()> {
    // Fetch the source's configured timestamp field once, so dedup/cursor logic
    // uses the right key on sources with a non-default ts field (e.g.
    // VictoriaLogs uses `_time`). Falls back to `_timestamp`/`timestamp`
    // probing (see `parse_entry_timestamp`) when the fetch fails or it's unset.
    let ts_field = fetch_ts_field(client, team_id, source_id).await;

    let mut start = Utc::now() - parse_duration(&args.since)?;
    let mut seen: HashMap<DedupKey, ()> = HashMap::new();
    let mut printed = 0usize;
    let mut backpressure_warned = false;

    loop {
        let end = Utc::now();
        let time_range = resolve_time_range(
            TimeInput::Instant { start, end },
            ctx.defaults.timezone.as_deref(),
        );
        let request = QueryRequest {
            query: args.query.clone(),
            start_time: time_range.start,
            end_time: time_range.end,
            timezone: Some(time_range.timezone),
            limit: Some(args.limit),
            query_timeout: Some(args.timeout),
        };

        let response = client
            .query_logchefql(team_id, source_id, &request)
            .await
            .context("Tail query failed")?;

        let returned = response.entries().len();
        let mut entries = response.entries().iter().collect::<Vec<_>>();
        entries.sort_by_key(|entry| parse_entry_timestamp(entry, ts_field.as_deref()));

        let mut newest = None;
        for entry in entries {
            let ts = parse_entry_timestamp(entry, ts_field.as_deref());
            let key = dedup_key(entry, ts);
            if seen.insert(key, ()).is_some() {
                continue;
            }
            newest = newest.max(ts);
            print_entry(
                &args.output,
                entry,
                &response.columns,
                fmt_options,
                highlighter,
            )?;
            printed += 1;
            if let Some(max_lines) = args.max_lines
                && printed >= max_lines
            {
                return Ok(());
            }
        }

        if let Some(ts) = newest {
            // Rolling lookback margin (mirrors the server-side fix, #87 item
            // 1): re-poll from just before the newest seen row rather than
            // exactly at it, so late-arriving rows with an earlier timestamp
            // than `ts` aren't silently missed. The dedup map absorbs the
            // resulting overlap.
            start = ts - LOOKBACK_MARGIN;
        }
        // Evict dedup entries older than the current window start; bounded by
        // the BETWEEN range the API filters with, so anything older cannot
        // reappear in a future poll. Entries with no parseable timestamp
        // can't be windowed this way — prune them every cycle instead of
        // keeping them forever, so a source with an unparseable/custom ts
        // field can't grow the map unbounded.
        seen.retain(|key, _| key.ts.map(|t| t >= start).unwrap_or(false));

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

/// Fetches the source's configured timestamp field (`_meta_ts_field`) for use
/// as the dedup/cursor key. Returns `None` (triggering the `_timestamp`/
/// `timestamp` fallback probing in `parse_entry_timestamp`) if the fetch
/// fails or the source has no field configured, so a transient API hiccup
/// degrades tail rather than aborting it.
async fn fetch_ts_field(client: &Client, team_id: i64, source_id: i64) -> Option<String> {
    match client.get_source(team_id, source_id).await {
        Ok(source) => source.meta_ts_field.filter(|f| !f.is_empty()),
        Err(err) => {
            eprintln!(
                "tail: could not fetch source detail ({err}); falling back to _timestamp/timestamp probing"
            );
            None
        }
    }
}

/// Extracts a row's timestamp for dedup/cursor purposes. `ts_field`, when
/// present, is the source's configured `_meta_ts_field` and is tried first;
/// otherwise (or if the field is absent from the row) falls back to probing
/// the hardcoded `_timestamp`/`timestamp` keys used by older/ClickHouse-only
/// behavior.
fn parse_entry_timestamp(entry: &LogEntry, ts_field: Option<&str>) -> Option<DateTime<Utc>> {
    let value = ts_field
        .and_then(|field| entry.get(field))
        .or_else(|| entry.get("_timestamp"))
        .or_else(|| entry.get("timestamp"))?;
    let s = value.as_str()?;
    DateTime::parse_from_rfc3339(s)
        .map(|dt| dt.with_timezone(&Utc))
        .or_else(|_| NaiveDateTime::parse_from_str(s, "%Y-%m-%d %H:%M:%S").map(|dt| dt.and_utc()))
        .ok()
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
        assert!(parse_entry_timestamp(&entry, None).is_some());
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
        let ts = parse_entry_timestamp(&a, None);
        assert_eq!(dedup_key(&a, ts), dedup_key(&b, ts));
    }

    #[test]
    fn dedup_key_differs_when_value_differs() {
        let a = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "hi")]);
        let b = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "ho")]);
        let ts = parse_entry_timestamp(&a, None);
        assert_ne!(dedup_key(&a, ts), dedup_key(&b, ts));
    }

    #[test]
    fn dedup_key_differs_when_timestamp_differs() {
        let a = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z"), ("msg", "hi")]);
        let b = entry_from(&[("_timestamp", "2026-05-19T09:15:01Z"), ("msg", "hi")]);
        let ts_a = parse_entry_timestamp(&a, None);
        let ts_b = parse_entry_timestamp(&b, None);
        assert_ne!(dedup_key(&a, ts_a), dedup_key(&b, ts_b));
    }

    #[test]
    fn parse_entry_timestamp_uses_custom_ts_field_when_present() {
        // A VictoriaLogs-style source with `_time` as its configured field:
        // the custom field must be tried before the hardcoded fallbacks, and
        // preferred even when a `timestamp`-named field is also present (as
        // an ordinary log attribute, not the actual cursor field).
        let entry = entry_from(&[
            ("_time", "2026-05-19T09:15:00Z"),
            ("timestamp", "not-a-real-timestamp-field"),
        ]);
        let ts = parse_entry_timestamp(&entry, Some("_time"));
        assert!(ts.is_some());
    }

    #[test]
    fn parse_entry_timestamp_falls_back_when_custom_field_absent_from_row() {
        // ts_field configured, but this particular row doesn't have it —
        // fall back to the hardcoded probing rather than returning None.
        let entry = entry_from(&[("_timestamp", "2026-05-19T09:15:00Z")]);
        let ts = parse_entry_timestamp(&entry, Some("_time"));
        assert!(ts.is_some());
    }

    #[test]
    fn parse_entry_timestamp_falls_back_when_no_ts_field_configured() {
        let entry = entry_from(&[("timestamp", "2026-05-19T09:15:00Z")]);
        let ts = parse_entry_timestamp(&entry, None);
        assert!(ts.is_some());
    }

    #[test]
    fn parse_entry_timestamp_returns_none_when_unparseable() {
        let entry = entry_from(&[("_time", "not-a-timestamp")]);
        assert!(parse_entry_timestamp(&entry, Some("_time")).is_none());
    }

    #[test]
    fn dedup_map_prunes_entries_without_a_parseable_timestamp() {
        // Regression test for the unbounded-growth bug: rows whose timestamp
        // can't be parsed (e.g. a custom ts field CLI doesn't yet know about,
        // or a malformed value) must not accumulate in the dedup map forever.
        let mut seen: HashMap<DedupKey, ()> = HashMap::new();
        let start = DateTime::parse_from_rfc3339("2026-05-19T09:15:00Z")
            .unwrap()
            .with_timezone(&Utc);

        // Simulate one poll's worth of no-ts-field entries landing in the map.
        for i in 0..50 {
            seen.insert(
                DedupKey {
                    ts: None,
                    fingerprint: i,
                },
                (),
            );
        }
        // And a handful of entries with a real, in-window timestamp.
        seen.insert(
            DedupKey {
                ts: Some(start),
                fingerprint: 999,
            },
            (),
        );

        // This is the same retention predicate used in the poll loop.
        seen.retain(|key, _| key.ts.map(|t| t >= start).unwrap_or(false));

        assert_eq!(
            seen.len(),
            1,
            "no-ts entries must be pruned, not retained forever"
        );
        assert!(seen.contains_key(&DedupKey {
            ts: Some(start),
            fingerprint: 999,
        }));
    }

    #[test]
    fn parse_duration_handles_seconds_default() {
        assert_eq!(parse_duration("30").unwrap(), ChronoDuration::seconds(30));
        assert_eq!(parse_duration("30s").unwrap(), ChronoDuration::seconds(30));
        assert_eq!(parse_duration("5m").unwrap(), ChronoDuration::minutes(5));
    }

    #[test]
    fn sse_parser_reads_a_rows_frame() {
        let mut parser = SseParser::new();
        let events = parser.feed(b"event: rows\ndata: [{\"msg\":\"hello\"}]\n\n");
        assert_eq!(events.len(), 1);
        match &events[0] {
            SseEvent::Rows(rows) => {
                assert_eq!(rows.len(), 1);
                assert_eq!(rows[0].get("msg").unwrap().as_str(), Some("hello"));
            }
            other => panic!("expected rows, got {other:?}"),
        }
    }

    #[test]
    fn sse_parser_ignores_comments_and_heartbeats() {
        let mut parser = SseParser::new();
        // Initial ": ok" open comment, then a heartbeat, carry no event.
        assert!(parser.feed(b": ok\n\n").is_empty());
        assert!(parser.feed(b": hb\n\n").is_empty());
    }

    #[test]
    fn sse_parser_reads_notice_and_end_frames() {
        let mut parser = SseParser::new();
        let events = parser.feed(
            b"event: notice\ndata: {\"code\":\"rate_limited\",\"message\":\"dropped 5 rows\"}\n\nevent: end\ndata: {\"reason\":\"ttl_expired\"}\n\n",
        );
        assert_eq!(events.len(), 2);
        assert_eq!(events[0], SseEvent::Notice("dropped 5 rows".to_string()));
        assert_eq!(
            events[1],
            SseEvent::End {
                reason: "ttl_expired".to_string(),
                message: None,
            }
        );
    }

    #[test]
    fn sse_parser_buffers_frames_split_across_chunks() {
        let mut parser = SseParser::new();
        // First chunk holds only part of the frame — no complete event yet.
        assert!(parser.feed(b"event: rows\ndata: [{\"a\":").is_empty());
        // Remainder completes the frame.
        let events = parser.feed(b"\"b\"}]\n\n");
        assert_eq!(events.len(), 1);
        match &events[0] {
            SseEvent::Rows(rows) => assert_eq!(rows[0].get("a").unwrap().as_str(), Some("b")),
            other => panic!("expected rows, got {other:?}"),
        }
    }

    #[test]
    fn columns_from_entry_covers_all_keys_sorted() {
        let entry = entry_from(&[("service", "api"), ("_time", "t"), ("msg", "hi")]);
        let cols = columns_from_entry(&entry);
        let names: Vec<&str> = cols.iter().map(|c| c.name.as_str()).collect();
        assert_eq!(names, vec!["_time", "msg", "service"]);
    }
}
