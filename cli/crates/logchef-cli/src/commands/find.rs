use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Client, Column, SqlQueryRequest, Team};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use serde::Serialize;

use crate::cli::GlobalArgs;
use crate::session;

const DEFAULT_COLUMNS: &[&str] = &["service", "service_name", "job_name", "app", "host", "msg"];
/// Columns that are treated as free-form text — a single sample row is more
/// informative than a GROUP BY of unique strings.
const SAMPLE_ONLY_COLUMNS: &[&str] = &["msg", "message", "body"];
const SAMPLE_VALUE_TRUNCATE: usize = 80;

#[derive(Args)]
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

    /// Query timeout in seconds per source.
    #[arg(long, default_value = "30")]
    timeout: u32,

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

pub async fn run(args: FindArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let teams = resolve_teams(client, &mut cache, args.team.as_deref()).await?;
    let candidates = if args.columns.is_empty() {
        DEFAULT_COLUMNS
            .iter()
            .map(|column| column.to_string())
            .collect()
    } else {
        args.columns.clone()
    };

    let mut results = Vec::new();
    let mut skipped = 0usize;
    for team in teams {
        let sources = client
            .list_sources(team.id)
            .await
            .with_context(|| format!("Failed to list sources for team {}", team.id))?;

        for source in sources {
            if !source_matches_filter(&source, args.source.as_deref()) {
                continue;
            }
            let Some(table) = source.table_ref() else {
                continue;
            };

            let schema = match client.get_schema(team.id, source.id).await {
                Ok(schema) => schema,
                Err(err) => {
                    tracing::debug!(
                        team = team.id,
                        source = source.id,
                        "skipping source: get_schema failed: {err:#}"
                    );
                    skipped += 1;
                    continue;
                }
            };
            let searchable = matching_columns(&schema, &candidates);
            if searchable.is_empty() {
                continue;
            }

            let timestamp_field = source
                .meta_ts_field
                .as_deref()
                .filter(|field| !field.trim().is_empty())
                .unwrap_or("_timestamp");
            let sql = find_sql(
                &table,
                timestamp_field,
                &searchable,
                &args.pattern,
                &args.since,
            );
            let request = SqlQueryRequest {
                query_text: sql,
                limit: Some(1),
                timezone: ctx.defaults.timezone.clone(),
                start_time: None,
                end_time: None,
                query_timeout: Some(args.timeout),
            };
            let response = match client.query_sql(team.id, source.id, &request).await {
                Ok(response) => response,
                Err(err) => {
                    tracing::debug!(
                        team = team.id,
                        source = source.id,
                        "skipping source: query failed: {err:#}"
                    );
                    skipped += 1;
                    continue;
                }
            };
            let matches = response
                .entries()
                .first()
                .and_then(|entry| entry.get("matches"))
                .and_then(json_to_i64)
                .unwrap_or(0);

            if matches > 0 {
                let samples = if args.no_samples {
                    Vec::new()
                } else {
                    fetch_samples(
                        client,
                        team.id,
                        source.id,
                        &table,
                        timestamp_field,
                        &searchable,
                        &schema,
                        &args.pattern,
                        &args.since,
                        args.timeout,
                    )
                    .await
                };
                results.push(FindResult {
                    team_id: team.id,
                    team_name: team.name.clone(),
                    source_id: source.id,
                    source_name: source.name.clone(),
                    table,
                    matches,
                    columns: searchable,
                    lookback: args.since.clone(),
                    samples,
                });
            }
        }
    }

    results.sort_by_key(|r| std::cmp::Reverse(r.matches));
    results.truncate(args.limit);
    print_results(&results, &args.output, skipped)
}

fn print_results(results: &[FindResult], output: &OutputFormat, skipped: usize) -> Result<()> {
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
                        result.matches,
                        result.lookback,
                        result.columns.join(",")
                    );
                    for sample in &result.samples {
                        let rendered = sample
                            .values
                            .iter()
                            .map(|v| match v.count {
                                Some(c) => format!("\"{}\" ({})", v.value, c),
                                None => format!("\"{}\" (sample)", v.value),
                            })
                            .collect::<Vec<_>>()
                            .join(", ");
                        println!("  ↳ {}: {}", sample.column, rendered);
                    }
                }
            }
            if skipped > 0 {
                eprintln!(
                    "({} source{} skipped due to errors — rerun with --debug for details)",
                    skipped,
                    if skipped == 1 { "" } else { "s" }
                );
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

fn source_matches_filter(source: &logchef_core::api::Source, filter: Option<&str>) -> bool {
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
            query_text: sql,
            limit: Some(request_limit),
            timezone: None,
            start_time: None,
            end_time: None,
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
        let values: Vec<SampleValue> = response
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
            .collect();
        if !values.is_empty() {
            out.push(ColumnSamples {
                column: column.clone(),
                values,
            });
        }
    }
    out
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
}
