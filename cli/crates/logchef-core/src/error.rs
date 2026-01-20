use thiserror::Error;

#[derive(Error, Debug)]
pub enum Error {
    #[error("Configuration error: {0}")]
    Config(String),

    #[error("Not authenticated. Run 'logchef auth' to log in.")]
    NotAuthenticated,

    #[error("Authentication failed: {0}")]
    Auth(String),

    #[error("API error: {message}")]
    Api {
        status: Option<u16>,
        message: String,
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

    pub fn auth(msg: impl Into<String>) -> Self {
        Self::Auth(msg.into())
    }

    pub fn api(status: Option<u16>, msg: impl Into<String>) -> Self {
        Self::Api {
            status,
            message: msg.into(),
        }
    }

    pub fn oauth(msg: impl Into<String>) -> Self {
        Self::OAuth(msg.into())
    }

    pub fn other(msg: impl Into<String>) -> Self {
        Self::Other(msg.into())
    }
}
