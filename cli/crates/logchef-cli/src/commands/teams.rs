use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use serde::Serialize;

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
pub struct TeamsArgs {
    /// Output format
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

#[derive(Serialize)]
struct TeamOut {
    id: i64,
    name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    role: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    member_count: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    description: Option<String>,
}

pub async fn run(args: TeamsArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;

    let teams = s
        .client
        .list_teams()
        .await
        .context("Failed to list teams")?;
    if teams.is_empty() {
        println!("No teams available.");
        return Ok(());
    }

    let rows: Vec<TeamOut> = teams
        .into_iter()
        .map(|t| TeamOut {
            id: t.id,
            name: t.name,
            role: t.role,
            member_count: t.member_count,
            description: t.description,
        })
        .collect();

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&rows)?);
        }
        OutputFormat::Jsonl => {
            for row in rows {
                println!("{}", serde_json::to_string(&row)?);
            }
        }
        OutputFormat::Text | OutputFormat::Table => {
            println!(
                "{:<4} {:<24} {:<12} {:<8} DESCRIPTION",
                "ID", "NAME", "ROLE", "MEMBERS"
            );
            println!("{}", "-".repeat(70));
            for row in &rows {
                let role = row.role.as_deref().unwrap_or("-");
                let members = row
                    .member_count
                    .map(|v| v.to_string())
                    .unwrap_or_else(|| "-".to_string());
                let desc = row.description.as_deref().unwrap_or("");
                let desc_truncated = truncate_str(desc, 38);

                println!(
                    "{:<4} {:<24} {:<12} {:<8} {}",
                    row.id,
                    truncate_str(&row.name, 24),
                    truncate_str(role, 12),
                    members,
                    desc_truncated
                );
            }
            println!("\n{} teams", rows.len());
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
