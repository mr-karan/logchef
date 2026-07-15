pub mod auth;
pub mod collections;
pub mod completions;
pub mod config;
pub mod doctor;
pub mod explain;
pub mod fields;
pub mod find;
pub mod histogram;
pub mod open;
pub mod query;
pub mod saved_queries;
pub mod schema;
pub mod skills;
pub mod sources;
pub mod sql;
pub mod tail;
pub mod teams;
pub mod whoami;

use anyhow::{Context, Result};
use chrono::Duration;
use logchef_core::api::Client;
use logchef_core::cache::{Cache, Identifier, parse_identifier};

/// Parses a relative lookback string (e.g. `15m`, `1h`, `24h`, `7d`, `2w`)
/// into a `chrono::Duration`. A bare number is treated as minutes. Shared by
/// the commands that build a `now - lookback` window.
pub(crate) fn parse_lookback(s: &str) -> Result<Duration> {
    let s = s.trim();
    if s.is_empty() {
        return Ok(Duration::minutes(15));
    }

    let (num, unit) = if let Some(rest) = s.strip_suffix('m') {
        (rest, "m")
    } else if let Some(rest) = s.strip_suffix('h') {
        (rest, "h")
    } else if let Some(rest) = s.strip_suffix('d') {
        (rest, "d")
    } else if let Some(rest) = s.strip_suffix('w') {
        (rest, "w")
    } else {
        (s, "m")
    };

    let num: i64 = num.parse().context("Invalid duration number")?;

    match unit {
        "h" => Ok(Duration::hours(num)),
        "d" => Ok(Duration::days(num)),
        "w" => Ok(Duration::weeks(num)),
        _ => Ok(Duration::minutes(num)),
    }
}

/// Resolves a team identifier (ID or name) to a team ID, populating the cache
/// on a name lookup. Shared by the non-interactive commands.
pub(crate) async fn resolve_team(
    client: &Client,
    cache: &mut Cache,
    team: Option<String>,
) -> Result<i64> {
    let team = team.ok_or_else(|| {
        anyhow::anyhow!(
            "Team not specified. Use --team or set defaults.team. List teams with 'logchef teams'."
        )
    })?;

    match parse_identifier(&team) {
        Identifier::Id(id) => Ok(id),
        Identifier::Name(name) => {
            if let Some(id) = cache.get_team_id(&name) {
                return Ok(id);
            }
            let teams = client.list_teams().await.context("Failed to list teams")?;
            cache.set_teams(
                &teams
                    .iter()
                    .map(|team| (team.name.clone(), team.id))
                    .collect::<Vec<_>>(),
            );
            teams
                .iter()
                .find(|team| team.name.eq_ignore_ascii_case(&name))
                .map(|team| team.id)
                .ok_or_else(|| anyhow::anyhow!("Team '{}' not found", name))
        }
    }
}

/// Resolves a source identifier (ID, name, or target ref) to a source ID
/// within a team, populating the cache on a name lookup. Shared by the
/// non-interactive commands.
pub(crate) async fn resolve_source(
    client: &Client,
    cache: &mut Cache,
    team_id: i64,
    source: Option<String>,
) -> Result<i64> {
    let source = source.ok_or_else(|| {
        anyhow::anyhow!(
            "Source not specified. Use --source or set defaults.source. List sources with 'logchef sources --team <team>'."
        )
    })?;

    match parse_identifier(&source) {
        Identifier::Id(id) => Ok(id),
        Identifier::Name(name) => {
            if let Some(id) = cache.get_source_id(team_id, &name) {
                return Ok(id);
            }
            let sources = client
                .list_sources(team_id)
                .await
                .context("Failed to list sources")?;
            let mut cache_entries = sources
                .iter()
                .map(|source| (source.name.clone(), source.id))
                .collect::<Vec<_>>();
            for source in &sources {
                if let Some(target_ref) = source.target_ref() {
                    cache_entries.push((target_ref, source.id));
                }
            }
            cache.set_sources(team_id, &cache_entries);
            sources
                .iter()
                .find(|source| source.name.eq_ignore_ascii_case(&name))
                .or_else(|| {
                    sources.iter().find(|source| {
                        source
                            .target_ref()
                            .map(|target| target.eq_ignore_ascii_case(&name))
                            .unwrap_or(false)
                    })
                })
                .map(|source| source.id)
                .ok_or_else(|| anyhow::anyhow!("Source '{}' not found", name))
        }
    }
}
