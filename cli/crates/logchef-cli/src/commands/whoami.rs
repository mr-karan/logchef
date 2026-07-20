use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Team, User};
use serde::Serialize;

use crate::cli::GlobalArgs;
use crate::session;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Show your identity, role, and teams
  logchef whoami

  # One-line JSON record (user + teams) for scripting
  logchef whoami --output jsonl | jq '.teams[].name'")]
pub struct WhoamiArgs {
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

#[derive(Serialize)]
struct WhoamiOutput {
    user: UserOutput,
    teams: Vec<TeamOutput>,
}

#[derive(Serialize)]
struct UserOutput {
    id: i64,
    email: String,
    full_name: Option<String>,
    role: String,
    status: Option<String>,
}

#[derive(Serialize)]
struct TeamOutput {
    id: i64,
    name: String,
    role: Option<String>,
}

pub async fn run(args: WhoamiArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;

    let user = s
        .client
        .get_current_user()
        .await
        .context("Failed to get user")?;
    let teams = s
        .client
        .list_teams()
        .await
        .context("Failed to list teams")?;
    let output = output(user, teams);

    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&output)?),
        OutputFormat::Jsonl => println!("{}", serde_json::to_string(&output)?),
        OutputFormat::Text => {
            println!(
                "{}{}",
                output.user.email,
                output
                    .user
                    .full_name
                    .as_ref()
                    .map(|name| format!(" ({})", name))
                    .unwrap_or_default()
            );
            println!("role: {}", output.user.role);
            println!("teams:");
            for team in output.teams {
                println!(
                    "  {}  {}{}",
                    team.id,
                    team.name,
                    team.role
                        .map(|role| format!(" ({})", role))
                        .unwrap_or_default()
                );
            }
        }
    }

    Ok(())
}

fn output(user: User, teams: Vec<Team>) -> WhoamiOutput {
    WhoamiOutput {
        user: UserOutput {
            id: user.id,
            email: user.email,
            full_name: user.full_name,
            role: user.role,
            status: user.status,
        },
        teams: teams
            .into_iter()
            .map(|team| TeamOutput {
                id: team.id,
                name: team.name,
                role: team.role,
            })
            .collect(),
    }
}
