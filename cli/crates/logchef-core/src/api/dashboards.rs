//! Dashboards, read only: the summaries of the dashboards the caller sees.

use chrono::{DateTime, Utc};
use serde::Deserialize;

use super::{ApiResponse, Client, Data, null_as_default};
use crate::error::Result;

/// The header that names the newest dashboard panel schema the client reads.
/// Without it the server answers as to a schema 1 client.
const DASHBOARD_SCHEMA_HEADER: &str = "X-Logchef-Dashboard-Schema";

/// One summary from `GET /api/v1/dashboards`. It carries no panels.
#[derive(Debug, Deserialize)]
pub struct DashboardSummary {
    pub id: i64,
    pub name: String,
    pub panel_count: i64,
    #[serde(default, deserialize_with = "null_as_default")]
    pub panel_types: Vec<String>,
    pub tab_count: i64,
    pub can_edit: bool,
    #[serde(default)]
    pub corrupt: bool,
    pub updated_at: DateTime<Utc>,
}

impl Client {
    /// Lists the dashboards the caller can see: its own, and those with a
    /// panel on one of its teams.
    pub async fn list_dashboards(&self) -> Result<Data<Vec<DashboardSummary>>> {
        let url = format!("{}/api/v1/dashboards", self.base_url);
        let response = self
            .send(|| self.http.get(&url).header(DASHBOARD_SCHEMA_HEADER, "2"))
            .await?;
        let response: ApiResponse<serde_json::Value> = self.handle_response(response).await?;
        Ok(Data::new(response.data))
    }
}
