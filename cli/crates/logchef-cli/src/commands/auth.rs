use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use inquire::Text;
use logchef_core::Config;
use logchef_core::api::Client;
use logchef_core::auth::{self, CallbackLimits, LoginOptions};
use logchef_core::config::{
    Context as CtxConfig, ContextAuth, ContextDefaults, context_lock_path, context_name_from_url,
    lock_exclusive,
};

use crate::cli::GlobalArgs;
use crate::session;
use crate::ui;

#[derive(Args)]
#[command(
    after_help = "Signs in through the server's Logchef OAuth with a browser on this \
machine. The server must advertise `oauth_issuer` in /api/v1/meta. Without it, use an API \
token: pass --token or set LOGCHEF_AUTH_TOKEN."
)]
pub struct AuthArgs {
    #[command(subcommand)]
    command: Option<AuthCmd>,

    /// Revoke the sign-in at the server, then remove it from this machine.
    #[arg(long, short)]
    logout: bool,

    /// Show the instance, user, method, scopes, and expiry.
    #[arg(long)]
    status: bool,

    /// Print the sign-in URL to stderr and do not open a browser.
    #[arg(long)]
    no_browser: bool,
}

#[derive(Subcommand)]
enum AuthCmd {
    /// Print the active context, server URL, and token source without
    /// hitting the network. Use `whoami` to fetch the user identity.
    Current,
}

pub async fn run(args: AuthArgs, global: GlobalArgs) -> Result<()> {
    let config = Config::load().context("Failed to load config")?;

    if let Some(AuthCmd::Current) = args.command {
        return current(&config, &global);
    }

    if args.logout {
        return logout(&config, &global).await;
    }

    if args.status {
        return status(&config, &global).await;
    }

    let open_browser = !args.no_browser && ui::interactive();
    login(&config, global, open_browser).await
}

fn current(config: &Config, global: &GlobalArgs) -> Result<()> {
    // Resolve context without hitting the network. Prefers --context, then
    // --server (matched against saved contexts), then the active context.
    let (ctx_name, server_url, auth) = if let Some(name) = &global.context {
        let ctx = config
            .get_context(name)
            .ok_or_else(|| anyhow::anyhow!("Context '{}' not found", name))?;
        (name.clone(), ctx.server_url.clone(), ctx.auth.as_ref())
    } else if let Some(url) = &global.server {
        match config.find_context_by_url(url) {
            Some((name, ctx)) => (name.to_string(), ctx.server_url.clone(), ctx.auth.as_ref()),
            None => ("(ephemeral)".to_string(), url.clone(), None),
        }
    } else if let Some(name) = config.current_context_name() {
        let ctx = config
            .current_context()
            .ok_or_else(|| anyhow::anyhow!("Current context '{}' not found", name))?;
        (name.to_string(), ctx.server_url.clone(), ctx.auth.as_ref())
    } else if let Ok(env_url) = std::env::var("LOGCHEF_SERVER_URL") {
        ("(ephemeral)".to_string(), env_url, None)
    } else {
        anyhow::bail!("No context configured and no --server/LOGCHEF_SERVER_URL provided.");
    };

    println!("context: {}", ctx_name);
    println!("server:  {}", server_url);
    println!(
        "token:   {}",
        token_line(auth, global.token.is_some(), ctx_name == "(ephemeral)")
    );

    if let Ok(team) = std::env::var("LOGCHEF_DEFAULT_TEAM") {
        println!("team:    {} (from LOGCHEF_DEFAULT_TEAM)", team);
    }
    if let Ok(source) = std::env::var("LOGCHEF_DEFAULT_SOURCE") {
        println!("source:  {} (from LOGCHEF_DEFAULT_SOURCE)", source);
    }

    Ok(())
}

fn token_line(auth: Option<&ContextAuth>, env_token: bool, is_ephemeral: bool) -> String {
    // --token / LOGCHEF_AUTH_TOKEN takes precedence over the saved credential,
    // and we don't know the env-supplied token's expiry, so skip it there.
    if env_token {
        return "set (from --token/LOGCHEF_AUTH_TOKEN)".to_string();
    }
    match auth {
        Some(ContextAuth::Pat { expires_at, .. }) => {
            let mut s = "API token (from config".to_string();
            if let Some(ts) = expires_at {
                let expired = *ts < chrono::Utc::now();
                s.push_str(if expired { ", EXPIRED " } else { ", expires " });
                s.push_str(&ts.to_rfc3339_opts(chrono::SecondsFormat::Secs, true));
                if expired {
                    s.push_str(". Run `logchef auth` to sign in again");
                }
            }
            s.push(')');
            s
        }
        Some(ContextAuth::OAuth(_)) => {
            "Logchef OAuth (from config, renewed automatically)".to_string()
        }
        None if is_ephemeral => {
            "not set (ephemeral context; pass --token or run `logchef auth`)".to_string()
        }
        None => "not set (run `logchef auth` to sign in)".to_string(),
    }
}

async fn logout(config: &Config, global: &GlobalArgs) -> Result<()> {
    let ctx_name = resolve_context_name(config, global)?;
    if config.get_context(&ctx_name).is_none() {
        println!("Context '{}' not found.", ctx_name);
        return Ok(());
    }

    // The context lock keeps a concurrent refresh from rotating the refresh
    // token between the revocation and the local removal.
    let config_path = Config::config_path()?;
    let lock_path = context_lock_path(&config_path, &ctx_name)?;
    let _lock = tokio::task::spawn_blocking(move || lock_exclusive(&lock_path)).await??;

    let saved = Config::load_from(&config_path)?
        .get_context(&ctx_name)
        .and_then(|ctx| ctx.auth.clone());
    let revoked = match &saved {
        Some(ContextAuth::OAuth(credential)) => auth::revoke(credential).await,
        _ => Ok(()),
    };

    Config::update(|config| {
        if let Some(ctx) = config.get_context_mut(&ctx_name) {
            ctx.auth = None;
        }
        Ok(())
    })
    .context("Failed to save config")?;

    match (saved, revoked) {
        (None, _) => println!("Context '{}' was not signed in.", ctx_name),
        (Some(_), Ok(())) => println!("Logged out from context '{}'.", ctx_name),
        (Some(_), Err(err)) => anyhow::bail!(
            "Removed the local sign-in for context '{}', but the server did not confirm the \
             revocation ({}). Revoke the CLI grant in Logchef under Settings, Connected apps.",
            ctx_name,
            err
        ),
    }
    Ok(())
}

async fn status(config: &Config, global: &GlobalArgs) -> Result<()> {
    let resolved = match session::resolve(config, global) {
        Ok(resolved) => resolved,
        Err(_) => {
            println!("No contexts configured. Run 'logchef auth --server <url>' to set up.");
            return Ok(());
        }
    };

    println!("Context:  {}", resolved.name);
    println!("Instance: {}", resolved.ctx.server_url);

    if !resolved.ctx.is_authenticated() && global.token.is_none() {
        println!("Status:   Not authenticated");
        return Ok(());
    }

    let client = session::client_for(&resolved, global, resolved.ctx.timeout_secs)?;
    let me = client.get_me().await?;
    println!("User:     {}", me.user.email);
    if let Some(name) = &me.user.full_name {
        println!("Name:     {}", name);
    }
    println!("Role:     {}", me.user.role);
    match &me.auth {
        Some(auth) => {
            let method = match auth.method.as_str() {
                "oauth" => "Logchef OAuth",
                "token" => "API token",
                other => other,
            };
            match &auth.client_id {
                Some(client_id) => println!("Method:   {} (client {})", method, client_id),
                None => println!("Method:   {}", method),
            }
            println!("Scopes:   {}", auth.scopes.join(" "));
            match auth.expires_at {
                Some(ts) => println!(
                    "Expires:  {}{}",
                    ts.to_rfc3339_opts(chrono::SecondsFormat::Secs, true),
                    if auth.method == "oauth" {
                        " (access token; renewed automatically)"
                    } else {
                        ""
                    }
                ),
                None => println!("Expires:  never"),
            }
        }
        None => {
            if let Some(method) = &me.auth_method {
                println!("Method:   {}", method);
            }
        }
    }

    Ok(())
}

async fn login(config: &Config, global: GlobalArgs, open_browser: bool) -> Result<()> {
    let server_url = get_server_url(config, &global)?;
    let server_url = server_url.trim_end_matches('/').to_string();

    eprintln!("Connecting to {}...", server_url);

    let client = Client::new(&server_url, 30)?;
    let meta = client
        .get_meta()
        .await
        .context("Failed to connect to server")?;

    let Some(issuer) = meta.data.oauth_issuer else {
        anyhow::bail!(
            "{} (Logchef {}) does not offer Logchef OAuth sign-in. Upgrade the server and \
             enable auth.oauth, or use an API token: pass --token or set LOGCHEF_AUTH_TOKEN.",
            server_url,
            meta.data.version
        );
    };
    if issuer != server_url {
        anyhow::bail!(
            "The server's OAuth issuer is {} but you connected to {}. Sign in with \
             `logchef auth --server {}`.",
            issuer,
            server_url,
            logchef_core::error::shell_quote(&issuer)
        );
    }

    let credential = auth::login(
        &issuer,
        LoginOptions {
            open_browser,
            limits: CallbackLimits::default(),
        },
    )
    .await?;

    let ctx_name = global
        .context
        .clone()
        .or_else(|| {
            config
                .find_context_by_url(&server_url)
                .map(|(n, _)| n.to_string())
        })
        .unwrap_or_else(|| context_name_from_url(&server_url));

    let config_path = Config::config_path()?;
    let lock_path = context_lock_path(&config_path, &ctx_name)?;
    let _lock = tokio::task::spawn_blocking(move || lock_exclusive(&lock_path)).await??;
    let saved_ctx = ctx_name.clone();
    Config::update(move |config| {
        // A new sign-in replaces only the credential and URL. Defaults and the
        // timeout of an existing context stay as they were.
        let ctx = match config.get_context(&saved_ctx) {
            Some(existing) => CtxConfig {
                server_url: server_url.clone(),
                auth: Some(ContextAuth::OAuth(credential)),
                ..existing.clone()
            },
            None => CtxConfig {
                auth: Some(ContextAuth::OAuth(credential)),
                defaults: ContextDefaults {
                    timezone: iana_time_zone::get_timezone().ok(),
                    ..Default::default()
                },
                ..CtxConfig::new(server_url.clone())
            },
        };
        config.add_or_update_context(saved_ctx, ctx);
        Ok(())
    })
    .context("Failed to save config")?;

    let resolved = session::ResolvedContext {
        ctx: Config::load()?
            .get_context(&ctx_name)
            .cloned()
            .ok_or_else(|| anyhow::anyhow!("Context '{}' not found after saving", ctx_name))?,
        name: ctx_name.clone(),
        is_ephemeral: false,
    };
    let no_override = GlobalArgs {
        token: None,
        ..global
    };
    let client = session::client_for(&resolved, &no_override, resolved.ctx.timeout_secs)?;
    match client.get_current_user().await {
        Ok(user) => eprintln!(
            "\nAuthenticated as {} (context: '{}')",
            user.email, ctx_name
        ),
        Err(_) => eprintln!("\nAuthenticated (context: '{}')", ctx_name),
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

    // Priority 3: Interactive prompt with optional default. Never prompt
    // without a terminal or in CI.
    let default = config.current_context().map(|ctx| ctx.server_url.clone());
    if !ui::interactive() {
        return default.ok_or_else(|| {
            anyhow::anyhow!("No server to sign in to. Pass --server <url> or --context <name>.")
        });
    }

    let mut prompt = Text::new("Logchef server URL:");
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
