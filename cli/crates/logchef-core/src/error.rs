use thiserror::Error;

#[derive(Error, Debug)]
pub enum Error {
    #[error("Configuration error: {0}")]
    Config(String),

    /// The saved credential is missing, expired, revoked or rejected. `fix`
    /// is the command that signs in again. Data commands report this instead
    /// of opening a browser.
    #[error("{reason}. To fix: {fix}")]
    AuthRequired { reason: String, fix: String },

    #[error("API error: {message}")]
    Api {
        status: Option<u16>,
        message: String,
        /// The server's `error_type` from `ApiErrorResponse`, when present.
        /// Used to select an actionable CLI hint (see `ui::hint_for_error`).
        error_type: Option<String>,
    },

    #[error("Network error: {0}")]
    Network(#[from] reqwest::Error),

    #[error("Invalid URL: {0}")]
    InvalidUrl(#[from] url::ParseError),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("OAuth error: {0}")]
    OAuth(String),

    /// The authorization server sent `error=` to the loopback callback.
    #[error("Sign-in was not completed: {error}{}", .description.as_deref().map(|d| format!(" ({d})")).unwrap_or_default())]
    OAuthRedirect {
        error: String,
        description: Option<String>,
    },

    #[error("Timeout waiting for authentication")]
    AuthTimeout,

    #[error("User cancelled authentication")]
    AuthCancelled,

    #[error("{0}")]
    Other(String),
}

pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    pub fn config(msg: impl Into<String>) -> Self {
        Self::Config(msg.into())
    }

    /// An auth error whose fix is signing in again to a saved context.
    pub fn auth_required(context: &str, reason: impl Into<String>) -> Self {
        Self::AuthRequired {
            reason: reason.into(),
            fix: format!("logchef auth --context {}", shell_quote(context)),
        }
    }

    pub fn api(status: Option<u16>, msg: impl Into<String>) -> Self {
        Self::Api {
            status,
            message: msg.into(),
            error_type: None,
        }
    }

    pub fn api_with_type(
        status: Option<u16>,
        msg: impl Into<String>,
        error_type: Option<String>,
    ) -> Self {
        Self::Api {
            status,
            message: msg.into(),
            error_type,
        }
    }

    pub fn oauth(msg: impl Into<String>) -> Self {
        Self::OAuth(msg.into())
    }

    pub fn other(msg: impl Into<String>) -> Self {
        Self::Other(msg.into())
    }
}

/// Quotes a value for POSIX shells only when it needs quoting, so the common
/// case (`logchef auth --context prod`) stays easy to read and copy.
pub fn shell_quote(value: &str) -> String {
    let plain = !value.is_empty()
        && !value.starts_with('-')
        && value
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '.' | '_' | '-' | ':' | '/'));
    if plain {
        value.to_string()
    } else {
        format!("'{}'", value.replace('\'', "'\\''"))
    }
}
