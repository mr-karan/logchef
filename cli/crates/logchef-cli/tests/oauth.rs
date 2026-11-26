//! `logchef auth` and OAuth credential use, through the real binary, real
//! loopback sockets, and real config files, against an in-test server.
//! Linux only: the tests place the config under `$HOME/.config/logchef`.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use std::process::Command;
use std::time::Duration;

/// Acts as the browser: registers the PKCE challenge the CLI sent, then
/// follows the redirect back to the CLI's loopback listener. Returns the
/// output, the rest of the CLI's stdout and stderr lines, and the request.
fn complete_login(
    server: &FakeServer,
    cmd: Command,
) -> (std::process::Output, String, AuthorizeRequest) {
    let (child, request, rest) = start_login(cmd);
    {
        let mut state = server.state();
        state.challenge = Some(request.get("code_challenge").to_string());
        state.redirect_uri = Some(request.get("redirect_uri").to_string());
    }
    let callback = format!(
        "{}?code={CODE}&state={}&iss={}",
        request.get("redirect_uri"),
        request.get("state"),
        url::form_urlencoded::byte_serialize(server.url.as_bytes()).collect::<String>()
    );
    let page = browser_get(&callback);
    assert!(page.starts_with("HTTP/1.1 200"), "{page}");
    let out = wait_with_timeout(child, Duration::from_secs(20));
    let lines: Vec<String> = rest.iter().collect();
    (out, lines.join("\n"), request)
}

#[test]
fn login_uses_logchef_oauth_and_saves_a_private_context() {
    let server = FakeServer::start(true);
    let home = TestHome::new("login");
    let mut cmd = home.logchef();
    cmd.args(["auth", "--server", &server.url]);
    let (out, output, request) = complete_login(&server, cmd);
    assert_success(&out);

    // The authorization request (T-CLI-6: stdin is not a terminal here).
    assert_eq!(request.get("response_type"), "code");
    assert_eq!(request.get("client_id"), "logchef-cli");
    assert_eq!(request.get("code_challenge_method"), "S256");
    assert_eq!(request.get("code_challenge").len(), 43);
    assert_eq!(request.get("resource"), format!("{}/api", server.url));
    assert_eq!(
        request.get("scope"),
        format!("{READ_SCOPES} offline_access")
    );
    let redirect = url::Url::parse(request.get("redirect_uri")).unwrap();
    assert_eq!(redirect.scheme(), "http");
    assert_eq!(redirect.host_str(), Some("127.0.0.1"));
    assert_eq!(redirect.path(), "/callback");
    assert!(redirect.port().is_some_and(|p| p != 0));
    assert!(!home.browser_opened(), "a browser was opened without a TTY");
    assert!(
        output.contains("Authenticated as user@example.com"),
        "{output}"
    );

    // The saved context (T-CLI-4).
    let config = home.read_config();
    let oauth = &config["contexts"]["127.0.0.1"]["oauth"];
    assert_eq!(oauth["issuer"], server.url.as_str());
    assert_eq!(oauth["client_id"], "logchef-cli");
    assert_eq!(oauth["access_token"], "access-1");
    assert_eq!(oauth["refresh_token"], "refresh-1");
    assert_eq!(oauth["scopes"].as_array().unwrap().len(), 10);
    assert!(config["contexts"]["127.0.0.1"].get("token").is_none());
    assert_eq!(mode(&home.config_path()), 0o600);
    assert_eq!(mode(&home.config_dir()), 0o700);

    // auth --status reads the method, scopes, and expiry from /me.
    let mut status = home.logchef();
    status.args(["auth", "--status"]);
    let out = run(status);
    assert_success(&out);
    let text = stdout(&out);
    assert!(
        text.contains("Method:   Logchef OAuth (client logchef-cli)"),
        "{text}"
    );
    assert!(text.contains(&format!("Scopes:   {READ_SCOPES}")), "{text}");
    assert!(text.contains("Expires:  2099-01-01T00:00:00Z"), "{text}");
    assert!(
        text.contains(&format!("Instance: {}", server.url)),
        "{text}"
    );
}

/// A server from before saved views rejects `saved_views:read`, so the CLI
/// asks only for the scopes the server advertises.
#[test]
fn login_requests_only_the_scopes_the_server_advertises() {
    let server = FakeServer::start(true);
    let older = "profile:read teams:read sources:read logs:read saved_queries:read collections:read alerts:read";
    server.state().scopes_supported = Some(
        format!("{older} offline_access")
            .split(' ')
            .map(String::from)
            .collect(),
    );
    let home = TestHome::new("login-older-server");
    let mut cmd = home.logchef();
    cmd.args(["auth", "--server", &server.url]);
    let (out, _, request) = complete_login(&server, cmd);
    assert_success(&out);
    assert_eq!(request.get("scope"), format!("{older} offline_access"));
}

#[test]
fn ci_and_no_browser_never_open_a_browser() {
    for (args, env) in [(vec!["--no-browser"], None), (vec![], Some(("CI", "true")))] {
        let server = FakeServer::start(true);
        let home = TestHome::new("nobrowser");
        let mut cmd = home.logchef();
        cmd.args(["auth", "--server", &server.url]).args(&args);
        if let Some((k, v)) = env {
            cmd.env(k, v);
        }
        let (out, _, _) = complete_login(&server, cmd);
        assert_success(&out);
        assert!(
            !home.browser_opened(),
            "browser opened with {args:?} {env:?}"
        );
    }
}

/// T-CLI-6 with a real terminal on stdin (through util-linux `script`): the
/// browser opens only when neither `CI` nor `--no-browser` is set. This also
/// proves the fake launcher detects an attempt.
#[test]
fn terminal_opens_browser_unless_ci_or_no_browser() {
    let script = std::path::Path::new("/usr/bin/script");
    if !script.exists() {
        eprintln!("skipping: /usr/bin/script is not installed");
        return;
    }
    for (extra, ci, expect_open) in [
        ("", false, true),
        (" --no-browser", false, false),
        ("", true, false),
    ] {
        let server = FakeServer::start(true);
        let home = TestHome::new("tty");
        // `script` runs the command on a pseudo-terminal.
        let mut cmd = home.command(script);
        cmd.args([
            "-qec",
            &format!(
                "{} auth --server {}{extra}",
                env!("CARGO_BIN_EXE_logchef"),
                server.url
            ),
            "/dev/null",
        ]);
        if ci {
            cmd.env("CI", "1");
        }
        let (out, _, _) = complete_login(&server, cmd);
        assert_success(&out);
        assert_eq!(
            home.browser_opened(),
            expect_open,
            "extra={extra:?} ci={ci}"
        );
    }
}

#[test]
fn auth_without_a_server_never_prompts() {
    let home = TestHome::new("noprompt");
    let mut cmd = home.logchef();
    cmd.arg("auth");
    let out = run(cmd);
    assert!(!out.status.success());
    assert!(
        stderr(&out).contains("Pass --server <url>"),
        "{}",
        stderr(&out)
    );
}

#[test]
fn server_without_oauth_issuer_fails_with_the_fix() {
    let server = FakeServer::start(false);
    let home = TestHome::new("nooauth");
    let mut cmd = home.logchef();
    cmd.args(["auth", "--server", &server.url]);
    let out = run(cmd);
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(
        err.contains("does not offer Logchef OAuth sign-in"),
        "{err}"
    );
    assert!(err.contains("Upgrade the server"), "{err}");
    assert!(
        err.contains("Create one under your user menu > Profile > API tokens (/settings/profile)"),
        "{err}"
    );
    assert!(err.contains("--token"), "{err}");
    assert!(err.contains("LOGCHEF_AUTH_TOKEN"), "{err}");
    assert!(!home.config_path().exists());
}

#[test]
fn expired_access_token_is_refreshed_once_and_saved() {
    let server = FakeServer::start(true);
    let home = TestHome::new("refresh");
    let credential = server.issue_grant(true);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["whoami", "--output", "json"]);
    assert_success(&run(cmd));
    assert_eq!(server.state().refresh_calls, 1);
    assert_eq!(server.state().bearer_seen.first().unwrap(), "access-2");
    let saved = &home.read_config()["contexts"]["test"]["oauth"];
    assert_eq!(saved["access_token"], "access-2");
    assert_eq!(saved["refresh_token"], "refresh-2");

    // The next run uses the saved token and does not refresh.
    let mut cmd = home.logchef();
    cmd.args(["whoami", "--output", "json"]);
    assert_success(&run(cmd));
    assert_eq!(server.state().refresh_calls, 1);
}

#[test]
fn rejected_access_token_triggers_one_refresh_and_retry() {
    let server = FakeServer::start(true);
    let home = TestHome::new("reject");
    let mut credential = server.issue_grant(false);
    credential["access_token"] = "access-unknown-to-server".into();
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["whoami", "--output", "json"]);
    let out = run(cmd);
    assert_success(&out);
    assert_eq!(server.state().refresh_calls, 1);
    assert_eq!(
        server.state().bearer_seen[..2],
        [
            "access-unknown-to-server".to_string(),
            "access-2".to_string()
        ]
    );
}

/// The local version of T-CLI-2: several processes find the same expired
/// access token at once. The server treats a reused refresh token as replay
/// and revokes the grant, so this passes only if exactly one process refreshes
/// and the others pick up its result.
#[test]
fn concurrent_processes_share_one_refresh() {
    let server = FakeServer::start(true);
    server.state().refresh_delay = Duration::from_millis(300);
    let home = TestHome::new("concurrent");
    let credential = server.issue_grant(true);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let children: Vec<_> = (0..6)
        .map(|_| {
            let mut cmd = home.logchef();
            cmd.args(["whoami", "--output", "json"])
                .stdout(std::process::Stdio::piped())
                .stderr(std::process::Stdio::piped());
            cmd.spawn().unwrap()
        })
        .collect();
    for child in children {
        assert_success(&wait_with_timeout(child, Duration::from_secs(30)));
    }
    let state = server.state();
    assert_eq!(state.refresh_calls, 1);
    assert!(!state.grant_revoked);
    assert_eq!(
        home.read_config()["contexts"]["test"]["oauth"]["refresh_token"],
        "refresh-2"
    );
}

/// T-CLI-5: a failed refresh ends in a structured auth error. With
/// `--output json`, stdout holds only the JSON error object.
#[test]
fn failed_refresh_is_a_structured_auth_error() {
    let server = FakeServer::start(true);
    let home = TestHome::new("authfail");
    let credential = server.issue_grant(true);
    server.state().grant_revoked = true;
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["whoami", "--output", "json"]);
    let out = run(cmd);
    assert_eq!(out.status.code(), Some(1));
    let json: serde_json::Value = serde_json::from_str(&stdout(&out))
        .unwrap_or_else(|e| panic!("stdout is not one JSON value ({e}): {}", stdout(&out)));
    assert_eq!(json["error"]["type"], "auth_required");
    assert_eq!(json["error"]["fix"], "logchef auth --context test");
    assert!(
        json["error"]["message"]
            .as_str()
            .unwrap()
            .contains("invalid_grant")
    );
    assert!(stderr(&out).contains("To fix: logchef auth --context test"));
    assert_eq!(server.state().refresh_calls, 1);
    assert!(!home.browser_opened());

    // Text output: nothing on stdout, the error and fix on stderr.
    let mut cmd = home.logchef();
    cmd.arg("whoami");
    let out = run(cmd);
    assert_eq!(out.status.code(), Some(1));
    assert_eq!(stdout(&out), "");
    assert!(stderr(&out).contains("logchef auth --context test"));
}

#[test]
fn missing_credential_is_a_structured_auth_error() {
    let server = FakeServer::start(true);
    let home = TestHome::new("nocred");
    home.write_config(&config_with(&server.url, serde_json::json!({})));

    let mut cmd = home.logchef();
    cmd.args(["teams", "--output", "jsonl"]);
    let out = run(cmd);
    assert_eq!(out.status.code(), Some(1));
    let json: serde_json::Value = serde_json::from_str(stdout(&out).trim()).unwrap();
    assert_eq!(json["error"]["type"], "auth_required");
    assert_eq!(json["error"]["fix"], "logchef auth --context test");
    assert!(server.state().bearer_seen.is_empty());
}

/// T-CLI-7: a context saved by an older CLI with a PAT still loads, works,
/// and is not rewritten.
#[test]
fn legacy_pat_context_still_works() {
    let server = FakeServer::start(true);
    let home = TestHome::new("legacy");
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({"token": PAT, "token_expires_at": "2099-01-01T00:00:00Z"}),
    ));
    let before = std::fs::read(home.config_path()).unwrap();

    let mut cmd = home.logchef();
    cmd.args(["whoami", "--output", "json"]);
    assert_success(&run(cmd));
    assert_eq!(
        server.state().bearer_seen,
        [PAT.to_string(), PAT.to_string()]
    );

    let mut status = home.logchef();
    status.args(["auth", "--status"]);
    let out = run(status);
    assert_success(&out);
    assert!(
        stdout(&out).contains("Method:   API token"),
        "{}",
        stdout(&out)
    );
    assert_eq!(std::fs::read(home.config_path()).unwrap(), before);
}

/// T-CLI-8: `--token` and `LOGCHEF_AUTH_TOKEN` win over a saved OAuth grant,
/// which is then neither sent nor refreshed.
#[test]
fn token_override_beats_oauth_context() {
    let server = FakeServer::start(true);
    let home = TestHome::new("override");
    let credential = server.issue_grant(true);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));
    let before = std::fs::read(home.config_path()).unwrap();

    let mut flag = home.logchef();
    flag.args(["whoami", "--output", "json", "--token", PAT]);
    assert_success(&run(flag));
    let mut env = home.logchef();
    env.args(["whoami", "--output", "json"])
        .env("LOGCHEF_AUTH_TOKEN", PAT);
    assert_success(&run(env));

    let state = server.state();
    assert_eq!(state.refresh_calls, 0);
    assert!(
        state.bearer_seen.iter().all(|b| b == PAT),
        "{:?}",
        state.bearer_seen
    );
    assert_eq!(std::fs::read(home.config_path()).unwrap(), before);
}

/// The local version of T-CLI-9.
#[test]
fn logout_revokes_at_the_server_then_removes_the_grant() {
    let server = FakeServer::start(true);
    let home = TestHome::new("logout");
    let credential = server.issue_grant(false);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["auth", "--logout"]);
    let out = run(cmd);
    assert_success(&out);
    assert!(stdout(&out).contains("Logged out from context 'test'"));
    let state = server.state();
    assert_eq!(state.revoked, ["refresh-1".to_string()]);
    assert!(state.grant_revoked);
    drop(state);
    let ctx = &home.read_config()["contexts"]["test"];
    assert!(ctx.get("oauth").is_none() && ctx.get("token").is_none());
    assert_eq!(ctx["server_url"], server.url.as_str());
}

#[test]
fn logout_removes_the_local_grant_even_when_revocation_fails() {
    let server = FakeServer::start(true);
    server.state().fail_revoke = true;
    let home = TestHome::new("logoutfail");
    let credential = server.issue_grant(false);
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["auth", "--logout"]);
    let out = run(cmd);
    assert_eq!(out.status.code(), Some(1));
    assert!(
        stderr(&out).contains("did not confirm the revocation"),
        "{}",
        stderr(&out)
    );
    assert!(
        home.read_config()["contexts"]["test"]
            .get("oauth")
            .is_none()
    );
}

const VIEWS: &str = "/api/v1/teams/1/sources/1/views";

/// A sign-in from before the server offered `saved_views:read`: refreshes
/// keep its old scopes, so the server denies the views list.
fn sign_in_without_saved_views(server: &FakeServer, home: &TestHome) {
    let mut credential = server.issue_grant(false);
    credential["scopes"] = READ_SCOPES
        .split(' ')
        .filter(|s| !["saved_views:read", "destinations:read"].contains(s))
        .collect::<Vec<_>>()
        .into();
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));
}

fn scope_denied(challenge: &str, message: &str) -> Stub {
    Stub::json(
        403,
        serde_json::json!({"status": "error", "message": message, "error_type": "AuthorizationError"}),
    )
    .header("WWW-Authenticate", challenge)
}

#[test]
fn a_denied_scope_names_the_scope_and_says_to_sign_in_again() {
    let server = FakeServer::start(true);
    server.stub(
        "GET",
        VIEWS,
        scope_denied(
            r#"Bearer error="insufficient_scope", scope="saved_views:read""#,
            "This sign-in does not grant the saved_views:read scope. Sign in again to grant it.",
        ),
    );
    let home = TestHome::new("scope-denied");
    sign_in_without_saved_views(&server, &home);

    let mut cmd = home.logchef();
    cmd.args(["views", "list", "-t", "1", "-S", "1"]);
    let out = run(cmd);
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(
        err.contains("lacks a scope this command needs: it does not grant saved_views:read"),
        "{err}"
    );
    assert!(err.contains("logchef auth --context test"), "{err}");

    let mut cmd = home.logchef();
    cmd.args(["views", "list", "-t", "1", "-S", "1", "--output", "json"]);
    let out = run(cmd);
    assert!(!out.status.success());
    let error: serde_json::Value = serde_json::from_str(&stdout(&out)).unwrap();
    assert_eq!(error["error"]["type"], "auth_required");
    assert_eq!(error["error"]["fix"], "logchef auth --context test");
}

/// Servers before v2.8.2 name no scope, so the CLI names the read scopes the
/// sign-in lacks.
#[test]
fn an_unnamed_denied_scope_lists_the_read_scopes_the_sign_in_lacks() {
    let server = FakeServer::start(true);
    server.stub(
        "GET",
        VIEWS,
        scope_denied(
            r#"Bearer error="insufficient_scope""#,
            "API token does not have the required scope",
        ),
    );
    let home = TestHome::new("scope-unnamed");
    sign_in_without_saved_views(&server, &home);

    let mut cmd = home.logchef();
    cmd.args(["views", "list", "-t", "1", "-S", "1"]);
    let out = run(cmd);
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(
        err.contains("lacks a scope this command needs: it does not grant saved_views:read, destinations:read"),
        "{err}"
    );
    assert!(err.contains("logchef auth --context test"), "{err}");
}

#[test]
fn an_api_token_without_the_scope_must_be_reissued() {
    let server = FakeServer::start(true);
    server.stub(
        "GET",
        VIEWS,
        scope_denied(
            r#"Bearer error="insufficient_scope", scope="saved_views:read""#,
            "API token does not have the required scope saved_views:read",
        ),
    );
    let home = TestHome::new("scope-pat");
    home.write_config(&config_with(&server.url, serde_json::json!({"token": PAT})));

    let mut cmd = home.logchef();
    cmd.args(["views", "list", "-t", "1", "-S", "1"]);
    let out = run(cmd);
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(
        err.contains(
            "Create one with the saved_views:read scope under your user menu > Profile > \
             API tokens (/settings/profile)"
        ),
        "{err}"
    );
    assert!(!err.contains("logchef auth"), "{err}");
}

#[test]
fn doctor_reports_read_scopes_the_sign_in_lacks() {
    let server = FakeServer::start(true);
    let home = TestHome::new("doctor-scopes");
    sign_in_without_saved_views(&server, &home);

    let mut cmd = home.logchef();
    cmd.args(["doctor", "--json"]);
    let out = run(cmd);
    assert!(!out.status.success(), "{}", stdout(&out));
    let checks: Vec<serde_json::Value> = serde_json::from_str(&stdout(&out)).unwrap();
    let check = checks
        .iter()
        .find(|c| c["check"] == "OAuth scopes")
        .unwrap_or_else(|| panic!("no scope check: {checks:#?}"));
    assert_eq!(check["status"], "fail");
    assert_eq!(
        check["detail"],
        "this sign-in does not grant saved_views:read, destinations:read"
    );
    assert!(
        check["hint"]
            .as_str()
            .unwrap()
            .contains("logchef auth --context test"),
        "{check}"
    );

    // A server older than destinations:read does not offer it, so only
    // saved_views:read is missing.
    server.state().scopes_supported = Some(
        READ_SCOPES
            .split(' ')
            .filter(|s| *s != "destinations:read")
            .map(String::from)
            .collect(),
    );
    let mut cmd = home.logchef();
    cmd.args(["doctor", "--json"]);
    let checks: Vec<serde_json::Value> = serde_json::from_str(&stdout(&run(cmd))).unwrap();
    let check = checks
        .iter()
        .find(|c| c["check"] == "OAuth scopes")
        .unwrap();
    assert_eq!(
        check["detail"],
        "this sign-in does not grant saved_views:read"
    );
}

#[test]
fn doctor_passes_a_sign_in_with_every_read_scope() {
    let server = FakeServer::start(true);
    let home = TestHome::new("doctor-scopes-ok");
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": server.issue_grant(false) }),
    ));
    let mut cmd = home.logchef();
    cmd.args(["doctor", "--json"]);
    let checks: Vec<serde_json::Value> = serde_json::from_str(&stdout(&run(cmd))).unwrap();
    let check = checks
        .iter()
        .find(|c| c["check"] == "OAuth scopes")
        .unwrap();
    assert_eq!(check["status"], "ok", "{check}");
}

/// A server older than destinations:read does not offer it, so a sign-in
/// that lacks it is not told to sign in again for a scope no sign-in can
/// hold, such as the reports scopes.
#[test]
fn an_unnamed_denied_scope_on_an_older_server_points_reports_to_an_api_token() {
    let server = FakeServer::start(true);
    let older: Vec<String> = READ_SCOPES
        .split(' ')
        .filter(|s| *s != "destinations:read")
        .map(String::from)
        .collect();
    server.state().scopes_supported = Some(older.clone());
    server.stub(
        "GET",
        "/api/v1/reports",
        scope_denied(
            r#"Bearer error="insufficient_scope""#,
            "API token does not have the required scope",
        ),
    );
    let home = TestHome::new("scope-older-reports");
    let mut credential = server.issue_grant(false);
    credential["scopes"] = older.into();
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": credential }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["reports", "list"]);
    let out = run(cmd);
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(
        err.contains("It needs an API token with the reports:read scope"),
        "{err}"
    );
    assert_eq!(
        err.matches("Create one under your user menu > Profile > API tokens (/settings/profile)")
            .count(),
        1,
        "{err}"
    );
    assert!(!err.contains("destinations:read"), "{err}");
}

/// No sign-in can hold a report scope, so the error gives the API-token fix
/// once and never says to sign in again.
#[test]
fn a_scope_no_sign_in_can_hold_points_to_an_api_token_once() {
    let server = FakeServer::start(true);
    server.stub(
        "GET",
        "/api/v1/reports",
        scope_denied(
            r#"Bearer error="insufficient_scope", scope="reports:read""#,
            "A Logchef OAuth sign-in cannot hold the reports:read scope. Use an API token that has it.",
        ),
    );
    let home = TestHome::new("scope-reports-oauth");
    home.write_config(&config_with(
        &server.url,
        serde_json::json!({ "oauth": server.issue_grant(false) }),
    ));

    let mut cmd = home.logchef();
    cmd.args(["reports", "list"]);
    let out = run(cmd);
    assert!(!out.status.success());
    let err = stderr(&out);
    assert!(!err.to_lowercase().contains("sign in again"), "{err}");
    assert!(!err.contains("logchef auth --context"), "{err}");
    assert_eq!(err.matches("sign-in cannot").count(), 1, "{err}");
    assert_eq!(err.matches("LOGCHEF_AUTH_TOKEN").count(), 1, "{err}");
    assert_eq!(err.matches("/settings/profile").count(), 1, "{err}");
    assert!(err.contains("reports:read"), "{err}");
}
