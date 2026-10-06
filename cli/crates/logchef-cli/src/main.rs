mod banner;
mod cli;
mod commands;
mod env_flags;
mod session;
mod ui;
mod update;

use clap::{CommandFactory, FromArgMatches};

#[tokio::main]
async fn main() {
    let matches = cli::Cli::command().get_matches();
    let json_output = ui::wants_json_output(&matches);
    let cli = cli::Cli::from_arg_matches(&matches).unwrap_or_else(|err| err.exit());
    let quiet = cli.quiet;
    if let Err(err) = cli.run().await {
        ui::report_error(&err, quiet, json_output);
        std::process::exit(1);
    }
}
