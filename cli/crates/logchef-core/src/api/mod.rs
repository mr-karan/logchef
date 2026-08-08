mod models;

pub use models::*;

use crate::config::Context;
use crate::error::{Error, Result};
use reqwest::Client as HttpClient;
use reqwest::header::{AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue, USER_AGENT};
use serde::de::DeserializeOwned;
use std::time::Duration;
use tracing::debug;

const USER_AGENT_VALUE: &str = concat!("logchef-cli/", env!("CARGO_PKG_VERSION"));

pub struct Client {
    http: HttpClient,
    base_url: String,
    token: Option<String>,
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
            token: None,
        })
    }

    pub fn from_context(ctx: &Context) -> Result<Self> {
        let mut client = Self::new(&ctx.server_url, ctx.timeout_secs)?;
        client.token = ctx.token.clone();
        Ok(client)
    }

    pub fn from_context_with_timeout(ctx: &Context, timeout_secs: u64) -> Result<Self> {
        let mut client = Self::new(&ctx.server_url, timeout_secs)?;
        client.token = ctx.token.clone();
        Ok(client)
    }

    pub fn with_token(mut self, token: String) -> Self {
        self.token = Some(token);
        self
    }

    fn headers(&self) -> HeaderMap {
        let mut headers = HeaderMap::new();
        headers.insert(USER_AGENT, HeaderValue::from_static(USER_AGENT_VALUE));
        headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));

        if let Some(ref token) = self.token
            && let Ok(value) = HeaderValue::from_str(&format!("Bearer {}", token))
        {
            headers.insert(AUTHORIZATION, value);
        }

        headers
    }

    async fn get<T: DeserializeOwned>(&self, path: &str) -> Result<T> {
        let url = format!("{}{}", self.base_url, path);
        debug!(url = %url, "GET request");

        let response = self.http.get(&url).headers(self.headers()).send().await?;

        self.handle_response(response).await
    }

    async fn post<T: DeserializeOwned, B: serde::Serialize>(
        &self,
        path: &str,
        body: &B,
    ) -> Result<T> {
        let url = format!("{}{}", self.base_url, path);
        debug!(url = %url, "POST request");

        let response = self
            .http
            .post(&url)
            .headers(self.headers())
            .json(body)
            .send()
            .await?;

        self.handle_response(response).await
    }

    async fn handle_response<T: DeserializeOwned>(&self, response: reqwest::Response) -> Result<T> {
        let status = response.status();
        let status_code = status.as_u16();

        if !status.is_success() {
            let body = response.text().await.unwrap_or_default();

            if let Ok(api_error) = serde_json::from_str::<ApiErrorResponse>(&body) {
                return Err(Error::api_with_type(
                    Some(status_code),
                    api_error.message,
                    api_error.error_type,
                ));
            }

            return Err(Error::api(
                Some(status_code),
                format!("HTTP {}: {}", status_code, body),
            ));
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

    pub async fn get_meta(&self) -> Result<MetaResponse> {
        let response: ApiResponse<MetaData> = self.get("/api/v1/meta").await?;
        Ok(MetaResponse {
            status: response.status,
            data: response.data,
        })
    }

    pub async fn get_current_user(&self) -> Result<User> {
        let response: ApiResponse<UserData> = self.get("/api/v1/me").await?;
        Ok(response.data.user)
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

        let response = self
            .http
            .post(&url)
            .headers(self.headers())
            .json(request)
            .send()
            .await?;

        let status = response.status();
        if !status.is_success() {
            let status_code = status.as_u16();
            let body = response.text().await.unwrap_or_default();

            if let Ok(api_error) = serde_json::from_str::<ApiErrorResponse>(&body) {
                return Err(Error::api_with_type(
                    Some(status_code),
                    api_error.message,
                    api_error.error_type,
                ));
            }

            return Err(Error::api(
                Some(status_code),
                format!("HTTP {}: {}", status_code, body),
            ));
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

        let response = http.get(&url).headers(self.headers()).send().await?;

        let status = response.status();
        if !status.is_success() {
            let status_code = status.as_u16();
            let body = response.text().await.unwrap_or_default();

            if let Ok(api_error) = serde_json::from_str::<ApiErrorResponse>(&body) {
                return Err(Error::api_with_type(
                    Some(status_code),
                    api_error.message,
                    api_error.error_type,
                ));
            }

            return Err(Error::api(
                Some(status_code),
                format!("HTTP {}: {}", status_code, body),
            ));
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

        let response = self.http.get(&url).headers(self.headers()).send().await?;
        let status = response.status();
        if !status.is_success() {
            let status_code = status.as_u16();
            let body = response.text().await.unwrap_or_default();

            if let Ok(api_error) = serde_json::from_str::<ApiErrorResponse>(&body) {
                return Err(Error::api_with_type(
                    Some(status_code),
                    api_error.message,
                    api_error.error_type,
                ));
            }

            return Err(Error::api(
                Some(status_code),
                format!("HTTP {}: {}", status_code, body),
            ));
        }

        Ok(response)
    }

    pub async fn exchange_token(&self, oidc_token: &str) -> Result<TokenExchangeData> {
        let url = format!("{}/api/v1/cli/token", self.base_url);
        debug!(url = %url, "Token exchange request");

        let mut headers = self.headers();
        if let Ok(value) = HeaderValue::from_str(&format!("Bearer {}", oidc_token)) {
            headers.insert(AUTHORIZATION, value);
        }

        let response = self.http.post(&url).headers(headers).send().await?;

        let api_response: TokenExchangeApiResponse = self.handle_response(response).await?;
        Ok(api_response.data)
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

    /// Fetches the caller's recent query history, newest first. The server
    /// defaults `limit` to 50 and caps it at 200.
    pub async fn get_query_history(&self, limit: u32) -> Result<Vec<QueryHistoryEntry>> {
        let response: ApiResponse<Vec<QueryHistoryEntry>> = self
            .get(&format!("/api/v1/me/query-history?limit={}", limit))
            .await?;
        Ok(response.data)
    }
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
            query: "".to_owned(),
            start_time: "2026-08-08 10:00:00".to_owned(),
            end_time: "2026-08-08 10:15:00".to_owned(),
            timezone: Some("UTC".to_owned()),
            limit: Some(100),
            query_timeout: Some(30),
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
