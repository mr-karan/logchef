//! `logchef alerts`, `silences` and `destinations` through the real binary
//! against the in-test server. Linux only: the tests place the config under
//! `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use serde_json::{Value, json};

fn signed_in(name: &str, server: &FakeServer) -> TestHome {
    let home = TestHome::new(name);
    home.write_config(&config_with(&server.url, json!({"token": PAT})));
    home
}

fn logchef(home: &TestHome, args: &[&str]) -> std::process::Output {
    let mut cmd = home.logchef();
    cmd.args(args);
    run(cmd)
}

fn alert() -> Value {
    json!({
        "id": 4, "source_id": 2, "name": "5xx burst", "query_language": "logchefql",
        "editor_mode": "condition", "query": "", "condition_json": "status>=500",
        "lookback_seconds": 300, "threshold_operator": "gt", "threshold_value": 10,
        "frequency_seconds": 60, "severity": "critical", "destination_ids": [3],
        "group_by": ["service"], "no_data_policy": "ok", "notify_on_resolve": true,
        "is_active": true, "last_state": "firing", "created_at": "2026-11-01T00:00:00Z",
        "updated_at": "2026-11-01T00:00:00Z",
        "instance_counts": {"firing": 1, "no_data": 0, "ok": 3}
    })
}

#[test]
fn alerts_list_prints_every_rule_as_a_table_and_as_raw_json() {
    let server = FakeServer::start(false);
    server.stub("GET", "/api/v1/alerts", Stub::data(json!([alert()])));
    let home = signed_in("alerts-list", &server);

    let out = logchef(&home, &["alerts", "list"]);
    assert_success(&out);
    let text = stdout(&out);
    let row = text.lines().nth(2).unwrap();
    for cell in [
        "5xx burst",
        "critical",
        "firing (1 firing, 0 no data, 3 ok)",
        "yes",
        "> 10 over 5m every 1m",
    ] {
        assert!(row.contains(cell), "{cell}: {text}");
    }
    assert_eq!(
        server.requests_to("GET", "/api/v1/alerts")[0].path,
        "/api/v1/alerts"
    );

    let out = logchef(&home, &["alerts", "list", "--output", "json"]);
    assert_success(&out);
    let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(printed, json!([alert()]));
}

#[test]
fn alerts_list_resolves_a_source_name_within_the_team() {
    let server = FakeServer::start(false);
    server.stub(
        "GET",
        "/api/v1/teams/1/sources",
        Stub::data(json!([{"id": 2, "name": "app-logs"}])),
    );
    server.stub("GET", "/api/v1/alerts", Stub::data(json!([])));
    let home = signed_in("alerts-list-source", &server);
    let out = logchef(&home, &["alerts", "list", "-t", "1", "-S", "app-logs"]);
    assert_success(&out);
    assert_eq!(stdout(&out), "No alert rules found.\n");
    assert_eq!(
        server.requests_to("GET", "/api/v1/alerts")[0].path,
        "/api/v1/alerts?source_id=2"
    );
}

fn rule() -> Value {
    json!({
        "query_language": "logchefql", "editor_mode": "condition",
        "condition_json": "status>=500", "lookback_seconds": 300,
        "threshold_operator": "gt", "threshold_value": 10, "group_by": ["service"]
    })
}

fn test_result() -> Value {
    json!({
        "value": 12, "threshold_met": true, "execution_time_ms": 8, "rows_returned": 2,
        "warnings": ["2 groups have no data"], "groups_total": 2, "no_data": false,
        "samples": [
            {"group": {"service": "api"}, "value": 12, "threshold_met": true},
            {"group": {"service": "web"}, "value": 3.5, "threshold_met": false}
        ]
    })
}

#[test]
fn alerts_test_posts_the_file_with_the_source_and_prints_the_samples() {
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/test", Stub::data(test_result()));
    let home = signed_in("alerts-test", &server);
    let file = home.root.join("rule.json");
    std::fs::write(&file, rule().to_string()).unwrap();

    let out = logchef(
        &home,
        &["alerts", "test", "-f", file.to_str().unwrap(), "-S", "2"],
    );
    assert_success(&out);
    let mut sent = rule();
    sent["source_id"] = 2.into();
    assert_eq!(
        server.requests_to("POST", "/api/v1/alerts/test")[0].json(),
        sent
    );
    let text = stdout(&out);
    for line in [
        "Value:          12",
        "Threshold met:  yes",
        "No data:        no",
        "Rows:           2",
        "Execution:      8 ms",
        "Samples (2 of 2 groups):",
    ] {
        assert!(text.contains(line), "{line}: {text}");
    }
    let rows: Vec<Vec<&str>> = text
        .lines()
        .skip_while(|l| !l.starts_with("---"))
        .skip(1)
        .map(|l| l.split_whitespace().collect())
        .collect();
    assert_eq!(
        rows,
        [
            vec!["service=api", "12", "yes"],
            vec!["service=web", "3.5", "no"]
        ]
    );
    assert_eq!(stderr(&out), "Warning: 2 groups have no data\n");
}

#[test]
fn alerts_test_reads_stdin_and_keeps_the_request_source() {
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/test", Stub::data(test_result()));
    let home = signed_in("alerts-test-stdin", &server);
    let mut request = rule();
    request["source_id"] = 7.into();
    let mut cmd = home.logchef();
    cmd.args(["alerts", "test", "-f", "-", "-S", "2", "--output", "json"]);
    let out = run_with_stdin(cmd, &request.to_string());
    assert_success(&out);
    assert_eq!(
        server.requests_to("POST", "/api/v1/alerts/test")[0].json(),
        request
    );
    let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(printed, test_result());
}

#[test]
fn alerts_test_without_a_source_names_the_fix_and_sends_nothing() {
    let server = FakeServer::start(false);
    let home = signed_in("alerts-test-nosource", &server);
    let mut cmd = home.logchef();
    cmd.args(["alerts", "test", "-f", "-"]);
    let out = run_with_stdin(cmd, &rule().to_string());
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("pass -S <source>"),
        "{}",
        stderr(&out)
    );
    assert!(server.requests_to("POST", "/api/v1/alerts/test").is_empty());
}

#[test]
fn alerts_test_surfaces_the_server_validation_message() {
    let server = FakeServer::start(false);
    server.stub(
        "POST",
        "/api/v1/alerts/test",
        Stub::json(
            400,
            json!({"status": "error", "message": "threshold_operator must be one of gt, gte, lt, lte, eq, neq", "error_type": "ValidationError"}),
        ),
    );
    let home = signed_in("alerts-test-invalid", &server);
    let mut cmd = home.logchef();
    cmd.args(["alerts", "test", "-f", "-", "-S", "2"]);
    let out = run_with_stdin(cmd, &rule().to_string());
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("threshold_operator must be one of"),
        "{}",
        stderr(&out)
    );
}

fn backtest_result() -> Value {
    json!({
        "start": "2026-11-24T10:00:00Z", "end": "2026-11-25T10:00:00Z", "step_seconds": 300,
        "evaluations": 120, "coverage": 0.5, "partial": false,
        "summary": {"episodes": 2, "firing_seconds": 5400, "breaching_evaluations": 18,
                    "notifications": 4, "no_data_evaluations": 0, "errors": 1},
        "series": [{"t": "2026-11-24T10:00:00Z", "status": "ok", "value": null, "firing_groups": 0}],
        "groups": [{"group": {"service": "api"}, "episodes": 2, "breaching_evaluations": 18,
                    "max_value": 40}],
        "groups_omitted": 3, "first_error": "query timed out"
    })
}

#[test]
fn alerts_backtest_sets_the_range_and_frequency_and_prints_the_summary() {
    let server = FakeServer::start(false);
    server.stub(
        "POST",
        "/api/v1/alerts/backtest",
        Stub::data(backtest_result()),
    );
    let home = signed_in("alerts-backtest", &server);
    let mut request = rule();
    request["source_id"] = 2.into();
    request["range_seconds"] = 3600.into();
    let mut cmd = home.logchef();
    cmd.args([
        "alerts",
        "backtest",
        "-f",
        "-",
        "--range",
        "24h",
        "--frequency",
        "300",
    ]);
    let out = run_with_stdin(cmd, &request.to_string());
    assert_success(&out);
    request["range_seconds"] = 86400.into();
    request["frequency_seconds"] = 300.into();
    assert_eq!(
        server.requests_to("POST", "/api/v1/alerts/backtest")[0].json(),
        request
    );
    let text = stdout(&out);
    for line in [
        "Range:            2026-11-24 10:00 to 2026-11-25 10:00 UTC, 120 evaluations every 5m",
        "Episodes:         2",
        "Notifications:    4",
        "Firing time:      1h30m",
        "Breaching evals:  18",
        "Errors:           1",
        "Coverage:         50.0%",
        "Partial:          no",
        "First error:      query timed out",
        "Top groups (1, 3 more not listed):",
    ] {
        assert!(text.contains(line), "{line}: {text}");
    }
    let last: Vec<&str> = text.lines().last().unwrap().split_whitespace().collect();
    assert_eq!(last, ["service=api", "2", "18", "40"]);
}

#[test]
fn alerts_backtest_surfaces_a_backtest_already_running() {
    let server = FakeServer::start(false);
    server.stub(
        "POST",
        "/api/v1/alerts/backtest",
        Stub::json(
            429,
            json!({"status": "error", "message": "A backtest is already running", "error_type": "RateLimitError"}),
        ),
    );
    let home = signed_in("alerts-backtest-busy", &server);
    let mut cmd = home.logchef();
    cmd.args(["alerts", "backtest", "-f", "-", "-S", "2"]);
    let out = run_with_stdin(cmd, &rule().to_string());
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(err.contains("Failed to backtest the alert rule"), "{err}");
    assert!(err.contains("A backtest is already running"), "{err}");
}

fn silences() -> Value {
    json!([
        {"id": 1, "source_id": 2, "matchers": {}, "comment": "maintenance window",
         "schedule_kind": "once", "starts_at": "2026-11-25T08:00:00Z",
         "ends_at": "2026-11-25T10:00:00Z", "created_at": "2026-11-01T00:00:00Z",
         "updated_at": "2026-11-01T00:00:00Z", "active": true},
        {"id": 2, "source_id": 2, "alert_id": 4, "matchers": {"service": "api"},
         "comment": "nightly deploys",
         "schedule_kind": "weekly", "weekly": {"weekdays": [1, 2, 3, 4, 5], "start": "22:00",
         "duration_minutes": 60, "timezone": "Asia/Kolkata"},
         "created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z",
         "active": false}
    ])
}

#[test]
fn silences_list_prints_the_schedules_of_a_source() {
    let server = FakeServer::start(false);
    server.stub("GET", "/api/v1/alert-silences", Stub::data(silences()));
    let home = signed_in("silences-list", &server);

    let out = logchef(&home, &["silences", "list", "-S", "2"]);
    assert_success(&out);
    assert_eq!(
        server.requests_to("GET", "/api/v1/alert-silences")[0].path,
        "/api/v1/alert-silences?source_id=2"
    );
    let text = stdout(&out);
    let rows: Vec<&str> = text.lines().skip(2).collect();
    assert!(
        rows[0].starts_with("1      all rules  (every instance)"),
        "{text}"
    );
    assert!(
        rows[0].contains("once 2026-11-25 08:00 to 2026-11-25 10:00 UTC"),
        "{text}"
    );
    assert!(rows[0].ends_with(" yes     maintenance window"), "{text}");
    assert!(rows[1].ends_with(" no      nightly deploys"), "{text}");
    assert!(
        rows[1].starts_with("2      4          service=api"),
        "{text}"
    );
    assert!(
        rows[1].contains("weekly Mon,Tue,Wed,Thu,Fri 22:00 +60m Asia/Kolkata"),
        "{text}"
    );

    let out = logchef(&home, &["silences", "list", "-S", "2", "--output", "json"]);
    let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(printed, silences());
}

#[test]
fn silences_list_needs_a_source() {
    let server = FakeServer::start(false);
    let home = signed_in("silences-nosource", &server);
    let out = logchef(&home, &["silences", "list"]);
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("Source not specified"),
        "{}",
        stderr(&out)
    );
}

#[test]
fn destinations_list_reads_the_summary_and_the_full_shape() {
    let summary = json!({"id": 3, "name": "oncall", "type": "slack", "scope": "team",
                         "team_id": 2, "is_active": true});
    let full = json!({"id": 5, "name": "pager", "type": "webhook", "scope": "global",
                      "config": {"url": "https://hooks.example.com/x"}, "has_secret": true,
                      "secret_hint": "…abcd", "is_active": false,
                      "created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z"});
    let server = FakeServer::start(false);
    server.stub(
        "GET",
        "/api/v1/notification-destinations",
        Stub::data(json!([summary, full])),
    );
    let home = signed_in("destinations-list", &server);

    let out = logchef(&home, &["destinations", "list"]);
    assert_success(&out);
    let rows: Vec<Vec<String>> = stdout(&out)
        .lines()
        .skip(2)
        .map(|l| l.split_whitespace().map(String::from).collect())
        .collect();
    assert_eq!(
        rows,
        [
            ["3", "oncall", "slack", "team", "2", "yes"],
            ["5", "pager", "webhook", "global", "-", "no"]
        ]
    );

    let out = logchef(&home, &["destinations", "list", "--output", "json"]);
    let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(printed, json!([summary, full]));
}
