//! Shared helpers for the CLI integration tests: an isolated HOME, the
//! `logchef` binary run with a cleared environment, and a small in-test
//! Logchef server that speaks the OAuth contract of the real one (phase 3):
//! metadata, code exchange with PKCE S256, refresh rotation with replay
//! detection, revocation, and `/api/v1/me`.

#![allow(dead_code)]

use std::collections::HashMap;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Output, Stdio};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

pub const PAT: &str = "logchef_test_pat";
pub const CODE: &str = "test-code";
pub const READ_SCOPES: &str = "profile:read teams:read sources:read logs:read saved_queries:read collections:read alerts:read";

/// An isolated HOME with a fake browser launcher on PATH. Any attempt to open
/// a browser writes to `browser_log`.
pub struct TestHome {
    pub root: PathBuf,
}

impl TestHome {
    pub fn new(name: &str) -> Self {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let root = PathBuf::from(env!("CARGO_TARGET_TMPDIR"))
            .join(format!("{name}-{}-{nanos}", std::process::id()));
        let bin = root.join("bin");
        std::fs::create_dir_all(&bin).unwrap();
        let home = Self { root };
        for launcher in [
            "xdg-open",
            "gio",
            "gnome-open",
            "kde-open",
            "wslview",
            "x-www-browser",
            "www-browser",
            "sensible-browser",
            "firefox",
        ] {
            let script = bin.join(launcher);
            std::fs::write(
                &script,
                format!(
                    "#!/bin/sh\necho \"{launcher} $*\" >> '{}'\n",
                    home.browser_log().display()
                ),
            )
            .unwrap();
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&script, std::fs::Permissions::from_mode(0o755)).unwrap();
        }
        home
    }

    pub fn config_dir(&self) -> PathBuf {
        self.root.join(".config").join("logchef")
    }

    pub fn config_path(&self) -> PathBuf {
        self.config_dir().join("logchef.json")
    }

    pub fn browser_log(&self) -> PathBuf {
        self.root.join("browser.log")
    }

    pub fn browser_opened(&self) -> bool {
        self.browser_log().exists()
    }

    pub fn write_config(&self, config: &serde_json::Value) {
        std::fs::create_dir_all(self.config_dir()).unwrap();
        std::fs::write(
            self.config_path(),
            serde_json::to_string_pretty(config).unwrap(),
        )
        .unwrap();
    }

    pub fn read_config(&self) -> serde_json::Value {
        serde_json::from_str(&std::fs::read_to_string(self.config_path()).unwrap()).unwrap()
    }

    /// `logchef` with only HOME and the fake PATH set: no DISPLAY, WAYLAND,
    /// DBUS, BROWSER, or CI, and stdin is not a terminal.
    pub fn logchef(&self) -> Command {
        self.command(Path::new(env!("CARGO_BIN_EXE_logchef")))
    }

    /// `program` with the same cleared environment as [`Self::logchef`].
    pub fn command(&self, program: &Path) -> Command {
        let mut cmd = Command::new(program);
        cmd.env_clear()
            .env("HOME", &self.root)
            .env("PATH", self.root.join("bin"))
            .env("LOGCHEF_NO_UPDATE_CHECK", "1")
            .stdin(Stdio::null());
        cmd
    }
}

/// Runs a command to completion, failing the test if it hangs.
pub fn run(mut cmd: Command) -> Output {
    let child = cmd
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    wait_with_timeout(child, Duration::from_secs(30))
}

pub fn wait_with_timeout(mut child: Child, limit: Duration) -> Output {
    let deadline = Instant::now() + limit;
    loop {
        if child.try_wait().unwrap().is_some() {
            return child.wait_with_output().unwrap();
        }
        if Instant::now() > deadline {
            let _ = child.kill();
            let out = child.wait_with_output().unwrap();
            panic!(
                "command did not finish within {limit:?}\nstdout: {}\nstderr: {}",
                String::from_utf8_lossy(&out.stdout),
                String::from_utf8_lossy(&out.stderr)
            );
        }
        std::thread::sleep(Duration::from_millis(20));
    }
}

/// The OAuth parameters the CLI put in its authorization URL.
pub struct AuthorizeRequest {
    pub params: HashMap<String, String>,
}

impl AuthorizeRequest {
    pub fn parse(line: &str) -> Option<Self> {
        let start = line.find("http")?;
        let url = url::Url::parse(line[start..].trim()).ok()?;
        if url.path() != "/oauth/authorize" {
            return None;
        }
        Some(Self {
            params: url.query_pairs().into_owned().collect(),
        })
    }

    pub fn get(&self, key: &str) -> &str {
        self.params
            .get(key)
            .unwrap_or_else(|| panic!("authorization URL has no {key}"))
    }
}

/// Starts `cmd`, reads its output until the authorization URL appears, and
/// returns the child, the request, and the remaining output lines.
pub fn start_login(mut cmd: Command) -> (Child, AuthorizeRequest, mpsc::Receiver<String>) {
    let mut child = cmd
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    let (tx, rx) = mpsc::channel();
    let stderr = child.stderr.take().unwrap();
    let stdout = child.stdout.take().unwrap();
    for stream in [
        Box::new(stderr) as Box<dyn Read + Send>,
        Box::new(stdout) as Box<dyn Read + Send>,
    ] {
        let tx = tx.clone();
        std::thread::spawn(move || {
            for line in BufReader::new(stream).lines().map_while(Result::ok) {
                let _ = tx.send(line);
            }
        });
    }
    let deadline = Instant::now() + Duration::from_secs(20);
    let mut seen = Vec::new();
    while Instant::now() < deadline {
        if let Ok(line) = rx.recv_timeout(Duration::from_millis(100)) {
            if let Some(request) = AuthorizeRequest::parse(&line) {
                return (child, request, rx);
            }
            seen.push(line);
        }
    }
    let _ = child.kill();
    let _ = child.wait();
    panic!("no authorization URL in output: {seen:#?}");
}

/// Sends a GET as the browser would, and returns the raw response.
pub fn browser_get(url: &str) -> String {
    let url = url::Url::parse(url).unwrap();
    let addr = format!("{}:{}", url.host_str().unwrap(), url.port().unwrap());
    let mut stream = TcpStream::connect(addr).unwrap();
    let target = match url.query() {
        Some(q) => format!("{}?{q}", url.path()),
        None => url.path().to_string(),
    };
    write!(
        stream,
        "GET {target} HTTP/1.1\r\nHost: {}\r\nConnection: close\r\n\r\n",
        url.host_str().unwrap()
    )
    .unwrap();
    let mut response = String::new();
    stream.read_to_string(&mut response).unwrap();
    response
}

#[derive(Default)]
pub struct ServerState {
    pub oauth_enabled: bool,
    pub expires_in: i64,
    /// Registered by the test, acting as the browser, from the authorize URL.
    pub challenge: Option<String>,
    pub redirect_uri: Option<String>,
    pub access_token: Option<String>,
    pub refresh_token: Option<String>,
    pub generation: u32,
    pub refresh_calls: u32,
    pub grant_revoked: bool,
    pub revoked: Vec<String>,
    pub fail_revoke: bool,
    /// Holds each refresh response this long, to widen race windows.
    pub refresh_delay: Duration,
    pub bearer_seen: Vec<String>,
    /// Advertised as `ui_url` in /api/v1/meta when set.
    pub ui_url: Option<String>,
}

pub struct FakeServer {
    pub url: String,
    pub state: Arc<Mutex<ServerState>>,
}

impl FakeServer {
    pub fn start(oauth_enabled: bool) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://127.0.0.1:{}", listener.local_addr().unwrap().port());
        let state = Arc::new(Mutex::new(ServerState {
            oauth_enabled,
            expires_in: 600,
            ..Default::default()
        }));
        let shared = Arc::clone(&state);
        let base = url.clone();
        std::thread::spawn(move || {
            for stream in listener.incoming().map_while(Result::ok) {
                let state = Arc::clone(&shared);
                let base = base.clone();
                std::thread::spawn(move || handle(stream, &base, &state));
            }
        });
        Self { url, state }
    }

    pub fn state(&self) -> std::sync::MutexGuard<'_, ServerState> {
        self.state.lock().unwrap()
    }

    /// Issues a grant directly, as if a login had completed, and returns the
    /// credential JSON a CLI context stores for it.
    pub fn issue_grant(&self, access_expired: bool) -> serde_json::Value {
        let mut state = self.state();
        state.generation += 1;
        let access = format!("access-{}", state.generation);
        let refresh = format!("refresh-{}", state.generation);
        state.access_token = Some(access.clone());
        state.refresh_token = Some(refresh.clone());
        let expires = if access_expired {
            "2020-01-01T00:00:00Z"
        } else {
            "2099-01-01T00:00:00Z"
        };
        serde_json::json!({
            "issuer": self.url,
            "client_id": "logchef-cli",
            "resource": format!("{}/api", self.url),
            "scopes": READ_SCOPES.split(' ').collect::<Vec<_>>(),
            "access_token": access,
            "access_expires_at": expires,
            "refresh_token": refresh,
        })
    }
}

/// A config with one context named `test` for `server_url`.
pub fn config_with(server_url: &str, auth: serde_json::Value) -> serde_json::Value {
    let mut ctx = serde_json::json!({
        "server_url": server_url,
        "timeout_secs": 10,
        "defaults": {"limit": 100, "since": "15m"}
    });
    if let serde_json::Value::Object(fields) = auth {
        for (k, v) in fields {
            ctx[k] = v;
        }
    }
    serde_json::json!({
        "version": 1,
        "current_context": "test",
        "contexts": {"test": ctx}
    })
}

struct Request {
    method: String,
    path: String,
    headers: HashMap<String, String>,
    body: String,
}

fn read_request(stream: &mut TcpStream) -> Option<Request> {
    stream
        .set_read_timeout(Some(Duration::from_secs(10)))
        .ok()?;
    let mut reader = BufReader::new(stream.try_clone().ok()?);
    let mut line = String::new();
    reader.read_line(&mut line).ok()?;
    let mut parts = line.split_whitespace();
    let method = parts.next()?.to_string();
    let path = parts.next()?.to_string();
    let mut headers = HashMap::new();
    loop {
        let mut header = String::new();
        reader.read_line(&mut header).ok()?;
        let header = header.trim_end();
        if header.is_empty() {
            break;
        }
        let (k, v) = header.split_once(':')?;
        headers.insert(k.trim().to_ascii_lowercase(), v.trim().to_string());
    }
    let len: usize = headers
        .get("content-length")
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    let mut body = vec![0; len];
    reader.read_exact(&mut body).ok()?;
    Some(Request {
        method,
        path,
        headers,
        body: String::from_utf8(body).ok()?,
    })
}

fn respond(stream: &mut TcpStream, status: u16, body: &serde_json::Value) {
    let body = body.to_string();
    let _ = write!(
        stream,
        "HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    );
}

fn handle(mut stream: TcpStream, base: &str, state: &Mutex<ServerState>) {
    let Some(req) = read_request(&mut stream) else {
        return;
    };
    let form: HashMap<String, String> = url::form_urlencoded::parse(req.body.as_bytes())
        .into_owned()
        .collect();
    let (status, body) = route(&req, &form, base, state);
    respond(&mut stream, status, &body);
}

fn oauth_error(error: &str) -> (u16, serde_json::Value) {
    (400, serde_json::json!({"error": error}))
}

fn route(
    req: &Request,
    form: &HashMap<String, String>,
    base: &str,
    state: &Mutex<ServerState>,
) -> (u16, serde_json::Value) {
    let path = req.path.split('?').next().unwrap_or_default();
    match (req.method.as_str(), path) {
        ("GET", "/api/v1/meta") => {
            let mut data = serde_json::json!({"version": "test"});
            let s = state.lock().unwrap();
            if s.oauth_enabled {
                data["oauth_issuer"] = base.into();
            }
            if let Some(ui_url) = &s.ui_url {
                data["ui_url"] = ui_url.as_str().into();
            }
            (200, serde_json::json!({"status": "success", "data": data}))
        }
        ("GET", "/.well-known/oauth-authorization-server") => (
            200,
            serde_json::json!({
                "issuer": base,
                "authorization_endpoint": format!("{base}/oauth/authorize"),
                "token_endpoint": format!("{base}/oauth/token"),
                "revocation_endpoint": format!("{base}/oauth/revoke"),
                "response_types_supported": ["code"],
                "grant_types_supported": ["authorization_code", "refresh_token"],
                "code_challenge_methods_supported": ["S256"],
                "token_endpoint_auth_methods_supported": ["none"],
                "authorization_response_iss_parameter_supported": true,
            }),
        ),
        ("POST", "/oauth/token") => token(form, base, state),
        ("POST", "/oauth/revoke") => {
            let mut s = state.lock().unwrap();
            if s.fail_revoke {
                return (503, serde_json::json!({"error": "temporarily_unavailable"}));
            }
            if let Some(token) = form.get("token") {
                s.revoked.push(token.clone());
                if s.refresh_token.as_ref() == Some(token) || s.access_token.as_ref() == Some(token)
                {
                    s.grant_revoked = true;
                }
            }
            (200, serde_json::json!({}))
        }
        ("GET", "/api/v1/me") | ("GET", "/api/v1/me/teams") => {
            let bearer = req
                .headers
                .get("authorization")
                .and_then(|h| h.strip_prefix("Bearer "))
                .unwrap_or_default()
                .to_string();
            let mut s = state.lock().unwrap();
            s.bearer_seen.push(bearer.clone());
            let auth = if bearer == PAT {
                serde_json::json!({"method": "token", "scopes": ["*"], "expires_at": null})
            } else if !s.grant_revoked && s.access_token.as_deref() == Some(bearer.as_str()) {
                serde_json::json!({
                    "method": "oauth",
                    "scopes": READ_SCOPES.split(' ').collect::<Vec<_>>(),
                    "expires_at": "2099-01-01T00:00:00Z",
                    "client_id": "logchef-cli",
                })
            } else {
                return (
                    401,
                    serde_json::json!({"status": "error", "message": "Invalid token", "error_type": "AuthenticationError"}),
                );
            };
            if path == "/api/v1/me/teams" {
                return (200, serde_json::json!({"status": "success", "data": []}));
            }
            (
                200,
                serde_json::json!({"status": "success", "data": {
                    "user": {"id": 1, "email": "user@example.com", "full_name": "Customer A", "role": "member"},
                    "auth_method": auth["method"],
                    "auth": auth,
                }}),
            )
        }
        _ => (
            404,
            serde_json::json!({"status": "error", "message": "not found"}),
        ),
    }
}

fn token(
    form: &HashMap<String, String>,
    base: &str,
    state: &Mutex<ServerState>,
) -> (u16, serde_json::Value) {
    if form.get("client_id").map(String::as_str) != Some("logchef-cli") {
        return (401, serde_json::json!({"error": "invalid_client"}));
    }
    if form.get("resource").map(String::as_str) != Some(format!("{base}/api").as_str()) {
        return oauth_error("invalid_target");
    }
    let mut s = state.lock().unwrap();
    let delay = match form.get("grant_type").map(String::as_str) {
        Some("authorization_code") => {
            let challenge = {
                use base64::Engine;
                use sha2::Digest;
                let verifier = form.get("code_verifier").cloned().unwrap_or_default();
                base64::engine::general_purpose::URL_SAFE_NO_PAD
                    .encode(sha2::Sha256::digest(verifier.as_bytes()))
            };
            if form.get("code").map(String::as_str) != Some(CODE)
                || s.challenge.as_deref() != Some(challenge.as_str())
                || s.redirect_uri.as_ref() != form.get("redirect_uri")
            {
                return oauth_error("invalid_grant");
            }
            Duration::ZERO
        }
        Some("refresh_token") => {
            s.refresh_calls += 1;
            if s.grant_revoked || s.refresh_token.as_ref() != form.get("refresh_token") {
                // Replay of a rotated refresh token revokes the grant.
                s.grant_revoked = true;
                return oauth_error("invalid_grant");
            }
            s.refresh_delay
        }
        _ => return oauth_error("unsupported_grant_type"),
    };
    // Rotate before the delay, so a second refresh with the same token
    // during the delay is a replay.
    s.generation += 1;
    let access = format!("access-{}", s.generation);
    let refresh = format!("refresh-{}", s.generation);
    s.access_token = Some(access.clone());
    s.refresh_token = Some(refresh.clone());
    let expires_in = s.expires_in;
    drop(s);
    std::thread::sleep(delay);
    (
        200,
        serde_json::json!({
            "access_token": access,
            "token_type": "Bearer",
            "expires_in": expires_in,
            "refresh_token": refresh,
            "scope": format!("{READ_SCOPES} offline_access"),
        }),
    )
}

pub fn assert_success(out: &Output) {
    assert!(
        out.status.success(),
        "exit {:?}\nstdout: {}\nstderr: {}",
        out.status.code(),
        String::from_utf8_lossy(&out.stdout),
        String::from_utf8_lossy(&out.stderr)
    );
}

pub fn stderr(out: &Output) -> String {
    String::from_utf8_lossy(&out.stderr).into_owned()
}

pub fn stdout(out: &Output) -> String {
    String::from_utf8_lossy(&out.stdout).into_owned()
}

pub fn mode(path: &Path) -> u32 {
    use std::os::unix::fs::PermissionsExt;
    std::fs::metadata(path).unwrap().permissions().mode() & 0o777
}
