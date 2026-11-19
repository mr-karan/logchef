use anyhow::{Context, Result, bail};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::api::{Client, SavedView, SavedViewList};
use logchef_core::cache::Cache;

use crate::cli::GlobalArgs;
use crate::commands::{resolve_source, resolve_team};
use crate::session;

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # List the saved Explorer views of a source
  logchef views list -t platform -S app-logs

  # Print the list response as JSON
  logchef views list -t 2 -S 2 --output json

  # Open the Explorer with a view
  logchef open -t 2 -S 2 --view 'Checkout errors'")]
pub struct ViewsArgs {
    #[command(subcommand)]
    command: ViewsCommand,
}

#[derive(Subcommand)]
enum ViewsCommand {
    /// List the views you can see for a source: your personal views, the
    /// team's views and the source's views.
    List(ListArgs),
}

#[derive(Args)]
struct ListArgs {
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
}

/// Definition keys in the order the list shows them.
const PARTS: [&str; 5] = ["columns", "sort", "display_mode", "filter", "time_range"];

pub async fn run(args: ViewsArgs, global: GlobalArgs) -> Result<()> {
    match args.command {
        ViewsCommand::List(args) => list(args, global).await,
    }
}

async fn list(args: ListArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;
    let s = session::authed(&config, &global)?;
    let (client, ctx) = (&s.client, &s.ctx);

    let mut cache = Cache::new(&ctx.server_url);
    let team = args.team.or_else(|| ctx.defaults.team_with_env());
    let source = args.source.or_else(|| ctx.defaults.source_with_env());
    let team_id = resolve_team(client, &mut cache, team).await?;
    let source_id = resolve_source(client, &mut cache, team_id, source).await?;

    let list = client
        .list_saved_views(team_id, source_id)
        .await
        .context("Failed to list saved views")?;

    match args.output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&list)?),
        OutputFormat::Text => print!("{}", render_text(&list)),
    }
    Ok(())
}

fn render_text(list: &SavedViewList) -> String {
    if list.views.is_empty() {
        return "No saved views found.\n".to_string();
    }
    let mut out = format!(
        "{:<6} {:<30} {:<9} {:<8} SETS\n{}\n",
        "ID",
        "NAME",
        "SCOPE",
        "DEFAULT",
        "-".repeat(80)
    );
    for view in &list.views {
        // "yes" is the view the default rules pick; "scope" is the default of
        // its own scope that a narrower default outranks.
        let default = if list.default_view_id == Some(view.id) {
            "yes"
        } else if view.is_default {
            "scope"
        } else {
            ""
        };
        out.push_str(&format!(
            "{:<6} {:<30} {:<9} {:<8} {}\n",
            view.id,
            view.name,
            view.scope,
            default,
            parts(view).join(", ")
        ));
    }
    out
}

fn parts(view: &SavedView) -> Vec<&'static str> {
    PARTS
        .into_iter()
        .filter(|part| view.definition.contains_key(*part))
        .collect()
}

/// Resolves `--view` to an id. A number is an id. Any other value is a name,
/// looked up in the views the caller can see for the team and source.
pub(crate) async fn resolve_view(
    client: &Client,
    team_id: i64,
    source_id: i64,
    view: &str,
) -> Result<i64> {
    if let Ok(id) = view.parse::<i64>() {
        return Ok(id);
    }
    let list = client
        .list_saved_views(team_id, source_id)
        .await
        .context("Failed to list saved views")?;
    find_by_name(&list, view)
}

/// Names are unique within a scope, so a name can match one view in each of
/// the three scopes. More than one match is ambiguous: pass the id instead.
fn find_by_name(list: &SavedViewList, name: &str) -> Result<i64> {
    let matches: Vec<&SavedView> = list.views.iter().filter(|v| v.name == name).collect();
    match matches.as_slice() {
        [] => bail!(
            "No view is named '{name}' for this team and source. List them with 'logchef views list'."
        ),
        [view] => Ok(view.id),
        _ => {
            let found = matches
                .iter()
                .map(|v| format!("  {} ({})", v.id, v.scope))
                .collect::<Vec<_>>()
                .join("\n");
            bail!("View '{name}' matches more than one view. Pass the id with --view:\n{found}")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn list() -> SavedViewList {
        serde_json::from_value(serde_json::json!({
            "views": [
                {"id": 3, "name": "Errors", "scope": "personal", "is_default": true,
                 "definition": {"version": 1, "columns": [{"name": "msg"}], "filter": "level=\"error\""},
                 "revision": 1},
                {"id": 7, "name": "Errors", "scope": "team", "is_default": false,
                 "definition": {"version": 1, "sort": [{"column": "ts", "desc": true}]}},
                {"id": 9, "name": "Wide", "scope": "source", "is_default": true,
                 "definition": {"version": 1, "display_mode": "table", "time_range": "1h"}}
            ],
            "default_view_id": 3
        }))
        .unwrap()
    }

    #[test]
    fn a_unique_name_resolves_to_its_id() {
        assert_eq!(find_by_name(&list(), "Wide").unwrap(), 9);
    }

    #[test]
    fn a_name_in_two_scopes_lists_both() {
        let err = find_by_name(&list(), "Errors").unwrap_err().to_string();
        assert!(err.contains("  3 (personal)"), "{err}");
        assert!(err.contains("  7 (team)"), "{err}");
    }

    #[test]
    fn an_unknown_name_names_the_list_command() {
        let err = find_by_name(&list(), "errors").unwrap_err().to_string();
        assert!(err.contains("logchef views list"), "{err}");
    }

    #[test]
    fn text_marks_defaults_and_parts() {
        let text = render_text(&list());
        let rows: Vec<Vec<&str>> = text
            .lines()
            .skip(2)
            .map(|l| l.split_whitespace().collect())
            .collect();
        assert_eq!(
            rows,
            vec![
                vec!["3", "Errors", "personal", "yes", "columns,", "filter"],
                vec!["7", "Errors", "team", "sort"],
                vec![
                    "9",
                    "Wide",
                    "source",
                    "scope",
                    "display_mode,",
                    "time_range"
                ],
            ]
        );
    }

    #[test]
    fn json_keeps_every_field() {
        let value = serde_json::to_value(list()).unwrap();
        assert_eq!(value["views"][0]["revision"], 1);
        assert_eq!(value["default_view_id"], 3);
    }
}
