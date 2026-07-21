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
pub struct Context {
    pub server_url: String,

    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,

    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,

    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub token_expires_at: Option<DateTime<Utc>>,

    #[serde(default)]
    pub defaults: ContextDefaults,
}

fn default_timeout() -> u64 {
    30
}

impl Context {
    pub fn new(server_url: String) -> Self {
        Self {
            server_url,
            timeout_secs: default_timeout(),
            token: None,
            token_expires_at: None,
            defaults: ContextDefaults::default(),
        }
    }

    pub fn is_authenticated(&self) -> bool {
        self.token.is_some()
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
