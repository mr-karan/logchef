//! `logchef sql --output json` through the real binary against the in-test
//! server. Linux only: the tests place the config under `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;

fn sql_json(name: &str, stats: serde_json::Value) -> serde_json::Value {
    let server = FakeServer::start(false);
    server.state().query_stats = stats;
    server.state().query_logs = vec![serde_json::json!({"n": 1})];
    let home = TestHome::new(name);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({"token": PAT, "token_expires_at": "2099-01-01T00:00:00Z"}),
    ));
    let mut cmd = home.logchef();
    cmd.args([
        "sql",
        "SELECT n FROM logs.app ORDER BY n DESC LIMIT 1",
        "--team",
        "1",
        "--source",
        "2",
        "--output",
        "json",
    ]);
    let out = run(cmd);
    assert_success(&out);
    serde_json::from_str(&stdout(&out)).unwrap()
}

#[test]
fn sql_json_keeps_the_limit_source_and_truncation() {
    let output = sql_json(
        "sql-json-limit",
        serde_json::json!({
            "execution_time_ms": 4, "rows_read": 9, "bytes_read": 72,
            "limit_source": "statement", "truncation": "none",
        }),
    );
    assert_eq!(
        output["stats"],
        serde_json::json!({
            "execution_time_ms": 4, "rows_read": 9, "bytes_read": 72,
            "limit_source": "statement", "truncation": "none",
        })
    );
    assert_eq!(output["count"], 1);
}

/// A server from before the LIMIT rule sends neither field, and the output
/// leaves both out rather than inventing a state.
#[test]
fn sql_json_reads_a_server_without_them() {
    let output = sql_json(
        "sql-json-older-server",
        serde_json::json!({"execution_time_ms": 4, "rows_read": 9, "bytes_read": 72}),
    );
    assert_eq!(
        output["stats"],
        serde_json::json!({"execution_time_ms": 4, "rows_read": 9, "bytes_read": 72})
    );
}

/// Loki and VictoriaLogs report sub-millisecond durations, so the server can
/// send `execution_time_ms` as a decimal. The CLI rounds it to whole
/// milliseconds instead of failing to read the response.
#[test]
fn sql_json_reads_a_fractional_execution_time() {
    let output = sql_json(
        "sql-json-fractional-ms",
        serde_json::json!({"execution_time_ms": 4.764, "rows_read": 9, "bytes_read": 72}),
    );
    assert_eq!(output["stats"]["execution_time_ms"], 5);
    assert_eq!(output["count"], 1);
}

/// Loki rejects a query without a time range, and a LogQL query has no place
/// to splice one, so `sql` on a Loki source always sends the window in the
/// request and leaves the query text alone.
#[test]
fn sql_on_a_loki_source_sends_the_default_window() {
    let server = FakeServer::start(false);
    server.state().source_type = Some("loki".to_string());
    server.state().query_logs = vec![serde_json::json!({"_msg": "boom"})];
    let home = TestHome::new("sql-loki-window");
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({"token": PAT, "token_expires_at": "2099-01-01T00:00:00Z"}),
    ));
    let query = r#"{service_name="payments-api"} |= "error""#;
    let mut cmd = home.logchef();
    cmd.args([
        "sql", query, "--team", "1", "--source", "2", "--output", "json",
    ]);
    let out = run(cmd);
    assert_success(&out);

    let requests = server.state().native_requests.clone();
    assert_eq!(requests.len(), 1);
    assert_eq!(requests[0]["query_text"], query);
    let start = chrono::DateTime::parse_from_rfc3339(requests[0]["start_time"].as_str().unwrap())
        .expect("start_time is RFC3339");
    let end = chrono::DateTime::parse_from_rfc3339(requests[0]["end_time"].as_str().unwrap())
        .expect("end_time is RFC3339");
    assert_eq!(end - start, chrono::Duration::minutes(15));
}

fn dry_run(name: &str, source_type: &str, sql: &str) -> String {
    let server = FakeServer::start(false);
    server.state().source_type = Some(source_type.to_string());
    let home = TestHome::new(name);
    let mut config = config_with(&server.url, serde_json::json!({"token": PAT}));
    config["contexts"]["test"]["defaults"]["timezone"] = "Asia/Kolkata".into();
    home.write_config(&config);
    let mut cmd = home.logchef();
    cmd.args(["sql", "-t", "1", "-S", "2", "--dry-run"])
        .args([
            "--from",
            "2026-11-25 13:30:00",
            "--to",
            "2026-11-25 14:30:00",
        ])
        .arg(sql);
    let out = run(cmd);
    assert_success(&out);
    stdout(&out)
}

#[test]
fn dry_run_renders_clickhouse_time_macros_with_the_source_timestamp() {
    assert_eq!(
        dry_run(
            "dry-run-macros",
            "clickhouse",
            "SELECT count() FROM logs.app WHERE {{__time_filter}} AND t < {{__to}}"
        ),
        "SELECT count() FROM logs.app WHERE `timestamp` >= toDateTime64('2026-11-25 08:00:00.000', 3, 'UTC') \
         AND `timestamp` < toDateTime64('2026-11-25 09:00:00.000', 3, 'UTC') \
         AND t < toDateTime64('2026-11-25 09:00:00.000', 3, 'UTC')\n"
    );
}

#[test]
fn dry_run_keeps_victorialogs_queries_as_written() {
    assert_eq!(
        dry_run("dry-run-vl", "victorialogs", "error | stats count()"),
        "error | stats count()\n"
    );
}
