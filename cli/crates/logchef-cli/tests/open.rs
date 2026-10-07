//! `logchef open` browser links, through the real binary against the in-test
//! server. Linux only: the tests place the config under `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;

fn open_print(server: &FakeServer, name: &str) -> String {
    let home = TestHome::new(name);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({"token": PAT, "token_expires_at": "2099-01-01T00:00:00Z"}),
    ));
    let mut cmd = home.logchef();
    cmd.args([
        "open", "--team", "1", "--source", "2", "--since", "1h", "--print",
    ]);
    let out = run(cmd);
    assert_success(&out);
    assert!(!home.browser_opened(), "--print opened a browser");
    stdout(&out).trim().to_string()
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
