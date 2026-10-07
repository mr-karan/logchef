//! Logchef OAuth for the CLI: the authorization code flow with PKCE (S256)
//! through a loopback redirect, refresh under a per-context lock, and
//! revocation. The server is the authorization server; the CLI is the
//! built-in public client `logchef-cli` with the resource `<issuer>/api`.

mod loopback;

pub use loopback::{CallbackLimits, CallbackListener, ExpectedCallback, wait_for_code};

use crate::config::{Config, ContextAuth, OAuthCredential, context_lock_path, lock_exclusive};
use crate::error::{Error, Result};
use chrono::{DateTime, Utc};
use serde::Deserialize;
use std::path::{Path, PathBuf};
use std::time::Duration;
use tracing::debug;
use url::Url;

pub const CLIENT_ID: &str = "logchef-cli";

/// The seven read scopes. The CLI asks for all of them plus `offline_access`.
pub const READ_SCOPES: [&str; 7] = [
    "profile:read",
    "teams:read",
    "sources:read",
    "logs:read",
    "saved_queries:read",
    "collections:read",
    "alerts:read",
];
const OFFLINE_ACCESS: &str = "offline_access";

const HTTP_TIMEOUT: Duration = Duration::from_secs(30);
/// An access token this close to expiry is refreshed before use.
const EXPIRY_SKEW: chrono::Duration = chrono::Duration::seconds(30);

/// The resource (audience) of tokens for the Logchef API.
pub fn api_resource(issuer: &str) -> String {
    format!("{issuer}/api")
}

/// The RFC 8414 document, reduced to the fields the CLI uses.
#[derive(Debug, Deserialize)]
struct ServerMetadata {
    issuer: String,
    authorization_endpoint: String,
    token_endpoint: String,
    #[serde(default)]
    revocation_endpoint: Option<String>,
    #[serde(default)]
    code_challenge_methods_supported: Vec<String>,
    #[serde(default)]
    authorization_response_iss_parameter_supported: bool,
}

#[derive(Deserialize)]
struct TokenResponse {
    access_token: String,
    token_type: String,
    expires_in: i64,
    #[serde(default)]
    refresh_token: Option<String>,
    #[serde(default)]
    scope: Option<String>,
}

#[derive(Deserialize)]
struct TokenErrorResponse {
    error: String,
    #[serde(default)]
    error_description: Option<String>,
}

/// A token endpoint failure: an OAuth error response, or a transport or
/// parsing problem.
enum TokenError {
    OAuth {
        error: String,
        description: Option<String>,
    },
    Other(Error),
}

impl From<TokenError> for Error {
    fn from(err: TokenError) -> Self {
        match err {
            TokenError::OAuth { error, description } => Error::oauth(match description {
                Some(d) => format!("token endpoint returned {error}: {d}"),
                None => format!("token endpoint returned {error}"),
            }),
            TokenError::Other(err) => err,
        }
    }
}

fn http_client() -> Result<reqwest::Client> {
    reqwest::Client::builder()
        .timeout(HTTP_TIMEOUT)
        .user_agent(concat!("logchef-cli/", env!("CARGO_PKG_VERSION")))
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .map_err(|e| Error::other(format!("Failed to create HTTP client: {e}")))
}

/// Fetches `<issuer>/.well-known/oauth-authorization-server` and checks it
/// with [`check_metadata`].
async fn discover(http: &reqwest::Client, issuer: &str) -> Result<ServerMetadata> {
    let url = format!("{issuer}/.well-known/oauth-authorization-server");
    debug!(url = %url, "Fetching authorization server metadata");
    let response = http.get(&url).send().await?;
    if !response.status().is_success() {
        return Err(Error::oauth(format!(
            "GET {url} returned HTTP {}",
            response.status().as_u16()
        )));
    }
    let metadata: ServerMetadata = response
        .json()
        .await
        .map_err(|e| Error::oauth(format!("Invalid authorization server metadata: {e}")))?;
    check_metadata(issuer, &metadata)?;
    Ok(metadata)
}

/// Checks that the metadata describes this issuer, supports S256 and RFC 9207
/// `iss`, and keeps the token and revocation endpoints (which receive
/// credentials) under the issuer. The authorization endpoint only receives a
/// PKCE challenge in the browser, so it may live on another origin (a server
/// whose browser host differs from its API host); it must still be https, or
/// http on a loopback host.
fn check_metadata(issuer: &str, metadata: &ServerMetadata) -> Result<()> {
    if metadata.issuer != issuer {
        return Err(Error::oauth(format!(
            "authorization server metadata names issuer {}, expected {issuer}",
            metadata.issuer
        )));
    }
    if !metadata
        .code_challenge_methods_supported
        .iter()
        .any(|m| m == "S256")
    {
        return Err(Error::oauth(
            "the authorization server does not support PKCE S256",
        ));
    }
    if !metadata.authorization_response_iss_parameter_supported {
        return Err(Error::oauth(
            "the authorization server does not send `iss` in authorization responses",
        ));
    }
    let prefix = format!("{issuer}/");
    let machine_endpoints = [
        Some(&metadata.token_endpoint),
        metadata.revocation_endpoint.as_ref(),
    ];
    if let Some(outside) = machine_endpoints
        .into_iter()
        .flatten()
        .find(|e| !e.starts_with(&prefix))
    {
        return Err(Error::oauth(format!(
            "authorization server endpoint {outside} is outside the issuer {issuer}"
        )));
    }
    let authorize = Url::parse(&metadata.authorization_endpoint).map_err(|e| {
        Error::oauth(format!(
            "invalid authorization endpoint {}: {e}",
            metadata.authorization_endpoint
        ))
    })?;
    let loopback = matches!(
        authorize.host(),
        Some(url::Host::Domain("localhost"))
            | Some(url::Host::Ipv4(std::net::Ipv4Addr::LOCALHOST))
            | Some(url::Host::Ipv6(std::net::Ipv6Addr::LOCALHOST))
    );
    if !(authorize.scheme() == "https" || (authorize.scheme() == "http" && loopback)) {
        return Err(Error::oauth(format!(
            "authorization endpoint {} must use https",
            metadata.authorization_endpoint
        )));
    }
    Ok(())
}

async fn post_token(
    http: &reqwest::Client,
    endpoint: &str,
    form: &[(&str, &str)],
) -> std::result::Result<TokenResponse, TokenError> {
    let response = http
        .post(endpoint)
        .form(form)
        .send()
        .await
        .map_err(|e| TokenError::Other(e.into()))?;
    let status = response.status();
    let body = response
        .text()
        .await
        .map_err(|e| TokenError::Other(e.into()))?;
    if !status.is_success() {
        return Err(match serde_json::from_str::<TokenErrorResponse>(&body) {
            Ok(err) => TokenError::OAuth {
                error: err.error,
                description: err.error_description,
            },
            Err(_) => TokenError::Other(Error::oauth(format!(
                "token endpoint returned HTTP {}",
                status.as_u16()
            ))),
        });
    }
    let token: TokenResponse = serde_json::from_str(&body)
        .map_err(|e| TokenError::Other(Error::oauth(format!("Invalid token response: {e}"))))?;
    if !token.token_type.eq_ignore_ascii_case("bearer") {
        return Err(TokenError::Other(Error::oauth(format!(
            "unsupported token type {}",
            token.token_type
        ))));
    }
    Ok(token)
}

fn expires_at(expires_in: i64) -> DateTime<Utc> {
    Utc::now() + chrono::Duration::seconds(expires_in)
}

fn granted_scopes(token: &TokenResponse, requested: &[String]) -> Vec<String> {
    match &token.scope {
        Some(scope) => scope
            .split_whitespace()
            .filter(|s| *s != OFFLINE_ACCESS)
            .map(str::to_string)
            .collect(),
        None => requested.to_vec(),
    }
}

/// How `login` shows the authorization URL.
#[derive(Clone, Copy, Debug)]
pub struct LoginOptions {
    /// Try to open the URL in a browser. The URL is printed either way.
    pub open_browser: bool,
    pub limits: CallbackLimits,
}

/// Runs the loopback authorization code flow against `issuer` and returns the
/// new credential. Progress and the URL go to stderr.
pub async fn login(issuer: &str, options: LoginOptions) -> Result<OAuthCredential> {
    let http = http_client()?;
    let metadata = discover(&http, issuer).await?;
    let resource = api_resource(issuer);
    let requested: Vec<String> = READ_SCOPES.iter().map(|s| s.to_string()).collect();

    let listener = CallbackListener::bind()?;
    let redirect_uri = listener.redirect_uri().to_string();
    let (verifier, challenge) = generate_pkce()?;
    let state = random_token(16)?;

    let mut authorize = Url::parse(&metadata.authorization_endpoint)?;
    authorize
        .query_pairs_mut()
        .append_pair("response_type", "code")
        .append_pair("client_id", CLIENT_ID)
        .append_pair("redirect_uri", &redirect_uri)
        .append_pair(
            "scope",
            &format!("{} {OFFLINE_ACCESS}", READ_SCOPES.join(" ")),
        )
        .append_pair("state", &state)
        .append_pair("code_challenge", &challenge)
        .append_pair("code_challenge_method", "S256")
        .append_pair("resource", &resource);

    eprintln!("To sign in to {issuer}, open this URL in a browser:\n\n  {authorize}\n");
    if options.open_browser
        && let Err(e) = open::that(authorize.as_str())
    {
        debug!(error = %e, "Could not open a browser");
    }
    eprintln!("Waiting for the browser to finish (Ctrl-C to cancel)...");

    let expected = ExpectedCallback {
        state,
        issuer: issuer.to_string(),
    };
    let code = wait_for_code(listener, expected, options.limits).await?;

    let token = post_token(
        &http,
        &metadata.token_endpoint,
        &[
            ("grant_type", "authorization_code"),
            ("code", &code),
            ("redirect_uri", &redirect_uri),
            ("client_id", CLIENT_ID),
            ("code_verifier", &verifier),
            ("resource", &resource),
        ],
    )
    .await?;
    let refresh_token = token.refresh_token.clone().ok_or_else(|| {
        Error::oauth("the token response has no refresh token, so the sign-in cannot be renewed")
    })?;

    Ok(OAuthCredential {
        issuer: issuer.to_string(),
        client_id: CLIENT_ID.to_string(),
        resource,
        scopes: granted_scopes(&token, &requested),
        access_expires_at: expires_at(token.expires_in),
        access_token: token.access_token,
        refresh_token,
    })
}

/// Returns a fresh credential for `context` after `stale_access_token` was
/// found expired or was rejected. Under the context's lock it re-reads the
/// config: when another process already refreshed, that result is used;
/// otherwise this process refreshes and saves the rotated tokens before
/// releasing the lock, so no two processes ever send the same refresh token.
pub async fn refresh_context(
    config_path: &Path,
    context: &str,
    stale_access_token: &str,
) -> Result<OAuthCredential> {
    let lock_path = context_lock_path(config_path, context)?;
    let _lock = tokio::task::spawn_blocking(move || lock_exclusive(&lock_path))
        .await
        .map_err(|e| Error::other(format!("Lock task failed: {e}")))??;

    let current = saved_credential(config_path, context)?;
    if current.access_token != stale_access_token
        && current.access_expires_at > Utc::now() + EXPIRY_SKEW
    {
        debug!(context, "Another process refreshed the access token");
        return Ok(current);
    }

    let http = http_client()?;
    let metadata = discover(&http, &current.issuer).await?;
    let token = post_token(
        &http,
        &metadata.token_endpoint,
        &[
            ("grant_type", "refresh_token"),
            ("refresh_token", &current.refresh_token),
            ("client_id", &current.client_id),
            ("resource", &current.resource),
        ],
    )
    .await
    .map_err(|err| match err {
        TokenError::OAuth { error, .. } => Error::auth_required(
            context,
            format!("The server rejected the saved sign-in for context '{context}' ({error})"),
        ),
        TokenError::Other(err) => err,
    })?;

    let refreshed = OAuthCredential {
        scopes: granted_scopes(&token, &current.scopes),
        access_expires_at: expires_at(token.expires_in),
        access_token: token.access_token,
        refresh_token: token.refresh_token.unwrap_or(current.refresh_token),
        ..current
    };
    let saved = refreshed.clone();
    Config::update_at(config_path, |config| {
        let ctx = config.get_context_mut(context).ok_or_else(|| {
            Error::auth_required(context, format!("Context '{context}' was removed"))
        })?;
        ctx.auth = Some(ContextAuth::OAuth(saved));
        Ok(())
    })?;
    Ok(refreshed)
}

fn saved_credential(config_path: &Path, context: &str) -> Result<OAuthCredential> {
    let config = Config::load_from(config_path)?;
    match config.get_context(context).and_then(|c| c.auth.clone()) {
        Some(ContextAuth::OAuth(credential)) => Ok(credential),
        _ => Err(Error::auth_required(
            context,
            format!("Context '{context}' is no longer signed in"),
        )),
    }
}

/// True when the access token must be refreshed before it is sent.
pub fn needs_refresh(credential: &OAuthCredential) -> bool {
    credential.access_expires_at <= Utc::now() + EXPIRY_SKEW
}

/// Revokes the grant behind `credential` at the server (RFC 7009). Revoking
/// the refresh token ends the whole grant, including its access tokens.
pub async fn revoke(credential: &OAuthCredential) -> Result<()> {
    let http = http_client()?;
    let metadata = discover(&http, &credential.issuer).await?;
    let endpoint = metadata.revocation_endpoint.ok_or_else(|| {
        Error::oauth("the authorization server does not advertise a revocation endpoint")
    })?;
    let response = http
        .post(&endpoint)
        .form(&[
            ("token", credential.refresh_token.as_str()),
            ("token_type_hint", "refresh_token"),
            ("client_id", credential.client_id.as_str()),
        ])
        .send()
        .await?;
    if !response.status().is_success() {
        return Err(Error::oauth(format!(
            "revocation endpoint returned HTTP {}",
            response.status().as_u16()
        )));
    }
    Ok(())
}

/// Where a saved OAuth credential lives, so a client can refresh it.
#[derive(Clone, Debug)]
pub struct SavedCredential {
    pub config_path: PathBuf,
    pub context: String,
    pub credential: OAuthCredential,
}

fn generate_pkce() -> Result<(String, String)> {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    use sha2::{Digest, Sha256};

    let verifier = random_token(32)?;
    let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
    Ok((verifier, challenge))
}

fn random_token(len: usize) -> Result<String> {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};

    let mut bytes = vec![0u8; len];
    getrandom::fill(&mut bytes)
        .map_err(|e| Error::other(format!("Failed to generate random bytes: {e}")))?;
    Ok(URL_SAFE_NO_PAD.encode(bytes))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn metadata(authorization_endpoint: &str) -> ServerMetadata {
        ServerMetadata {
            issuer: "https://logchef-api.example.com".into(),
            authorization_endpoint: authorization_endpoint.into(),
            token_endpoint: "https://logchef-api.example.com/oauth/token".into(),
            revocation_endpoint: Some("https://logchef-api.example.com/oauth/revoke".into()),
            code_challenge_methods_supported: vec!["S256".into()],
            authorization_response_iss_parameter_supported: true,
        }
    }

    const ISSUER: &str = "https://logchef-api.example.com";

    #[test]
    fn authorization_endpoint_may_use_the_browser_host() {
        for endpoint in [
            "https://logchef-api.example.com/oauth/authorize",
            "https://logchef.example.com/oauth/authorize",
            "http://localhost:8147/oauth/authorize",
            "http://127.0.0.1:8147/oauth/authorize",
        ] {
            check_metadata(ISSUER, &metadata(endpoint))
                .unwrap_or_else(|e| panic!("{endpoint}: {e}"));
        }
    }

    #[test]
    fn authorization_endpoint_must_be_https_or_loopback() {
        for endpoint in [
            "http://logchef.example.com/oauth/authorize",
            "not a url",
            "javascript:alert(1)",
        ] {
            assert!(
                check_metadata(ISSUER, &metadata(endpoint)).is_err(),
                "{endpoint} accepted"
            );
        }
    }

    #[test]
    fn credential_endpoints_stay_under_the_issuer() {
        let mut token = metadata("https://logchef.example.com/oauth/authorize");
        token.token_endpoint = "https://logchef.example.com/oauth/token".into();
        assert!(check_metadata(ISSUER, &token).is_err());
        let mut revoke = metadata("https://logchef.example.com/oauth/authorize");
        revoke.revocation_endpoint = Some("https://evil.example.com/oauth/revoke".into());
        assert!(check_metadata(ISSUER, &revoke).is_err());
    }

    #[test]
    fn issuer_s256_and_iss_are_required() {
        let mut other = metadata("https://logchef.example.com/oauth/authorize");
        other.issuer = "https://logchef.example.com".into();
        assert!(check_metadata(ISSUER, &other).is_err());
        let mut plain = metadata("https://logchef.example.com/oauth/authorize");
        plain.code_challenge_methods_supported = vec!["plain".into()];
        assert!(check_metadata(ISSUER, &plain).is_err());
        let mut no_iss = metadata("https://logchef.example.com/oauth/authorize");
        no_iss.authorization_response_iss_parameter_supported = false;
        assert!(check_metadata(ISSUER, &no_iss).is_err());
    }
}
