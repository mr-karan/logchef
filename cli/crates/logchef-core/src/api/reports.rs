//! Scheduled reports (`/api/v1/reports`): the list, the run history and a
//! manual run. The CLI does not create or edit a report; the web editor
//! does. Enumerated values stay strings: the server adds kinds without a CLI
//! release.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{ApiResponse, Client};
use crate::error::Result;

/// One report from `GET /api/v1/reports`. A recipient who may not edit the
/// report gets a reduced view: the options, the state, the next run and the
/// last run are empty.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Report {
    pub id: i64,
    pub name: String,
    pub source: ReportSource,
    pub schedule: ReportSchedule,
    pub timezone: String,
    #[serde(default)]
    pub time_range: String,
    pub owner: Option<ReportOwner>,
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub pause_reason: String,
    pub next_run_at: Option<DateTime<Utc>>,
    pub last_run: Option<ReportLastRun>,
    #[serde(default)]
    pub can_edit: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportSource {
    /// `dashboard` or `saved_query`.
    pub kind: String,
    pub name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub dashboard_id: Option<i64>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub tab_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub saved_query_id: Option<i64>,
}

/// The typed schedule, in the report's timezone. A field the frequency does
/// not use is absent.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportSchedule {
    /// `hourly`, `daily`, `weekly` or `monthly`.
    pub frequency: String,
    pub minute: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub hour: Option<u32>,
    /// 0 to 6, Sunday is 0.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub weekdays: Vec<u32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub day_of_month: Option<u32>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportOwner {
    pub id: i64,
    pub name: String,
    pub email: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportLastRun {
    pub id: i64,
    pub status: String,
    #[serde(default)]
    pub reason: String,
    pub started_at: DateTime<Utc>,
    #[serde(default)]
    pub finished_at: Option<DateTime<Utc>>,
}

/// One run from `GET /api/v1/reports/:id/runs`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportRun {
    pub id: i64,
    pub report_id: i64,
    /// `schedule` or `manual`.
    pub trigger: String,
    pub scheduled_for: DateTime<Utc>,
    /// `running`, `sent`, `partial`, `skipped`, `failed` or `interrupted`.
    pub status: String,
    #[serde(default)]
    pub reason: String,
    #[serde(default)]
    pub window_start: Option<DateTime<Utc>>,
    #[serde(default)]
    pub window_end: Option<DateTime<Utc>>,
    #[serde(default)]
    pub panels: Vec<ReportPanelOutcome>,
    #[serde(default)]
    pub deliveries: Vec<ReportDelivery>,
    pub started_at: DateTime<Utc>,
    #[serde(default)]
    pub finished_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportPanelOutcome {
    pub panel_id: String,
    pub title: String,
    /// `ok`, `empty`, `locked`, `failed` or `unsupported`.
    pub status: String,
    pub rows: u64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub error: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReportDelivery {
    /// `user` or `external`.
    pub class: String,
    pub target: String,
    pub via: String,
    /// `sent`, `failed`, `skipped` or `unknown`.
    pub status: String,
    pub attempts: u32,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
}

#[derive(Serialize)]
struct RunRequest<'a> {
    recipients: &'a str,
}

impl Client {
    /// Lists reports. `scope` is `mine`, `received` or `all` (global admins).
    /// Needs `reports:read` on an API token.
    pub async fn list_reports(&self, scope: &str) -> Result<Vec<Report>> {
        let response: ApiResponse<Vec<Report>> = self
            .get(&format!(
                "/api/v1/reports?scope={}",
                urlencoding::encode(scope)
            ))
            .await?;
        Ok(response.data)
    }

    /// Lists up to `limit` runs of a report, newest first. Only the owner
    /// and global admins read them. Needs `reports:read` on an API token.
    pub async fn list_report_runs(&self, report_id: i64, limit: u32) -> Result<Vec<ReportRun>> {
        let response: ApiResponse<Vec<ReportRun>> = self
            .get(&format!("/api/v1/reports/{report_id}/runs?limit={limit}"))
            .await?;
        Ok(response.data)
    }

    /// Starts a manual run that sends to the caller (`all == false`) or to
    /// every recipient, and returns the claimed run. The run finishes in the
    /// background. Needs `reports:write` on an API token.
    pub async fn run_report(&self, report_id: i64, all: bool) -> Result<ReportRun> {
        let body = RunRequest {
            recipients: if all { "all" } else { "me" },
        };
        let response: ApiResponse<ReportRun> = self
            .post(&format!("/api/v1/reports/{report_id}/run"), &body)
            .await?;
        Ok(response.data)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_recipient_view_parses() {
        // The reduced view a recipient reads: no options, no state, no runs.
        let body = serde_json::json!({
            "id": 4, "name": "Daily 5xx",
            "source": {"kind": "dashboard", "name": "HTTP"},
            "schedule": {"frequency": "daily", "minute": 0, "hour": 9},
            "timezone": "Asia/Kolkata", "time_range": "", "range_anchor": "",
            "variables": null, "max_rows": 0, "skip_if_empty": false, "attach_csv": false,
            "owner": {"id": 1, "name": "Owner", "email": "owner@example.com"},
            "state": "", "pause_reason": "", "next_run_at": null, "last_run": null,
            "can_edit": false, "warnings": [],
            "created_at": "0001-01-01T00:00:00Z", "updated_at": "0001-01-01T00:00:00Z"
        });
        let report: Report = serde_json::from_value(body).expect("parse a recipient view");
        assert!(!report.can_edit);
        assert_eq!(report.schedule.hour, Some(9));
        assert!(report.next_run_at.is_none() && report.last_run.is_none());
    }
}
