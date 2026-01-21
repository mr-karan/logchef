use anyhow::{Context, Result};
use clap::Args;
use inquire::Text;
use logchef_core::Config;
use logchef_core::api::Client;
use logchef_core::auth::AuthFlow;
use logchef_core::config::{Context as CtxConfig, context_name_from_url};

use crate::cli::GlobalArgs;

#[derive(Args)]
pub struct AuthArgs {
    #[arg(long, short)]
    logout: bool,

    #[arg(long)]
    status: bool,
}

pub async fn run(args: AuthArgs, global: GlobalArgs) -> Result<()> {
    let mut config = Config::load().context("Failed to load config")?;

    if args.logout {
        return logout(&mut config, &global);
    }

    if args.status {
        return status(&config, &global).await;
    }

    login(&mut config, global).await
}

fn logout(config: &mut Config, global: &GlobalArgs) -> Result<()> {
    let ctx_name = resolve_context_name(config, global)?;

    if let Some(ctx) = config.get_context_mut(&ctx_name) {
        ctx.token = None;
        ctx.token_expires_at = None;
        config.save().context("Failed to save config")?;
        println!("Logged out from context '{}'.", ctx_name);
    } else {
        println!("Context '{}' not found.", ctx_name);
    }

    Ok(())
}

async fn status(config: &Config, global: &GlobalArgs) -> Result<()> {
    let ctx_name = match resolve_context_name(config, global) {
        Ok(name) => name,
        Err(_) => {
            println!("No contexts configured. Run 'logchef auth --server <url>' to set up.");
            return Ok(());
        }
    };

    let ctx = config
        .get_context(&ctx_name)
        .ok_or_else(|| anyhow::anyhow!("Context '{}' not found", ctx_name))?;

    println!("Context: {}", ctx_name);
    println!("Server:  {}", ctx.server_url);

    if !ctx.is_authenticated() {
        println!("Status:  Not authenticated");
        return Ok(());
    }

    let client = Client::from_context(ctx)?;
    match client.get_current_user().await {
        Ok(user) => {
            println!("User:    {}", user.email);
            if let Some(name) = &user.full_name {
                println!("Name:    {}", name);
            }
            println!("Role:    {}", user.role);
        }
        Err(e) => {
            println!("Status:  Token may be invalid or expired ({})", e);
        }
    }

    Ok(())
}

async fn login(config: &mut Config, global: GlobalArgs) -> Result<()> {
    let server_url = get_server_url(config, &global)?;
    let server_url = server_url.trim_end_matches('/').to_string();

    println!("Connecting to {}...", server_url);

    let client = Client::new(&server_url, 30)?;
    let meta = client
        .get_meta()
        .await
        .context("Failed to connect to server")?;

    println!("Connected to LogChef {}", meta.data.version);

    if !meta.data.oidc_enabled() {
        anyhow::bail!(
            "CLI authentication not configured on this server. Ask your admin to set oidc.cli_client_id in server config."
        );
    }

    let oidc_issuer = meta
        .data
        .oidc_issuer
        .ok_or_else(|| anyhow::anyhow!("Server did not provide OIDC issuer URL"))?;

    let cli_client_id = meta
        .data
        .cli_client_id
        .ok_or_else(|| anyhow::anyhow!("Server did not provide CLI client ID"))?;

    let auth_flow = AuthFlow::new(server_url.clone(), oidc_issuer, cli_client_id);
    let result = auth_flow.run().await?;

    let ctx_name = global
        .context
        .clone()
        .or_else(|| {
            config
                .find_context_by_url(&server_url)
                .map(|(n, _)| n.to_string())
        })
        .unwrap_or_else(|| context_name_from_url(&server_url));

    let ctx = CtxConfig {
        server_url: server_url.clone(),
        timeout_secs: 30,
        token: Some(result.token),
        token_expires_at: result.expires_at,
        defaults: Default::default(),
    };

    config.add_or_update_context(ctx_name.clone(), ctx);
    config.save().context("Failed to save config")?;

    if let Some(email) = result.user_email {
        println!("\nAuthenticated as {} (context: '{}')", email, ctx_name);
    } else {
        println!("\nAuthenticated! (context: '{}')", ctx_name);
    }

    Ok(())
}

fn resolve_context_name(config: &Config, global: &GlobalArgs) -> Result<String> {
    if let Some(name) = &global.context {
        return Ok(name.clone());
    }

    if let Some(url) = &global.server {
        if let Some((name, _)) = config.find_context_by_url(url) {
            return Ok(name.to_string());
        }
        return Ok(context_name_from_url(url));
    }

    config
        .current_context_name()
        .map(|s| s.to_string())
        .ok_or_else(|| anyhow::anyhow!("No current context set"))
}

fn get_server_url(config: &Config, global: &GlobalArgs) -> Result<String> {
    // Priority 1: Use --server flag
    if let Some(url) = &global.server {
        return Ok(url.clone());
    }

    // Priority 2: Use --context flag
    if let Some(ctx_name) = &global.context {
        if let Some(ctx) = config.get_context(ctx_name) {
            return Ok(ctx.server_url.clone());
        }
        anyhow::bail!("Context '{}' not found", ctx_name);
    }

    // Priority 3: Interactive prompt with optional default
    let default = config.current_context().map(|ctx| ctx.server_url.clone());

    let mut prompt = Text::new("LogChef server URL:");
    if let Some(ref default_url) = default {
        prompt = prompt
            .with_default(default_url)
            .with_help_message("Press Enter for default");
    }

    let input = prompt.prompt().context("Failed to read server URL")?;

    if input.trim().is_empty() {
        anyhow::bail!("Server URL is required");
    }

    Ok(input.trim().to_string())
}
