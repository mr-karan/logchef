use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use logchef_core::Config;

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
    let mut config = Config::load().context("Failed to load config")?;
    config.use_context(name)?;
    config.save().context("Failed to save config")?;
    println!("Switched to context '{}'.", name);
    Ok(())
}

fn rename_context(old_name: &str, new_name: &str) -> Result<()> {
    let mut config = Config::load().context("Failed to load config")?;
    config.rename_context(old_name, new_name)?;
    config.save().context("Failed to save config")?;
    println!("Renamed '{}' to '{}'.", old_name, new_name);
    Ok(())
}

fn delete_context(name: &str) -> Result<()> {
    let mut config = Config::load().context("Failed to load config")?;
    config.delete_context(name)?;
    config.save().context("Failed to save config")?;
    println!("Deleted context '{}'.", name);

    if let Some(current) = config.current_context_name() {
        println!("Current context is now '{}'.", current);
    }

    Ok(())
}

fn show_config() -> Result<()> {
    let config = Config::load().context("Failed to load config")?;

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

    if let Some(ref token) = ctx.token {
        let masked = if token.len() > 14 {
            format!("{}****...", &token[..10])
        } else {
            "****".to_string()
        };
        println!("Token:   {}", masked);
    } else {
        println!("Token:   (not set)");
    }

    if let Some(ref expires) = ctx.token_expires_at {
        println!("Expires: {}", expires);
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
    if let Some(ref tz) = ctx.defaults.timezone {
        println!("  timezone: {}", tz);
    }

    Ok(())
}

fn show_path() -> Result<()> {
    let path = Config::config_path()?;
    println!("{}", path.display());
    Ok(())
}

fn set_value(key: &str, value: &str) -> Result<()> {
    let mut config = Config::load().context("Failed to load config")?;

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
            ctx.defaults.timezone = Some(value.to_string());
        }
        _ => anyhow::bail!(
            "Unknown key: '{}'. Valid keys: team, source, limit, since, timezone, timeout",
            key
        ),
    }

    config.save().context("Failed to save config")?;
    println!("Set {} = {}", key, value);
    Ok(())
}
