use anyhow::{Context, Result};
use clap::Args;
use inquire::Select;
use logchef_core::Config;
use logchef_core::api::Client;
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use serde::Serialize;
use std::io::IsTerminal;

use crate::cli::GlobalArgs;

#[derive(Args)]
pub struct SourcesArgs {
    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

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
struct SourceOut {
    id: i64,
    name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    table: Option<String>,
    connected: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    description: Option<String>,
}

pub async fn run(args: SourcesArgs, global: GlobalArgs) -> Result<()> {
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

    let mut cache = Cache::new(&ctx.server_url);

    let is_interactive =
        args.team.is_none() && ctx.defaults.team.is_none() && std::io::stdin().is_terminal();

    let team_id = if is_interactive {
        prompt_team_interactive(&client, &mut cache).await?
    } else {
        let team_input = args.team.or(ctx.defaults.team.clone()).ok_or_else(|| {
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

    let sources = client
        .list_sources(team_id)
        .await
        .context("Failed to list sources")?;

    if sources.is_empty() {
        println!("No sources available for this team.");
        return Ok(());
    }

    let rows: Vec<SourceOut> = sources
        .into_iter()
        .map(|s| {
            let table = s.table_ref();
            SourceOut {
                id: s.id,
                name: s.name,
                table,
                connected: s.is_connected,
                description: s.description,
            }
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
                "{:<4} {:<24} {:<26} {:<10} DESCRIPTION",
                "ID", "NAME", "TABLE", "CONNECTED"
            );
            println!("{}", "-".repeat(90));
            for row in &rows {
                let desc = row.description.as_deref().unwrap_or("");
                let desc_truncated = truncate_str(desc, 32);
                let table = row.table.as_deref().unwrap_or("-");
                let connected = if row.connected { "yes" } else { "no" };

                println!(
                    "{:<4} {:<24} {:<26} {:<10} {}",
                    row.id,
                    truncate_str(&row.name, 24),
                    truncate_str(table, 26),
                    connected,
                    desc_truncated
                );
            }
            println!("\n{} sources", rows.len());
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

fn truncate_str(s: &str, max_len: usize) -> String {
    if s.len() > max_len {
        format!("{}...", &s[..max_len.saturating_sub(3)])
    } else {
        s.to_string()
    }
}
