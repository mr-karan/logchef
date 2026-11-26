//! `logchef dashboards list` through the real binary against the in-test
//! server. Linux only: the tests place the config under
//! `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use serde_json::{Value, json};

#[test]
fn dashboards_list_asks_for_schema_2_and_prints_the_summaries() {
    let overview = json!({"id": 7, "name": "checkout-api overview", "description": "",
        "panel_count": 6, "panel_types": ["chart", "stat", "table", "toplist"], "tab_count": 2,
        "revision": 1, "can_edit": true, "can_delete": true, "corrupt": false, "report_count": 0,
        "created_at": "2026-11-20T10:00:00Z", "updated_at": "2026-11-21T09:30:00Z"});
    let broken = json!({"id": 9, "name": "old", "description": "", "panel_count": 0,
        "panel_types": [], "tab_count": 0, "revision": 4, "can_edit": false, "can_delete": false,
        "corrupt": true, "report_count": 0,
        "created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-02T00:00:00Z"});
    let server = FakeServer::start(false);
    server.stub(
        "GET",
        "/api/v1/dashboards",
        Stub::data(json!([overview, broken])),
    );
    let home = TestHome::new("dashboards-list");
    home.write_config(&config_with(&server.url, json!({"token": PAT})));

    let mut cmd = home.logchef();
    cmd.args(["dashboards", "list"]);
    let out = run(cmd);
    assert_success(&out);
    let rows: Vec<Vec<String>> = stdout(&out)
        .lines()
        .skip(2)
        .map(|l| l.split_whitespace().map(String::from).collect())
        .collect();
    assert_eq!(rows.len(), 2, "{}", stdout(&out));
    assert_eq!(
        rows[0],
        [
            "7",
            "checkout-api",
            "overview",
            "6",
            "2",
            "chart,stat,table,toplist",
            "yes",
            "2026-11-21",
            "09:30"
        ]
    );
    assert_eq!(
        rows[1],
        ["9", "old", "0", "0", "corrupt", "no", "2026-11-02", "00:00"]
    );
    let sent = server.requests_to("GET", "/api/v1/dashboards");
    assert_eq!(
        sent[0]
            .headers
            .get("x-logchef-dashboard-schema")
            .map(String::as_str),
        Some("2"),
        "without the schema header the server answers as to a schema 1 client"
    );

    let mut cmd = home.logchef();
    cmd.args(["dashboards", "list", "--output", "json"]);
    let out = run(cmd);
    let printed: Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(printed, json!([overview, broken]));
}
