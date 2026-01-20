use crate::api::Client;
use crate::error::{Error, Result};
use std::collections::HashMap;
use std::io::{BufRead, BufReader, Write};
use std::net::TcpListener;
use std::sync::mpsc;
use std::time::Duration;
use tracing::{debug, info};
use url::Url;

const CALLBACK_TIMEOUT: Duration = Duration::from_secs(300);

pub struct AuthFlow {
    server_url: String,
    oidc_issuer: String,
    client_id: String,
}

pub struct AuthResult {
    pub token: String,
    pub expires_at: Option<chrono::DateTime<chrono::Utc>>,
    pub user_email: Option<String>,
}

impl AuthFlow {
    pub fn new(server_url: String, oidc_issuer: String, client_id: String) -> Self {
        Self {
            server_url,
            oidc_issuer,
            client_id,
        }
    }

    pub async fn run(&self) -> Result<AuthResult> {
        let listener = TcpListener::bind("127.0.0.1:19876")
            .or_else(|_| TcpListener::bind("127.0.0.1:19877"))
            .or_else(|_| TcpListener::bind("127.0.0.1:19878"))
            .or_else(|_| TcpListener::bind("127.0.0.1:0"))
            .map_err(|e| Error::auth(format!("Failed to start callback server: {}", e)))?;

        let port = listener
            .local_addr()
            .map_err(|e| Error::auth(format!("Failed to get callback port: {}", e)))?
            .port();

        let redirect_url = format!("http://127.0.0.1:{}/callback", port);
        debug!(redirect_url = %redirect_url, "Callback server listening");

        let (pkce_verifier, pkce_challenge) = generate_pkce();
        let state = generate_state();

        let oidc_config = self.discover_oidc_config().await?;

        let auth_url = format!(
            "{}?client_id={}&redirect_uri={}&response_type=code&scope={}&state={}&code_challenge={}&code_challenge_method=S256",
            oidc_config.authorization_endpoint,
            urlencoding::encode(&self.client_id),
            urlencoding::encode(&redirect_url),
            urlencoding::encode("openid email profile"),
            &state,
            &pkce_challenge,
        );

        info!("Opening browser for authentication...");
        println!("\nOpening browser for authentication...");
        println!("If the browser doesn't open automatically, visit:");
        println!("  {}\n", auth_url);

        if let Err(e) = open::that(&auth_url) {
            debug!(error = %e, "Failed to open browser automatically");
        }

        let (tx, rx) = mpsc::channel();
        let expected_state = state.clone();

        std::thread::spawn(move || {
            listener
                .set_nonblocking(false)
                .expect("Cannot set blocking");

            for stream in listener.incoming() {
                match stream {
                    Ok(mut stream) => {
                        let mut reader = BufReader::new(stream.try_clone().unwrap());
                        let mut request_line = String::new();
                        if reader.read_line(&mut request_line).is_err() {
                            continue;
                        }

                        let path = match request_line.split_whitespace().nth(1) {
                            Some(p) => p,
                            None => continue,
                        };

                        let full_url = format!("http://127.0.0.1{}", path);
                        let url = match Url::parse(&full_url) {
                            Ok(u) => u,
                            Err(_) => continue,
                        };

                        let code = url
                            .query_pairs()
                            .find(|(k, _)| k == "code")
                            .map(|(_, v)| v.to_string());
                        let received_state = url
                            .query_pairs()
                            .find(|(k, _)| k == "state")
                            .map(|(_, v)| v.to_string());

                        let response_html = if code.is_some() {
                            r#"<!DOCTYPE html>
<html>
<head><title>LogChef CLI</title></head>
<body style="font-family: system-ui; text-align: center; padding-top: 50px;">
<h1>Authentication Successful!</h1>
<p>You can close this window and return to the terminal.</p>
</body>
</html>"#
                        } else {
                            r#"<!DOCTYPE html>
<html>
<head><title>LogChef CLI</title></head>
<body style="font-family: system-ui; text-align: center; padding-top: 50px;">
<h1>Authentication Failed</h1>
<p>Please try again.</p>
</body>
</html>"#
                        };

                        let response = format!(
                            "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                            response_html.len(),
                            response_html
                        );
                        let _ = stream.write_all(response.as_bytes());
                        let _ = stream.flush();

                        if let (Some(code), Some(state)) = (code, received_state) {
                            let _ = tx.send((code, state));
                            break;
                        }
                    }
                    Err(_) => continue,
                }
            }
        });

        let (code, received_state) = rx
            .recv_timeout(CALLBACK_TIMEOUT)
            .map_err(|_| Error::AuthTimeout)?;

        if received_state != expected_state {
            return Err(Error::auth("CSRF state mismatch"));
        }

        info!("Received authorization code, exchanging for token...");

        let token_response = self
            .exchange_code_for_tokens(
                &oidc_config.token_endpoint,
                &code,
                &redirect_url,
                &pkce_verifier,
            )
            .await?;

        let id_token = token_response
            .get("id_token")
            .and_then(|v| v.as_str())
            .ok_or_else(|| Error::oauth("No ID token in response"))?;

        info!("Exchanging OIDC token for LogChef API token...");

        let api_client = Client::new(&self.server_url, 30)?;
        let exchange_response = api_client.exchange_token(id_token).await?;

        Ok(AuthResult {
            token: exchange_response.token,
            expires_at: exchange_response.expires_at,
            user_email: exchange_response.user.map(|u| u.email),
        })
    }

    async fn discover_oidc_config(&self) -> Result<OidcConfig> {
        let discovery_url = format!(
            "{}/.well-known/openid-configuration",
            self.oidc_issuer.trim_end_matches('/')
        );

        debug!(url = %discovery_url, "Discovering OIDC configuration");

        let response = reqwest::get(&discovery_url)
            .await
            .map_err(|e| Error::oauth(format!("Failed to fetch OIDC configuration: {}", e)))?;

        if !response.status().is_success() {
            return Err(Error::oauth(format!(
                "OIDC discovery failed with status {}",
                response.status()
            )));
        }

        response
            .json::<OidcConfig>()
            .await
            .map_err(|e| Error::oauth(format!("Failed to parse OIDC configuration: {}", e)))
    }

    async fn exchange_code_for_tokens(
        &self,
        token_endpoint: &str,
        code: &str,
        redirect_uri: &str,
        pkce_verifier: &str,
    ) -> Result<HashMap<String, serde_json::Value>> {
        let client = reqwest::Client::new();

        let params = [
            ("grant_type", "authorization_code"),
            ("client_id", self.client_id.as_str()),
            ("code", code),
            ("redirect_uri", redirect_uri),
            ("code_verifier", pkce_verifier),
        ];

        let response = client
            .post(token_endpoint)
            .form(&params)
            .send()
            .await
            .map_err(|e| Error::oauth(format!("Token exchange request failed: {}", e)))?;

        if !response.status().is_success() {
            let body = response.text().await.unwrap_or_default();
            return Err(Error::oauth(format!("Token exchange failed: {}", body)));
        }

        response
            .json()
            .await
            .map_err(|e| Error::oauth(format!("Failed to parse token response: {}", e)))
    }
}

#[derive(Debug, serde::Deserialize)]
struct OidcConfig {
    authorization_endpoint: String,
    token_endpoint: String,
}

fn generate_pkce() -> (String, String) {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    use sha2::{Digest, Sha256};

    let mut verifier_bytes = [0u8; 32];
    getrandom::getrandom(&mut verifier_bytes).expect("Failed to generate random bytes");
    let verifier = URL_SAFE_NO_PAD.encode(verifier_bytes);

    let mut hasher = Sha256::new();
    hasher.update(verifier.as_bytes());
    let challenge = URL_SAFE_NO_PAD.encode(hasher.finalize());

    (verifier, challenge)
}

fn generate_state() -> String {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};

    let mut state_bytes = [0u8; 16];
    getrandom::getrandom(&mut state_bytes).expect("Failed to generate random bytes");
    URL_SAFE_NO_PAD.encode(state_bytes)
}
