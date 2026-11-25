mod alerts;
mod dashboards;
mod models;
mod reports;

pub use alerts::*;
pub use dashboards::*;
pub use models::*;
pub use reports::*;

use crate::auth::{self, SavedCredential};
use crate::error::{Error, INSUFFICIENT_SCOPE, Result};
use reqwest::Client as HttpClient;
use reqwest::header::{
    AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue, USER_AGENT, WWW_AUTHENTICATE,
};
use reqwest::{RequestBuilder, Response, StatusCode};
use serde::Deserialize;
use serde::de::DeserializeOwned;
use std::time::Duration;
use tokio::sync::Mutex;
use tracing::debug;

/// Reads `null` as the empty value: servers up to v2.8.2 send an empty list
/// or map as `null`.
fn null_as_default<'de, D, T>(deserializer: D) -> std::result::Result<T, D::Error>
where
    D: serde::Deserializer<'de>,
    T: serde::Deserialize<'de> + Default,
{
    Ok(Option::<T>::deserialize(deserializer)?.unwrap_or_default())
}

const USER_AGENT_VALUE: &str = concat!("logchef-cli/", env!("CARGO_PKG_VERSION"));

pub struct Client {
    http: HttpClient,
    base_url: String,
    credential: Credential,
}

enum Credential {
    None,
    /// A PAT or `--token`. Sent as is and never refreshed.
    Bearer(String),
    /// A saved OAuth grant. Refreshed at most once per client: before the
    /// first request when the access token has expired, or after a 401.
    OAuth(Box<Mutex<OAuthState>>),
}

struct OAuthState {
    saved: SavedCredential,
    refreshed: bool,
}

impl Client {
    fn complete_query_response(response: ApiResponse<QueryResponse>) -> Result<QueryResponse> {
        let query = response.data;
        if let Some(message) = query.error.as_deref() {
            return Err(Error::api_with_type(
                Some(200),
                message.to_owned(),
                Some("DatabaseError".to_owned()),
            ));
        }
        Ok(query)
    }

    pub fn new(server_url: &str, timeout_secs: u64) -> Result<Self> {
        let base_url = server_url.trim_end_matches('/').to_string();
        let timeout = Duration::from_secs(timeout_secs);

        let http = HttpClient::builder()
            .timeout(timeout)
            .build()
            .map_err(|e| Error::other(format!("Failed to create HTTP client: {}", e)))?;

        Ok(Self {
            http,
            base_url,
            credential: Credential::None,
        })
    }

    pub fn with_token(mut self, token: String) -> Self {
        self.credential = Credential::Bearer(token);
        self
    }

    pub fn with_oauth(mut self, saved: SavedCredential) -> Self {
        self.credential = Credential::OAuth(Box::new(Mutex::new(OAuthState {
            saved,
            refreshed: false,
        })));
        self
    }

    fn headers(token: Option<&str>) -> HeaderMap {
        let mut headers = HeaderMap::new();
        headers.insert(USER_AGENT, HeaderValue::from_static(USER_AGENT_VALUE));
        headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));

        if let Some(token) = token
            && let Ok(value) = HeaderValue::from_str(&format!("Bearer {}", token))
        {
            headers.insert(AUTHORIZATION, value);
        }

        headers
    }

    /// The bearer token for the next request. An expired OAuth access token
    /// is refreshed first, which uses up the client's one refresh.
    async fn bearer(&self) -> Result<Option<String>> {
        match &self.credential {
            Credential::None => Ok(None),
            Credential::Bearer(token) => Ok(Some(token.clone())),
            Credential::OAuth(state) => {
                let mut state = state.lock().await;
                if !state.refreshed && auth::needs_refresh(&state.saved.credential) {
                    refresh(&mut state).await?;
                }
                Ok(Some(state.saved.credential.access_token.clone()))
            }
        }
    }

    /// Sends the request built by `build` with the current bearer token. On a
    /// 401 with an OAuth grant it refreshes once and sends the request again.
    /// A 401 after that is an auth error that names the fix.
    async fn send(&self, build: impl Fn() -> RequestBuilder) -> Result<Response> {
        let token = self.bearer().await?;
        let response = build()
            .headers(Self::headers(token.as_deref()))
            .send()
            .await?;
        let (Credential::OAuth(state), Some(used)) = (&self.credential, token) else {
            return Ok(response);
        };
        if response.status() != StatusCode::UNAUTHORIZED {
            return Ok(response);
        }

        let token = {
            let mut state = state.lock().await;
            if state.saved.credential.access_token == used {
                if state.refreshed {
                    return Err(rejected(&state.saved.context));
                }
                refresh(&mut state).await?;
            }
            state.saved.credential.access_token.clone()
        };
        let response = build().headers(Self::headers(Some(&token))).send().await?;
        if response.status() == StatusCode::UNAUTHORIZED {
            let state = state.lock().await;
            return Err(rejected(&state.saved.context));
        }
        Ok(response)
    }

    async fn get<T: DeserializeOwned>(&self, path: &str) -> Result<T> {
        let url = format!("{}{}", self.base_url, path);
        debug!(url = %url, "GET request");

        let response = self.send(|| self.http.get(&url)).await?;

        self.handle_response(response).await
    }

    async fn post<T: DeserializeOwned, B: serde::Serialize>(
        &self,
        path: &str,
        body: &B,
    ) -> Result<T> {
        let url = format!("{}{}", self.base_url, path);
        debug!(url = %url, "POST request");

        let response = self.send(|| self.http.post(&url).json(body)).await?;

        self.handle_response(response).await
    }

    async fn handle_response<T: DeserializeOwned>(&self, response: reqwest::Response) -> Result<T> {
        let status_code = response.status().as_u16();
        if !response.status().is_success() {
            return Err(self.error_from(response).await);
        }

        let body = response.text().await?;

        // Streaming endpoints may have already committed a successful HTTP
        // status before the backend query fails. In that case the server keeps
        // the body valid by returning its standard top-level error envelope.
        // Surface that error directly instead of trying to deserialize it as a
        // success response and masking the useful message as "missing data".
        if let Ok(api_error) = serde_json::from_str::<ApiErrorResponse>(&body)
            && api_error.status == "error"
        {
            return Err(Error::api_with_type(
                Some(status_code),
                api_error.message,
                api_error.error_type,
            ));
        }

        serde_json::from_str(&body)
            .map_err(|e| Error::other(format!("Failed to parse response: {} (body: {})", e, body)))
    }

    /// The error for a failed response. A 403 for a missing scope becomes an
    /// error that names the scope and how to get a credential with it.
    async fn error_from(&self, response: Response) -> Error {
        let status = response.status().as_u16();
        let challenge = response
            .headers()
            .get(WWW_AUTHENTICATE)
            .and_then(|v| v.to_str().ok())
            .map(str::to_owned);
        let body = response.text().await.unwrap_or_default();
        let Ok(api_error) = serde_json::from_str::<ApiErrorResponse>(&body) else {
            return Error::api(Some(status), format!("HTTP {status}: {body}"));
        };
        if status == 403
            && let Some(scope) = denied_scope(challenge.as_deref(), &api_error.message)
        {
            return self.scope_error(scope, api_error).await;
        }
        Error::api_with_type(Some(status), api_error.message, api_error.error_type)
    }

    /// A saved OAuth sign-in keeps the scopes it was granted across refreshes,
    /// so it gets a read scope the server added later only by signing in
    /// again. It never holds another scope, and an API token never gains one,
    /// so those need a new API token. For a sign-in the server's message is
    /// left out: it says the same, or on servers before v2.8.2 names an API
    /// token the caller does not use.
    async fn scope_error(&self, scope: Option<String>, api_error: ApiErrorResponse) -> Error {
        let needs = scope
            .as_ref()
            .map_or("the scope this command needs".to_owned(), |s| {
                format!("the {s} scope")
            });
        let message = match &self.credential {
            Credential::OAuth(state) => {
                let saved = state.lock().await.saved.clone();
                if let Some(reason) = sign_in_lacks(&saved, scope.as_deref()).await {
                    return Error::auth_required(&saved.context, reason);
                }
                format!(
                    "A `logchef auth` sign-in cannot hold {needs}. Pass an API token that has it with --token or LOGCHEF_AUTH_TOKEN."
                )
            }
            Credential::Bearer(_) | Credential::None => format!(
                "{}. Create a new API token with {needs}: a token cannot gain scopes after it is created",
                api_error.message.trim_end_matches('.')
            ),
        };
        Error::api_with_type(Some(403), message, Some(INSUFFICIENT_SCOPE.to_owned()))
    }

    pub async fn get_meta(&self) -> Result<MetaResponse> {
        let response: ApiResponse<MetaData> = self.get("/api/v1/meta").await?;
        Ok(MetaResponse {
            status: response.status,
            data: response.data,
        })
    }

    pub async fn get_current_user(&self) -> Result<User> {
        Ok(self.get_me().await?.user)
    }

    /// `/api/v1/me`, including how the caller authenticated.
    pub async fn get_me(&self) -> Result<UserData> {
        let response: ApiResponse<UserData> = self.get("/api/v1/me").await?;
        Ok(response.data)
    }

    pub async fn list_teams(&self) -> Result<Vec<Team>> {
        let response: ApiResponse<Vec<Team>> = self.get("/api/v1/me/teams").await?;
        Ok(response.data)
    }

    pub async fn list_sources(&self, team_id: i64) -> Result<Vec<Source>> {
        let response: ApiResponse<Vec<Source>> = self
            .get(&format!("/api/v1/teams/{}/sources", team_id))
            .await?;
        Ok(response.data)
    }

    /// Fetches full source detail (including the configured `_meta_ts_field`),
    /// as opposed to `list_sources` which is used for name/ID resolution.
    pub async fn get_source(&self, team_id: i64, source_id: i64) -> Result<Source> {
        let response: ApiResponse<Source> = self
            .get(&format!("/api/v1/teams/{}/sources/{}", team_id, source_id))
            .await?;
        Ok(response.data)
    }

    pub async fn get_schema(&self, team_id: i64, source_id: i64) -> Result<Vec<Column>> {
        let response: ApiResponse<Vec<Column>> = self
            .get(&format!(
                "/api/v1/teams/{}/sources/{}/schema",
                team_id, source_id
            ))
            .await?;
        Ok(response.data)
    }

    pub async fn query_logchefql(
        &self,
        team_id: i64,
        source_id: i64,
        request: &QueryRequest,
    ) -> Result<QueryResponse> {
        let response: ApiResponse<QueryResponse> = self
            .post(
                &format!(
                    "/api/v1/teams/{}/sources/{}/logchefql/query",
                    team_id, source_id
                ),
                request,
            )
            .await?;
        Self::complete_query_response(response)
    }

    pub async fn translate_logchefql(
        &self,
        team_id: i64,
        source_id: i64,
        request: &TranslateRequest,
    ) -> Result<TranslateResponse> {
        let response: ApiResponse<TranslateResponse> = self
            .post(
                &format!(
                    "/api/v1/teams/{}/sources/{}/logchefql/translate",
                    team_id, source_id
                ),
                request,
            )
            .await?;
        Ok(response.data)
    }

    pub async fn validate_logchefql(
        &self,
        team_id: i64,
        source_id: i64,
        request: &ValidateRequest,
    ) -> Result<ValidateResponse> {
        let response: ApiResponse<ValidateResponse> = self
            .post(
                &format!(
                    "/api/v1/teams/{}/sources/{}/logchefql/validate",
                    team_id, source_id
                ),
                request,
            )
            .await?;
        Ok(response.data)
    }

    pub async fn get_histogram(
        &self,
        team_id: i64,
        source_id: i64,
        request: &HistogramRequest,
    ) -> Result<HistogramResponse> {
        let response: ApiResponse<HistogramResponse> = self
            .post(
                &format!(
                    "/api/v1/teams/{}/sources/{}/logs/histogram",
                    team_id, source_id
                ),
                request,
            )
            .await?;
        Ok(response.data)
    }

    /// Fetches observed values for a single field within a time range.
    pub async fn get_field_values(
        &self,
        team_id: i64,
        source_id: i64,
        query: &FieldValuesQuery<'_>,
    ) -> Result<FieldValuesResult> {
        let path = format!(
            "/api/v1/teams/{}/sources/{}/fields/{}/values?type={}&start_time={}&end_time={}&timezone={}&limit={}",
            team_id,
            source_id,
            urlencoding::encode(query.field_name),
            urlencoding::encode(query.field_type),
            urlencoding::encode(query.start),
            urlencoding::encode(query.end),
            urlencoding::encode(query.timezone),
            query.limit,
        );
        let response: ApiResponse<FieldValuesResult> = self.get(&path).await?;
        Ok(response.data)
    }

    pub async fn query_sql(
        &self,
        team_id: i64,
        source_id: i64,
        request: &SqlQueryRequest,
    ) -> Result<QueryResponse> {
        let response: ApiResponse<QueryResponse> = self
            .post(
                &format!("/api/v1/teams/{}/sources/{}/logs/query", team_id, source_id),
                request,
            )
            .await?;
        Self::complete_query_response(response)
    }

    pub async fn export_sql(
        &self,
        team_id: i64,
        source_id: i64,
        request: &ExportSqlRequest,
    ) -> Result<reqwest::Response> {
        let url = format!(
            "{}/api/v1/teams/{}/sources/{}/logs/export",
            self.base_url, team_id, source_id
        );
        debug!(url = %url, "POST stream request");

        let response = self.send(|| self.http.post(&url).json(request)).await?;

        if !response.status().is_success() {
            return Err(self.error_from(response).await);
        }

        Ok(response)
    }

    /// Opens the native live-tail Server-Sent Events stream
    /// (`GET .../logs/tail`). The server handles ClickHouse polling and
    /// VictoriaLogs native streaming internally; the caller reads SSE frames
    /// off the returned response body. `query_language` may be empty (the
    /// server defaults to LogchefQL), `"logchefql"`, or `"logsql"`.
    ///
    /// A dedicated HTTP client with NO total-request timeout is used: an SSE
    /// stream is long-lived and would otherwise be aborted by the shared
    /// client's `timeout`. A connect timeout still guards the handshake.
    pub async fn tail_stream(
        &self,
        team_id: i64,
        source_id: i64,
        query: &str,
        query_language: &str,
    ) -> Result<reqwest::Response> {
        let url = format!(
            "{}/api/v1/teams/{}/sources/{}/logs/tail?query={}&query_language={}",
            self.base_url,
            team_id,
            source_id,
            urlencoding::encode(query),
            urlencoding::encode(query_language),
        );
        debug!(url = %url, "GET tail SSE stream");

        let http = HttpClient::builder()
            .connect_timeout(Duration::from_secs(30))
            .build()
            .map_err(|e| Error::other(format!("Failed to build tail client: {}", e)))?;

        let response = self.send(|| http.get(&url)).await?;

        if !response.status().is_success() {
            return Err(self.error_from(response).await);
        }

        Ok(response)
    }

    pub async fn create_export_job(
        &self,
        team_id: i64,
        source_id: i64,
        request: &ExportSqlRequest,
    ) -> Result<ExportJobResponse> {
        let response: ApiResponse<ExportJobResponse> = self
            .post(
                &format!("/api/v1/teams/{}/sources/{}/exports", team_id, source_id),
                request,
            )
            .await?;
        Ok(response.data)
    }

    pub async fn get_export_job(
        &self,
        team_id: i64,
        source_id: i64,
        export_id: &str,
    ) -> Result<ExportJobResponse> {
        let response: ApiResponse<ExportJobResponse> = self
            .get(&format!(
                "/api/v1/teams/{}/sources/{}/exports/{}",
                team_id, source_id, export_id
            ))
            .await?;
        Ok(response.data)
    }

    pub async fn download_export_job(
        &self,
        team_id: i64,
        source_id: i64,
        export_id: &str,
    ) -> Result<reqwest::Response> {
        let url = format!(
            "{}/api/v1/teams/{}/sources/{}/exports/{}/download",
            self.base_url, team_id, source_id, export_id
        );
        debug!(url = %url, "GET export download request");

        let response = self.send(|| self.http.get(&url)).await?;
        if !response.status().is_success() {
            return Err(self.error_from(response).await);
        }

        Ok(response)
    }

    pub async fn list_collections(&self, _team_id: i64, source_id: i64) -> Result<Vec<Collection>> {
        // v2.0: saved queries are no longer team-scoped. The team_id arg is
        // accepted for API compatibility with older callers but ignored —
        // visibility is computed server-side from the caller's team
        // membership.
        let response: ApiResponse<Vec<Collection>> = self
            .get(&format!("/api/v1/saved-queries?source_id={}", source_id))
            .await?;
        Ok(response.data)
    }

    pub async fn list_saved_queries(&self, source_id: Option<i64>) -> Result<Vec<Collection>> {
        let path = match source_id {
            Some(source_id) => format!("/api/v1/saved-queries?source_id={}", source_id),
            None => "/api/v1/saved-queries".to_string(),
        };
        let response: ApiResponse<Vec<Collection>> = self.get(&path).await?;
        Ok(response.data)
    }

    pub async fn list_saved_views(&self, team_id: i64, source_id: i64) -> Result<SavedViewList> {
        let response: ApiResponse<SavedViewList> = self
            .get(&format!(
                "/api/v1/teams/{team_id}/sources/{source_id}/views"
            ))
            .await?;
        Ok(response.data)
    }

    pub async fn get_saved_query(&self, query_id: i64) -> Result<Collection> {
        let response: ApiResponse<Collection> = self
            .get(&format!("/api/v1/saved-queries/{}", query_id))
            .await?;
        Ok(response.data)
    }

    pub async fn resolve_saved_query(
        &self,
        query_id: i64,
        team_id: Option<i64>,
    ) -> Result<ResolvedSavedQuery> {
        let path = match team_id {
            Some(team_id) => format!(
                "/api/v1/saved-queries/{}/resolve?team_id={}",
                query_id, team_id
            ),
            None => format!("/api/v1/saved-queries/{}/resolve", query_id),
        };
        let response: ApiResponse<ResolvedSavedQuery> = self.get(&path).await?;
        Ok(response.data)
    }

    /// Lists up to `limit` audit events, newest first, following the cursor
    /// across pages. Needs a global admin and, for an API token, the
    /// `audit:read` scope.
    pub async fn list_audit_events(
        &self,
        filter: &AuditEventFilter,
        limit: usize,
    ) -> Result<Vec<AuditEvent>> {
        let mut events = Vec::new();
        let mut cursor = String::new();
        while events.len() < limit {
            let page_size = (limit - events.len()).min(AUDIT_PAGE_MAX);
            let path = format!(
                "/api/v1/admin/audit-events?{}",
                audit_query(filter, page_size, &cursor)
            );
            let response: ApiResponse<AuditEventsPage> = self.get(&path).await?;
            events.extend(response.data.events);
            if response.data.next_cursor.is_empty() {
                break;
            }
            cursor = response.data.next_cursor;
        }
        events.truncate(limit);
        Ok(events)
    }

    /// Fetches the caller's recent query history, newest first. The server
    /// defaults `limit` to 50 and caps it at 200.
    pub async fn get_query_history(&self, limit: u32) -> Result<Vec<QueryHistoryEntry>> {
        let response: ApiResponse<Vec<QueryHistoryEntry>> = self
            .get(&format!("/api/v1/me/query-history?limit={}", limit))
            .await?;
        Ok(response.data)
    }

    /// Lists every source. Needs a global admin and, for an API token, the
    /// `sources:read` scope.
    pub async fn list_all_sources(&self) -> Result<Vec<Source>> {
        let response: ApiResponse<Vec<Source>> = self.get("/api/v1/admin/sources").await?;
        Ok(response.data)
    }

    /// The stored schema advisor report of a source, as the server sent it
    /// (a `SchemaAdvice`). Needs a global admin and, for an API token, the
    /// `sources:read` scope.
    pub async fn get_schema_advice(&self, source_id: i64) -> Result<serde_json::Value> {
        let response: ApiResponse<serde_json::Value> = self
            .get(&format!("/api/v1/admin/sources/{source_id}/advisor"))
            .await?;
        Ok(response.data)
    }

    /// Analyzes a source and returns the new report, as `get_schema_advice`
    /// does. Without `window_days` the server uses its default window.
    pub async fn refresh_schema_advice(
        &self,
        source_id: i64,
        window_days: Option<u32>,
    ) -> Result<serde_json::Value> {
        let body = match window_days {
            Some(days) => serde_json::json!({ "window_days": days }),
            None => serde_json::json!({}),
        };
        let response: ApiResponse<serde_json::Value> = self
            .post(
                &format!("/api/v1/admin/sources/{source_id}/advisor/refresh"),
                &body,
            )
            .await?;
        Ok(response.data)
    }
}

/// The server clamps a larger page to this.
const AUDIT_PAGE_MAX: usize = 500;

fn audit_query(filter: &AuditEventFilter, limit: usize, cursor: &str) -> String {
    let time =
        |t: &chrono::DateTime<chrono::Utc>| t.to_rfc3339_opts(chrono::SecondsFormat::Millis, true);
    let mut query = url::form_urlencoded::Serializer::new(String::new());
    let pairs = [
        ("from", filter.from.as_ref().map(time)),
        ("to", filter.to.as_ref().map(time)),
        ("action", filter.action.clone()),
        ("actor_id", filter.actor_id.map(|id| id.to_string())),
        ("target_type", filter.target_type.clone()),
        ("target_id", filter.target_id.clone()),
        ("outcome", filter.outcome.clone()),
        ("limit", Some(limit.to_string())),
        ("cursor", Some(cursor.to_owned()).filter(|c| !c.is_empty())),
    ];
    for (key, value) in pairs {
        if let Some(value) = value {
            query.append_pair(key, &value);
        }
    }
    query.finish()
}

/// Reads a 403 that denies a missing scope: `Some(Some(scope))` when the
/// `WWW-Authenticate` challenge or the message names the scope, `Some(None)`
/// when neither does (servers before v2.8.2), and `None` for any other 403.
fn denied_scope(challenge: Option<&str>, message: &str) -> Option<Option<String>> {
    let challenge = challenge.unwrap_or_default();
    let from_challenge = challenge
        .split(',')
        .filter_map(|param| param.split_once('='))
        .find(|(key, _)| {
            let key = key.trim();
            key == "scope" || key.ends_with(" scope")
        })
        .map(|(_, value)| value.trim().trim_matches('"').to_owned());
    let from_message = message
        .split_once("required scope ")
        .and_then(|(_, rest)| rest.split_whitespace().next())
        .or_else(|| {
            message
                .split_once("does not grant the ")
                .and_then(|(_, rest)| rest.split_once(" scope"))
                .map(|(scope, _)| scope)
        })
        .map(|scope| scope.trim_end_matches('.').to_owned());
    let denied = challenge.contains("insufficient_scope")
        || message.contains("required scope")
        || from_message.is_some();
    denied.then(|| from_challenge.or(from_message).filter(|s| !s.is_empty()))
}

/// Why a saved sign-in was denied, when signing in again would grant the
/// scope: the denied scope is a read scope the CLI requests, or the server
/// did not name it and the sign-in lacks a read scope the server offers.
async fn sign_in_lacks(saved: &SavedCredential, scope: Option<&str>) -> Option<String> {
    let lacks = format!(
        "The saved sign-in for context '{}' lacks a scope this command needs",
        saved.context
    );
    match scope {
        Some(scope) => scope
            .split_whitespace()
            .all(|s| auth::READ_SCOPES.contains(&s))
            .then(|| format!("{lacks}: it does not grant {scope}")),
        None => match auth::missing_read_scopes(&saved.credential).await {
            Ok(missing) if missing.is_empty() => None,
            Ok(missing) => Some(format!("{lacks}: it does not grant {}", missing.join(", "))),
            Err(err) => {
                debug!(error = %err, "Could not read the scopes the server offers");
                Some(lacks)
            }
        },
    }
}

/// Refreshes the grant and marks the client's one refresh as used.
async fn refresh(state: &mut OAuthState) -> Result<()> {
    state.refreshed = true;
    state.saved.credential = auth::refresh_context(
        &state.saved.config_path,
        &state.saved.context,
        &state.saved.credential.access_token,
    )
    .await?;
    Ok(())
}

fn rejected(context: &str) -> Error {
    Error::auth_required(
        context,
        format!("The server rejected the access token for context '{context}'"),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpListener;
    use std::thread;

    fn serve_once(body: &'static str) -> (String, thread::JoinHandle<()>) {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
        let address = listener.local_addr().expect("read test server address");

        let server = thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("accept test request");
            let mut request = [0_u8; 4096];
            let _ = stream.read(&mut request).expect("read test request");

            let response = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            stream
                .write_all(response.as_bytes())
                .expect("write test response");
        });

        (format!("http://{}", address), server)
    }

    /// Answers one request per body, in order, and returns each request line.
    fn serve_each(bodies: Vec<String>) -> (String, thread::JoinHandle<Vec<String>>) {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
        let address = listener.local_addr().expect("read test server address");
        let server = thread::spawn(move || {
            let mut lines = Vec::new();
            for body in bodies {
                let (mut stream, _) = listener.accept().expect("accept test request");
                let mut request = [0_u8; 4096];
                let n = stream.read(&mut request).expect("read test request");
                let text = String::from_utf8_lossy(&request[..n]);
                lines.push(text.lines().next().unwrap_or_default().to_owned());
                let response = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                    body.len(),
                    body
                );
                stream
                    .write_all(response.as_bytes())
                    .expect("write test response");
            }
            lines
        });
        (format!("http://{}", address), server)
    }

    fn audit_page(ids: std::ops::Range<usize>, next_cursor: &str) -> String {
        let events: Vec<serde_json::Value> = ids
            .map(|i| {
                serde_json::json!({
                    "id": format!("e{i}"), "occurred_at": "2026-11-16T10:00:00.123Z",
                    "action": "source.update", "outcome": "success", "reason": "",
                    "actor_type": "user", "actor_id": 1, "actor_label": "admin@example.com",
                    "auth_method": "api_token", "credential_id": "7", "client_id": "",
                    "target_type": "source", "target_id": "7", "target_label": "app",
                    "team_id": null, "source_id": 7, "ip": "192.0.2.1", "user_agent": "logchef-cli",
                    "http_method": "PUT", "route": "/api/v1/admin/sources/:sourceID", "status": 200,
                    "changes": {"ttl_days": {"from": 7, "to": 30}}, "detail": {}
                })
            })
            .collect();
        serde_json::json!({"status": "success", "data": {"events": events, "next_cursor": next_cursor}})
            .to_string()
    }

    #[tokio::test]
    async fn list_audit_events_follows_the_cursor_and_stops_at_the_limit() {
        let (server_url, server) =
            serve_each(vec![audit_page(0..500, "c1"), audit_page(500..700, "c2")]);
        let client = Client::new(&server_url, 5).expect("create client");
        let filter = AuditEventFilter {
            from: Some("2026-11-09T00:00:00+05:30".parse().expect("parse time")),
            action: Some("source.".to_owned()),
            target_type: Some("source".to_owned()),
            target_id: Some("7".to_owned()),
            ..AuditEventFilter::default()
        };

        let events = client
            .list_audit_events(&filter, 700)
            .await
            .expect("list audit events");

        assert_eq!(events.len(), 700);
        assert_eq!(events[699].id, "e699");
        assert_eq!(events[0].changes["ttl_days"]["to"], 30);
        let lines = server.join().expect("join test server");
        assert_eq!(
            lines,
            [
                "GET /api/v1/admin/audit-events?from=2026-11-08T18%3A30%3A00.000Z&action=source.&target_type=source&target_id=7&limit=500 HTTP/1.1",
                "GET /api/v1/admin/audit-events?from=2026-11-08T18%3A30%3A00.000Z&action=source.&target_type=source&target_id=7&limit=200&cursor=c1 HTTP/1.1",
            ]
        );
    }

    #[tokio::test]
    async fn list_audit_events_stops_on_the_last_page() {
        let (server_url, server) = serve_each(vec![audit_page(0..3, "")]);
        let client = Client::new(&server_url, 5).expect("create client");

        let events = client
            .list_audit_events(&AuditEventFilter::default(), 100)
            .await
            .expect("list audit events");

        assert_eq!(events.len(), 3);
        let lines = server.join().expect("join test server");
        assert_eq!(lines, ["GET /api/v1/admin/audit-events?limit=100 HTTP/1.1"]);
    }

    #[tokio::test]
    async fn list_reports_and_runs_send_the_scope_and_limit() {
        let report = serde_json::json!({
            "id": 4, "name": "Daily 5xx",
            "source": {"kind": "saved_query", "name": "5xx by status", "saved_query_id": 9},
            "schedule": {"frequency": "weekly", "minute": 30, "hour": 9, "weekdays": [1, 5]},
            "timezone": "Asia/Kolkata", "time_range": "24h",
            "owner": null, "state": "active", "pause_reason": "",
            "next_run_at": "2026-11-20T04:00:00Z",
            "last_run": {"id": 12, "status": "sent", "reason": "", "started_at": "2026-11-17T04:00:01Z"},
            "can_edit": true
        });
        let run = serde_json::json!({
            "id": 12, "report_id": 4, "trigger": "schedule",
            "scheduled_for": "2026-11-17T04:00:00Z", "status": "partial", "reason": "panel_locked",
            "panels": [{"panel_id": "query", "title": "5xx", "status": "ok", "rows": 3}],
            "deliveries": [{"class": "user", "target": "7", "via": "team:3", "status": "sent", "attempts": 1}],
            "started_at": "2026-11-17T04:00:01Z"
        });
        let (server_url, server) = serve_each(vec![
            serde_json::json!({"status": "success", "data": [report]}).to_string(),
            serde_json::json!({"status": "success", "data": [run]}).to_string(),
        ]);
        let client = Client::new(&server_url, 5).expect("create client");

        let reports = client.list_reports("received").await.expect("list reports");
        assert_eq!(reports[0].schedule.weekdays, [1, 5]);
        assert!(reports[0].owner.is_none());
        let runs = client.list_report_runs(4, 20).await.expect("list runs");
        assert_eq!(runs[0].deliveries[0].via, "team:3");

        let lines = server.join().expect("server thread");
        assert_eq!(
            lines,
            [
                "GET /api/v1/reports?scope=received HTTP/1.1",
                "GET /api/v1/reports/4/runs?limit=20 HTTP/1.1",
            ]
        );
    }

    #[test]
    fn denied_scope_reads_the_challenge_then_the_message() {
        let oauth =
            "This sign-in does not grant the saved_views:read scope. Sign in again to grant it.";
        let pat = "API token does not have the required scope saved_views:read";
        let older = "API token does not have the required scope";
        let cases = [
            (
                Some(r#"Bearer error="insufficient_scope", scope="saved_views:read""#),
                older,
                Some(Some("saved_views:read")),
            ),
            (None, oauth, Some(Some("saved_views:read"))),
            (None, pat, Some(Some("saved_views:read"))),
            (
                Some(r#"Bearer error="insufficient_scope""#),
                older,
                Some(None),
            ),
            (None, older, Some(None)),
            (None, "You do not have access to this source", None),
        ];
        for (challenge, message, expected) in cases {
            assert_eq!(
                denied_scope(challenge, message),
                expected.map(|s| s.map(str::to_owned)),
                "{challenge:?} {message}"
            );
        }
    }

    #[tokio::test]
    async fn handle_response_surfaces_error_envelope_with_success_http_status() {
        let (server_url, server) = serve_once(
            r#"{"status":"error","message":"backend query failed","error_type":"DatabaseError"}"#,
        );
        let client = Client::new(&server_url, 5).expect("create client");

        let result = client.get::<ApiResponse<serde_json::Value>>("/test").await;

        match result {
            Err(Error::Api {
                status,
                message,
                error_type,
            }) => {
                assert_eq!(status, Some(200));
                assert_eq!(message, "backend query failed");
                assert_eq!(error_type.as_deref(), Some("DatabaseError"));
            }
            other => panic!("expected API error, got {other:?}"),
        }

        server.join().expect("join test server");
    }

    #[tokio::test]
    async fn handle_response_still_parses_success_envelope() {
        let (server_url, server) = serve_once(r#"{"status":"success","data":{"value":42}}"#);
        let client = Client::new(&server_url, 5).expect("create client");

        let response: ApiResponse<serde_json::Value> =
            client.get("/test").await.expect("parse success response");

        assert_eq!(response.status, "success");
        assert_eq!(response.data["value"], 42);

        server.join().expect("join test server");
    }

    #[tokio::test]
    async fn query_surfaces_error_attached_after_partial_stream() {
        let (server_url, server) = serve_once(
            r#"{"status":"success","data":{"logs":[{"message":"partial row"}],"columns":[],"stats":{},"error":"stream interrupted"}}"#,
        );
        let client = Client::new(&server_url, 5).expect("create client");
        let request = QueryRequest {
            variables: Vec::new(),
            query: "".to_owned(),
            start_time: "2026-08-08 10:00:00".to_owned(),
            end_time: "2026-08-08 10:15:00".to_owned(),
            timezone: Some("UTC".to_owned()),
            limit: Some(100),
            query_timeout: Some(30),
            interval: None,
        };

        let result = client.query_logchefql(1, 2, &request).await;

        match result {
            Err(Error::Api {
                status,
                message,
                error_type,
            }) => {
                assert_eq!(status, Some(200));
                assert_eq!(message, "stream interrupted");
                assert_eq!(error_type.as_deref(), Some("DatabaseError"));
            }
            other => panic!("expected API error, got {other:?}"),
        }

        server.join().expect("join test server");
    }
}
