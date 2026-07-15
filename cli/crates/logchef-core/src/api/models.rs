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
    #[serde(default, rename = "_meta_ts_field")]
    pub meta_ts_field: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default = "default_source_type")]
    pub source_type: String,
    #[serde(default)]
    pub connection: Option<SourceConnection>,
    #[serde(default)]
    pub is_connected: bool,
}

#[derive(Debug, Clone, Deserialize)]
pub struct SourceConnection {
    #[serde(default)]
    pub host: Option<String>,
    #[serde(default)]
    pub database: Option<String>,
    #[serde(default)]
    pub table_name: Option<String>,
    #[serde(default)]
    pub base_url: Option<String>,
}

impl Source {
    /// Returns the database.table_name reference if both are available.
    pub fn table_ref(&self) -> Option<String> {
        self.connection
            .as_ref()
            .and_then(|c| match (&c.database, &c.table_name) {
                (Some(db), Some(table)) => Some(format!("{}.{}", db, table)),
                _ => None,
            })
    }

    pub fn target_ref(&self) -> Option<String> {
        match self.source_type.as_str() {
            "victorialogs" => self
                .connection
                .as_ref()
                .and_then(|c| c.base_url.as_ref())
                .map(|base_url| base_url.to_string()),
            _ => self.table_ref(),
        }
    }

    pub fn source_type_label(&self) -> &'static str {
        match self.source_type.as_str() {
            "victorialogs" => "VictoriaLogs",
            _ => "ClickHouse",
        }
    }

    pub fn display_target(&self) -> String {
        if let Some(target) = self.target_ref() {
            format!("{} [{}]", target, self.source_type_label())
        } else {
            self.source_type_label().to_string()
        }
    }

    pub fn display_name(&self) -> String {
        format!("{} ({})", self.name, self.display_target())
    }
}

fn default_source_type() -> String {
    "clickhouse".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Column {
    pub name: String,
    #[serde(rename = "type")]
    pub column_type: String,
    #[serde(default)]
    pub description: Option<String>,
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
    pub query_text: String,
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

#[derive(Debug, Serialize)]
pub struct ExportSqlRequest {
    pub raw_sql: String,
    pub format: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub query_timeout: Option<u32>,
}

#[derive(Debug, Deserialize)]
pub struct ExportJobResponse {
    pub id: String,
    pub status: String,
    #[serde(default)]
    pub format: Option<String>,
    #[serde(default)]
    pub file_name: Option<String>,
    #[serde(default)]
    pub error_message: Option<String>,
    #[serde(default)]
    pub rows_exported: Option<i64>,
    #[serde(default)]
    pub bytes_written: Option<i64>,
    #[serde(default)]
    pub download_url: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct TranslateRequest {
    pub query: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_time: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub end_time: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timezone: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<u32>,
}

#[derive(Debug, Deserialize)]
pub struct TranslateResponse {
    #[serde(default)]
    pub sql: String,
    #[serde(default)]
    pub full_sql: String,
    #[serde(default)]
    pub generated_query: String,
    #[serde(default)]
    pub generated_query_language: Option<String>,
    #[serde(default)]
    pub valid: bool,
    #[serde(default)]
    pub error: Option<QueryParseError>,
}

impl TranslateResponse {
    /// The most complete generated query available: the full executable SQL
    /// when a time range was supplied, otherwise the filter-only translation
    /// (ClickHouse) or the native query (VictoriaLogs).
    pub fn generated_query(&self) -> &str {
        if !self.full_sql.is_empty() {
            &self.full_sql
        } else if !self.generated_query.is_empty() {
            &self.generated_query
        } else {
            &self.sql
        }
    }

    /// Human-readable label for the generated query language.
    pub fn language_label(&self) -> &'static str {
        match self.generated_query_language.as_deref() {
            Some("logsql") => "LogsQL",
            Some("clickhouse-sql") => "ClickHouse SQL",
            _ => "query",
        }
    }
}

#[derive(Debug, Serialize)]
pub struct ValidateRequest {
    pub query: String,
}

#[derive(Debug, Deserialize)]
pub struct ValidateResponse {
    #[serde(default)]
    pub valid: bool,
    #[serde(default)]
    pub error: Option<QueryParseError>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueryParseError {
    #[serde(default)]
    pub code: String,
    pub message: String,
    #[serde(default)]
    pub position: Option<QueryParsePosition>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueryParsePosition {
    pub line: i32,
    pub column: i32,
}

#[derive(Debug, Serialize)]
pub struct HistogramRequest {
    pub query_text: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_timestamp: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub end_timestamp: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub window: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub group_by: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timezone: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub limit: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub query_timeout: Option<u32>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct HistogramResponse {
    #[serde(default)]
    pub granularity: String,
    #[serde(default)]
    pub data: Vec<HistogramBucket>,
    #[serde(default)]
    pub notice: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct HistogramBucket {
    pub bucket: String,
    #[serde(default)]
    pub log_count: i64,
    #[serde(default)]
    pub group_value: Option<String>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct FieldValuesResult {
    #[serde(default)]
    pub field_name: String,
    #[serde(default)]
    pub field_type: String,
    #[serde(default)]
    pub is_low_cardinality: bool,
    #[serde(default)]
    pub values: Vec<FieldValueInfo>,
    #[serde(default)]
    pub total_distinct: i64,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct FieldValueInfo {
    pub value: String,
    #[serde(default)]
    pub count: i64,
}

/// Query parameters for [`crate::api::Client::get_field_values`]. The field
/// `type` (from the source schema) and an RFC3339 `start`/`end` window are
/// required by the server for performance.
pub struct FieldValuesQuery<'a> {
    pub field_name: &'a str,
    pub field_type: &'a str,
    pub start: &'a str,
    pub end: &'a str,
    pub timezone: &'a str,
    pub limit: u32,
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
    #[serde(default)]
    pub generated_query: Option<String>,
    #[serde(default)]
    pub generated_query_language: Option<String>,
}

impl QueryResponse {
    pub fn entries(&self) -> &[LogEntry] {
        if !self.logs.is_empty() {
            &self.logs
        } else {
            &self.data
        }
    }

    pub fn generated_query(&self) -> Option<&str> {
        self.generated_query
            .as_deref()
            .or(self.generated_sql.as_deref())
    }

    pub fn generated_query_language(&self) -> Option<&str> {
        self.generated_query_language
            .as_deref()
            .or_else(|| self.generated_sql.as_ref().map(|_| "clickhouse-sql"))
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

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Collection {
    pub id: i64,
    pub source_id: i64,
    #[serde(default)]
    pub created_from_team_id: Option<i64>,
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    pub query_language: String,
    pub editor_mode: String,
    pub query_content: String,
    #[serde(default)]
    pub is_bookmarked: bool,
    #[serde(default)]
    pub created_by: Option<i64>,
    #[serde(default)]
    pub created_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub updated_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub source_name: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResolvedSavedQuery {
    #[serde(flatten)]
    pub query: Collection,
    pub resolved_team_id: i64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CollectionQueryContent {
    #[serde(default)]
    pub version: Option<i32>,
    #[serde(default, rename = "sourceId")]
    pub source_id: Option<i64>,
    #[serde(default, rename = "timeRange")]
    pub time_range: Option<CollectionTimeRange>,
    #[serde(default)]
    pub limit: Option<u32>,
    #[serde(default)]
    pub content: Option<String>,
    #[serde(default)]
    pub variables: Option<Vec<CollectionVariable>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CollectionTimeRange {
    #[serde(default)]
    pub relative: Option<String>,
    #[serde(default)]
    pub absolute: Option<CollectionAbsoluteTime>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CollectionAbsoluteTime {
    pub start: i64,
    pub end: i64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CollectionVariable {
    pub name: String,
    #[serde(default, rename = "type")]
    pub var_type: Option<String>,
    #[serde(default)]
    pub label: Option<String>,
    #[serde(default, rename = "inputType")]
    pub input_type: Option<String>,
    #[serde(default)]
    pub value: Option<serde_json::Value>,
}
