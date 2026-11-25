use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::DashboardSummary;

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # The dashboards you can see, to check for one before you make another
  logchef dashboards list

  # As JSON
  logchef dashboards list --output json

A dashboard you see is one you created or one with a panel on a team you
belong to. Create and edit dashboards in the web UI.")]
pub struct DashboardsArgs {
    #[command(subcommand)]
    command: DashboardsCommand,
}

#[derive(Subcommand)]
enum DashboardsCommand {
    /// List the dashboards you can see.
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

pub async fn run(args: DashboardsArgs, global: GlobalArgs) -> Result<()> {
    let DashboardsCommand::List(args) = args.command;
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let dashboards = s
        .client
        .list_dashboards()
        .await
        .context("Failed to list dashboards")?;
    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&dashboards.raw)?),
        OutputFormat::Text => print!("{}", render(&dashboards.parsed)),
    }
    Ok(())
}

fn render(dashboards: &[DashboardSummary]) -> String {
    if dashboards.is_empty() {
        return "No dashboards found.\n".to_owned();
    }
    let mut out = format!(
        "{:<6} {:<32} {:<6} {:<4} {:<24} {:<4} UPDATED (UTC)\n{}\n",
        "ID",
        "NAME",
        "PANELS",
        "TABS",
        "KINDS",
        "EDIT",
        "-".repeat(96)
    );
    for d in dashboards {
        let kinds = if d.corrupt {
            "corrupt".to_owned()
        } else {
            d.panel_types.join(",")
        };
        out.push_str(&format!(
            "{:<6} {:<32} {:<6} {:<4} {:<24} {:<4} {}\n",
            d.id,
            ui::truncate(&d.name, 32),
            d.panel_count,
            d.tab_count,
            ui::truncate(&kinds, 24),
            if d.can_edit { "yes" } else { "no" },
            d.updated_at.format("%Y-%m-%d %H:%M")
        ));
    }
    out
}
