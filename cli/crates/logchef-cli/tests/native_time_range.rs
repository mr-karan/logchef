#![cfg(target_os = "linux")]

mod common;

use chrono::{DateTime, Utc};
use common::*;
use serde_json::{Value, json};

fn request_body(command: &str, time_range: Value, extra: &[&str]) -> Value {
    let server = FakeServer::start(false);
    server.state().saved_query = json!({
        "id": 7, "source_id": 2, "name": "Window", "query_language": "clickhouse-sql",
        "editor_mode": "native", "query_content": json!({
            "version": 1, "content": "SELECT 1", "limit": 10, "timeRange": time_range
        }).to_string()
    });
    let home = TestHome::new("native-window");
    let mut config = config_with(&server.url, json!({"token": PAT}));
    config["contexts"]["test"]["defaults"]["timezone"] = json!("Asia/Kolkata");
    home.write_config(&config);
    let mut cmd = home.logchef();
    cmd.args([
        command,
        if command == "collections" {
            "Window"
        } else {
            "7"
        },
        "--team",
        "1",
        "--source",
        "2",
        "--output",
        "json",
    ])
    .args(extra);
    let out = run(cmd);
    assert_success(&out);
    let state = server.state();
    assert_eq!(state.native_requests.len(), 1);
    state.native_requests[0].clone()
}

fn bounds(body: &Value) -> (DateTime<Utc>, DateTime<Utc>) {
    assert_eq!(body["timezone"], "Asia/Kolkata");
    let parse = |key: &str| {
        let raw = body[key].as_str().expect("native time bound is required");
        DateTime::parse_from_rfc3339(raw)
            .unwrap_or_else(|error| panic!("{key}={raw:?} is not RFC3339: {error}"))
            .with_timezone(&Utc)
    };
    (parse("start_time"), parse("end_time"))
}

#[test]
fn collections_relative_override_sends_rfc3339_request_bounds() {
    let before = Utc::now();
    let body = request_body(
        "collections",
        json!({"relative": "15m"}),
        &["--since", "2h"],
    );
    let after = Utc::now();
    let (start, end) = bounds(&body);
    assert_eq!(end - start, chrono::Duration::hours(2));
    assert!(end >= before && end <= after);
}

#[test]
fn collections_absolute_range_preserves_instants_in_non_utc_zone() {
    let body = request_body(
        "collections",
        json!({"absolute": {
            "start": 1783985322000_i64, "end": 1783988922000_i64
        }}),
        &[],
    );
    let (start, end) = bounds(&body);
    assert_eq!(start.timestamp_millis(), 1783985322000_i64);
    assert_eq!(end.timestamp_millis(), 1783988922000_i64);
}

#[test]
fn saved_query_stored_relative_range_sends_rfc3339_request_bounds() {
    let before = Utc::now();
    let body = request_body("saved-queries", json!({"relative": "3h"}), &[]);
    let after = Utc::now();
    let (start, end) = bounds(&body);
    assert_eq!(end - start, chrono::Duration::hours(3));
    assert!(end >= before && end <= after);
}
