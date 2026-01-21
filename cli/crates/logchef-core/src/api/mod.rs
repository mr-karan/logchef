mod models;

pub use models::*;

use crate::config::Context;
use crate::error::{Error, Result};
use reqwest::Client as HttpClient;
use reqwest::header::{AUTHORIZATION, CONTENT_TYPE, HeaderMap, HeaderValue, USER_AGENT};
use serde::de::DeserializeOwned;
use std::time::Duration;
use tracing::debug;

const USER_AGENT_VALUE: &str = "logchef-cli/0.1.0";

pub struct Client {
    http: HttpClient,
    base_url: String,
    token: Option<String>,
}

impl Client {
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
                return Err(Error::api(Some(status_code), api_error.message));
            }

            return Err(Error::api(
                Some(status_code),
                format!("HTTP {}: {}", status_code, body),
            ));
        }

        let body = response.text().await?;
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
        Ok(response.data)
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

    pub async fn list_collections(&self, team_id: i64, source_id: i64) -> Result<Vec<Collection>> {
        let response: ApiResponse<Vec<Collection>> = self
            .get(&format!(
                "/api/v1/teams/{}/sources/{}/collections",
                team_id, source_id
            ))
            .await?;
        Ok(response.data)
    }
}
