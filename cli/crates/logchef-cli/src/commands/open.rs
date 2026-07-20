use anyhow::{Context, Result, bail};
use clap::Args;
use logchef_core::Config;
use logchef_core::cache::Cache;
use logchef_core::timerange::wall_clock_to_epoch_millis;
use url::Url;

use crate::cli::GlobalArgs;
use crate::commands::{resolve_source, resolve_team};
use crate::session;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # Open a LogchefQL query in the web explorer (relative window)
  logchef open 'status>=500' -t 2 -S 2 --since 1h

  # Open an absolute range investigation (wall-clock in your effective timezone)
  logchef open 'recipient~\"alice@example.com\"' -t 2 -S 2 \\
    --from '2026-06-30 00:00:00' --to '2026-06-30 23:59:59'

  # Open a raw native (ClickHouse SQL / VictoriaLogs LogsQL) query
  logchef open 'SELECT * FROM logs.app WHERE level=' --sql -t 2 -S 2

  # Just print the URL (don't launch a browser)
  logchef open 'level=\"error\"' -t 2 -S 2 --since 15m --print")]
pub struct OpenArgs {
    /// Query to preload in the explorer (LogchefQL by default, or native with --sql)
    query: Option<String>,

    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Treat the query as raw native (ClickHouse SQL / VictoriaLogs LogsQL)
    #[arg(long)]
    sql: bool,

    /// Relative time range to preselect (e.g. 15m, 1h, 24h). Ignored if --from/--to are given.
    #[arg(long, short = 's')]
    since: Option<String>,

    /// Absolute start, 'YYYY-MM-DD HH:MM:SS' in the effective timezone. Requires --to.
    #[arg(long)]
    from: Option<String>,

    /// Absolute end, 'YYYY-MM-DD HH:MM:SS' in the effective timezone. Requires --from.
    #[arg(long)]
    to: Option<String>,

    /// Row limit to preselect
    #[arg(long, short = 'l')]
    limit: Option<u64>,

    /// Print the URL instead of opening a browser
    #[arg(long)]
    print: bool,
}

pub async fn run(args: OpenArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let team = args.team.clone().or_else(|| ctx.defaults.team_with_env());
    let source = args
        .source
        .clone()
        .or_else(|| ctx.defaults.source_with_env());

    let team_id = resolve_team(client, &mut cache, team).await?;
    let source_id = resolve_source(client, &mut cache, team_id, source).await?;

    // The web explorer lives at /logs/explore and hydrates its state from query
    // params (see frontend useUrlState.ts / stores/explore.ts):
    //   team/source  numeric IDs
    //   q            LogchefQL          | sql   raw native query
    //   t            relative time      | start/end  absolute epoch-MILLISECONDS
    //   limit        row cap
    // Relative `t` wins over absolute `start`/`end` in the UI, so only one is set.
    let mut pairs: Vec<(&str, String)> = vec![
        ("team", team_id.to_string()),
        ("source", source_id.to_string()),
    ];

    if let Some(query) = args
        .query
        .as_deref()
        .map(str::trim)
        .filter(|q| !q.is_empty())
    {
        pairs.push((if args.sql { "sql" } else { "q" }, query.to_string()));
    }

    match (args.from.as_deref(), args.to.as_deref()) {
        (Some(from), Some(to)) => {
            let tz = ctx.defaults.timezone.as_deref();
            let start = wall_clock_to_epoch_millis(from, tz).with_context(|| {
                format!("Invalid --from '{from}' (expected 'YYYY-MM-DD HH:MM:SS')")
            })?;
            let end = wall_clock_to_epoch_millis(to, tz)
                .with_context(|| format!("Invalid --to '{to}' (expected 'YYYY-MM-DD HH:MM:SS')"))?;
            pairs.push(("start", start.to_string()));
            pairs.push(("end", end.to_string()));
        }
        (Some(_), None) | (None, Some(_)) => bail!("--from and --to must be provided together"),
        (None, None) => {
            if let Some(since) = args
                .since
                .as_deref()
                .map(str::trim)
                .filter(|s| !s.is_empty())
            {
                pairs.push(("t", since.to_string()));
            }
        }
    }

    if let Some(limit) = args.limit {
        pairs.push(("limit", limit.to_string()));
    }

    let mut url = Url::parse(&ctx.server_url).context("Invalid server URL")?;
    url.set_path("/logs/explore");
    {
        let mut qp = url.query_pairs_mut();
        for (key, value) in &pairs {
            qp.append_pair(key, value);
        }
    }
    let url = url.to_string();

    if args.print {
        println!("{}", url);
        return Ok(());
    }

    println!("Opening {}", url);
    if let Err(e) = open::that(&url) {
        eprintln!("Failed to open browser automatically: {}", e);
        eprintln!("Open this URL manually:\n  {}", url);
    }

    Ok(())
}
