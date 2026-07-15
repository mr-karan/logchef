use anyhow::{Context, Result};
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{TranslateRequest, ValidateRequest};
use logchef_core::cache::Cache;
use serde::Serialize;

use crate::cli::GlobalArgs;
use crate::commands::{resolve_source, resolve_team};
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # See the ClickHouse SQL / LogsQL a LogchefQL filter compiles to
  logchef explain 'level=\"error\" and service=\"api\"' -t platform -S app-logs

  # Validate a filter's syntax in a script (exit stays 0; check the JSON)
  logchef explain 'status>=500' --output json | jq '.valid'")]
pub struct ExplainArgs {
    /// LogchefQL query to translate (e.g. `level="error" and service="api"`)
    query: String,

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
}

#[derive(Serialize)]
struct JsonOutput<'a> {
    query: &'a str,
    valid: bool,
    generated_query: &'a str,
    generated_query_language: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<&'a logchef_core::api::QueryParseError>,
}

pub async fn run(args: ExplainArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let team = args.team.or_else(|| ctx.defaults.team_with_env());
    let source = args.source.or_else(|| ctx.defaults.source_with_env());

    let team_id = resolve_team(client, &mut cache, team).await?;
    let source_id = resolve_source(client, &mut cache, team_id, source).await?;

    // Translate without a time range: this reports the engine-agnostic
    // translation (filter conditions for ClickHouse, native LogsQL for
    // VictoriaLogs) without executing anything.
    let translate = client
        .translate_logchefql(
            team_id,
            source_id,
            &TranslateRequest {
                query: args.query.clone(),
                start_time: None,
                end_time: None,
                timezone: None,
                limit: None,
            },
        )
        .await
        .context("Failed to translate query")?;

    // The validate endpoint is the authoritative syntax check.
    let validate = client
        .validate_logchefql(
            team_id,
            source_id,
            &ValidateRequest {
                query: args.query.clone(),
            },
        )
        .await
        .context("Failed to validate query")?;

    let valid = translate.valid && validate.valid;
    let error = validate.error.as_ref().or(translate.error.as_ref());

    match args.output {
        OutputFormat::Json => {
            let output = JsonOutput {
                query: &args.query,
                valid,
                generated_query: translate.generated_query(),
                generated_query_language: translate.generated_query_language.as_deref(),
                error,
            };
            println!("{}", serde_json::to_string_pretty(&output)?);
        }
        OutputFormat::Jsonl => {
            let output = JsonOutput {
                query: &args.query,
                valid,
                generated_query: translate.generated_query(),
                generated_query_language: translate.generated_query_language.as_deref(),
                error,
            };
            println!("{}", serde_json::to_string(&output)?);
        }
        OutputFormat::Text => {
            if valid {
                println!("Valid: yes");
            } else {
                println!("Valid: no");
                if let Some(err) = error {
                    print!("Error: {}", err.message);
                    if let Some(pos) = &err.position {
                        print!(" (line {}, column {})", pos.line, pos.column);
                    }
                    println!();
                }
                return Ok(());
            }

            let generated = translate.generated_query();
            println!("\nGenerated {}:", translate.language_label());
            if generated.trim().is_empty() {
                println!("  (no filter - matches all logs)");
            } else {
                let rendered = ui::highlight_query(
                    generated,
                    translate.generated_query_language.as_deref(),
                    ui::human(global.quiet),
                );
                println!("  {}", rendered);
            }
        }
    }

    Ok(())
}
