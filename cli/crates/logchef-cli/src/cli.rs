use anyhow::Result;
use clap::{CommandFactory, Parser, Subcommand};
use tracing_subscriber::EnvFilter;

use crate::commands::{auth, collections, config, query, schema, sources, sql, teams};

#[derive(Parser)]
#[command(name = "logchef")]
#[command(author, version, about = "LogChef CLI - A sophisticated log viewer", long_about = None)]
pub struct Cli {
    #[command(subcommand)]
    command: Option<Commands>,

    #[arg(
        long,
        short,
        env = "LOGCHEF_CONTEXT",
        global = true,
        help = "Use a specific context"
    )]
    context: Option<String>,

    #[arg(
        long,
        env = "LOGCHEF_SERVER_URL",
        global = true,
        help = "Override server URL"
    )]
    server: Option<String>,

    #[arg(
        long,
        env = "LOGCHEF_AUTH_TOKEN",
        global = true,
        help = "Override auth token"
    )]
    token: Option<String>,

    #[arg(long, short, global = true)]
    debug: bool,
}

#[derive(Subcommand)]
enum Commands {
    #[command(about = "Authenticate with LogChef server")]
    Auth(auth::AuthArgs),

    #[command(about = "Execute a LogChefQL query")]
    Query(query::QueryArgs),

    #[command(about = "Execute a raw SQL query")]
    Sql(sql::SqlArgs),

    #[command(about = "List and run saved collections")]
    Collections(collections::CollectionsArgs),

    #[command(about = "List available teams")]
    Teams(teams::TeamsArgs),

    #[command(about = "List sources for a team")]
    Sources(sources::SourcesArgs),

    #[command(about = "Show schema for a source")]
    Schema(schema::SchemaArgs),

    #[command(about = "Manage CLI configuration")]
    Config(config::ConfigArgs),
}

pub struct GlobalArgs {
    pub context: Option<String>,
    pub server: Option<String>,
    pub token: Option<String>,
}

impl Cli {
    pub async fn run(self) -> Result<()> {
        let filter = if self.debug {
            EnvFilter::new("debug")
        } else {
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("warn"))
        };

        tracing_subscriber::fmt()
            .with_env_filter(filter)
            .with_target(false)
            .init();

        let global = GlobalArgs {
            context: self.context,
            server: self.server,
            token: self.token,
        };

        match self.command {
            Some(Commands::Auth(args)) => auth::run(args, global).await,
            Some(Commands::Query(args)) => query::run(args, global).await,
            Some(Commands::Sql(args)) => sql::run(args, global).await,
            Some(Commands::Collections(args)) => collections::run(args, global).await,
            Some(Commands::Teams(args)) => teams::run(args, global).await,
            Some(Commands::Sources(args)) => sources::run(args, global).await,
            Some(Commands::Schema(args)) => schema::run(args, global).await,
            Some(Commands::Config(args)) => config::run(args).await,
            None => {
                let mut cmd = Cli::command();
                cmd.print_help()?;
                println!();
                Ok(())
            }
        }
    }
}
