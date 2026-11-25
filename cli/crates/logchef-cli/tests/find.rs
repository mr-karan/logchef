//! `logchef find` across several sources through the real binary against
//! the in-test server. Linux only: the tests place the config under
//! `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use serde_json::json;
use std::time::{Duration, Instant};

fn source(id: i64, name: &str) -> serde_json::Value {
    json!({"id": id, "name": name, "_meta_ts_field": "ts", "source_type": "clickhouse",
           "connection": {"database": "logs", "table_name": name}})
}

fn server_with_sources() -> FakeServer {
    let server = FakeServer::start(false);
    server.stub(
        "GET",
        "/api/v1/me/teams",
        Stub::data(json!([{"id": 1, "name": "platform"}])),
    );
    server.stub(
        "GET",
        "/api/v1/teams/1/sources",
        Stub::data(json!([source(1, "fast"), source(2, "slow")])),
    );
    let schema = Stub::data(json!([{"name": "service", "type": "String"}]));
    server.stub("GET", "/api/v1/teams/1/sources/1/schema", schema.clone());
    server.stub(
        "GET",
        "/api/v1/teams/1/sources/2/schema",
        schema.delay(Duration::from_secs(6)),
    );
    server.stub(
        "POST",
        "/api/v1/teams/1/sources/1/logs/query",
        Stub::data(json!({"logs": [{"matches": 42}], "columns": [], "stats": {}})),
    );
    server
}

#[test]
fn find_prints_the_matches_and_names_a_source_that_timed_out() {
    let server = server_with_sources();
    let home = TestHome::new("find-slow");
    home.write_config(&config_with(&server.url, json!({"token": PAT})));
    let started = Instant::now();
    let mut cmd = home.logchef();
    cmd.args([
        "find",
        "checkout-api",
        "-s",
        "24h",
        "--timeout",
        "1",
        "--no-samples",
    ]);
    let out = run(cmd);
    assert!(
        started.elapsed() < Duration::from_secs(5),
        "{:?}",
        started.elapsed()
    );
    assert_success(&out);
    assert_eq!(
        stdout(&out),
        "team=1 source=1 (logs.fast) matches=42 in 24h columns=service\n"
    );
    assert_eq!(
        stderr(&out),
        "Skipped 1 team or source (raise --timeout for slow sources):\n  \
         source 2 slow (team 1): timed out after 1s\n"
    );
    let count = &server.requests_to("POST", "/api/v1/teams/1/sources/1/logs/query")[0];
    assert!(
        count.json()["query_text"]
            .as_str()
            .unwrap()
            .contains("'checkout-api'"),
        "{}",
        count.body
    );
}

#[test]
fn find_fails_when_no_source_could_be_searched() {
    let server = server_with_sources();
    let home = TestHome::new("find-all-slow");
    home.write_config(&config_with(&server.url, json!({"token": PAT})));
    let mut cmd = home.logchef();
    cmd.args([
        "find",
        "x",
        "-S",
        "slow",
        "--timeout",
        "1",
        "--output",
        "json",
    ]);
    let out = run(cmd);
    assert!(!out.status.success());
    assert_eq!(stdout(&out), "[]\n");
    let err = stderr(&out);
    assert!(
        err.contains("source 2 slow (team 1): timed out after 1s"),
        "{err}"
    );
    assert!(err.contains("No source could be searched"), "{err}");
}
