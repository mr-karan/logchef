use anyhow::{Context, Result};
use chrono::Utc;
use clap::Args;
use logchef_core::Config;
use logchef_core::api::{Column, FieldValueInfo, FieldValuesQuery};
use logchef_core::cache::Cache;

use crate::cli::GlobalArgs;
use crate::commands::{parse_lookback, resolve_source, resolve_team};
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # List all fields in a source
  logchef fields -t platform -S app-logs

  # Top 20 observed values for `service` in the last hour
  logchef fields service --since 1h

  # Machine-readable value counts
  logchef fields status --limit 50 --output jsonl")]
pub struct FieldsArgs {
    /// Field to enumerate values for. Omit to list the source's fields.
    field: Option<String>,

    /// Team ID or name
    #[arg(long, short = 't')]
    team: Option<String>,

    /// Source ID or name
    #[arg(long, short = 'S')]
    source: Option<String>,

    /// Relative lookback window for value enumeration (e.g. 15m, 1h, 24h)
    #[arg(long, short = 's')]
    since: Option<String>,

    /// Max number of values to return (when a field is given)
    #[arg(long, default_value = "20")]
    limit: u32,

    /// Output format
    #[arg(long, default_value = "text")]
    output: OutputFormat,
}

#[derive(Clone, Debug, clap::ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
    Table,
}

pub async fn run(args: FieldsArgs, global: GlobalArgs) -> Result<()> {
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

    let columns = client
        .get_schema(team_id, source_id)
        .await
        .context("Failed to get schema")?;

    match &args.field {
        None => list_fields(&columns, &args.output),
        Some(field) => enumerate_values(client, team_id, source_id, field, &columns, &args, ctx)
            .await
            .with_context(|| format!("Failed to get values for field '{}'", field)),
    }
}

fn list_fields(columns: &[Column], output: &OutputFormat) -> Result<()> {
    if columns.is_empty() {
        println!("No fields found for this source.");
        return Ok(());
    }

    match output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&columns)?);
        }
        OutputFormat::Jsonl => {
            for col in columns {
                println!("{}", serde_json::to_string(col)?);
            }
        }
        OutputFormat::Text | OutputFormat::Table => {
            println!("{:<30} TYPE", "NAME");
            println!("{}", "-".repeat(60));
            for col in columns {
                println!("{:<30} {}", col.name, col.column_type);
            }
            println!("\n{} fields", columns.len());
        }
    }

    Ok(())
}

async fn enumerate_values(
    client: &logchef_core::api::Client,
    team_id: i64,
    source_id: i64,
    field: &str,
    columns: &[Column],
    args: &FieldsArgs,
    ctx: &logchef_core::config::Context,
) -> Result<()> {
    let field_type = columns
        .iter()
        .find(|c| c.name == field)
        .map(|c| c.column_type.clone())
        .ok_or_else(|| {
            anyhow::anyhow!(
                "Field '{}' not found in source schema. List fields with 'logchef fields'.",
                field
            )
        })?;

    // The server enumerates values over an absolute window; build it from the
    // lookback and send RFC3339 UTC instants (which the endpoint requires).
    let since = args
        .since
        .clone()
        .unwrap_or_else(|| ctx.defaults.since.clone());
    let end = Utc::now();
    let start = end - parse_lookback(&since)?;

    let result = client
        .get_field_values(
            team_id,
            source_id,
            &FieldValuesQuery {
                field_name: field,
                field_type: &field_type,
                start: &start.to_rfc3339(),
                end: &end.to_rfc3339(),
                timezone: "UTC",
                limit: args.limit,
            },
        )
        .await?;

    match args.output {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&result)?);
        }
        OutputFormat::Jsonl => {
            for value in &result.values {
                println!("{}", serde_json::to_string(value)?);
            }
        }
        OutputFormat::Text => {
            if result.values.is_empty() {
                println!("No values observed for '{}' in the last {}.", field, since);
                return Ok(());
            }
            for FieldValueInfo { value, count } in &result.values {
                println!("{:>12}  {}", ui::thousands(*count), value);
            }
            println!(
                "\n{} values shown | {} distinct",
                result.values.len(),
                ui::thousands(result.total_distinct)
            );
        }
        OutputFormat::Table => {
            if result.values.is_empty() {
                println!("No values observed for '{}' in the last {}.", field, since);
                return Ok(());
            }
            println!("{:>12}  VALUE", "COUNT");
            println!("{}", "-".repeat(60));
            for FieldValueInfo { value, count } in &result.values {
                println!("{:>12}  {}", ui::thousands(*count), value);
            }
        }
    }

    Ok(())
}
