#![cfg(target_os = "linux")]

mod common;

use chrono::{DateTime, Utc};
use common::*;
use serde_json::{Value, json};

fn execute(
    command: &str,
    time_range: Value,
    extra: &[&str],
    dsl: bool,
    zone: &str,
) -> (std::process::Output, Vec<Value>) {
    let server = FakeServer::start(false);
    server.state().saved_query = json!({
        "id": 7, "source_id": 2, "name": "Window", "query_language": "clickhouse-sql",
        "editor_mode": "native", "query_content": json!({
            "version": 2,
            "content": "SELECT 1 [[WHERE env IN ({{env}})]]",
            "variables": [{
                "name":"env", "type":"text", "value":"",
                "defaultValue":"prod", "options":[{"value":"prod"}]
            }],
            "limit": 10, "timeRange": time_range
        }).to_string()
    });
    if dsl {
        server.state().saved_query["query_language"] = json!("logchefql");
    }
    let home = TestHome::new("native-window");
    let mut config = config_with(&server.url, json!({"token": PAT}));
    config["contexts"]["test"]["defaults"]["timezone"] = json!(zone);
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
    let requests = server.state().native_requests.clone();
    (out, requests)
}
fn request_body(command: &str, time_range: Value, extra: &[&str]) -> Value {
    let (out, requests) = execute(command, time_range, extra, false, "Asia/Kolkata");
    assert_success(&out);
    assert_eq!(requests.len(), 1);
    requests[0].clone()
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

#[test]
fn variables_and_calendar_ranges_reach_both_commands_and_dialects() {
    for command in ["collections", "saved-queries"] {
        for dsl in [false, true] {
            for relative in ["15m", "today", "yesterday"] {
                for (extra, value) in [
                    (vec![], json!("prod")),
                    (vec!["--var", "env="], json!("")),
                    (vec!["--var", "env=stage"], json!("stage")),
                    (vec!["--var", "env=a", "--var", "env=b"], json!(["a", "b"])),
                ] {
                    let (out, requests) =
                        execute(command, json!({"relative":relative}), &extra, dsl, "UTC");
                    assert_success(&out);
                    let body = &requests[0];
                    assert_eq!(
                        body["variables"],
                        json!([{"name":"env","type":"text","value":value}])
                    );
                    assert_eq!(
                        body[if dsl { "query" } else { "query_text" }],
                        "SELECT 1 [[WHERE env IN ({{env}})]]"
                    );
                    let start =
                        DateTime::parse_from_rfc3339(body["start_time"].as_str().unwrap()).unwrap();
                    let end =
                        DateTime::parse_from_rfc3339(body["end_time"].as_str().unwrap()).unwrap();
                    use chrono::Timelike;
                    if relative != "15m" {
                        assert_eq!(start.hour(), 0);
                    }
                    if relative == "yesterday" {
                        assert_eq!(end.hour(), 0);
                        assert_eq!(end - start, chrono::Duration::days(1));
                    }
                }
            }
            let (out, requests) = execute(
                command,
                json!({"absolute":{"start":1793511000000_i64,"end":1793514600000_i64}}),
                &[],
                dsl,
                "America/New_York",
            );
            assert_success(&out);
            for (key, epoch) in [
                ("start_time", 1793511000000_i64),
                ("end_time", 1793514600000_i64),
            ] {
                assert_eq!(
                    DateTime::parse_from_rfc3339(requests[0][key].as_str().unwrap())
                        .unwrap()
                        .timestamp_millis(),
                    epoch
                );
            }
        }
    }
}
#[test]
fn invalid_overrides_fail_with_saved_names() {
    for command in ["collections", "saved-queries"] {
        for invalid in ["missing", " env=x", "env =x", "unknown=x"] {
            let (out, requests) = execute(
                command,
                json!({"relative":"15m"}),
                &["--var", invalid],
                false,
                "UTC",
            );
            assert!(!out.status.success());
            assert!(String::from_utf8_lossy(&out.stderr).contains("Saved variables: env"));
            assert!(requests.is_empty());
        }
    }
}
#[test]
fn show_sql_lists_resolution_and_translates_dsl() {
    for dsl in [false, true] {
        for (extra, origin) in [
            (vec!["--show-sql"], "default"),
            (vec!["--show-sql", "--var", "env=stage"], "--var"),
        ] {
            let (out, _) = execute(
                "saved-queries",
                json!({"relative":"15m"}),
                &extra,
                dsl,
                "UTC",
            );
            assert_success(&out);
            let text = String::from_utf8_lossy(&out.stdout);
            assert!(text.contains("{{env}}"));
            assert!(text.contains("env") && text.contains("text") && text.contains(origin));
            if dsl {
                assert!(text.contains("SELECT translated"));
            }
        }
    }
}

#[test]
fn stored_calendar_ranges_run_in_both_commands() {
    for command in ["collections", "saved-queries"] {
        for relative in ["today", "yesterday"] {
            let (out, _) = execute(command, json!({"relative":relative}), &[], false, "UTC");
            assert_success(&out);
        }
    }
}

#[test]
fn fallback_hour_bounds_preserve_distinct_instants_in_both_commands() {
    for command in ["collections", "saved-queries"] {
        let (out, requests) = execute(
            command,
            json!({"absolute":{
                "start":1793511000000_i64,"end":1793514600000_i64
            }}),
            &[],
            true,
            "America/New_York",
        );
        assert_success(&out);
        assert_eq!(requests[0]["timezone"], "America/New_York");
        for (key, epoch) in [
            ("start_time", 1793511000000_i64),
            ("end_time", 1793514600000_i64),
        ] {
            assert_eq!(
                DateTime::parse_from_rfc3339(requests[0][key].as_str().unwrap())
                    .unwrap()
                    .timestamp_millis(),
                epoch
            );
        }
    }
}
