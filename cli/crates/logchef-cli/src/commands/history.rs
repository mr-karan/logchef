use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::QueryHistoryEntry;

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Show your last 50 queries
  logchef history

  # Last 10, most recent first
  logchef history --limit 10

  # Scriptable: re-run the most recent query's text
  logchef history --limit 1 --output jsonl | jq -r '.query_text'")]
pub struct HistoryArgs {
    /// Number of entries to fetch (server default 50, max 200).
    #[arg(long, short = 'l', default_value_t = 50)]
    limit: u32,

    /// Output format.
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    Table,
}

pub async fn run(args: HistoryArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;

    let entries = s
        .client
        .get_query_history(args.limit)
        .await
        .context("Failed to get query history")?;

    if entries.is_empty() {
        if matches!(args.output, OutputFormat::Json) {
            println!("[]");
        } else if !matches!(args.output, OutputFormat::Jsonl) {
            println!("No query history found.");
        }
        return Ok(());
    }

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&entries)?);
        }
        OutputFormat::Jsonl => {
            for entry in &entries {
                println!("{}", serde_json::to_string(entry)?);
            }
        }
        OutputFormat::Text => print_text(&entries),
        OutputFormat::Table => print_table(&entries),
    }

    Ok(())
}

fn print_text(entries: &[QueryHistoryEntry]) {
    for entry in entries {
        println!(
            "#{}  {}  source={}  {}  {}ms  {} rows",
            entry.id,
            relative_time(entry.created_at),
            entry.source_id,
            entry.query_language,
            entry.duration_ms,
            entry.row_count,
        );
        println!("  {}", entry.query_text);
    }
    println!("\n{} queries", entries.len());
}

fn print_table(entries: &[QueryHistoryEntry]) {
    println!(
        "{:<5} {:<10} {:<8} {:<10} {:>8} {:>8}  QUERY",
        "ID", "WHEN", "SOURCE", "LANG", "MS", "ROWS"
    );
    println!("{}", "-".repeat(100));
    for entry in entries {
        println!(
            "{:<5} {:<10} {:<8} {:<10} {:>8} {:>8}  {}",
            entry.id,
            relative_time(entry.created_at),
            entry.source_id,
            entry.query_language,
            entry.duration_ms,
            entry.row_count,
            truncate_str(&single_line(&entry.query_text), 50),
        );
    }
    println!("\n{} queries", entries.len());
}

/// Formats a timestamp as a short "N unit ago" string, falling back to an
/// absolute date once it's a week or older.
fn relative_time(dt: DateTime<Utc>) -> String {
    let delta = Utc::now().signed_duration_since(dt);
    let secs = delta.num_seconds();

    if secs < 0 {
        return dt.format("%Y-%m-%d %H:%M").to_string();
    }
    if secs < 60 {
        return format!("{}s ago", secs);
    }
    let mins = delta.num_minutes();
    if mins < 60 {
        return format!("{}m ago", mins);
    }
    let hours = delta.num_hours();
    if hours < 24 {
        return format!("{}h ago", hours);
    }
    let days = delta.num_days();
    if days < 7 {
        return format!("{}d ago", days);
    }
    dt.format("%Y-%m-%d %H:%M").to_string()
}

/// Collapses a possibly multi-line query into one line for table display.
fn single_line(s: &str) -> String {
    s.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn truncate_str(s: &str, max_len: usize) -> String {
    if s.len() > max_len {
        format!("{}...", &s[..max_len.saturating_sub(3)])
    } else {
        s.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Duration;

    #[test]
    fn relative_time_buckets() {
        let now = Utc::now();
        assert_eq!(relative_time(now - Duration::seconds(5)), "5s ago");
        assert_eq!(relative_time(now - Duration::minutes(5)), "5m ago");
        assert_eq!(relative_time(now - Duration::hours(5)), "5h ago");
        assert_eq!(relative_time(now - Duration::days(3)), "3d ago");
    }

    #[test]
    fn relative_time_falls_back_to_date_after_a_week() {
        let now = Utc::now();
        let formatted = relative_time(now - Duration::days(10));
        assert!(!formatted.ends_with("ago"));
    }

    #[test]
    fn single_line_collapses_whitespace() {
        assert_eq!(single_line("a\n  b\tc"), "a b c");
    }

    #[test]
    fn truncate_str_adds_ellipsis() {
        assert_eq!(truncate_str("hello world", 8), "hello...");
        assert_eq!(truncate_str("short", 8), "short");
    }
}
