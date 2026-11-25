//! `logchef alerts`, `silences` and `destinations` through the real binary
//! against the in-test server. Linux only: the tests place the config under
//! `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use serde_json::{Value, json};

/// Signed in with an API token, in UTC so output does not follow the host's
/// zone.
fn signed_in(name: &str, server: &FakeServer) -> TestHome {
    signed_in_at(name, server, "UTC")
}

fn signed_in_at(name: &str, server: &FakeServer, timezone: &str) -> TestHome {
    let home = TestHome::new(name);
    let mut config = config_with(&server.url, json!({"token": PAT}));
    config["contexts"]["test"]["defaults"]["timezone"] = json!(timezone);
    home.write_config(&config);
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
        "Range:            2026-11-24 10:00:00 to 2026-11-25 10:00:00 UTC",
        "Evaluated 120 times, every 5m (lookback 5m): coverage 50% of the range",
        "Episodes:         2",
        "Notifications:    4",
        "Firing time:      1h30m",
        "Breaching evals:  18",
        "Errors:           1",
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

/// Servers up to v2.8.2 send `null` for an empty list.
#[test]
fn alerts_test_and_backtest_read_null_lists_in_text_and_json() {
    let mut test = test_result();
    test["warnings"] = Value::Null;
    test["samples"] = Value::Null;
    let mut backtest = backtest_result();
    backtest["warnings"] = Value::Null;
    backtest["groups"] = Value::Null;
    backtest["series"] = Value::Null;
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/test", Stub::data(test.clone()));
    server.stub(
        "POST",
        "/api/v1/alerts/backtest",
        Stub::data(backtest.clone()),
    );
    let home = signed_in("alerts-null-lists", &server);
    let request = rule().to_string();

    for (command, data, line) in [
        ("test", &test, "Value:          12"),
        ("backtest", &backtest, "Episodes:         2"),
    ] {
        let mut cmd = home.logchef();
        cmd.args(["alerts", command, "-f", "-", "-S", "2"]);
        let out = run_with_stdin(cmd, &request);
        assert_success(&out);
        assert!(stdout(&out).contains(line), "{}", stdout(&out));

        let mut cmd = home.logchef();
        cmd.args(["alerts", command, "-f", "-", "-S", "2", "--output", "json"]);
        let out = run_with_stdin(cmd, &request);
        assert_success(&out);
        let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
        assert_eq!(&printed, data);
    }
}

/// An ungrouped rule gets no samples: `null` from servers up to v2.8.2.
#[test]
fn alerts_test_reads_an_ungrouped_reply_with_null_samples() {
    let reply = json!({
        "value": 3, "threshold_met": false, "execution_time_ms": 5, "rows_returned": 1,
        "warnings": [], "samples": null, "groups_total": 0, "no_data": false
    });
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/test", Stub::data(reply.clone()));
    let home = signed_in("alerts-test-ungrouped", &server);
    let mut request = rule();
    request.as_object_mut().unwrap().remove("group_by");

    let mut cmd = home.logchef();
    cmd.args(["alerts", "test", "-f", "-", "-S", "2"]);
    let out = run_with_stdin(cmd, &request.to_string());
    assert_success(&out);
    assert_eq!(
        stdout(&out),
        "Value:          3\nThreshold met:  no\nNo data:        no\nRows:           1\n\
         Execution:      5 ms\n"
    );
    assert_eq!(stderr(&out), "");

    let mut cmd = home.logchef();
    cmd.args(["alerts", "test", "-f", "-", "-S", "2", "--output", "json"]);
    let out = run_with_stdin(cmd, &request.to_string());
    assert_success(&out);
    let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(printed, reply);
}

fn backtest(home: &TestHome, request: &Value, args: &[&str]) -> std::process::Output {
    let mut cmd = home.logchef();
    cmd.args(["alerts", "backtest", "-f", "-", "-S", "2"])
        .args(args);
    run_with_stdin(cmd, &request.to_string())
}

#[test]
fn alerts_backtest_from_and_to_set_the_end_and_the_range() {
    let server = FakeServer::start(false);
    server.stub(
        "POST",
        "/api/v1/alerts/backtest",
        Stub::data(backtest_result()),
    );
    let home = TestHome::new("alerts-backtest-window");
    let mut config = config_with(&server.url, json!({"token": PAT}));
    config["contexts"]["test"]["defaults"]["timezone"] = json!("Asia/Kolkata");
    home.write_config(&config);

    // RFC3339, and wall-clock in the effective timezone: 21:30 IST is 16:00 UTC.
    let out = backtest(
        &home,
        &rule(),
        &[
            "--from",
            "2026-11-24T10:00:00Z",
            "--to",
            "2026-11-24 21:30:00",
            "--frequency",
            "300",
        ],
    );
    assert_success(&out);
    let out = backtest(
        &home,
        &rule(),
        &["--end", "2026-11-24T16:00:00Z", "--range", "1h"],
    );
    assert_success(&out);
    let sent: Vec<(Value, Value)> = server
        .requests_to("POST", "/api/v1/alerts/backtest")
        .iter()
        .map(|r| {
            let body = r.json();
            (body["end_time"].clone(), body["range_seconds"].clone())
        })
        .collect();
    assert_eq!(
        sent,
        [
            (json!("2026-11-24T16:00:00Z"), json!(6 * 3600)),
            (json!("2026-11-24T16:00:00Z"), json!(3600)),
        ]
    );
}

#[test]
fn alerts_backtest_rejects_a_window_outside_1h_to_7d_or_mixed_with_range() {
    let server = FakeServer::start(false);
    let home = signed_in("alerts-backtest-window-bad", &server);
    for (args, message) in [
        (
            &[
                "--from",
                "2026-11-24T10:00:00Z",
                "--to",
                "2026-11-24T10:30:00Z",
            ][..],
            "spans 30m",
        ),
        (
            &[
                "--from",
                "2026-11-14T10:00:00Z",
                "--to",
                "2026-11-24T10:00:00Z",
            ][..],
            "spans 10d",
        ),
        (
            &[
                "--from",
                "2026-11-24T12:00:00Z",
                "--to",
                "2026-11-24T10:00:00Z",
            ][..],
            "--to must be after --from",
        ),
        (
            &[
                "--from",
                "2026-11-24T10:00:00Z",
                "--to",
                "2026-11-24T16:00:00Z",
                "--range",
                "1h",
            ][..],
            "cannot be used with",
        ),
        (
            &[
                "--from",
                "2026-11-24T10:00:00Z",
                "--to",
                "2026-11-24T16:00:00Z",
                "--end",
                "2026-11-24T16:00:00Z",
            ][..],
            "cannot be used with",
        ),
        (&["--from", "2026-11-24T10:00:00Z"][..], "--to"),
        (&["--end", "yesterday"][..], "Invalid time format"),
    ] {
        let out = backtest(&home, &rule(), args);
        assert!(!out.status.success(), "{args:?}");
        assert!(stderr(&out).contains(message), "{args:?}: {}", stderr(&out));
    }
    assert!(
        server
            .requests_to("POST", "/api/v1/alerts/backtest")
            .is_empty()
    );
}

#[test]
fn alerts_backtest_prints_the_server_warnings_and_whole_result_episodes() {
    let mut result = backtest_result();
    result["coverage"] = json!(0.4167);
    result["step_seconds"] = json!(720);
    result["warnings"] = json!(["the evaluations read 42% of the range"]);
    result["summary"]["episodes"] = json!(5);
    result["summary"]["empty_result_episodes"] = json!(3);
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/backtest", Stub::data(result));
    let home = signed_in("alerts-backtest-warnings", &server);

    let out = backtest(&home, &rule(), &[]);
    assert_success(&out);
    let text = stdout(&out);
    for line in [
        "Evaluated 120 times, every 12m (lookback 5m): coverage 42% of the range",
        "Episodes:         5 (3 whole-result no-data episodes, not in the groups table)",
    ] {
        assert!(text.contains(line), "{line}: {text}");
    }
    assert_eq!(
        stderr(&out),
        "warning: the evaluations read 42% of the range\n"
    );
}

#[test]
fn alerts_backtest_warns_of_partial_coverage_an_older_server_left_unsaid() {
    let server = FakeServer::start(false);
    server.stub(
        "POST",
        "/api/v1/alerts/backtest",
        Stub::data(backtest_result()),
    );
    let home = signed_in("alerts-backtest-coverage", &server);
    let out = backtest(&home, &rule(), &[]);
    assert_success(&out);
    let err = stderr(&out);
    assert!(err.starts_with("warning: "), "{err}");
    for part in ["50%", "shorter than the 5m step", "--from/--to"] {
        assert!(err.contains(part), "{part}: {err}");
    }

    let mut full = backtest_result();
    full["coverage"] = json!(1.0);
    server.stub("POST", "/api/v1/alerts/backtest", Stub::data(full));
    let out = backtest(&home, &rule(), &[]);
    assert_success(&out);
    assert_eq!(stderr(&out), "");
}

#[test]
fn alerts_backtest_shows_the_lowest_value_of_a_below_threshold_rule() {
    let mut result = backtest_result();
    result["groups"] = json!([
        {"group": {"service": "api"}, "episodes": 2, "breaching_evaluations": 18,
         "min_value": 1.5, "max_value": 40},
        {"group": {"service": "web"}, "episodes": 0, "breaching_evaluations": 0,
         "min_value": null, "max_value": null}
    ]);
    let mut older = backtest_result();
    older["groups"] = json!([{"group": {"service": "api"}, "episodes": 2,
                              "breaching_evaluations": 18, "max_value": 40}]);
    let server = FakeServer::start(false);
    let home = signed_in("alerts-backtest-min", &server);
    let mut below = rule();
    below["threshold_operator"] = json!("lt");

    let table = |result: &Value, request: &Value| {
        server.stub(
            "POST",
            "/api/v1/alerts/backtest",
            Stub::data(result.clone()),
        );
        let out = backtest(&home, request, &[]);
        assert_success(&out);
        let text = stdout(&out);
        let mut lines = text.lines().skip_while(|l| !l.starts_with("GROUP"));
        let header = lines
            .next()
            .unwrap()
            .split_whitespace()
            .last()
            .unwrap()
            .to_owned();
        let rows: Vec<String> = lines
            .skip(1)
            .map(|l| l.split_whitespace().last().unwrap().to_owned())
            .collect();
        (header, rows)
    };
    assert_eq!(
        table(&result, &below),
        ("MIN".to_owned(), vec!["1.5".to_owned(), "-".to_owned()])
    );
    assert_eq!(
        table(&result, &rule()),
        ("MAX".to_owned(), vec!["40".to_owned(), "-".to_owned()])
    );
    assert_eq!(
        table(&older, &below),
        ("MAX".to_owned(), vec!["40".to_owned()])
    );
}

fn point(minute: u32, status: &str) -> Value {
    let t = chrono::DateTime::parse_from_rfc3339("2026-11-24T10:00:00Z").unwrap()
        + chrono::Duration::minutes(minute.into());
    json!({"t": t.to_rfc3339_opts(chrono::SecondsFormat::Secs, true), "status": status,
           "value": null, "firing_groups": u32::from(status == "firing")})
}

#[test]
fn alerts_backtest_lists_the_episodes_and_the_range_in_the_effective_timezone() {
    let mut result = backtest_result();
    result["series"] = json!([
        point(0, "ok"),
        point(5, "firing"),
        point(10, "error"),
        point(15, "ok"),
        point(20, "error"),
        point(25, "no_data"),
        point(30, "firing"),
        point(35, "ok"),
        point(40, "ok"),
        point(45, "firing"),
    ]);
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/backtest", Stub::data(result));
    let home = signed_in_at("alerts-backtest-episodes", &server, "Asia/Kolkata");

    let out = backtest(&home, &rule(), &[]);
    assert_success(&out);
    let text = stdout(&out);
    assert!(
        text.contains(
            "Range:            2026-11-24 15:30:00 to 2026-11-25 15:30:00 Asia/Kolkata\n"
        ),
        "{text}"
    );
    assert!(
        text.contains(
            "\nFiring or no-data episodes (Asia/Kolkata):\n\
             2026-11-24 15:35:00 to 2026-11-24 15:45:00\n\
             2026-11-24 15:55:00 to 2026-11-24 16:05:00\n\
             2026-11-24 16:15:00 to ongoing at the end of the range\n\n"
        ),
        "{text}"
    );
}

#[test]
fn alerts_backtest_lists_at_most_20_episodes() {
    let mut result = backtest_result();
    result["series"] = (0..50)
        .map(|i| point(i * 5, if i % 2 == 0 { "firing" } else { "ok" }))
        .collect();
    let server = FakeServer::start(false);
    server.stub("POST", "/api/v1/alerts/backtest", Stub::data(result));
    let home = signed_in("alerts-backtest-episodes-cap", &server);

    let out = backtest(&home, &rule(), &[]);
    assert_success(&out);
    let text = stdout(&out);
    let listed: Vec<&str> = text
        .lines()
        .skip_while(|l| !l.starts_with("Firing or no-data episodes"))
        .skip(1)
        .take_while(|l| !l.is_empty())
        .collect();
    assert_eq!(listed.len(), 21, "{text}");
    assert_eq!(listed[0], "2026-11-24 10:00:00 to 2026-11-24 10:05:00");
    assert_eq!(listed[19], "2026-11-24 13:10:00 to 2026-11-24 13:15:00");
    assert_eq!(listed[20], "... and 5 more");
}
