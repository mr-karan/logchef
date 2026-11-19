//! `logchef open` browser links, through the real binary against the in-test
//! server. Linux only: the tests place the config under `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;

fn open_command(
    server: &FakeServer,
    name: &str,
    extra: &[&str],
) -> (TestHome, std::process::Output) {
    let home = TestHome::new(name);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({"token": PAT, "token_expires_at": "2099-01-01T00:00:00Z"}),
    ));
    let mut cmd = home.logchef();
    cmd.args([
        "open", "--team", "1", "--source", "2", "--since", "1h", "--print",
    ])
    .args(extra);
    let out = run(cmd);
    (home, out)
}

fn open_print(server: &FakeServer, name: &str) -> String {
    open_print_with(server, name, &[])
}

fn open_print_with(server: &FakeServer, name: &str, extra: &[&str]) -> String {
    let (home, out) = open_command(server, name, extra);
    assert_success(&out);
    assert!(!home.browser_opened(), "--print opened a browser");
    stdout(&out).trim().to_string()
}

fn serve_views(server: &FakeServer) {
    server.state().saved_views = Some(serde_json::json!({
        "views": [
            {"id": 3, "name": "Errors", "scope": "personal", "is_default": false,
             "definition": {"version": 1, "filter": "level=\"error\""}},
            {"id": 7, "name": "Errors", "scope": "team", "is_default": false,
             "definition": {"version": 1, "columns": [{"name": "msg"}]}},
            {"id": 12, "name": "Checkout", "scope": "source", "is_default": true,
             "definition": {"version": 1, "sort": [{"column": "timestamp", "desc": true}]}}
        ],
        "default_view_id": 12
    }));
}

#[test]
fn open_view_id_adds_the_view_parameter() {
    let server = FakeServer::start(false);
    assert_eq!(
        open_print_with(&server, "open-view-id", &["--view", "12"]),
        format!("{}/logs/explore?team=1&source=2&view=12&t=1h", server.url)
    );
}

#[test]
fn open_view_name_resolves_to_its_id() {
    let server = FakeServer::start(false);
    serve_views(&server);
    assert_eq!(
        open_print_with(&server, "open-view-name", &["--view", "Checkout"]),
        format!("{}/logs/explore?team=1&source=2&view=12&t=1h", server.url)
    );
}

#[test]
fn open_view_name_in_two_scopes_fails_and_lists_both() {
    let server = FakeServer::start(false);
    serve_views(&server);
    let (home, out) = open_command(&server, "open-view-ambiguous", &["--view", "Errors"]);
    assert!(!out.status.success(), "an ambiguous name succeeded");
    assert!(stdout(&out).is_empty(), "{}", stdout(&out));
    let err = String::from_utf8_lossy(&out.stderr);
    assert!(err.contains("3 (personal)"), "{err}");
    assert!(err.contains("7 (team)"), "{err}");
    assert!(!home.browser_opened());
}

#[test]
fn views_list_prints_the_views_and_the_response_as_json() {
    let server = FakeServer::start(false);
    serve_views(&server);
    let home = TestHome::new("views-list");
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({"token": PAT, "token_expires_at": "2099-01-01T00:00:00Z"}),
    ));

    let mut cmd = home.logchef();
    cmd.args(["views", "list", "-t", "1", "-S", "2"]);
    let out = run(cmd);
    assert_success(&out);
    let text = stdout(&out);
    assert!(
        text.lines()
            .any(|l| l.split_whitespace().collect::<Vec<_>>()
                == ["12", "Checkout", "source", "yes", "sort"]),
        "{text}"
    );

    let mut cmd = home.logchef();
    cmd.args(["views", "list", "-t", "1", "-S", "2", "--output", "json"]);
    let out = run(cmd);
    assert_success(&out);
    let json: serde_json::Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(json, server.state().saved_views.clone().unwrap());
}

/// Two-host servers: the API URL the CLI talks to is not where browsers go.
/// The explorer link uses the advertised `ui_url`, not the API host.
#[test]
fn open_print_links_to_the_advertised_ui_url() {
    let server = FakeServer::start(true);
    server.state().ui_url = Some("https://logchef-ui.example.com".into());
    let url = open_print(&server, "open-two-host");
    assert_eq!(
        url,
        "https://logchef-ui.example.com/logs/explore?team=1&source=2&t=1h"
    );
    assert!(!url.contains(&server.url), "{url} uses the API host");
}

/// A `ui_url` with a base path keeps it.
#[test]
fn open_print_keeps_the_ui_base_path() {
    let server = FakeServer::start(true);
    server.state().ui_url = Some("https://example.com/logchef".into());
    assert_eq!(
        open_print(&server, "open-base-path"),
        "https://example.com/logchef/logs/explore?team=1&source=2&t=1h"
    );
}

/// An older server without `ui_url` keeps the previous behavior.
#[test]
fn open_print_falls_back_to_the_server_url() {
    let server = FakeServer::start(false);
    assert_eq!(
        open_print(&server, "open-fallback"),
        format!("{}/logs/explore?team=1&source=2&t=1h", server.url)
    );
}
