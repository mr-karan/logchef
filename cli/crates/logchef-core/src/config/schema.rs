use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

pub const CONFIG_VERSION: u32 = 1;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    #[serde(default = "default_version")]
    pub version: u32,

    #[serde(default)]
    pub current_context: Option<String>,

    #[serde(default)]
    pub contexts: HashMap<String, Context>,

    #[serde(default)]
    pub highlights: HighlightsConfig,

    /// Show the ASCII startup banner on bare `logchef` (TTY only). Defaults to
    /// true; absent in old config files, which load fine via the serde default.
    #[serde(default = "default_true")]
    pub show_banner: bool,

    /// Check GitHub for a newer CLI release and print a notice to stderr (TTY
    /// only). Defaults to true; absent in old config files, which load fine.
    #[serde(default = "default_true")]
    pub check_updates: bool,
}

fn default_version() -> u32 {
    CONFIG_VERSION
}

fn default_true() -> bool {
    true
}

impl Default for Config {
    fn default() -> Self {
        Self {
            version: CONFIG_VERSION,
            current_context: None,
            contexts: HashMap::new(),
            highlights: HighlightsConfig::default(),
            show_banner: true,
            check_updates: true,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(try_from = "ContextRecord", into = "ContextRecord")]
pub struct Context {
    pub server_url: String,
    pub timeout_secs: u64,
    pub auth: Option<ContextAuth>,
    pub defaults: ContextDefaults,
}

/// The saved credential of a context. A context holds at most one.
#[derive(Clone, PartialEq)]
pub enum ContextAuth {
    /// A Logchef API token (`logchef_...`), pasted in or saved by an older CLI.
    Pat {
        token: String,
        expires_at: Option<DateTime<Utc>>,
    },
    /// A Logchef OAuth grant from `logchef auth`.
    OAuth(OAuthCredential),
}

#[derive(Clone, PartialEq, Serialize, Deserialize)]
pub struct OAuthCredential {
    pub issuer: String,
    pub client_id: String,
    pub resource: String,
    pub scopes: Vec<String>,
    pub access_token: String,
    pub access_expires_at: DateTime<Utc>,
    pub refresh_token: String,
}

impl std::fmt::Debug for ContextAuth {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Pat { expires_at, .. } => f
                .debug_struct("Pat")
                .field("expires_at", expires_at)
                .finish_non_exhaustive(),
            Self::OAuth(credential) => f.debug_tuple("OAuth").field(credential).finish(),
        }
    }
}

impl std::fmt::Debug for OAuthCredential {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("OAuthCredential")
            .field("issuer", &self.issuer)
            .field("client_id", &self.client_id)
            .field("resource", &self.resource)
            .field("scopes", &self.scopes)
            .field("access_expires_at", &self.access_expires_at)
            .finish_non_exhaustive()
    }
}

/// The on-disk shape of a context. A PAT keeps the `token` and
/// `token_expires_at` keys that older CLIs wrote and still read. An OAuth
/// credential lives under `oauth`, which older CLIs ignore.
#[derive(Serialize, Deserialize)]
struct ContextRecord {
    server_url: String,
    #[serde(default = "default_timeout")]
    timeout_secs: u64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    token: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    token_expires_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    oauth: Option<OAuthCredential>,
    #[serde(default)]
    defaults: ContextDefaults,
}

impl TryFrom<ContextRecord> for Context {
    type Error = String;

    fn try_from(record: ContextRecord) -> Result<Self, Self::Error> {
        let auth = match (record.token, record.oauth) {
            (Some(_), Some(_)) => {
                return Err(format!(
                    "context for {} has both a token and an OAuth credential",
                    record.server_url
                ));
            }
            (Some(token), None) => Some(ContextAuth::Pat {
                token,
                expires_at: record.token_expires_at,
            }),
            (None, Some(credential)) => Some(ContextAuth::OAuth(credential)),
            (None, None) => None,
        };
        Ok(Self {
            server_url: record.server_url,
            timeout_secs: record.timeout_secs,
            auth,
            defaults: record.defaults,
        })
    }
}

impl From<Context> for ContextRecord {
    fn from(context: Context) -> Self {
        let (token, token_expires_at, oauth) = match context.auth {
            Some(ContextAuth::Pat { token, expires_at }) => (Some(token), expires_at, None),
            Some(ContextAuth::OAuth(credential)) => (None, None, Some(credential)),
            None => (None, None, None),
        };
        Self {
            server_url: context.server_url,
            timeout_secs: context.timeout_secs,
            token,
            token_expires_at,
            oauth,
            defaults: context.defaults,
        }
    }
}

fn default_timeout() -> u64 {
    30
}

impl Context {
    pub fn new(server_url: String) -> Self {
        Self {
            server_url,
            timeout_secs: default_timeout(),
            auth: None,
            defaults: ContextDefaults::default(),
        }
    }

    pub fn is_authenticated(&self) -> bool {
        self.auth.is_some()
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ContextDefaults {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub team: Option<String>,

    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,

    #[serde(default = "default_limit")]
    pub limit: u32,

    #[serde(default = "default_since")]
    pub since: String,

    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub timezone: Option<String>,
}

impl ContextDefaults {
    pub fn team_with_env(&self) -> Option<String> {
        env_default("LOGCHEF_DEFAULT_TEAM").or_else(|| self.team.clone())
    }

    pub fn source_with_env(&self) -> Option<String> {
        env_default("LOGCHEF_DEFAULT_SOURCE").or_else(|| self.source.clone())
    }
}

fn env_default(name: &str) -> Option<String> {
    std::env::var(name)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn default_limit() -> u32 {
    100
}

fn default_since() -> String {
    "15m".to_string()
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct HighlightsConfig {
    #[serde(default)]
    pub custom_keywords: Vec<String>,

    #[serde(default)]
    pub disable_builtin: bool,

    #[serde(default)]
    pub custom_regexes: Vec<RegexHighlight>,

    #[serde(default)]
    pub disabled_groups: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegexHighlight {
    pub pattern: String,

    #[serde(default = "default_regex_color")]
    pub color: String,

    #[serde(default)]
    pub bold: bool,

    #[serde(default)]
    pub italic: bool,
}

fn default_regex_color() -> String {
    "magenta".to_string()
}

pub fn context_name_from_url(url: &str) -> String {
    url::Url::parse(url)
        .ok()
        .and_then(|u| u.host_str().map(|h| h.to_string()))
        .unwrap_or_else(|| "default".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn old_config_without_new_fields_defaults_true() {
        // A config written before show_banner/check_updates existed.
        let json = r#"{"version":1,"current_context":null,"contexts":{}}"#;
        let config: Config = serde_json::from_str(json).expect("should load old config");
        assert!(config.show_banner);
        assert!(config.check_updates);
    }

    #[test]
    fn legacy_pat_context_loads_and_saves_in_the_old_shape() {
        let json = r#"{"version":1,"current_context":"prod","contexts":{"prod":{
            "server_url":"https://logs.example.com","timeout_secs":30,
            "token":"logchef_1_abc","token_expires_at":"2030-01-01T00:00:00Z",
            "defaults":{"team":"ops","limit":100,"since":"15m"}}}}"#;
        let config: Config = serde_json::from_str(json).expect("legacy config loads");
        let ctx = config.get_context("prod").unwrap();
        let Some(ContextAuth::Pat { token, expires_at }) = &ctx.auth else {
            panic!("expected a PAT, got {:?}", ctx.auth);
        };
        assert_eq!(token, "logchef_1_abc");
        assert!(expires_at.is_some());
        assert_eq!(ctx.defaults.team.as_deref(), Some("ops"));

        let saved = serde_json::to_value(&config).unwrap();
        let saved_ctx = &saved["contexts"]["prod"];
        assert_eq!(saved_ctx["token"], "logchef_1_abc");
        assert_eq!(saved_ctx["token_expires_at"], "2030-01-01T00:00:00Z");
        assert!(saved_ctx.get("oauth").is_none());
    }

    #[test]
    fn oauth_context_round_trips() {
        let mut ctx = Context::new("https://logs.example.com".into());
        ctx.auth = Some(ContextAuth::OAuth(OAuthCredential {
            issuer: "https://logs.example.com".into(),
            client_id: "logchef-cli".into(),
            resource: "https://logs.example.com/api".into(),
            scopes: vec!["logs:read".into()],
            access_token: "access".into(),
            access_expires_at: "2030-01-01T00:00:00Z".parse().unwrap(),
            refresh_token: "refresh".into(),
        }));
        let json = serde_json::to_value(&ctx).unwrap();
        assert!(json.get("token").is_none());
        assert_eq!(json["oauth"]["refresh_token"], "refresh");
        let back: Context = serde_json::from_value(json).unwrap();
        assert_eq!(back.auth, ctx.auth);
    }

    #[test]
    fn context_with_both_credentials_is_rejected() {
        let json = r#"{"server_url":"https://l.example.com","token":"t","oauth":{
            "issuer":"i","client_id":"c","resource":"r","scopes":[],"access_token":"a",
            "access_expires_at":"2030-01-01T00:00:00Z","refresh_token":"x"}}"#;
        let err = serde_json::from_str::<Context>(json).unwrap_err();
        assert!(
            err.to_string()
                .contains("both a token and an OAuth credential")
        );
    }

    #[test]
    fn debug_output_hides_tokens() {
        let auth = ContextAuth::Pat {
            token: "logchef_secret".into(),
            expires_at: None,
        };
        assert!(!format!("{auth:?}").contains("logchef_secret"));
    }

    #[test]
    fn banner_flag_round_trips() {
        let mut config = Config::default();
        assert!(config.show_banner);
        config.show_banner = false;
        let json = serde_json::to_string(&config).unwrap();
        let reloaded: Config = serde_json::from_str(&json).unwrap();
        assert!(!reloaded.show_banner);
        assert!(reloaded.check_updates);
    }
}
