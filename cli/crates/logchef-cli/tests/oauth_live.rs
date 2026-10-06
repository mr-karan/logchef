//! T-CLI-2 and T-CLI-9 against a real Logchef server with OAuth enabled and
//! local login. Skipped unless these are set:
//!
//! - `LOGCHEF_TEST_OAUTH_SERVER`: the server URL, equal to `server.public_url`
//! - `LOGCHEF_TEST_LOCAL_EMAIL`, `LOGCHEF_TEST_LOCAL_PASSWORD`: a local user
//!
//! The test signs in through the real consent API as that user, so use a
//! disposable server.
#![cfg(target_os = "linux")]

mod common;

use common::*;
use reqwest::header::{CONTENT_TYPE, COOKIE, LOCATION, ORIGIN, SET_COOKIE};
use std::time::Duration;

struct Live {
    url: String,
    email: String,
    password: String,
}

fn live() -> Option<Live> {
    let get = |k: &str| std::env::var(k).ok().filter(|v| !v.is_empty());
    Some(Live {
        url: get("LOGCHEF_TEST_OAUTH_SERVER")?
            .trim_end_matches('/')
            .to_string(),
        email: get("LOGCHEF_TEST_LOCAL_EMAIL")?,
        password: get("LOGCHEF_TEST_LOCAL_PASSWORD")?,
    })
}

/// Logs in locally and returns the session cookie header value.
async fn session_cookie(http: &reqwest::Client, live: &Live) -> String {
    let response = http
        .post(format!("{}/api/v1/auth/local/login", live.url))
        .json(&serde_json::json!({"email": live.email, "password": live.password}))
        .send()
        .await
        .unwrap();
    assert!(
        response.status().is_success(),
        "local login failed: {}",
        response.status()
    );
    response
        .headers()
        .get_all(SET_COOKIE)
        .iter()
        .filter_map(|v| v.to_str().ok()?.split(';').next().map(str::to_string))
        .collect::<Vec<_>>()
        .join("; ")
}

/// Runs `logchef auth --no-browser` and approves the request through the
/// consent API, as the SPA would. The local login runs first, so a failed
/// login (for example the server's 5 per minute limit) never leaves a CLI
/// waiting for its callback.
async fn sign_in(home: &TestHome, live: &Live, http: &reqwest::Client) {
    let cookie = session_cookie(http, live).await;
    let mut cmd = home.logchef();
    cmd.args(["auth", "--no-browser", "--server", &live.url]);
    let (child, request, _rest) = start_login(cmd);

    let mut authorize = url::Url::parse(&format!("{}/oauth/authorize", live.url)).unwrap();
    authorize.query_pairs_mut().extend_pairs(&request.params);
    let response = http.get(authorize).send().await.unwrap();
    assert_eq!(
        response.status(),
        302,
        "authorize did not redirect to consent"
    );
    let consent = url::Url::parse(&live.url)
        .unwrap()
        .join(response.headers()[LOCATION].to_str().unwrap())
        .unwrap();
    let id = consent
        .query_pairs()
        .find(|(k, _)| k == "request")
        .map(|(_, v)| v.into_owned())
        .expect("consent URL has no request id");

    let pending: serde_json::Value = http
        .get(format!("{}/api/v1/oauth/requests/{id}", live.url))
        .header(COOKIE, &cookie)
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    assert_eq!(pending["data"]["client"]["id"], "logchef-cli");
    assert_eq!(pending["data"]["resource_kind"], "api");
    assert_eq!(pending["data"]["offline_access"], true);

    let decision: serde_json::Value = http
        .post(format!("{}/api/v1/oauth/requests/{id}/decision", live.url))
        .header(COOKIE, &cookie)
        .header(ORIGIN, &live.url)
        .header(CONTENT_TYPE, "application/json")
        .body(r#"{"approve":true}"#)
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    let redirect = decision["data"]["redirect_url"]
        .as_str()
        .unwrap()
        .to_string();
    let page = tokio::task::spawn_blocking(move || browser_get(&redirect))
        .await
        .unwrap();
    assert!(page.starts_with("HTTP/1.1 200"), "{page}");
    let out =
        tokio::task::spawn_blocking(move || wait_with_timeout(child, Duration::from_secs(30)))
            .await
            .unwrap();
    assert_success(&out);
    assert!(!home.browser_opened());
}

fn context_name(live: &Live) -> String {
    url::Url::parse(&live.url)
        .unwrap()
        .host_str()
        .unwrap()
        .to_string()
}

#[tokio::test]
async fn live_refresh_race_and_logout_revocation() {
    let Some(live) = live() else {
        eprintln!("skipping: LOGCHEF_TEST_OAUTH_SERVER and local login credentials are not set");
        return;
    };
    let http = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .unwrap();
    let home = TestHome::new("live");
    sign_in(&home, &live, &http).await;
    let ctx = context_name(&live);

    // T-CLI-2: expire the access token, then refresh from several processes
    // at once. All succeed and the grant stays active.
    let mut config = home.read_config();
    let first_refresh = config["contexts"][&ctx]["oauth"]["refresh_token"].clone();
    config["contexts"][&ctx]["oauth"]["access_expires_at"] = "2020-01-01T00:00:00Z".into();
    home.write_config(&config);
    let children: Vec<_> = (0..4)
        .map(|_| {
            let mut cmd = home.logchef();
            cmd.args(["whoami", "--output", "json"])
                .stdout(std::process::Stdio::piped())
                .stderr(std::process::Stdio::piped());
            cmd.spawn().unwrap()
        })
        .collect();
    for child in children {
        let out =
            tokio::task::spawn_blocking(move || wait_with_timeout(child, Duration::from_secs(60)))
                .await
                .unwrap();
        assert_success(&out);
    }
    let rotated = home.read_config()["contexts"][&ctx]["oauth"]["refresh_token"].clone();
    assert_ne!(rotated, first_refresh, "the refresh token was not rotated");
    let mut status = home.logchef();
    status.args(["auth", "--status"]);
    let out = run(status);
    assert_success(&out);
    assert!(
        stdout(&out).contains("Method:   Logchef OAuth"),
        "{}",
        stdout(&out)
    );

    // T-CLI-9: logout revokes at the server, so the saved refresh token is
    // dead afterwards.
    let mut logout = home.logchef();
    logout.args(["auth", "--logout"]);
    assert_success(&run(logout));
    assert!(home.read_config()["contexts"][&ctx].get("oauth").is_none());
    let response = http
        .post(format!("{}/oauth/token", live.url))
        .form(&[
            ("grant_type", "refresh_token"),
            ("refresh_token", rotated.as_str().unwrap()),
            ("client_id", "logchef-cli"),
            ("resource", &format!("{}/api", live.url)),
        ])
        .send()
        .await
        .unwrap();
    assert_eq!(response.status(), 400);
    let body: serde_json::Value = response.json().await.unwrap();
    assert_eq!(body["error"], "invalid_grant");
}
