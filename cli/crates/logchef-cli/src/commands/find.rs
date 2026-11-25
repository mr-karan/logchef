use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Client, Column, QueryResponse, Source, SqlQueryRequest, Team};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use logchef_core::timerange::resolve_timezone;
use serde::Serialize;
use std::future::Future;
use std::sync::Arc;
use std::time::Duration;
use tokio::task::JoinSet;

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

const DEFAULT_COLUMNS: &[&str] = &["service", "service_name", "job_name", "app", "host", "msg"];
/// Columns that are treated as free-form text — a single sample row is more
/// informative than a GROUP BY of unique strings.
const SAMPLE_ONLY_COLUMNS: &[&str] = &["msg", "message", "body"];
const SAMPLE_VALUE_TRUNCATE: usize = 80;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Which source holds logs mentioning `checkout`? (searches all teams)
  logchef find checkout

  # Narrow to one team and a longer window
  logchef find payment-gateway -t platform --since 7d

  # Just the match counts, no per-column samples, as JSON
  logchef find nginx --no-samples --output jsonl

  # Search many sources faster, giving each at most 10 seconds
  logchef find checkout-api --concurrency 8 --timeout 10

A source that fails or takes longer than --timeout is skipped and named on
stderr; the matches from the other sources still print. The command fails
only when no source could be searched.")]
pub struct FindArgs {
    /// Service, job, host, or message pattern to search for.
    pattern: String,

    /// Restrict discovery to one team ID or name.
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Restrict discovery to one source ID, name, or database.table.
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Lookback window (e.g., 15m, 1h, 24h).
    #[arg(long, short = 's', default_value = "24h")]
    since: String,

    /// Candidate columns to search. Can be passed multiple times.
    #[arg(long = "column", value_name = "COLUMN")]
    columns: Vec<String>,

    /// Maximum number of matching sources to print.
    #[arg(long, default_value = "10")]
    limit: usize,

    /// Seconds each source may take, from its schema read to its samples.
    /// A source that takes longer is skipped and named.
    #[arg(long, default_value = "30")]
    timeout: u32,

    /// How many sources to search at once.
    #[arg(long, default_value_t = 4, value_parser = clap::value_parser!(u16).range(1..=32))]
    concurrency: u16,

    /// Skip the per-column sample fetch. Useful when you only need the
    /// match-count summary.
    #[arg(long)]
    no_samples: bool,

    /// Output format.
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
}

#[derive(Debug, Serialize)]
struct FindResult {
    team_id: i64,
    team_name: String,
    source_id: i64,
    source_name: String,
    table: String,
    matches: i64,
    columns: Vec<String>,
    lookback: String,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    samples: Vec<ColumnSamples>,
}

#[derive(Debug, Serialize)]
struct ColumnSamples {
    column: String,
    values: Vec<SampleValue>,
}

#[derive(Debug, Serialize)]
struct SampleValue {
    value: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    count: Option<i64>,
}

/// Why a team or source was not searched.
#[derive(Debug, PartialEq)]
enum Skip {
    TimedOut,
    Failed(String),
}

/// A team or source that was not searched.
struct Skipped {
    what: String,
    why: Skip,
}

/// What every source probe searches for.
struct Search {
    pattern: String,
    since: String,
    candidates: Vec<String>,
    query_timeout: u32,
    samples: bool,
    timezone: String,
}

pub async fn run(args: FindArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let mut cache = Cache::new(&s.ctx.server_url);
    let timezone = resolve_timezone(s.ctx.defaults.timezone.as_deref()).to_string();
    let client = Arc::new(s.client);
    let teams = resolve_teams(&client, &mut cache, args.team.as_deref()).await?;
    let search = Arc::new(Search {
        pattern: args.pattern.clone(),
        since: args.since.clone(),
        candidates: if args.columns.is_empty() {
            DEFAULT_COLUMNS.iter().map(|c| c.to_string()).collect()
        } else {
            args.columns.clone()
        },
        query_timeout: args.timeout,
        samples: !args.no_samples,
        timezone,
    });
    let concurrency = usize::from(args.concurrency);
    let deadline = Duration::from_secs(u64::from(args.timeout));
    let progress = ui::stderr_human(global.quiet);
    let mut skipped = Vec::new();

    let listed = run_bounded(
        teams,
        concurrency,
        deadline,
        |team: Team| {
            let client = Arc::clone(&client);
            async move {
                client
                    .list_sources(team.id)
                    .await
                    .map_err(|err| format!("{err:#}"))
            }
        },
        |_, _, _, _| {},
    )
    .await;
    let mut targets = Vec::new();
    for (team, outcome) in listed {
        match outcome {
            Ok(sources) => targets.extend(
                sources
                    .into_iter()
                    .filter(|source| source_matches_filter(source, args.source.as_deref()))
                    .map(|source| (team.clone(), source)),
            ),
            Err(why) => skipped.push(Skipped {
                what: format!("team {} ({})", team.id, team.name),
                why,
            }),
        }
    }

    let probed = run_bounded(
        targets,
        concurrency,
        deadline,
        |(team, source): (Team, Source)| {
            let client = Arc::clone(&client);
            let search = Arc::clone(&search);
            async move {
                probe_source(&client, &team, &source, &search)
                    .await
                    .map_err(|err| format!("{err:#}"))
            }
        },
        |done, total, (team, source), outcome| {
            if !progress {
                return;
            }
            if let Err(why) = outcome {
                eprintln!(
                    "\r\x1b[K  skipped {}: {}",
                    source_label(team, source),
                    describe(why, deadline)
                );
            }
            eprint!("\r\x1b[Ksearching {done}/{total} sources…");
        },
    )
    .await;
    if progress {
        eprint!("\r\x1b[K");
    }

    let searched = probed.iter().filter(|(_, outcome)| outcome.is_ok()).count();
    let mut results = Vec::new();
    for ((team, source), outcome) in probed {
        match outcome {
            Ok(Some(result)) => results.push(result),
            Ok(None) => {}
            Err(why) => skipped.push(Skipped {
                what: source_label(&team, &source),
                why,
            }),
        }
    }
    results.sort_by_key(|r| (std::cmp::Reverse(r.matches), r.team_id, r.source_id));
    results.truncate(args.limit);
    print_results(&results, &args.output)?;
    report_skipped(&skipped, deadline);
    if searched == 0 && !skipped.is_empty() {
        anyhow::bail!("No source could be searched: every one was skipped (see above)");
    }
    Ok(())
}

fn source_label(team: &Team, source: &Source) -> String {
    format!("source {} {} (team {})", source.id, source.name, team.id)
}

fn describe(skip: &Skip, deadline: Duration) -> String {
    match skip {
        Skip::TimedOut => format!("timed out after {}s", deadline.as_secs()),
        Skip::Failed(err) => err.lines().next().unwrap_or_default().to_string(),
    }
}

fn report_skipped(skipped: &[Skipped], deadline: Duration) {
    if skipped.is_empty() {
        return;
    }
    eprintln!(
        "Skipped {} (raise --timeout for slow sources):",
        if skipped.len() == 1 {
            "1 team or source".to_string()
        } else {
            format!("{} teams or sources", skipped.len())
        }
    );
    for skip in skipped {
        eprintln!("  {}: {}", skip.what, describe(&skip.why, deadline));
    }
}

/// Runs `probe` on every item, at most `concurrency` at once, and stops each
/// after `deadline`. `on_done` sees each outcome as it finishes, with the
/// number finished and the total. Returns the outcomes in finishing order.
async fn run_bounded<T, R, F, Fut>(
    items: Vec<T>,
    concurrency: usize,
    deadline: Duration,
    probe: F,
    mut on_done: impl FnMut(usize, usize, &T, &Result<R, Skip>),
) -> Vec<(T, Result<R, Skip>)>
where
    T: Clone + Send + 'static,
    R: Send + 'static,
    F: Fn(T) -> Fut,
    Fut: Future<Output = Result<R, String>> + Send + 'static,
{
    let total = items.len();
    let mut pending = items.into_iter();
    let mut running = JoinSet::new();
    let start = |running: &mut JoinSet<_>, item: T| {
        let work = probe(item.clone());
        running.spawn(async move {
            let outcome = match tokio::time::timeout(deadline, work).await {
                Ok(Ok(found)) => Ok(found),
                Ok(Err(err)) => Err(Skip::Failed(err)),
                Err(_) => Err(Skip::TimedOut),
            };
            (item, outcome)
        });
    };
    for item in pending.by_ref().take(concurrency.max(1)) {
        start(&mut running, item);
    }
    let mut finished = Vec::with_capacity(total);
    while let Some(joined) = running.join_next().await {
        let (item, outcome) = joined.expect("a find probe panicked");
        on_done(finished.len() + 1, total, &item, &outcome);
        finished.push((item, outcome));
        if let Some(item) = pending.next() {
            start(&mut running, item);
        }
    }
    finished
}

/// Counts the matches in one source and, when there are any, fetches samples.
/// `None` when the source has no searchable column or no match.
async fn probe_source(
    client: &Client,
    team: &Team,
    source: &Source,
    search: &Search,
) -> Result<Option<FindResult>> {
    // VictoriaLogs sources have no ClickHouse-style `db.table` ref: use the
    // base_url (falling back to the source name) as the display target.
    let is_vl = source.source_type.eq_ignore_ascii_case("victorialogs");
    let target = if is_vl {
        source.target_ref().unwrap_or_else(|| source.name.clone())
    } else {
        match source.table_ref() {
            Some(table) => table,
            None => return Ok(None),
        }
    };

    let schema = client
        .get_schema(team.id, source.id)
        .await
        .context("Failed to read the schema")?;
    let searchable = matching_columns(&schema, &search.candidates);
    if searchable.is_empty() {
        return Ok(None);
    }

    let timestamp_field = source
        .meta_ts_field
        .as_deref()
        .filter(|field| !field.trim().is_empty())
        .unwrap_or("_timestamp");
    // Count matches through the same `/logs/query` path for both engines;
    // only the query text differs.
    let count_query = if is_vl {
        vl_find_query(&searchable, &search.pattern, &search.since)
    } else {
        find_sql(
            &target,
            timestamp_field,
            &searchable,
            &search.pattern,
            &search.since,
        )
    };
    let request = SqlQueryRequest {
        variables: Vec::new(),
        query_text: count_query,
        limit: Some(1),
        timezone: Some(search.timezone.clone()),
        start_time: None,
        end_time: None,
        interval: None,
        query_timeout: Some(search.query_timeout),
    };
    let response = client
        .query_sql(team.id, source.id, &request)
        .await
        .context("Failed to count matches")?;
    let matches = response
        .entries()
        .first()
        .and_then(|entry| entry.get("matches"))
        .and_then(json_to_i64)
        .unwrap_or(0);
    if matches <= 0 {
        return Ok(None);
    }

    let samples = if !search.samples {
        Vec::new()
    } else if is_vl {
        vl_fetch_samples(
            client,
            team.id,
            source.id,
            &searchable,
            &search.pattern,
            &search.since,
            search.query_timeout,
        )
        .await
    } else {
        fetch_samples(
            client,
            team.id,
            source.id,
            &target,
            timestamp_field,
            &searchable,
            &schema,
            &search.pattern,
            &search.since,
            search.query_timeout,
        )
        .await
    };
    Ok(Some(FindResult {
        team_id: team.id,
        team_name: team.name.clone(),
        source_id: source.id,
        source_name: source.name.clone(),
        table: target,
        matches,
        columns: searchable,
        lookback: search.since.clone(),
        samples,
    }))
}

fn print_results(results: &[FindResult], output: &OutputFormat) -> Result<()> {
    match output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(results)?),
        OutputFormat::Jsonl => {
            for result in results {
                println!("{}", serde_json::to_string(result)?);
            }
        }
        OutputFormat::Text => {
            if results.is_empty() {
                println!("No matching sources found.");
            } else {
                for result in results {
                    println!(
                        "team={} source={} ({}) matches={} in {} columns={}",
                        result.team_id,
                        result.source_id,
                        result.table,
                        ui::thousands(result.matches),
                        result.lookback,
                        result.columns.join(",")
                    );
                    for sample in &result.samples {
                        let rendered = sample
                            .values
                            .iter()
                            .map(|v| match v.count {
                                Some(c) => format!("\"{}\" ({})", v.value, ui::thousands(c)),
                                None => format!("\"{}\" (sample)", v.value),
                            })
                            .collect::<Vec<_>>()
                            .join(", ");
                        println!("  ↳ {}: {}", sample.column, rendered);
                    }
                }
            }
        }
    }
    Ok(())
}

async fn resolve_teams(
    client: &Client,
    cache: &mut Cache,
    team: Option<&str>,
) -> Result<Vec<Team>> {
    let teams = client.list_teams().await.context("Failed to list teams")?;
    cache.set_teams(
        &teams
            .iter()
            .map(|team| (team.name.clone(), team.id))
            .collect::<Vec<_>>(),
    );

    let Some(team) = team else {
        return Ok(teams);
    };

    let team_id = match parse_identifier(team) {
        Identifier::Id(id) => id,
        Identifier::Name(name) => teams
            .iter()
            .find(|team| team.name.eq_ignore_ascii_case(&name))
            .map(|team| team.id)
            .ok_or_else(|| anyhow::anyhow!("Team '{}' not found", name))?,
    };

    Ok(teams
        .into_iter()
        .filter(|team| team.id == team_id)
        .collect())
}

fn source_matches_filter(source: &Source, filter: Option<&str>) -> bool {
    let Some(filter) = filter else {
        return true;
    };

    match parse_identifier(filter) {
        Identifier::Id(id) => source.id == id,
        Identifier::Name(name) => {
            source.name.eq_ignore_ascii_case(&name)
                || source
                    .table_ref()
                    .map(|table| table.eq_ignore_ascii_case(&name))
                    .unwrap_or(false)
        }
    }
}

fn matching_columns(schema: &[Column], candidates: &[String]) -> Vec<String> {
    candidates
        .iter()
        .filter_map(|candidate| {
            schema
                .iter()
                .find(|column| column.name.eq_ignore_ascii_case(candidate))
                .map(|column| column.name.clone())
        })
        .collect()
}

fn find_sql(
    table: &str,
    timestamp_field: &str,
    columns: &[String],
    pattern: &str,
    since: &str,
) -> String {
    let predicates = columns
        .iter()
        .map(|column| {
            format!(
                "positionCaseInsensitive(toString({}), '{}') > 0",
                sql_identifier(column),
                sql_string(pattern)
            )
        })
        .collect::<Vec<_>>()
        .join(" OR ");
    let (num, unit) = clickhouse_interval(since);
    format!(
        "SELECT count() AS matches FROM {} WHERE ({}) AND {} >= now() - INTERVAL {} {}",
        table,
        predicates,
        sql_identifier(timestamp_field),
        num,
        unit
    )
}

#[allow(clippy::too_many_arguments)]
async fn fetch_samples(
    client: &Client,
    team_id: i64,
    source_id: i64,
    table: &str,
    timestamp_field: &str,
    matched_columns: &[String],
    schema: &[Column],
    pattern: &str,
    since: &str,
    timeout: u32,
) -> Vec<ColumnSamples> {
    let mut out = Vec::new();
    for column in matched_columns {
        let with_count = column_uses_group_by(schema, column);
        let sql = sample_sql(table, timestamp_field, column, pattern, since);
        // Mirror the SQL's intent in the request envelope too: the server's
        // own `limit` clamp would otherwise override the inline LIMIT 1 we
        // bake into sample-only-column SQL.
        let request_limit = if with_count { 3 } else { 1 };
        let request = SqlQueryRequest {
            variables: Vec::new(),
            query_text: sql,
            limit: Some(request_limit),
            timezone: None,
            start_time: None,
            end_time: None,
            interval: None,
            query_timeout: Some(timeout),
        };
        let response = match client.query_sql(team_id, source_id, &request).await {
            Ok(r) => r,
            Err(err) => {
                tracing::debug!(
                    team = team_id,
                    source = source_id,
                    column = %column,
                    "sample fetch failed: {err:#}"
                );
                continue;
            }
        };
        let values = extract_sample_values(&response, column, with_count);
        if !values.is_empty() {
            out.push(ColumnSamples {
                column: column.clone(),
                values,
            });
        }
    }
    out
}

/// Extracts sample values for `column` from a query response, shared by the
/// ClickHouse and VictoriaLogs sample fetchers. `with_count` rows carry a
/// frequency under the `c` alias; single-sample rows do not.
fn extract_sample_values(
    response: &QueryResponse,
    column: &str,
    with_count: bool,
) -> Vec<SampleValue> {
    response
        .entries()
        .iter()
        .filter_map(|entry| {
            let raw = entry.get(column)?;
            let value = truncate(json_value_to_string(raw), SAMPLE_VALUE_TRUNCATE);
            let count = if with_count {
                entry.get("c").and_then(json_to_i64)
            } else {
                None
            };
            Some(SampleValue { value, count })
        })
        .collect()
}

/// Builds a LogsQL count query for `find` on a VictoriaLogs source, mirroring
/// the ClickHouse branch: a case-insensitive substring match on any candidate
/// column, bounded by the lookback window, aggregated to a single `matches`
/// count.
///
/// VictoriaLogs has no ClickHouse-style `toDateTime(...) BETWEEN`, so the
/// window is a LogsQL `_time:<lookback>` filter and the substring match uses a
/// RE2 `(?i)` regexp filter per column, OR-combined. VL-specific limits: the
/// match is a regexp scan over the raw field value (no ClickHouse
/// `positionCaseInsensitive`), and the lookback granularity is whatever
/// `_time:` accepts (s/m/h/d/w/y — anything else falls back to 24h).
fn vl_find_query(columns: &[String], pattern: &str, since: &str) -> String {
    format!(
        "_time:{} {} | stats count() as matches",
        vl_lookback(since),
        vl_pattern_filter(columns, pattern)
    )
}

#[allow(clippy::too_many_arguments)]
async fn vl_fetch_samples(
    client: &Client,
    team_id: i64,
    source_id: i64,
    matched_columns: &[String],
    pattern: &str,
    since: &str,
    timeout: u32,
) -> Vec<ColumnSamples> {
    let mut out = Vec::new();
    let lookback = vl_lookback(since);
    for column in matched_columns {
        let with_count = !is_sample_only_column(column);
        let filter = vl_pattern_filter(std::slice::from_ref(column), pattern);
        let query = if with_count {
            // Top-3 distinct values by frequency, mirroring the ClickHouse
            // `GROUP BY col ORDER BY count() DESC LIMIT 3`.
            format!(
                "_time:{} {} | stats by ({}) count() as c | sort by (c desc) | limit 3",
                lookback,
                filter,
                vl_field(column)
            )
        } else {
            // Free-form text (msg/message/body): one sample row is more useful
            // than a frequency table of unique strings.
            format!(
                "_time:{} {} | fields {} | limit 1",
                lookback,
                filter,
                vl_field(column)
            )
        };
        let request_limit = if with_count { 3 } else { 1 };
        let request = SqlQueryRequest {
            variables: Vec::new(),
            query_text: query,
            limit: Some(request_limit),
            timezone: None,
            start_time: None,
            end_time: None,
            interval: None,
            query_timeout: Some(timeout),
        };
        let response = match client.query_sql(team_id, source_id, &request).await {
            Ok(r) => r,
            Err(err) => {
                tracing::debug!(
                    team = team_id,
                    source = source_id,
                    column = %column,
                    "vl sample fetch failed: {err:#}"
                );
                continue;
            }
        };
        let values = extract_sample_values(&response, column, with_count);
        if !values.is_empty() {
            out.push(ColumnSamples {
                column: column.clone(),
                values,
            });
        }
    }
    out
}

/// OR-combines a case-insensitive substring regexp filter across `columns`,
/// wrapped in parens so it ANDs cleanly with the surrounding `_time:` filter.
fn vl_pattern_filter(columns: &[String], pattern: &str) -> String {
    let content = vl_regex_literal_ci(pattern);
    let terms: Vec<String> = columns
        .iter()
        .map(|column| format!("{}:~\"{}\"", vl_field(column), content))
        .collect();
    format!("({})", terms.join(" OR "))
}

/// Renders the double-quoted CONTENT of a LogsQL regexp filter (`field:~"…"`)
/// that matches `pattern` as a literal, case-insensitive substring. The
/// pattern is first RE2-escaped (so metacharacters match literally), then the
/// result is escaped for the surrounding LogsQL double-quoted string literal.
fn vl_regex_literal_ci(pattern: &str) -> String {
    let mut regex = String::from("(?i)");
    for ch in pattern.chars() {
        if "\\.+*?()|[]{}^$".contains(ch) {
            regex.push('\\');
        }
        regex.push(ch);
    }
    regex.replace('\\', "\\\\").replace('"', "\\\"")
}

/// Renders a LogsQL field name: bare for simple identifiers, double-quoted
/// (with escaping) otherwise.
fn vl_field(name: &str) -> String {
    let simple = !name.is_empty()
        && name
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '.');
    if simple {
        name.to_string()
    } else {
        format!("\"{}\"", name.replace('\\', "\\\\").replace('"', "\\\""))
    }
}

/// Sanitizes a lookback string for a LogsQL `_time:` filter, which accepts
/// s/m/h/d/w/y suffixes. Anything else falls back to 24h.
fn vl_lookback(since: &str) -> String {
    let t = since.trim();
    let valid = t.len() >= 2
        && t.ends_with(|c: char| "smhdwy".contains(c))
        && t[..t.len() - 1].chars().all(|c| c.is_ascii_digit());
    if valid {
        t.to_string()
    } else {
        "24h".to_string()
    }
}

fn sample_sql(
    table: &str,
    timestamp_field: &str,
    column: &str,
    pattern: &str,
    since: &str,
) -> String {
    let (num, unit) = clickhouse_interval(since);
    let ts = sql_identifier(timestamp_field);
    let col = sql_identifier(column);
    let pat = sql_string(pattern);
    let where_clause = format!(
        "positionCaseInsensitive(toString({col}), '{pat}') > 0 AND {ts} >= now() - INTERVAL {num} {unit}",
    );

    if is_sample_only_column(column) {
        // Free-form text: one truncated sample row is more useful than a
        // GROUP BY of unique strings.
        format!("SELECT {col} FROM {table} WHERE {where_clause} LIMIT 1")
    } else {
        // Top-3 by frequency. Cheap because the predicate is the same as the
        // count query.
        format!(
            "SELECT {col}, count() AS c FROM {table} WHERE {where_clause} GROUP BY {col} ORDER BY c DESC LIMIT 3"
        )
    }
}

fn is_sample_only_column(column: &str) -> bool {
    SAMPLE_ONLY_COLUMNS
        .iter()
        .any(|name| column.eq_ignore_ascii_case(name))
}

fn column_uses_group_by(schema: &[Column], column: &str) -> bool {
    if is_sample_only_column(column) {
        return false;
    }
    // Default to GROUP BY for non-msg columns regardless of declared type:
    // the LIMIT 3 keeps it cheap even on unbounded cardinality columns and
    // surfaces useful top-N data.
    let _ = schema;
    true
}

fn json_value_to_string(value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Null => String::new(),
        _ => value.to_string(),
    }
}

fn truncate(s: String, max: usize) -> String {
    if s.chars().count() <= max {
        return s;
    }
    let mut out: String = s.chars().take(max.saturating_sub(1)).collect();
    out.push('…');
    out
}

fn clickhouse_interval(since: &str) -> (i64, &'static str) {
    let trimmed = since.trim();
    let (num, unit) = if trimmed.ends_with('m') {
        (trimmed.trim_end_matches('m'), "MINUTE")
    } else if trimmed.ends_with('h') {
        (trimmed.trim_end_matches('h'), "HOUR")
    } else if trimmed.ends_with('d') {
        (trimmed.trim_end_matches('d'), "DAY")
    } else if trimmed.ends_with('w') {
        (trimmed.trim_end_matches('w'), "WEEK")
    } else {
        (trimmed, "MINUTE")
    };
    (num.parse().unwrap_or(24), unit)
}

fn sql_identifier(value: &str) -> String {
    format!("`{}`", value.trim_matches('`').replace('`', "``"))
}

fn sql_string(value: &str) -> String {
    value.replace('\\', "\\\\").replace('\'', "\\'")
}

fn json_to_i64(value: &serde_json::Value) -> Option<i64> {
    match value {
        serde_json::Value::Number(n) => n.as_i64().or_else(|| n.as_u64().map(|v| v as i64)),
        serde_json::Value::String(s) => s.parse().ok(),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use logchef_core::api::Source;

    use std::sync::atomic::{AtomicUsize, Ordering};

    #[tokio::test]
    async fn run_bounded_keeps_at_most_concurrency_probes_running() {
        let running = Arc::new(AtomicUsize::new(0));
        let peak = Arc::new(AtomicUsize::new(0));
        let mut seen = Vec::new();
        let finished = run_bounded(
            (0..10).collect(),
            3,
            Duration::from_secs(5),
            |i: i32| {
                let (running, peak) = (Arc::clone(&running), Arc::clone(&peak));
                async move {
                    let now = running.fetch_add(1, Ordering::SeqCst) + 1;
                    peak.fetch_max(now, Ordering::SeqCst);
                    tokio::time::sleep(Duration::from_millis(20)).await;
                    running.fetch_sub(1, Ordering::SeqCst);
                    Ok::<_, String>(i * 2)
                }
            },
            |done, total, _, _| seen.push((done, total)),
        )
        .await;
        assert_eq!(peak.load(Ordering::SeqCst), 3);
        let mut doubled: Vec<i32> = finished.into_iter().map(|(_, r)| r.unwrap()).collect();
        doubled.sort();
        assert_eq!(doubled, (0..10).map(|i| i * 2).collect::<Vec<_>>());
        assert_eq!(seen, (1..=10).map(|done| (done, 10)).collect::<Vec<_>>());
    }

    #[tokio::test]
    async fn run_bounded_skips_a_slow_or_failing_probe_and_keeps_the_rest() {
        let started = std::time::Instant::now();
        let finished = run_bounded(
            vec!["slow", "bad", "ok"],
            4,
            Duration::from_millis(100),
            |name: &'static str| async move {
                match name {
                    "slow" => {
                        tokio::time::sleep(Duration::from_secs(10)).await;
                        Ok(0)
                    }
                    "bad" => Err("Failed to read the schema: HTTP 500".to_string()),
                    _ => Ok(7),
                }
            },
            |_, _, _, _| {},
        )
        .await;
        assert!(started.elapsed() < Duration::from_secs(2));
        let outcome = |name| &finished.iter().find(|(n, _)| *n == name).unwrap().1;
        assert_eq!(outcome("slow"), &Err(Skip::TimedOut));
        assert_eq!(
            outcome("bad"),
            &Err(Skip::Failed("Failed to read the schema: HTTP 500".into()))
        );
        assert_eq!(outcome("ok"), &Ok(7));
    }

    #[test]
    fn a_skip_names_its_cause() {
        let deadline = Duration::from_secs(30);
        assert_eq!(describe(&Skip::TimedOut, deadline), "timed out after 30s");
        assert_eq!(
            describe(&Skip::Failed("API error: boom\nmore".into()), deadline),
            "API error: boom"
        );
    }

    #[test]
    fn builds_find_sql() {
        let sql = find_sql(
            "logs.app",
            "_timestamp",
            &["service".to_string()],
            "api",
            "2h",
        );
        assert!(sql.contains("positionCaseInsensitive(toString(`service`), 'api') > 0"));
        assert!(sql.contains("INTERVAL 2 HOUR"));
    }

    #[test]
    fn sample_sql_uses_group_by_for_label_columns() {
        let sql = sample_sql("logs.app", "_timestamp", "service", "api", "1h");
        assert!(sql.contains("GROUP BY `service`"));
        assert!(sql.contains("ORDER BY c DESC LIMIT 3"));
    }

    #[test]
    fn sample_sql_uses_single_row_for_msg() {
        let sql = sample_sql("logs.app", "_timestamp", "msg", "api", "1h");
        assert!(sql.contains("SELECT `msg` FROM logs.app"));
        assert!(sql.ends_with("LIMIT 1"));
        assert!(!sql.contains("GROUP BY"));
    }

    #[test]
    fn truncate_keeps_ascii_intact() {
        assert_eq!(truncate("hi".to_string(), 80), "hi");
        let long = "x".repeat(120);
        let t = truncate(long, 10);
        assert_eq!(t.chars().count(), 10);
        assert!(t.ends_with('…'));
    }

    #[test]
    fn clickhouse_interval_parses_units() {
        assert_eq!(clickhouse_interval("15m"), (15, "MINUTE"));
        assert_eq!(clickhouse_interval("3h"), (3, "HOUR"));
        assert_eq!(clickhouse_interval("7d"), (7, "DAY"));
        assert_eq!(clickhouse_interval("2w"), (2, "WEEK"));
    }

    #[test]
    fn clickhouse_interval_falls_back_to_default_for_garbage() {
        // No unit + non-numeric value falls back to 24-minute default.
        assert_eq!(clickhouse_interval("garbage"), (24, "MINUTE"));
    }

    #[test]
    fn sql_string_escapes_quotes_and_backslashes() {
        assert_eq!(sql_string("a'b"), "a\\'b");
        assert_eq!(sql_string("a\\b"), "a\\\\b");
    }

    fn make_source(id: i64, name: &str, db: &str, table: &str) -> Source {
        let json = serde_json::json!({
            "id": id,
            "name": name,
            "connection": {
                "database": db,
                "table_name": table,
            },
        });
        serde_json::from_value(json).unwrap()
    }

    #[test]
    fn source_filter_matches_by_id() {
        let src = make_source(11, "app-logs", "logs", "app");
        assert!(source_matches_filter(&src, Some("11")));
        assert!(!source_matches_filter(&src, Some("12")));
    }

    #[test]
    fn source_filter_matches_by_name_case_insensitive() {
        let src = make_source(11, "App-Logs", "logs", "app");
        assert!(source_matches_filter(&src, Some("app-logs")));
    }

    #[test]
    fn source_filter_matches_by_table_ref() {
        let src = make_source(11, "app-logs", "logs", "app");
        assert!(source_matches_filter(&src, Some("logs.app")));
    }

    #[test]
    fn source_filter_passes_when_no_filter() {
        let src = make_source(11, "app-logs", "logs", "app");
        assert!(source_matches_filter(&src, None));
    }

    #[test]
    fn vl_find_query_bounds_time_and_ors_columns() {
        let q = vl_find_query(&["service".to_string(), "msg".to_string()], "api", "1h");
        assert_eq!(
            q,
            "_time:1h (service:~\"(?i)api\" OR msg:~\"(?i)api\") | stats count() as matches"
        );
    }

    #[test]
    fn vl_regex_literal_escapes_metachars_and_quotes() {
        // `.` is a regex metachar → escaped to `\.` in the regex, then the
        // backslash is doubled for the LogsQL string literal.
        assert_eq!(vl_regex_literal_ci("a.b"), "(?i)a\\\\.b");
        // A double quote must be escaped for the string literal.
        assert_eq!(vl_regex_literal_ci("a\"b"), "(?i)a\\\"b");
    }

    #[test]
    fn vl_lookback_sanitizes_units() {
        assert_eq!(vl_lookback("15m"), "15m");
        assert_eq!(vl_lookback("2w"), "2w");
        assert_eq!(vl_lookback("garbage"), "24h");
        assert_eq!(vl_lookback(""), "24h");
    }

    #[test]
    fn vl_field_quotes_only_when_needed() {
        assert_eq!(vl_field("service_name"), "service_name");
        assert_eq!(vl_field("app.version"), "app.version");
        assert_eq!(vl_field("weird field"), "\"weird field\"");
    }
}
