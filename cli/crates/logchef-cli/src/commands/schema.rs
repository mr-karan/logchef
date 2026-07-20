use anyhow::{Context, Result};
use clap::Args;
use inquire::Select;
use logchef_core::Config;
use logchef_core::api::{Client, Column};
use logchef_core::cache::{Cache, Identifier, parse_identifier};
use std::io::IsTerminal;

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
pub struct SchemaArgs {
    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

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

pub async fn run(args: SchemaArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let default_team = ctx.defaults.team_with_env();
    let default_source = ctx.defaults.source_with_env();

    let is_interactive = args.team.is_none()
        && args.source.is_none()
        && default_team.is_none()
        && default_source.is_none()
        && std::io::stdin().is_terminal();

    let team_id = if is_interactive {
        prompt_team_interactive(client, &mut cache).await?
    } else {
        let team_input = args.team.or(default_team).ok_or_else(|| {
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

    let source_id = if is_interactive {
        prompt_source_interactive(client, team_id, &mut cache).await?
    } else {
        let source_input = args.source.or(default_source).ok_or_else(|| {
            anyhow::anyhow!(
                "Source not specified. Use --source or set defaults.source. List sources with 'logchef sources --team <team>'."
            )
        })?;

        match parse_identifier(&source_input) {
            Identifier::Id(id) => id,
            Identifier::Name(name) => {
                if let Some(id) = cache.get_source_id(team_id, &name) {
                    id
                } else {
                    let sources = client
                        .list_sources(team_id)
                        .await
                        .context("Failed to list sources")?;

                    let mut cache_entries: Vec<(String, i64)> =
                        sources.iter().map(|s| (s.name.clone(), s.id)).collect();
                    for s in &sources {
                        if let Some(target_ref) = s.target_ref() {
                            cache_entries.push((target_ref, s.id));
                        }
                    }
                    cache.set_sources(team_id, &cache_entries);

                    sources
                        .iter()
                        .find(|s| s.name.eq_ignore_ascii_case(&name))
                        .or_else(|| {
                            sources.iter().find(|s| {
                                s.target_ref()
                                    .map(|r| r.eq_ignore_ascii_case(&name))
                                    .unwrap_or(false)
                            })
                        })
                        .map(|s| s.id)
                        .ok_or_else(|| anyhow::anyhow!("Source '{}' not found", name))?
                }
            }
        }
    };

    let columns = client
        .get_schema(team_id, source_id)
        .await
        .context("Failed to get schema")?;

    if columns.is_empty() {
        println!("No columns found for this source.");
        return Ok(());
    }

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&columns)?);
        }
        OutputFormat::Jsonl => {
            for col in columns {
                println!("{}", serde_json::to_string(&col)?);
            }
        }
        OutputFormat::Text | OutputFormat::Table => {
            print_schema_table(&columns);
            println!("\n{} columns", columns.len());
        }
    }

    Ok(())
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

async fn prompt_source_interactive(
    client: &Client,
    team_id: i64,
    cache: &mut Cache,
) -> Result<i64> {
    let sources = client
        .list_sources(team_id)
        .await
        .context("Failed to list sources")?;
    if sources.is_empty() {
        anyhow::bail!("No sources available for this team");
    }

    let options: Vec<String> = sources.iter().map(|s| s.display_name()).collect();
    let selection = Select::new("Select source:", options)
        .prompt()
        .context("Failed to select source")?;

    let source = sources
        .iter()
        .find(|s| selection.starts_with(&s.name))
        .ok_or_else(|| anyhow::anyhow!("Source not found"))?;

    let mut cache_entries: Vec<(String, i64)> =
        sources.iter().map(|s| (s.name.clone(), s.id)).collect();
    for s in &sources {
        if let Some(target_ref) = s.target_ref() {
            cache_entries.push((target_ref, s.id));
        }
    }
    cache.set_sources(team_id, &cache_entries);

    Ok(source.id)
}

fn print_schema_table(columns: &[Column]) {
    let has_descriptions = columns.iter().any(|col| col.description.is_some());
    if has_descriptions {
        println!("{:<30} {:<30} DESCRIPTION", "NAME", "TYPE");
        println!("{}", "-".repeat(90));
    } else {
        println!("{:<30} TYPE", "NAME");
        println!("{}", "-".repeat(60));
    }

    for col in columns {
        if has_descriptions {
            println!(
                "{:<30} {:<30} {}",
                col.name,
                col.column_type,
                col.description.as_deref().unwrap_or("")
            );
        } else {
            println!("{:<30} {}", col.name, col.column_type);
        }
    }
}
