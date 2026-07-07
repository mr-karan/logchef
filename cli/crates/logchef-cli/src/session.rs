use anyhow::Result;
use logchef_core::Config;
use logchef_core::api::Client;
use logchef_core::config::Context;

use crate::cli::GlobalArgs;

pub struct AuthedSession {
    pub client: Client,
    pub ctx: Context,
}

pub fn authed(config: &Config, global: &GlobalArgs) -> Result<AuthedSession> {
    let resolved = resolve(config, global)?;
    enforce_auth(&resolved, global)?;
    let client = build_client(&resolved.ctx, global.token.as_deref(), None)?;
    Ok(AuthedSession {
        client,
        ctx: resolved.ctx,
    })
}

pub fn authed_with_timeout(
    config: &Config,
    global: &GlobalArgs,
    pick_timeout: impl FnOnce(&Context) -> u64,
) -> Result<AuthedSession> {
    let resolved = resolve(config, global)?;
    enforce_auth(&resolved, global)?;
    let timeout_secs = pick_timeout(&resolved.ctx);
    let client = build_client(&resolved.ctx, global.token.as_deref(), Some(timeout_secs))?;
    Ok(AuthedSession {
        client,
        ctx: resolved.ctx,
    })
}

pub struct ResolvedContext {
    pub ctx: Context,
    pub name: String,
    pub is_ephemeral: bool,
}

pub fn resolve(config: &Config, global: &GlobalArgs) -> Result<ResolvedContext> {
    if let Some(name) = &global.context {
        let ctx = config
            .get_context(name)
            .ok_or_else(|| anyhow::anyhow!("Context '{}' not found", name))?;
        return Ok(ResolvedContext {
            ctx: ctx.clone(),
            name: name.clone(),
            is_ephemeral: false,
        });
    }

    if let Some(url) = &global.server {
        if let Some((name, ctx)) = config.find_context_by_url(url) {
            return Ok(ResolvedContext {
                ctx: ctx.clone(),
                name: name.to_string(),
                is_ephemeral: false,
            });
        }
        return Ok(ResolvedContext {
            ctx: Context::new(url.clone()),
            name: "(ephemeral)".to_string(),
            is_ephemeral: true,
        });
    }

    let name = config
        .current_context_name()
        .ok_or_else(|| anyhow::anyhow!("No context configured. Run 'logchef auth' first."))?
        .to_string();
    let ctx = config
        .current_context()
        .ok_or_else(|| anyhow::anyhow!("Current context '{}' not found", name))?
        .clone();

    Ok(ResolvedContext {
        ctx,
        name,
        is_ephemeral: false,
    })
}

fn enforce_auth(resolved: &ResolvedContext, global: &GlobalArgs) -> Result<()> {
    if resolved.ctx.is_authenticated() || global.token.is_some() {
        return Ok(());
    }
    if resolved.is_ephemeral {
        anyhow::bail!(
            "Token required for server '{}'. Use --token or run 'logchef auth --server {}'.",
            resolved.ctx.server_url,
            resolved.ctx.server_url
        );
    }
    anyhow::bail!(
        "Not authenticated for context '{}'. Run 'logchef auth' first.",
        resolved.name
    );
}

fn build_client(ctx: &Context, token: Option<&str>, timeout_secs: Option<u64>) -> Result<Client> {
    let client = match timeout_secs {
        Some(t) => Client::from_context_with_timeout(ctx, t)?,
        None => Client::from_context(ctx)?,
    };
    match token {
        Some(t) => Ok(client.with_token(t.to_string())),
        None => Ok(client),
    }
}
