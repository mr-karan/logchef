use anyhow::{Context, Result, anyhow};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Client, SchemaAdvice, SchemaFinding};
use logchef_core::cache::{Identifier, parse_identifier};

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Show the stored report of source 7
  logchef advisor 7

  # Analyze the source again over the last 14 days, then show the report
  logchef advisor app-logs --refresh --window-days 14

  # Print only the DDL statements, to review and run with clickhouse-client
  logchef advisor 7 --ddl

The schema advisor is for global admins. Use an API token with the
sources:read scope (pass it with --token or LOGCHEF_AUTH_TOKEN). A
`logchef auth` sign-in cannot reach admin routes. Logchef never runs the DDL.")]
pub struct AdvisorArgs {
    /// Source ID, name or table (database.table). Names are looked up among
    /// all sources.
    source: String,

    /// Analyze the source before printing the report. It reads ClickHouse
    /// system tables and a small sample, within budgets.
    #[arg(long)]
    refresh: bool,

    /// Days of recorded queries that --refresh analyzes. Default: the
    /// server's, 30.
    #[arg(long, requires = "refresh", value_parser = clap::value_parser!(u32).range(7..=90))]
    window_days: Option<u32>,

    /// Print only the DDL statements of the findings that are not dismissed,
    /// one per line, in the order to run them.
    #[arg(long, conflicts_with = "output")]
    ddl: bool,

    /// Output format. json prints the server's response unchanged,
    /// dismissed findings included.
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Clone, Copy, Debug, PartialEq, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
}

/// A refresh runs on the server for up to 30 seconds.
const REFRESH_TIMEOUT_SECS: u64 = 60;

pub async fn run(args: AdvisorArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed_with_timeout(&config, &global, |ctx| {
        if args.refresh {
            ctx.timeout_secs.max(REFRESH_TIMEOUT_SECS)
        } else {
            ctx.timeout_secs
        }
    })?;
    let client = &s.client;

    let source_id = resolve(client, &args.source)
        .await
        .map_err(explain_forbidden)?;
    let raw = if args.refresh {
        client
            .refresh_schema_advice(source_id, args.window_days)
            .await
    } else {
        client.get_schema_advice(source_id).await
    }
    .map_err(explain_forbidden)?;

    if args.output == OutputFormat::Json {
        println!("{}", serde_json::to_string_pretty(&raw)?);
        return Ok(());
    }
    let advice: SchemaAdvice =
        serde_json::from_value(raw).context("Failed to read the schema advisor response")?;
    if args.ddl {
        print!("{}", render_ddl(&advice));
        if advice.report.is_none() {
            eprintln!("{}", no_report(source_id));
        }
    } else {
        print!("{}", render_text(&advice));
    }
    Ok(())
}

/// Resolves a source ID, or a name or table among all sources.
async fn resolve(client: &Client, source: &str) -> logchef_core::Result<i64> {
    let name = match parse_identifier(source) {
        Identifier::Id(id) => return Ok(id),
        Identifier::Name(name) => name,
    };
    let sources = client.list_all_sources().await?;
    let mut matches: Vec<_> = sources
        .iter()
        .filter(|s| s.name.eq_ignore_ascii_case(&name))
        .collect();
    if matches.is_empty() {
        matches = sources
            .iter()
            .filter(|s| {
                s.target_ref()
                    .is_some_and(|target| target.eq_ignore_ascii_case(&name))
            })
            .collect();
    }
    match matches.as_slice() {
        [one] => Ok(one.id),
        [] => Err(logchef_core::Error::other(format!(
            "No source is named '{name}'. Pass a source ID; global admins see every source on the Sources page."
        ))),
        many => Err(logchef_core::Error::other(format!(
            "'{name}' names {} sources (IDs {}). Pass one ID.",
            many.len(),
            many.iter()
                .map(|s| s.id.to_string())
                .collect::<Vec<_>>()
                .join(", ")
        ))),
    }
}

/// The server refuses the advisor with a 403 to anyone but a global admin,
/// to an API token without sources:read, and to every OAuth sign-in. The
/// generic 403 hint (team/source access, sign in again) would mislead, so
/// the error is replaced.
fn explain_forbidden(err: logchef_core::Error) -> anyhow::Error {
    let scope_denied = err.is_scope_denied();
    match err {
        logchef_core::Error::Api {
            status: Some(403),
            message,
            ..
        } => anyhow!(
            "The server refused the schema advisor{}\n\
             It needs a global admin's API token with the sources:read scope (or *). \
             A `logchef auth` sign-in cannot reach admin routes. Create one on your Profile page, \
             then pass it with --token or LOGCHEF_AUTH_TOKEN.",
            // A missing scope's message says what the advice below says.
            if scope_denied {
                ".".to_owned()
            } else {
                format!(": {message}")
            }
        ),
        other => anyhow::Error::new(other).context("Failed to read the schema advisor report"),
    }
}

fn no_report(source_id: i64) -> String {
    format!("No report yet. Run `logchef advisor {source_id} --refresh` to analyze the source.")
}

/// The findings that no dismissal names, and the count of the others.
fn active(advice: &SchemaAdvice) -> (Vec<&SchemaFinding>, usize) {
    let Some(report) = &advice.report else {
        return (Vec::new(), 0);
    };
    let (dismissed, active): (Vec<_>, Vec<_>) = report
        .findings
        .iter()
        .partition(|f| advice.dismissals.iter().any(|d| d.finding_id == f.id));
    (active, dismissed.len())
}

fn render_ddl(advice: &SchemaAdvice) -> String {
    let (findings, _) = active(advice);
    let mut out = String::new();
    for statement in findings.iter().flat_map(|f| &f.ddl) {
        out.push_str(statement);
        out.push('\n');
    }
    out
}

fn render_text(advice: &SchemaAdvice) -> String {
    let Some(report) = &advice.report else {
        return format!("{}\n", no_report(advice.source_id));
    };
    let mut out = String::new();
    let target = report.target.as_ref().map(|t| {
        let mut name = format!("{}.{} ({})", t.database, t.table, t.kind);
        if !t.cluster.is_empty() {
            name.push_str(&format!(" on cluster {}", t.cluster));
        }
        name
    });
    out.push_str(&format!(
        "Source {}: {}, ClickHouse {}\n",
        advice.source_id,
        target.as_deref().unwrap_or("no target table"),
        if report.clickhouse_version.is_empty() {
            "version unknown"
        } else {
            &report.clickhouse_version
        }
    ));
    if let Some(at) = advice.generated_at {
        out.push_str(&format!(
            "Report from {}{}, over {} days of queries\n",
            at.format("%Y-%m-%d %H:%M UTC"),
            if advice.stale { " (stale)" } else { "" },
            advice
                .window_days
                .map_or_else(|| "?".to_owned(), |d| d.to_string())
        ));
    }
    out.push_str(&format!("Status: {}", report.status));
    if !report.reason.is_empty() {
        out.push_str(&format!(" ({})", report.reason));
    }
    out.push('\n');
    for input in report.inputs.iter().filter(|i| i.state != "ok") {
        out.push_str(&format!("Input {}: {}", input.name, input.state));
        if !input.detail.is_empty() {
            out.push_str(&format!(" ({})", input.detail));
        }
        out.push('\n');
    }
    out.push_str(&format!(
        "Queries analyzed: {}, not analyzed: {}\n",
        report.queries_analyzed, report.queries_unanalyzed
    ));

    let (findings, dismissed) = active(advice);
    if findings.is_empty() {
        out.push_str("\nNo findings.\n");
    }
    for f in findings {
        out.push_str(&format!("\n[{}] {}\n", f.impact, f.title));
        let mut tags = vec![format!("rule {}", f.rule), format!("id {}", f.id)];
        if f.rebuild {
            tags.push("needs a table rebuild".to_owned());
        }
        if f.candidate {
            tags.push("validate first".to_owned());
        }
        out.push_str(&format!("  {}\n", tags.join(", ")));
        for e in &f.evidence {
            out.push_str(&format!("  {}: {}\n", e.label, e.value));
        }
        if !f.ddl.is_empty() {
            out.push_str("  DDL:\n");
            for statement in &f.ddl {
                out.push_str(&format!("    {statement}\n"));
            }
        }
        for (label, text) in [("Gain", &f.gain), ("Cost", &f.cost), ("Verify", &f.verify)] {
            if !text.is_empty() {
                out.push_str(&format!("  {label}: {text}\n"));
            }
        }
    }
    if dismissed > 0 {
        out.push_str(&format!(
            "\n{dismissed} dismissed finding{} not shown. --output json lists them.\n",
            if dismissed == 1 { "" } else { "s" }
        ));
    }
    if !report.notes.is_empty() {
        out.push_str("\nNotes:\n");
        for note in &report.notes {
            out.push_str(&format!("  - {note}\n"));
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn advice() -> SchemaAdvice {
        serde_json::from_value(serde_json::json!({
            "source_id": 7,
            "generated_at": "2026-11-16T10:00:00Z",
            "generated_by": 1,
            "window_days": 30,
            "stale": false,
            "report": {
                "status": "partial",
                "clickhouse_version": "26.8.2",
                "target": {"kind": "table", "database": "logs", "table": "app", "engine": "MergeTree"},
                "inputs": [
                    {"name": "system.tables", "state": "ok", "read_rows": 1, "read_bytes": 1},
                    {"name": "system.query_log", "state": "missing", "detail": "query logging is off", "read_rows": 0, "read_bytes": 0}
                ],
                "queries_analyzed": 1900,
                "queries_unanalyzed": 12,
                "findings": [
                    {"id": "aaaaaaaaaaaaaaaa", "rule": "skip_index_bloom", "impact": "medium", "title": "Add a bloom filter index on request_id",
                     "evidence": [{"label": "Queries with = or IN", "value": "412 of 1,900 (22%)", "source": "logchef"}],
                     "ddl": ["ALTER TABLE `logs`.`app` ADD INDEX IF NOT EXISTS `idx_request_id_bloom` `request_id` TYPE bloom_filter GRANULARITY 1;",
                             "ALTER TABLE `logs`.`app` MATERIALIZE INDEX `idx_request_id_bloom`;"],
                     "rebuild": false, "candidate": false, "gain": "Skips granules.", "cost": "A mutation.",
                     "verify": "EXPLAIN indexes = 1 SELECT count() FROM `logs`.`app` WHERE `request_id` = '<value>'"},
                    {"id": "bbbbbbbbbbbbbbbb", "rule": "sort_key", "impact": "high", "title": "Sort by service first",
                     "evidence": [], "ddl": [], "rebuild": true, "candidate": true, "gain": "Fewer granules.", "cost": "A rebuild."},
                    {"id": "cccccccccccccccc", "rule": "codec", "impact": "low", "title": "Compress msg with ZSTD",
                     "evidence": [], "ddl": ["ALTER TABLE `logs`.`app` MODIFY COLUMN `msg` CODEC(ZSTD(1));"],
                     "rebuild": false, "candidate": false, "gain": "Smaller.", "cost": "CPU."}
                ],
                "notes": ["text index advice needs ClickHouse 26.8"]
            },
            "dismissals": [{"finding_id": "cccccccccccccccc", "reason": "ZSTD is the default here", "dismissed_by": 1, "dismissed_at": "2026-11-16T11:00:00Z"}]
        }))
        .expect("parse advice")
    }

    #[test]
    fn ddl_prints_only_the_statements_of_active_findings() {
        let advice = advice();
        assert_eq!(
            render_ddl(&advice),
            "ALTER TABLE `logs`.`app` ADD INDEX IF NOT EXISTS `idx_request_id_bloom` `request_id` TYPE bloom_filter GRANULARITY 1;\n\
             ALTER TABLE `logs`.`app` MATERIALIZE INDEX `idx_request_id_bloom`;\n"
        );
    }

    #[test]
    fn text_shows_findings_inputs_and_notes_and_hides_dismissed() {
        let text = render_text(&advice());
        for want in [
            "Source 7: logs.app (table), ClickHouse 26.8.2",
            "Report from 2026-11-16 10:00 UTC, over 30 days of queries",
            "Status: partial",
            "Input system.query_log: missing (query logging is off)",
            "Queries analyzed: 1900, not analyzed: 12",
            "[medium] Add a bloom filter index on request_id",
            "  Queries with = or IN: 412 of 1,900 (22%)",
            "    ALTER TABLE `logs`.`app` MATERIALIZE INDEX `idx_request_id_bloom`;",
            "[high] Sort by service first",
            "  rule sort_key, id bbbbbbbbbbbbbbbb, needs a table rebuild, validate first",
            "1 dismissed finding not shown. --output json lists them.",
            "  - text index advice needs ClickHouse 26.8",
        ] {
            assert!(text.contains(want), "missing {want:?} in:\n{text}");
        }
        assert!(!text.contains("Compress msg"), "{text}");
        assert!(!text.contains("system.tables"), "{text}");
    }

    #[test]
    fn no_report_before_the_first_run() {
        let advice: SchemaAdvice = serde_json::from_value(serde_json::json!({
            "source_id": 7, "generated_at": null, "generated_by": null, "window_days": null,
            "stale": false, "report": null, "dismissals": []
        }))
        .expect("parse advice");
        assert_eq!(render_ddl(&advice), "");
        assert_eq!(
            render_text(&advice),
            "No report yet. Run `logchef advisor 7 --refresh` to analyze the source.\n"
        );
    }

    #[test]
    fn a_403_names_the_admin_token_and_scope() {
        let err = explain_forbidden(logchef_core::Error::api(Some(403), "Admin access required"));
        let text = format!("{err:#}");
        assert!(text.contains("sources:read"), "{text}");
        assert!(text.contains("global admin's API token"), "{text}");
        assert!(text.contains("Admin access required"), "{text}");
        assert_eq!(crate::ui::hint_for_error(&err), None);
    }
}
