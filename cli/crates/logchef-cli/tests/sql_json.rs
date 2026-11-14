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
