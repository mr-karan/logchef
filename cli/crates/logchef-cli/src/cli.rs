use anyhow::Result;
use clap::{CommandFactory, Parser, Subcommand};
use tracing_subscriber::EnvFilter;

use crate::commands::{
    auth, collections, completions, config, doctor, explain, fields, find, histogram, history,
    open, query, saved_queries, schema, skills, sources, sql, tail, teams, whoami,
};

const LONG_ABOUT: &str = "\
Logchef CLI — search and investigate logs from your terminal.

Logchef sources are backed by either ClickHouse or VictoriaLogs. Pick a command
by what you want to do:

  query      Search with LogchefQL — the portable filter language that works on
             BOTH engines. Start here. e.g. `level=\"error\" and service=\"api\"`.
  sql        Run a raw native query when LogchefQL isn't enough: ClickHouse SQL
             for ClickHouse sources, LogsQL for VictoriaLogs sources.
  explain    Show the ClickHouse SQL / LogsQL a LogchefQL query compiles to,
             without running it. Great for learning and debugging filters.
  histogram  Plot log counts over time (trends, spikes, error rates).
  fields     Discover a source's fields, or the observed values of one field.
  find       Locate which source holds a given service, host, or message.
  tail       Follow matching logs live.

Set a default team and source once (`logchef config set team …` /
`… set source …`) so you can drop -t/-S. Every data command supports
--output json|jsonl for scripting, and --quiet for clean agent-friendly output.

New here? Run `logchef doctor` to check your setup, or `logchef skills get core`
for the full usage guide.";

#[derive(Parser)]
#[command(name = "logchef")]
#[command(author, version, about = "Logchef CLI - search logs from your terminal", long_about = LONG_ABOUT)]
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

    #[arg(
        long,
        short,
        global = true,
        help = "Suppress stderr stats, highlighting, and spinners (data still goes to stdout)"
    )]
    pub quiet: bool,
}

#[derive(Subcommand)]
enum Commands {
    #[command(about = "Authenticate with Logchef server")]
    Auth(auth::AuthArgs),

    #[command(about = "Execute a LogchefQL query")]
    Query(query::QueryArgs),

    #[command(
        visible_alias = "native",
        about = "Execute a raw native query (SQL for ClickHouse, LogsQL for VictoriaLogs)"
    )]
    Sql(sql::SqlArgs),

    #[command(
        about = "Translate and validate a LogchefQL query without executing it (shows the generated ClickHouse SQL or LogsQL)"
    )]
    Explain(explain::ExplainArgs),

    #[command(about = "Discover fields for a source, or observed values for a field")]
    Fields(fields::FieldsArgs),

    #[command(about = "Show log counts over time as a terminal bar chart")]
    Histogram(histogram::HistogramArgs),

    #[command(about = "Show your recent query history")]
    History(history::HistoryArgs),

    #[command(about = "Open the current team/source (and optional query) in the web explorer")]
    Open(open::OpenArgs),

    #[command(about = "List and run saved collections")]
    Collections(collections::CollectionsArgs),

    #[command(name = "saved-queries", about = "List and run saved queries")]
    SavedQueries(saved_queries::SavedQueriesArgs),

    #[command(about = "Find sources that contain a service, job, host, or message pattern")]
    Find(find::FindArgs),

    #[command(about = "Follow matching LogChefQL results")]
    Tail(tail::TailArgs),

    #[command(about = "List available teams")]
    Teams(teams::TeamsArgs),

    #[command(about = "Show current user and accessible teams")]
    Whoami(whoami::WhoamiArgs),

    #[command(about = "List sources for a team")]
    Sources(sources::SourcesArgs),

    #[command(about = "Show schema for a source")]
    Schema(schema::SchemaArgs),

    #[command(about = "Diagnose config, connectivity, auth, and defaults")]
    Doctor(doctor::DoctorArgs),

    #[command(about = "Manage CLI configuration")]
    Config(config::ConfigArgs),

    #[command(about = "Show bundled skills for using Logchef")]
    Skills(skills::SkillsArgs),

    #[command(about = "Generate shell completion scripts")]
    Completions(completions::CompletionsArgs),
}

pub struct GlobalArgs {
    pub context: Option<String>,
    pub server: Option<String>,
    pub token: Option<String>,
    pub quiet: bool,
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
            quiet: self.quiet,
        };

        match self.command {
            Some(Commands::Auth(args)) => auth::run(args, global).await,
            Some(Commands::Query(args)) => query::run(args, global).await,
            Some(Commands::Sql(args)) => sql::run(args, global).await,
            Some(Commands::Explain(args)) => explain::run(args, global).await,
            Some(Commands::Fields(args)) => fields::run(args, global).await,
            Some(Commands::Histogram(args)) => histogram::run(args, global).await,
            Some(Commands::History(args)) => history::run(args, global).await,
            Some(Commands::Open(args)) => open::run(args, global).await,
            Some(Commands::Collections(args)) => collections::run(args, global).await,
            Some(Commands::SavedQueries(args)) => saved_queries::run(args, global).await,
            Some(Commands::Find(args)) => find::run(args, global).await,
            Some(Commands::Tail(args)) => tail::run(args, global).await,
            Some(Commands::Teams(args)) => teams::run(args, global).await,
            Some(Commands::Whoami(args)) => whoami::run(args, global).await,
            Some(Commands::Sources(args)) => sources::run(args, global).await,
            Some(Commands::Schema(args)) => schema::run(args, global).await,
            Some(Commands::Doctor(args)) => doctor::run(args, global).await,
            Some(Commands::Config(args)) => config::run(args).await,
            Some(Commands::Skills(args)) => skills::run(args).await,
            Some(Commands::Completions(args)) => completions::run(args).await,
            None => {
                let mut cmd = Cli::command();
                cmd.print_help()?;
                println!();
                Ok(())
            }
        }
    }
}
