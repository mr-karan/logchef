use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use logchef_core::Config;
use logchef_core::config::ContextAuth;
use logchef_core::timerange::{parse_timezone, resolve_timezone};

#[derive(Args)]
pub struct ConfigArgs {
    #[command(subcommand)]
    command: ConfigCommands,
}

#[derive(Subcommand)]
enum ConfigCommands {
    #[command(about = "List all contexts")]
    List,

    #[command(about = "Switch to a context")]
    Use { name: String },

    #[command(about = "Rename a context")]
    Rename { old_name: String, new_name: String },

    #[command(about = "Delete a context")]
    Delete { name: String },

    #[command(about = "Show current context configuration")]
    Show,

    #[command(about = "Show configuration file path")]
    Path,

    #[command(about = "Set a configuration value in current context")]
    Set { key: String, value: String },
}

pub async fn run(args: ConfigArgs) -> Result<()> {
    match args.command {
        ConfigCommands::List => list_contexts(),
        ConfigCommands::Use { name } => use_context(&name),
        ConfigCommands::Rename { old_name, new_name } => rename_context(&old_name, &new_name),
        ConfigCommands::Delete { name } => delete_context(&name),
        ConfigCommands::Show => show_config(),
        ConfigCommands::Path => show_path(),
        ConfigCommands::Set { key, value } => set_value(&key, &value),
    }
}

fn list_contexts() -> Result<()> {
    let config = Config::load().context("Failed to load config")?;

    if config.is_empty() {
        println!("No contexts configured. Run 'logchef auth --server <url>' to set up.");
        return Ok(());
    }

    println!("{:<3} {:<20} {:<40} AUTH", "", "CONTEXT", "SERVER");

    let mut names: Vec<_> = config.context_names();
    names.sort();

    for name in names {
        let Some(ctx) = config.get_context(name) else {
            continue;
        };
        let current = if config.current_context_name() == Some(name) {
            "*"
        } else {
            ""
        };
        let auth_status = if ctx.is_authenticated() { "yes" } else { "no" };

        let server_display = if ctx.server_url.len() > 38 {
            format!("{}...", &ctx.server_url[..35])
        } else {
            ctx.server_url.clone()
        };

        println!(
            "{:<3} {:<20} {:<40} {}",
            current, name, server_display, auth_status
        );
    }

    Ok(())
}

fn use_context(name: &str) -> Result<()> {
    Config::update(|config| config.use_context(name))?;
    println!("Switched to context '{}'.", name);
    Ok(())
}

fn rename_context(old_name: &str, new_name: &str) -> Result<()> {
    Config::update(|config| config.rename_context(old_name, new_name))?;
    println!("Renamed '{}' to '{}'.", old_name, new_name);
    Ok(())
}

fn delete_context(name: &str) -> Result<()> {
    let current = Config::update(|config| {
        config.delete_context(name)?;
        Ok(config.current_context_name().map(str::to_string))
    })?;
    println!("Deleted context '{}'.", name);

    if let Some(current) = current {
        println!("Current context is now '{}'.", current);
    }

    Ok(())
}

fn show_config() -> Result<()> {
    let config = Config::load().context("Failed to load config")?;

    println!("CLI preferences:");
    println!("  banner:        {}", config.show_banner);
    println!("  check-updates: {}", config.check_updates);
    println!();

    let ctx_name = match config.current_context_name() {
        Some(name) => name,
        None => {
            println!("No current context. Run 'logchef auth' to set up.");
            return Ok(());
        }
    };

    let ctx = match config.current_context() {
        Some(ctx) => ctx,
        None => {
            println!(
                "Current context '{}' not found in config. Run 'logchef auth' to set up.",
                ctx_name
            );
            return Ok(());
        }
    };

    println!("Context: {}", ctx_name);
    println!("Server:  {}", ctx.server_url);
    println!("Timeout: {}s", ctx.timeout_secs);

    match &ctx.auth {
        Some(ContextAuth::Pat { token, expires_at }) => {
            let masked = if token.len() > 14 {
                format!("{}****...", &token[..10])
            } else {
                "****".to_string()
            };
            println!("Token:   {}", masked);
            if let Some(expires) = expires_at {
                println!("Expires: {}", expires);
            }
        }
        Some(ContextAuth::OAuth(credential)) => {
            println!("Auth:    Logchef OAuth ({})", credential.issuer);
            println!("Scopes:  {}", credential.scopes.join(" "));
        }
        None => println!("Token:   (not set)"),
    }

    println!("\nDefaults:");
    if let Some(ref team) = ctx.defaults.team {
        println!("  team:     {}", team);
    }
    if let Some(ref source) = ctx.defaults.source {
        println!("  source:   {}", source);
    }
    println!("  limit:    {}", ctx.defaults.limit);
    println!("  since:    {}", ctx.defaults.since);
    let effective_tz = resolve_timezone(ctx.defaults.timezone.as_deref());
    match &ctx.defaults.timezone {
        Some(tz) if parse_timezone(tz).is_some() => println!("  timezone: {}", tz),
        Some(tz) => println!(
            "  timezone: '{}' is not a valid IANA zone, using detected system zone '{}'",
            tz, effective_tz
        ),
        None => println!(
            "  timezone: (not set, using detected system zone '{}')",
            effective_tz
        ),
    }

    Ok(())
}

fn show_path() -> Result<()> {
    let path = Config::config_path()?;
    println!("{}", path.display());
    Ok(())
}

fn set_value(key: &str, value: &str) -> Result<()> {
    let shown = Config::update(|config| {
        apply_setting(config, key, value).map_err(|e| logchef_core::Error::other(format!("{e:#}")))
    })?;
    println!("Set {} = {}", key, shown);
    Ok(())
}

/// Applies one `config set` and returns the value to print.
fn apply_setting(config: &mut Config, key: &str, value: &str) -> Result<String> {
    // Global (non-context) CLI preferences. Handled before requiring a context
    // so they can be toggled even without an authenticated context.
    match key {
        "banner" | "show_banner" => {
            config.show_banner = parse_bool(value)?;
            return Ok(config.show_banner.to_string());
        }
        "check-updates" | "check_updates" => {
            config.check_updates = parse_bool(value)?;
            return Ok(config.check_updates.to_string());
        }
        _ => {}
    }

    let ctx = config
        .current_context_mut()
        .ok_or_else(|| anyhow::anyhow!("No current context. Run 'logchef auth' first."))?;

    match key {
        "timeout" | "timeout_secs" => {
            ctx.timeout_secs = value.parse().context("Invalid timeout value")?;
        }
        "team" | "defaults.team" => {
            ctx.defaults.team = Some(value.to_string());
        }
        "source" | "defaults.source" => {
            ctx.defaults.source = Some(value.to_string());
        }
        "limit" | "defaults.limit" => {
            ctx.defaults.limit = value.parse().context("Invalid limit value")?;
        }
        "since" | "defaults.since" => {
            ctx.defaults.since = value.to_string();
        }
        "timezone" | "defaults.timezone" => {
            let tz = parse_timezone(value).ok_or_else(|| {
                anyhow::anyhow!(
                    "Invalid timezone: '{}'. Use an IANA zone name such as Asia/Kolkata or UTC, not an abbreviation like IST.",
                    value
                )
            })?;
            ctx.defaults.timezone = Some(tz.to_string());
        }
        _ => anyhow::bail!(
            "Unknown key: '{}'. Valid keys: team, source, limit, since, timezone, timeout, banner, check-updates",
            key
        ),
    }

    Ok(value.to_string())
}

fn parse_bool(value: &str) -> Result<bool> {
    match value.trim().to_ascii_lowercase().as_str() {
        "true" | "1" | "yes" | "on" => Ok(true),
        "false" | "0" | "no" | "off" => Ok(false),
        _ => anyhow::bail!("Invalid boolean '{}'. Use true or false.", value),
    }
}
