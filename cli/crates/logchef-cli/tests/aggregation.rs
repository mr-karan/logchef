//! LogchefQL aggregations (RFC 0005) through the real binary against the
//! in-test server. Linux only: the tests place the config under
//! `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use serde_json::{Value, json};

fn grouped_info() -> Value {
    json!({
        "shape": "grouped", "interval": "", "limit": 0,
        "columns": [
            {"name": "service", "role": "group", "type": "String"},
            {"name": "count", "role": "value", "type": "UInt64"},
        ],
    })
}

fn grouped_response() -> (u16, Value) {
    (
        200,
        json!({"status": "success", "data": {
            "logs": [{"service": "api", "count": 12}, {"service": "web", "count": 3}],
            "columns": [{"name": "service", "type": "String"}, {"name": "count", "type": "UInt64"}],
            "stats": {"execution_time_ms": 2, "rows_read": 15, "bytes_read": 90},
            "generated_query_language": "clickhouse-sql",
            "aggregation": grouped_info(),
        }}),
    )
}

fn rejected(message: &str) -> (u16, Value) {
    (
        400,
        json!({"status": "error", "message": message, "error_type": "ValidationError"}),
    )
}

fn logchef(server: &FakeServer, name: &str, args: &[&str]) -> std::process::Output {
    let home = TestHome::new(name);
    home.write_config(&config_with(&server.url, json!({"token": PAT})));
    let mut cmd = home.logchef();
    cmd.args(args).args(["--team", "1", "--source", "2"]);
    run(cmd)
}

#[test]
fn query_prints_the_result_columns_as_a_table() {
    let server = FakeServer::start(false);
    server.state().query_response = Some(grouped_response());
    let out = logchef(
        &server,
        "agg-table",
        &["query", "level=error | count() by service"],
    );
    assert_success(&out);
    let dashes = "-".repeat(80);
    assert_eq!(
        stdout(&out),
        format!("service | count\n{dashes}\napi | 12\nweb | 3\n")
    );
    assert!(server.state().native_requests[0].get("interval").is_none());
}

#[test]
fn query_sends_the_interval_and_keeps_the_time_column_in_place() {
    let server = FakeServer::start(false);
    server.state().query_response = Some((
        200,
        json!({"status": "success", "data": {
            "logs": [{"count": 4, "service": "api", "time": "2026-11-16 10:00:00"}],
            "columns": [],
            "stats": {},
            "aggregation": {
                "shape": "grouped_timeseries", "interval": "5m", "limit": 0,
                "columns": [
                    {"name": "time", "role": "time", "type": "DateTime"},
                    {"name": "service", "role": "group", "type": "String"},
                    {"name": "count", "role": "value", "type": "UInt64"},
                ],
            },
        }}),
    ));
    let out = logchef(
        &server,
        "agg-interval",
        &[
            "query",
            "| count() by time, service",
            "--interval",
            "5m",
            "--output",
            "table",
        ],
    );
    assert_success(&out);
    assert_eq!(
        stdout(&out).lines().collect::<Vec<_>>(),
        [
            "time | service | count",
            &"-".repeat(80),
            "2026-11-16 10:00:00 | api | 4"
        ]
    );
    assert_eq!(server.state().native_requests[0]["interval"], "5m");
}

#[test]
fn query_without_an_interval_prints_the_server_message_and_fails() {
    let server = FakeServer::start(false);
    server.state().query_response = Some(rejected(
        "this query buckets by time, so the request needs an interval",
    ));
    let out = logchef(&server, "agg-no-interval", &["query", "| count() by time"]);
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("this query buckets by time, so the request needs an interval"),
        "stderr: {}",
        stderr(&out)
    );
}

#[test]
fn query_json_keeps_the_rows_and_adds_the_aggregation() {
    let server = FakeServer::start(false);
    server.state().query_response = Some(grouped_response());
    let out = logchef(
        &server,
        "agg-json",
        &["query", "| count() by service", "--output", "json"],
    );
    assert_success(&out);
    let output: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(
        output["logs"],
        json!([{"service": "api", "count": 12}, {"service": "web", "count": 3}])
    );
    assert_eq!(output["aggregation"], grouped_info());

    let out = logchef(
        &server,
        "agg-jsonl",
        &["query", "| count() by service", "--output", "jsonl"],
    );
    assert_success(&out);
    let rows: Vec<Value> = stdout(&out)
        .lines()
        .map(|line| serde_json::from_str(line).unwrap())
        .collect();
    assert_eq!(
        rows,
        [
            json!({"service": "api", "count": 12}),
            json!({"service": "web", "count": 3})
        ]
    );
}

#[test]
fn explain_prints_the_aggregated_query_over_the_default_range() {
    let server = FakeServer::start(false);
    let filter = json!({
        "valid": true, "sql": "(`level` = 'error')", "generated_query": "(`level` = 'error')",
        "generated_query_language": "clickhouse-sql", "aggregation": grouped_info(),
    });
    let mut ranged = filter.clone();
    ranged["full_sql"] =
        json!("SELECT `service` AS `service`, count() AS `count` GROUP BY `service`");
    server.state().translate_responses = [filter, ranged].into();
    let out = logchef(
        &server,
        "agg-explain",
        &[
            "explain",
            "level=error | count() by service",
            "--interval",
            "5m",
        ],
    );
    assert_success(&out);
    assert!(
        stdout(&out)
            .contains("SELECT `service` AS `service`, count() AS `count` GROUP BY `service`"),
        "stdout: {}",
        stdout(&out)
    );
    let requests = server.state().translate_requests.clone();
    assert_eq!(requests.len(), 2);
    assert!(requests[0].get("start_time").is_none());
    for key in ["start_time", "end_time", "timezone"] {
        assert!(requests[1][key].is_string(), "{key}: {}", requests[1]);
    }
    assert_eq!(requests[0]["interval"], "5m");
    assert_eq!(requests[1]["interval"], "5m");
}

#[test]
fn explain_of_a_filter_translates_once() {
    let server = FakeServer::start(false);
    server.state().translate_responses = [json!({
        "valid": true, "sql": "(`level` = 'error')", "generated_query": "(`level` = 'error')",
        "generated_query_language": "clickhouse-sql",
    })]
    .into();
    let out = logchef(&server, "agg-explain-filter", &["explain", "level=error"]);
    assert_success(&out);
    assert!(stdout(&out).contains("(`level` = 'error')"));
    assert_eq!(server.state().translate_requests.len(), 1);
}

#[test]
fn tail_prints_the_server_denial_and_stops() {
    let server = FakeServer::start(false);
    let message =
        "this feature needs log rows, and the query aggregates. Remove the pipe after the filter";
    server.state().tail_response = Some(rejected(message));
    let out = logchef(&server, "agg-tail", &["tail", "| count() by service"]);
    assert!(!out.status.success());
    assert!(stderr(&out).contains(message), "stderr: {}", stderr(&out));
    assert!(!stderr(&out).contains("reconnecting"));
}

#[test]
fn tail_poll_rejects_an_aggregated_response() {
    let server = FakeServer::start(false);
    server.state().query_response = Some(grouped_response());
    let out = logchef(
        &server,
        "agg-tail-poll",
        &["tail", "| count() by service", "--poll"],
    );
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("the query aggregates"),
        "stderr: {}",
        stderr(&out)
    );
    assert_eq!(stdout(&out), "");
}

#[test]
fn histogram_rejects_an_aggregation() {
    let server = FakeServer::start(false);
    server.state().translate_responses = [json!({
        "valid": true, "full_sql": "SELECT aggregated", "aggregation": grouped_info(),
    })]
    .into();
    let out = logchef(
        &server,
        "agg-histogram",
        &["histogram", "| count() by service"],
    );
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("the query aggregates"),
        "stderr: {}",
        stderr(&out)
    );
    assert!(server.state().histogram_requests.is_empty());
}

/// Runs a stored LogchefQL aggregation through `saved-queries` (by id) and
/// `collections` (by name).
fn run_saved(command: &str, extra: &[&str]) -> std::process::Output {
    let server = FakeServer::start(false);
    server.state().saved_query = json!({
        "id": 7, "source_id": 2, "name": "By service", "query_language": "logchefql",
        "editor_mode": "builder", "query_content": json!({
            "version": 2, "content": "level=error | count() by service", "limit": 10,
        }).to_string()
    });
    server.state().query_response = Some(grouped_response());
    let selector = if command == "collections" {
        "By service"
    } else {
        "7"
    };
    let mut args = vec![command, selector];
    args.extend_from_slice(extra);
    logchef(&server, &format!("agg-{command}"), &args)
}

#[test]
fn saved_queries_and_collections_print_an_aggregation_as_a_table() {
    let dashes = "-".repeat(80);
    for command in ["saved-queries", "collections"] {
        for output in ["text", "table"] {
            let out = run_saved(command, &["--output", output]);
            assert_success(&out);
            assert_eq!(
                stdout(&out),
                format!("service | count\n{dashes}\napi | 12\nweb | 3\n"),
                "{command} --output {output}"
            );
        }
    }
}

#[test]
fn saved_queries_and_collections_json_adds_the_aggregation() {
    for command in ["saved-queries", "collections"] {
        let out = run_saved(command, &["--output", "json"]);
        assert_success(&out);
        let output: Value = serde_json::from_str(&stdout(&out)).unwrap();
        assert_eq!(
            output["logs"],
            json!([{"service": "api", "count": 12}, {"service": "web", "count": 3}]),
            "{command}"
        );
        assert_eq!(output["aggregation"], grouped_info(), "{command}");
    }
}
