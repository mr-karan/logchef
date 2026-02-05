use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::Client;
use serde::Serialize;

use crate::cli::GlobalArgs;

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

    let teams = client.list_teams().await.context("Failed to list teams")?;
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

fn truncate_str(s: &str, max_len: usize) -> String {
    if s.len() > max_len {
        format!("{}...", &s[..max_len.saturating_sub(3)])
    } else {
        s.to_string()
    }
}
