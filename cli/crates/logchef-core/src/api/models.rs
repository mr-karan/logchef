use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Deserialize)]
pub struct ApiResponse<T> {
    pub status: String,
    pub data: T,
}

#[derive(Debug, Deserialize)]
pub struct ApiErrorResponse {
    pub status: String,
    pub message: String,
    #[serde(default)]
    pub error_type: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct MetaResponse {
    pub status: String,
    pub data: MetaData,
}

#[derive(Debug, Deserialize)]
pub struct MetaData {
    pub version: String,
    #[serde(default)]
    pub build_info: Option<String>,
    #[serde(default)]
    pub oidc_issuer: Option<String>,
    #[serde(default)]
    pub cli_client_id: Option<String>,
}

impl MetaData {
    pub fn oidc_enabled(&self) -> bool {
        self.oidc_issuer.is_some() && self.cli_client_id.is_some()
    }
}

#[derive(Debug, Deserialize)]
pub struct UserData {
    pub user: User,
    #[serde(default)]
    pub auth_method: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct User {
    pub id: i64,
    pub email: String,
    #[serde(default)]
    pub full_name: Option<String>,
    pub role: String,
    #[serde(default)]
    pub status: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Team {
    pub id: i64,
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub role: Option<String>,
    #[serde(default)]
    pub member_count: Option<i32>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Source {
    pub id: i64,
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub database: Option<String>,
    #[serde(default)]
    pub table_name: Option<String>,
    #[serde(default)]
    pub is_connected: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Column {
    pub name: String,
    #[serde(rename = "type")]
    pub column_type: String,
}

#[derive(Debug, Serialize)]
pub struct QueryRequest {
    pub query: String,
    pub start_time: String,
    pub end_time: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timezone: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub query_timeout: Option<u32>,
}

#[derive(Debug, Serialize)]
pub struct SqlQueryRequest {
    pub raw_sql: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timezone: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_time: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub end_time: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub query_timeout: Option<u32>,
}

#[derive(Debug, Deserialize)]
pub struct QueryResponse {
    #[serde(default)]
    pub logs: Vec<LogEntry>,
    #[serde(default)]
    pub data: Vec<LogEntry>,
    #[serde(default)]
    pub columns: Vec<Column>,
    #[serde(default)]
    pub stats: QueryStats,
    #[serde(default)]
    pub query_id: Option<String>,
    #[serde(default)]
    pub generated_sql: Option<String>,
}

impl QueryResponse {
    pub fn entries(&self) -> &[LogEntry] {
        if !self.logs.is_empty() {
            &self.logs
        } else {
            &self.data
        }
    }
}

pub type LogEntry = HashMap<String, serde_json::Value>;

#[derive(Debug, Default, Serialize, Deserialize)]
pub struct QueryStats {
    #[serde(default)]
    pub execution_time_ms: i64,
    #[serde(default)]
    pub rows_read: i64,
    #[serde(default)]
    pub bytes_read: i64,
}

#[derive(Debug, Deserialize)]
pub struct TokenExchangeApiResponse {
    pub status: String,
    pub data: TokenExchangeData,
}

#[derive(Debug, Deserialize)]
pub struct TokenExchangeData {
    pub token: String,
    #[serde(default)]
    pub expires_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub user: Option<TokenUser>,
}

#[derive(Debug, Deserialize)]
pub struct TokenUser {
    pub id: i64,
    pub email: String,
    #[serde(default)]
    pub full_name: Option<String>,
    #[serde(default)]
    pub role: Option<String>,
}
