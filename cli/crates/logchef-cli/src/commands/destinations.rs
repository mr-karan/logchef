use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::Destination;

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # The notification destinations alert rules can send to
  logchef destinations list

  # As JSON, to look up an id for an alert rule's destination_ids
  logchef destinations list --output json

Create and edit destinations in the web UI.")]
pub struct DestinationsArgs {
    #[command(subcommand)]
    command: DestinationsCommand,
}

#[derive(Subcommand)]
enum DestinationsCommand {
    /// List the notification destinations you can see.
    List(ListArgs),
}

#[derive(Args)]
struct ListArgs {
    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Clone, Copy, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
}

pub async fn run(args: DestinationsArgs, global: GlobalArgs) -> Result<()> {
    let DestinationsCommand::List(args) = args.command;
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let destinations = s
        .client
        .list_destinations()
        .await
        .context("Failed to list notification destinations")?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&destinations.raw)?),
        OutputFormat::Text => print!("{}", render(&destinations.parsed)),
    }
    Ok(())
}

fn render(destinations: &[Destination]) -> String {
    if destinations.is_empty() {
        return "No notification destinations found.\n".to_owned();
    }
    let mut out = format!(
        "{:<6} {:<30} {:<12} {:<8} {:<6} ACTIVE\n{}\n",
        "ID",
        "NAME",
        "TYPE",
        "SCOPE",
        "TEAM",
        "-".repeat(72)
    );
    for d in destinations {
        out.push_str(&format!(
            "{:<6} {:<30} {:<12} {:<8} {:<6} {}\n",
            d.id,
            ui::truncate(&d.name, 30),
            d.kind,
            d.scope,
            d.team_id.map_or("-".to_owned(), |id| id.to_string()),
            if d.is_active { "yes" } else { "no" }
        ));
    }
    out
}
