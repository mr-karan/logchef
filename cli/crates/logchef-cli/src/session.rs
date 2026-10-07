use anyhow::Result;
use logchef_core::api::Client;
use logchef_core::auth::SavedCredential;
use logchef_core::config::{Context, ContextAuth};
use logchef_core::{Config, Error};

use crate::cli::GlobalArgs;

pub struct AuthedSession {
    pub client: Client,
    pub ctx: Context,
}

pub fn authed(config: &Config, global: &GlobalArgs) -> Result<AuthedSession> {
    authed_with_timeout(config, global, |ctx| ctx.timeout_secs)
}

pub fn authed_with_timeout(
    config: &Config,
    global: &GlobalArgs,
    pick_timeout: impl FnOnce(&Context) -> u64,
) -> Result<AuthedSession> {
    let resolved = resolve(config, global)?;
    let timeout_secs = pick_timeout(&resolved.ctx);
    let client = client_for(&resolved, global, timeout_secs)?;
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

/// Builds a client with the credential that applies: `--token` or
/// `LOGCHEF_AUTH_TOKEN` first, then the context's saved PAT or OAuth grant.
/// Without either, the error names the fix and no prompt or browser opens.
pub fn client_for(
    resolved: &ResolvedContext,
    global: &GlobalArgs,
    timeout_secs: u64,
) -> Result<Client> {
    let client = Client::new(&resolved.ctx.server_url, timeout_secs)?;
    if let Some(token) = &global.token {
        return Ok(client.with_token(token.clone()));
    }
    match &resolved.ctx.auth {
        Some(ContextAuth::Pat { token, .. }) => Ok(client.with_token(token.clone())),
        Some(ContextAuth::OAuth(credential)) => Ok(client.with_oauth(SavedCredential {
            config_path: Config::config_path()?,
            context: resolved.name.clone(),
            credential: credential.clone(),
        })),
        None if resolved.is_ephemeral => Err(Error::AuthRequired {
            reason: format!("No token for server '{}'", resolved.ctx.server_url),
            fix: format!(
                "pass --token, or run `logchef auth --server {}`",
                logchef_core::error::shell_quote(&resolved.ctx.server_url)
            ),
        }
        .into()),
        None => Err(Error::auth_required(
            &resolved.name,
            format!("Context '{}' is not signed in", resolved.name),
        )
        .into()),
    }
}
