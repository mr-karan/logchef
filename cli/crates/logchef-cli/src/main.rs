mod banner;
mod cli;
mod commands;
mod env_flags;
mod session;
mod ui;
mod update;

use clap::Parser;

#[tokio::main]
async fn main() {
    let cli = cli::Cli::parse();
    let quiet = cli.quiet;
    if let Err(err) = cli.run().await {
        ui::report_error(&err, quiet);
        std::process::exit(1);
    }
}
