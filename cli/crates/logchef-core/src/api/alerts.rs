//! Alert rules, their dry runs (test and backtest), alert silences and
//! notification destinations, read only. Each call returns the response
//! `data` as the server sent it, for `--output json`, which the text tables
//! parse. The models hold only the fields the tables show; enumerated values
//! stay strings, since the server adds kinds without a CLI release.

use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde::de::DeserializeOwned;
use std::collections::BTreeMap;
use std::marker::PhantomData;

use super::{ApiResponse, Client, null_as_default};
use crate::error::{Error, Result};

/// A response's `data` as the server sent it. Only `parse` reads it into the
/// model, so `--output json` prints a field the model misreads unchanged.
#[derive(Debug)]
pub struct Data<T> {
    pub raw: serde_json::Value,
    model: PhantomData<fn() -> T>,
}

impl<T: DeserializeOwned> Data<T> {
    pub(super) fn new(raw: serde_json::Value) -> Self {
        Self {
            raw,
            model: PhantomData,
        }
    }

    pub fn parse(&self) -> Result<T> {
        serde_json::from_value(self.raw.clone()).map_err(|e| {
            Error::other(format!(
                "Failed to parse response: {e} (data: {})",
                self.raw
            ))
        })
    }
}

/// One rule from `GET /api/v1/alerts`.
#[derive(Debug, Deserialize)]
pub struct Alert {
    pub id: i64,
    pub source_id: i64,
    pub name: String,
    pub severity: String,
    pub threshold_operator: String,
    pub threshold_value: f64,
    pub lookback_seconds: i64,
    pub frequency_seconds: i64,
    pub is_active: bool,
    pub last_state: String,
    #[serde(default)]
    pub instance_counts: Option<AlertInstanceCounts>,
}

#[derive(Debug, Deserialize)]
pub struct AlertInstanceCounts {
    pub firing: i64,
    pub no_data: i64,
    pub ok: i64,
}

/// `POST /api/v1/alerts/test`: one evaluation of an unsaved rule. For a
/// grouped rule `value` is the first sample's and `threshold_met` reports
/// that some group meets the threshold.
#[derive(Debug, Deserialize)]
pub struct AlertTestResult {
    pub value: f64,
    pub threshold_met: bool,
    pub execution_time_ms: i64,
    pub rows_returned: i64,
    #[serde(default, deserialize_with = "null_as_default")]
    pub warnings: Vec<String>,
    #[serde(default, deserialize_with = "null_as_default")]
    pub samples: Vec<AlertSample>,
    #[serde(default)]
    pub groups_total: i64,
    pub no_data: bool,
}

#[derive(Debug, Deserialize)]
pub struct AlertSample {
    #[serde(default, deserialize_with = "null_as_default")]
    pub group: BTreeMap<String, String>,
    pub value: f64,
    pub threshold_met: bool,
}

/// `POST /api/v1/alerts/backtest`: what an unsaved rule would have done over
/// a past range. `coverage` is the share (0 to 1) of the range the evaluated
/// lookback windows read. Older servers send no `warnings`, no
/// `empty_result_episodes` and no `min_value`.
#[derive(Debug, Deserialize)]
pub struct BacktestResult {
    pub start: DateTime<Utc>,
    pub end: DateTime<Utc>,
    pub step_seconds: i64,
    pub evaluations: i64,
    pub coverage: f64,
    pub partial: bool,
    pub summary: BacktestSummary,
    #[serde(default, deserialize_with = "null_as_default")]
    pub series: Vec<BacktestPoint>,
    #[serde(default, deserialize_with = "null_as_default")]
    pub groups: Vec<BacktestGroup>,
    #[serde(default)]
    pub groups_omitted: i64,
    #[serde(default)]
    pub first_error: Option<String>,
    #[serde(default, deserialize_with = "null_as_default")]
    pub warnings: Vec<String>,
}

#[derive(Debug, Deserialize)]
pub struct BacktestSummary {
    pub episodes: i64,
    pub firing_seconds: i64,
    pub breaching_evaluations: i64,
    pub notifications: i64,
    pub no_data_evaluations: i64,
    pub errors: i64,
    /// Episodes of a grouped rule whose whole result was empty, under its
    /// no-data policy. They belong to no group, and `episodes` counts them.
    #[serde(default)]
    pub empty_result_episodes: i64,
}

/// The rule after one evaluation of the replay. `status` is `ok`, `firing`,
/// `no_data` or `error`.
#[derive(Debug, Deserialize)]
pub struct BacktestPoint {
    pub t: DateTime<Utc>,
    pub status: String,
}

#[derive(Debug, Deserialize)]
pub struct BacktestGroup {
    #[serde(default, deserialize_with = "null_as_default")]
    pub group: BTreeMap<String, String>,
    pub episodes: i64,
    pub breaching_evaluations: i64,
    #[serde(default)]
    pub min_value: Option<f64>,
    #[serde(default)]
    pub max_value: Option<f64>,
}

/// One silence from `GET /api/v1/alert-silences`. A `once` silence has
/// `starts_at` and `ends_at`; a `weekly` one has `weekly`.
#[derive(Debug, Deserialize)]
pub struct AlertSilence {
    pub id: i64,
    /// None covers every rule of the source.
    #[serde(default)]
    pub alert_id: Option<i64>,
    #[serde(default, deserialize_with = "null_as_default")]
    pub matchers: BTreeMap<String, String>,
    #[serde(default)]
    pub comment: String,
    pub schedule_kind: String,
    #[serde(default)]
    pub starts_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub ends_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub weekly: Option<SilenceWeekly>,
    pub active: bool,
}

/// Starts at `start` (HH:MM) in `timezone` on each weekday (0 is Sunday).
#[derive(Debug, Deserialize)]
pub struct SilenceWeekly {
    pub weekdays: Vec<u8>,
    pub start: String,
    pub duration_minutes: i64,
    pub timezone: String,
}

/// One destination from `GET /api/v1/notification-destinations`. An OAuth
/// sign-in gets only these fields; a token or session also gets the config.
#[derive(Debug, Deserialize)]
pub struct Destination {
    pub id: i64,
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub scope: String,
    #[serde(default)]
    pub team_id: Option<i64>,
    pub is_active: bool,
}

impl Client {
    async fn get_data<T: DeserializeOwned>(&self, path: &str) -> Result<Data<T>> {
        let response: ApiResponse<serde_json::Value> = self.get(path).await?;
        Ok(Data::new(response.data))
    }

    async fn post_data<T: DeserializeOwned>(
        &self,
        path: &str,
        body: &serde_json::Value,
    ) -> Result<Data<T>> {
        let response: ApiResponse<serde_json::Value> = self.post(path, body).await?;
        Ok(Data::new(response.data))
    }

    /// Lists the rules the caller can see, of one source or of all.
    pub async fn list_alerts(&self, source_id: Option<i64>) -> Result<Data<Vec<Alert>>> {
        let path = match source_id {
            Some(id) => format!("/api/v1/alerts?source_id={id}"),
            None => "/api/v1/alerts".to_owned(),
        };
        self.get_data(&path).await
    }

    /// Evaluates an unsaved rule once. Saves nothing and notifies no one.
    pub async fn test_alert(&self, request: &serde_json::Value) -> Result<Data<AlertTestResult>> {
        self.post_data("/api/v1/alerts/test", request).await
    }

    /// Replays an unsaved rule over a past range. The server allows one
    /// backtest at a time and stops one after 60 seconds, so the client needs
    /// a longer timeout.
    pub async fn backtest_alert(
        &self,
        request: &serde_json::Value,
    ) -> Result<Data<BacktestResult>> {
        self.post_data("/api/v1/alerts/backtest", request).await
    }

    pub async fn list_silences(&self, source_id: i64) -> Result<Data<Vec<AlertSilence>>> {
        self.get_data(&format!("/api/v1/alert-silences?source_id={source_id}"))
            .await
    }

    pub async fn list_destinations(&self) -> Result<Data<Vec<Destination>>> {
        self.get_data("/api/v1/notification-destinations").await
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_destination_parses_from_either_shape() {
        let summary = serde_json::json!({
            "id": 3, "name": "oncall", "type": "slack", "scope": "team", "team_id": 2,
            "is_active": true
        });
        let mut full = summary.clone();
        full["config"] = serde_json::json!({"channel": "#oncall"});
        full["has_secret"] = true.into();
        full["created_at"] = "2026-11-01T00:00:00Z".into();
        for raw in [summary, full] {
            let destination: Destination = Data::new(raw).parse().expect("parse");
            assert_eq!(destination.kind, "slack");
            assert_eq!(destination.team_id, Some(2));
        }
    }
}
