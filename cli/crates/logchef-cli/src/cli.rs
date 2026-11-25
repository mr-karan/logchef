use anyhow::Result;
use clap::{CommandFactory, Parser, Subcommand};
use tracing_subscriber::EnvFilter;

use crate::commands::{
    advisor, agent, alerts, audit, auth, collections, completions, config, destinations, doctor,
    explain, fields, find, histogram, history, open, query, reports, saved_queries, schema,
    silences, skills, sources, sql, tail, teams, views, whoami,
};

const LONG_ABOUT: &str = "\
Logchef CLI — search and investigate logs from your terminal.

Logchef sources are backed by ClickHouse, VictoriaLogs or Grafana Loki. Pick a
command by what you want to do:

  query      Search with LogchefQL — the portable filter language that works on
             every engine. Start here. e.g. `level=\"error\" and service=\"api\"`.
             Aggregate with a pipe: `level=\"error\" | count() by service`.
  sql        Run a raw native query when LogchefQL isn't enough: ClickHouse SQL
             for ClickHouse sources, LogsQL for VictoriaLogs sources, LogQL for
             Loki sources.
  explain    Show the ClickHouse SQL / LogsQL / LogQL a LogchefQL query compiles
             to, without running it. Great for learning and debugging filters.
  histogram  Plot log counts over time (trends, spikes, error rates).
  fields     Discover a source's fields, or the observed values of one field.
  find       Locate which source holds a given service, host, or message.
  tail       Follow matching logs live.
  alerts     List alert rules, and test or backtest a rule before saving it.

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
        about = "Execute a raw native query (SQL for ClickHouse, LogsQL for VictoriaLogs, LogQL for Loki)"
    )]
    Sql(sql::SqlArgs),

    #[command(
        about = "Translate and validate a LogchefQL query without executing it (shows the generated ClickHouse SQL, LogsQL or LogQL)"
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

    #[command(about = "List saved Explorer views")]
    Views(views::ViewsArgs),

    #[command(about = "List audit events (global admins, API token with audit:read)")]
    Audit(audit::AuditArgs),

    #[command(about = "List scheduled reports and their runs, and send one now")]
    Reports(reports::ReportsArgs),

    #[command(about = "List alert rules, and test or backtest a rule without saving it")]
    Alerts(alerts::AlertsArgs),

    #[command(about = "List the alert silences of a source")]
    Silences(silences::SilencesArgs),

    #[command(about = "List notification destinations")]
    Destinations(destinations::DestinationsArgs),

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

    #[command(
        about = "Show schema advice for a ClickHouse source (global admins, API token with sources:read)"
    )]
    Advisor(advisor::AdvisorArgs),

    #[command(about = "Show schema for a source")]
    Schema(schema::SchemaArgs),

    #[command(about = "Diagnose config, connectivity, auth, and defaults")]
    Doctor(doctor::DoctorArgs),

    #[command(about = "Manage CLI configuration")]
    Config(config::ConfigArgs),

    #[command(about = "Preview how to connect a coding agent to Logchef")]
    Agent(agent::AgentArgs),

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
            .with_writer(std::io::stderr)
            .init();

        let quiet = self.quiet;
        // The completions command emits a script, and the agent preview is
        // read-only (the notifier writes a cache file); skip the notice for both.
        let run_update_check = !matches!(
            self.command,
            Some(Commands::Completions(_) | Commands::Agent(_))
        );

        let global = GlobalArgs {
            context: self.context,
            server: self.server,
            token: self.token,
            quiet: self.quiet,
        };

        let result = match self.command {
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
            Some(Commands::Views(args)) => views::run(args, global).await,
            Some(Commands::Audit(args)) => audit::run(args, global).await,
            Some(Commands::Reports(args)) => reports::run(args, global).await,
            Some(Commands::Alerts(args)) => alerts::run(args, global).await,
            Some(Commands::Silences(args)) => silences::run(args, global).await,
            Some(Commands::Destinations(args)) => destinations::run(args, global).await,
            Some(Commands::Find(args)) => find::run(args, global).await,
            Some(Commands::Tail(args)) => tail::run(args, global).await,
            Some(Commands::Teams(args)) => teams::run(args, global).await,
            Some(Commands::Whoami(args)) => whoami::run(args, global).await,
            Some(Commands::Sources(args)) => sources::run(args, global).await,
            Some(Commands::Advisor(args)) => advisor::run(args, global).await,
            Some(Commands::Schema(args)) => schema::run(args, global).await,
            Some(Commands::Doctor(args)) => doctor::run(args, global).await,
            Some(Commands::Config(args)) => config::run(args).await,
            Some(Commands::Agent(args)) => agent::run(args, global).await,
            Some(Commands::Skills(args)) => skills::run(args).await,
            Some(Commands::Completions(args)) => completions::run(args).await,
            None => {
                let show_banner = logchef_core::Config::load()
                    .map(|c| c.show_banner)
                    .unwrap_or(true);
                crate::banner::print_banner(show_banner);
                let mut cmd = Cli::command();
                cmd.print_help()?;
                println!();
                Ok(())
            }
        };

        // Fire the update notifier only after a successful command, and never
        // for `completions`. It self-gates (config/TTY/quiet/env) and is
        // time-boxed, so it never delays or breaks the command.
        if result.is_ok() && run_update_check {
            crate::update::check_and_notify(quiet).await;
        }

        result
    }
}
