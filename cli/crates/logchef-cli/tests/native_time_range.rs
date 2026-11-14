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
                "name":"env", "type":"text", "inputType":"multiselect", "value":"",
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
    let requests = if extra.contains(&"--show-sql") && dsl {
        server.state().translate_requests.clone()
    } else {
        server.state().native_requests.clone()
    };
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
                    (vec!["--var", "env="], json!([])),
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
            let (out, requests) = execute(
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
                assert_eq!(requests.len(), 1);
                assert_eq!(
                    requests[0]["variables"],
                    json!([{"name":"env","type":"text","value":if origin == "--var" {"stage"} else {"prod"}}])
                );
                assert_eq!(requests[0]["timezone"], "UTC");
                let start =
                    DateTime::parse_from_rfc3339(requests[0]["start_time"].as_str().unwrap())
                        .unwrap();
                let end = DateTime::parse_from_rfc3339(requests[0]["end_time"].as_str().unwrap())
                    .unwrap();
                assert_eq!(end - start, chrono::Duration::minutes(15));
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

#[test]
fn show_sql_marks_missing_defaults_as_unset_without_null() {
    let server = FakeServer::start(false);
    server.state().saved_query = json!({
        "id":7,"source_id":2,"name":"Unset","query_language":"clickhouse-sql","editor_mode":"native",
        "query_content":json!({"content":"SELECT {{env}}","variables":[{"name":"env","type":"text"}]}).to_string()
    });
    let home = TestHome::new("unset-variable");
    home.write_config(&config_with(&server.url, json!({"token":PAT})));
    let out = run({
        let mut cmd = home.logchef();
        cmd.args(["saved-queries", "7", "--show-sql"]);
        cmd
    });
    assert_success(&out);
    let text = String::from_utf8_lossy(&out.stdout);
    assert!(text.contains("env (text) [unset]"), "{text}");
    assert!(!text.contains("null"), "{text}");
}

fn since_across_fallback(command: &str, victorialogs: bool) {
    let server = FakeServer::start(false);
    server.state().source_type = Some(
        if victorialogs {
            "victorialogs"
        } else {
            "clickhouse"
        }
        .into(),
    );
    if command == "tail" {
        server.state().query_logs =
            vec![json!({"timestamp":"2025-11-02T05:30:00Z","message":"row"})];
    }
    let home = TestHome::new("since-fallback");
    let mut config = config_with(&server.url, json!({"token":PAT}));
    config["contexts"]["test"]["defaults"]["timezone"] = json!("America/New_York");
    home.write_config(&config);
    let anchor = DateTime::parse_from_rfc3339("2025-11-02T05:30:00Z")
        .unwrap()
        .with_timezone(&Utc);
    let minutes = (Utc::now() - anchor).num_minutes();
    assert!(minutes > 0);
    let since = format!("{minutes}m");
    let before = Utc::now();
    let mut cmd = home.logchef();
    cmd.args([
        command,
        if command == "sql" {
            "SELECT * FROM logs"
        } else {
            ""
        },
        "--team",
        "1",
        "--source",
        "2",
        "--since",
        &since,
    ]);
    if command == "tail" {
        cmd.args(["--poll", "--max-lines", "1"]);
    }
    let out = run(cmd);
    assert_success(&out);
    let after = Utc::now();
    let state = server.state();
    let body = if command == "histogram" {
        &state.translate_requests[0]
    } else {
        &state.native_requests[0]
    };
    assert_eq!(body["timezone"], "America/New_York");
    if command == "sql" && !victorialogs {
        let sql = body["query_text"].as_str().unwrap();
        assert!(sql.contains("fromUnixTimestamp64Milli(toInt64("), "{sql}");
        let instants: Vec<i64> = sql
            .split("fromUnixTimestamp64Milli(toInt64(")
            .skip(1)
            .map(|value| value.split(')').next().unwrap().parse().unwrap())
            .collect();
        assert_eq!(instants.len(), 2, "{sql}");
        assert!(instants[0] >= (before - chrono::Duration::minutes(minutes)).timestamp_millis());
        assert!(instants[0] <= (after - chrono::Duration::minutes(minutes)).timestamp_millis());
        assert!(instants[1] >= before.timestamp_millis());
        assert!(instants[1] <= after.timestamp_millis());
        assert_eq!(instants[1] - instants[0], minutes * 60_000);
    } else {
        let start = DateTime::parse_from_rfc3339(body["start_time"].as_str().unwrap())
            .expect("start must preserve an instant");
        let end = DateTime::parse_from_rfc3339(body["end_time"].as_str().unwrap())
            .expect("end must preserve an instant");
        assert!(start >= before - chrono::Duration::minutes(minutes));
        assert!(start <= after - chrono::Duration::minutes(minutes));
        assert!(end >= before && end <= after);
        assert!(
            (end - start - chrono::Duration::minutes(minutes))
                .num_milliseconds()
                .abs()
                < 1000
        );
        if command == "histogram" {
            assert_eq!(
                state.histogram_requests[0]["start_timestamp"],
                start.timestamp_millis()
            );
            assert_eq!(
                state.histogram_requests[0]["end_timestamp"],
                end.timestamp_millis()
            );
        }
    }
}

#[test]
fn query_since_preserves_fallback_instants() {
    since_across_fallback("query", false);
}
#[test]
fn sql_since_preserves_fallback_instants() {
    since_across_fallback("sql", false);
    since_across_fallback("sql", true);
}
#[test]
fn tail_since_preserves_fallback_instants() {
    since_across_fallback("tail", false);
}
#[test]
fn histogram_since_preserves_fallback_instants() {
    since_across_fallback("histogram", false);
}

/// A macro inside a comment renders nothing on the server, so it must not stop
/// the CLI from adding the ClickHouse time condition for --since.
#[test]
fn a_commented_macro_keeps_the_since_filter() {
    let server = FakeServer::start(false);
    let home = TestHome::new("commented-macro");
    home.write_config(&config_with(&server.url, json!({"token": PAT})));
    let mut cmd = home.logchef();
    cmd.args([
        "sql",
        "SELECT * FROM logs /* {{__from}} */",
        "--since",
        "2h",
        "--team",
        "1",
        "--source",
        "2",
        "--output",
        "json",
    ]);
    let out = run(cmd);
    assert_success(&out);
    let state = server.state();
    let body = &state.native_requests[0];
    assert!(
        body["query_text"]
            .as_str()
            .unwrap()
            .contains("fromUnixTimestamp64Milli"),
        "a macro in a comment dropped the time condition: {body}"
    );
    assert!(body.get("start_time").is_none(), "{body}");
}
