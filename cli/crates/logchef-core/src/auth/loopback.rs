//! The loopback redirect listener for the authorization code flow
//! (RFC 8252 §7.3). It accepts one `GET /callback` that carries the expected
//! `state` and `iss`, and nothing else. Every wait is bounded: per connection,
//! overall, and by a cancel flag that Ctrl-C sets.

use crate::error::{Error, Result};
use std::io::{ErrorKind, Read, Write};
use std::net::{Ipv4Addr, TcpListener, TcpStream};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};
use tracing::debug;
use url::Url;

/// Longest request line accepted. Real callbacks are far shorter.
const MAX_REQUEST_LINE: usize = 8 * 1024;
/// How often a blocked accept or read checks the cancel flag and deadlines.
const POLL_INTERVAL: Duration = Duration::from_millis(50);
const WRITE_TIMEOUT: Duration = Duration::from_secs(2);
const MAX_ERROR_TEXT: usize = 300;

#[derive(Clone, Copy, Debug)]
pub struct CallbackLimits {
    /// The whole wait, from bind to callback.
    pub overall: Duration,
    /// One connection, from accept to the end of its request line.
    pub per_connection: Duration,
}

impl Default for CallbackLimits {
    fn default() -> Self {
        Self {
            overall: Duration::from_secs(5 * 60),
            per_connection: Duration::from_secs(10),
        }
    }
}

/// What a valid callback must carry: the `state` sent in the authorization
/// request and the issuer recorded before the redirect (RFC 9207).
#[derive(Clone, Debug)]
pub struct ExpectedCallback {
    pub state: String,
    pub issuer: String,
}

pub struct CallbackListener {
    listener: TcpListener,
    redirect_uri: String,
}

enum Outcome {
    Code(String),
    Fail(Error),
    Ignore,
}

impl CallbackListener {
    /// Binds `127.0.0.1` on an ephemeral port.
    pub fn bind() -> Result<Self> {
        let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0))?;
        listener.set_nonblocking(true)?;
        let port = listener.local_addr()?.port();
        Ok(Self {
            listener,
            redirect_uri: format!("http://127.0.0.1:{port}/callback"),
        })
    }

    pub fn redirect_uri(&self) -> &str {
        &self.redirect_uri
    }

    /// Waits on the calling thread for the authorization code. Requests with
    /// another path or method, or a `state` that does not match, get an error
    /// page and the wait goes on. A matching `state` ends the wait: with the
    /// code, or with an error for a wrong `iss`, an `error=` response, or a
    /// missing code.
    pub fn wait(
        self,
        expected: &ExpectedCallback,
        limits: CallbackLimits,
        cancel: &AtomicBool,
    ) -> Result<String> {
        let deadline = Instant::now() + limits.overall;
        loop {
            if cancel.load(Ordering::SeqCst) {
                return Err(Error::AuthCancelled);
            }
            let now = Instant::now();
            if now >= deadline {
                return Err(Error::AuthTimeout);
            }
            match self.listener.accept() {
                Ok((stream, _)) => {
                    let conn_deadline = deadline.min(now + limits.per_connection);
                    match handle_connection(stream, expected, conn_deadline, cancel)? {
                        Outcome::Code(code) => return Ok(code),
                        Outcome::Fail(err) => return Err(err),
                        Outcome::Ignore => {}
                    }
                }
                Err(e) if e.kind() == ErrorKind::WouldBlock => std::thread::sleep(POLL_INTERVAL),
                Err(e) if e.kind() == ErrorKind::Interrupted => {}
                Err(e) => return Err(e.into()),
            }
        }
    }
}

/// Runs [`CallbackListener::wait`] on a blocking thread. Ctrl-C sets the
/// cancel flag. The thread is joined on every path before this returns.
pub async fn wait_for_code(
    listener: CallbackListener,
    expected: ExpectedCallback,
    limits: CallbackLimits,
) -> Result<String> {
    let cancel = Arc::new(AtomicBool::new(false));
    let flag = Arc::clone(&cancel);
    let mut task = tokio::task::spawn_blocking(move || listener.wait(&expected, limits, &flag));
    let ctrl_c = async {
        if tokio::signal::ctrl_c().await.is_err() {
            std::future::pending::<()>().await;
        }
    };
    let joined = tokio::select! {
        joined = &mut task => joined,
        () = ctrl_c => {
            cancel.store(true, Ordering::SeqCst);
            task.await
        }
    };
    joined.map_err(|e| Error::other(format!("Callback listener failed: {e}")))?
}

fn handle_connection(
    mut stream: TcpStream,
    expected: &ExpectedCallback,
    deadline: Instant,
    cancel: &AtomicBool,
) -> Result<Outcome> {
    // BSD and macOS hand out accepted sockets with the listener's
    // non-blocking flag. Reads below rely on timeouts instead.
    if stream.set_nonblocking(false).is_err() {
        return Ok(Outcome::Ignore);
    }
    let Some(line) = read_request_line(&mut stream, deadline, cancel)? else {
        debug!("Callback connection closed, timed out, or sent an oversized request line");
        return Ok(Outcome::Ignore);
    };
    let (outcome, status, page) = evaluate(&line, expected);
    respond(&mut stream, status, page);
    Ok(outcome)
}

/// Reads up to the first `\n`. Returns `None` when the peer closes, the
/// deadline passes, or the line exceeds [`MAX_REQUEST_LINE`].
fn read_request_line(
    stream: &mut TcpStream,
    deadline: Instant,
    cancel: &AtomicBool,
) -> Result<Option<String>> {
    let mut buf = Vec::with_capacity(512);
    let mut chunk = [0u8; 1024];
    loop {
        match buf.iter().position(|&b| b == b'\n') {
            Some(end) if end <= MAX_REQUEST_LINE => {
                buf.truncate(end);
                return Ok(String::from_utf8(buf).ok());
            }
            Some(_) => return Ok(None),
            None if buf.len() > MAX_REQUEST_LINE => return Ok(None),
            None => {}
        }
        if cancel.load(Ordering::SeqCst) {
            return Err(Error::AuthCancelled);
        }
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return Ok(None);
        }
        stream.set_read_timeout(Some(remaining.min(POLL_INTERVAL)))?;
        match stream.read(&mut chunk) {
            Ok(0) => return Ok(None),
            Ok(n) => buf.extend_from_slice(&chunk[..n]),
            Err(e) if matches!(e.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut) => {}
            Err(e) if e.kind() == ErrorKind::Interrupted => {}
            Err(_) => return Ok(None),
        }
    }
}

fn evaluate(line: &str, expected: &ExpectedCallback) -> (Outcome, u16, &'static str) {
    let mut parts = line.trim_end_matches('\r').split(' ');
    let (Some(method), Some(target)) = (parts.next(), parts.next()) else {
        return (Outcome::Ignore, 400, PAGE_BAD_REQUEST);
    };
    if method != "GET" {
        return (Outcome::Ignore, 405, PAGE_BAD_REQUEST);
    }
    if !target.starts_with('/') {
        return (Outcome::Ignore, 400, PAGE_BAD_REQUEST);
    }
    let Ok(url) = Url::parse(&format!("http://127.0.0.1{target}")) else {
        return (Outcome::Ignore, 400, PAGE_BAD_REQUEST);
    };
    if url.path() != "/callback" {
        return (Outcome::Ignore, 404, PAGE_NOT_FOUND);
    }

    let param = |name: &str| -> std::result::Result<Option<String>, ()> {
        let mut values = url.query_pairs().filter(|(k, _)| k == name);
        let first = values.next().map(|(_, v)| v.into_owned());
        if values.next().is_some() {
            return Err(());
        }
        Ok(first)
    };
    let (Ok(state), Ok(iss), Ok(error), Ok(description), Ok(code)) = (
        param("state"),
        param("iss"),
        param("error"),
        param("error_description"),
        param("code"),
    ) else {
        return (Outcome::Ignore, 400, PAGE_BAD_REQUEST);
    };

    // Nothing in a response for another login attempt is acted on.
    if state.as_deref() != Some(expected.state.as_str()) {
        debug!("Ignoring callback with a state that does not match this login");
        return (Outcome::Ignore, 400, PAGE_STATE_MISMATCH);
    }
    if iss.as_deref() != Some(expected.issuer.as_str()) {
        let got = iss
            .as_deref()
            .map(printable)
            .unwrap_or_else(|| "none".into());
        return (
            Outcome::Fail(Error::oauth(format!(
                "the authorization response came from issuer {got}, expected {}",
                expected.issuer
            ))),
            400,
            PAGE_FAILED,
        );
    }
    if let Some(error) = error {
        return (
            Outcome::Fail(Error::OAuthRedirect {
                error: printable(&error),
                description: description.as_deref().map(printable),
            }),
            200,
            PAGE_FAILED,
        );
    }
    match code {
        Some(code) if !code.is_empty() => (Outcome::Code(code), 200, PAGE_SUCCESS),
        _ => (
            Outcome::Fail(Error::oauth("the authorization response has no code")),
            400,
            PAGE_FAILED,
        ),
    }
}

/// Server-supplied text goes to the terminal. Control characters are dropped
/// so it cannot move the cursor or change colors, and the length is capped.
fn printable(text: &str) -> String {
    text.chars()
        .filter(|c| !c.is_control())
        .take(MAX_ERROR_TEXT)
        .collect()
}

fn respond(stream: &mut TcpStream, status: u16, page: &str) {
    let reason = match status {
        200 => "OK",
        404 => "Not Found",
        405 => "Method Not Allowed",
        _ => "Bad Request",
    };
    let response = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nCache-Control: no-store\r\nReferrer-Policy: no-referrer\r\nConnection: close\r\n\r\n{page}",
        page.len()
    );
    let _ = stream.set_write_timeout(Some(WRITE_TIMEOUT));
    let _ = stream.write_all(response.as_bytes());
    let _ = stream.flush();
}

const PAGE_SUCCESS: &str = "<!DOCTYPE html><html><head><title>Logchef CLI</title></head><body style=\"font-family:system-ui;text-align:center;padding-top:50px\"><h1>Signed in</h1><p>You can close this window and return to the terminal.</p></body></html>";
const PAGE_FAILED: &str = "<!DOCTYPE html><html><head><title>Logchef CLI</title></head><body style=\"font-family:system-ui;text-align:center;padding-top:50px\"><h1>Sign-in failed</h1><p>Return to the terminal for details.</p></body></html>";
const PAGE_STATE_MISMATCH: &str = "<!DOCTYPE html><html><head><title>Logchef CLI</title></head><body style=\"font-family:system-ui;text-align:center;padding-top:50px\"><h1>Unknown sign-in</h1><p>This response does not belong to the sign-in that the terminal is waiting for.</p></body></html>";
const PAGE_BAD_REQUEST: &str = "Bad request";
const PAGE_NOT_FOUND: &str = "Not found";

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::SocketAddr;
    use std::sync::mpsc;
    use std::thread::JoinHandle;

    const ISSUER: &str = "https://logs.example.com";
    const STATE: &str = "state-123";

    fn expected() -> ExpectedCallback {
        ExpectedCallback {
            state: STATE.into(),
            issuer: ISSUER.into(),
        }
    }

    struct Running {
        addr: SocketAddr,
        cancel: Arc<AtomicBool>,
        done: mpsc::Receiver<Result<String>>,
        thread: JoinHandle<()>,
    }

    /// Runs `wait` on its own thread, the way `wait_for_code` does.
    fn start(limits: CallbackLimits) -> Running {
        let listener = CallbackListener::bind().unwrap();
        let addr = listener.listener.local_addr().unwrap();
        assert_eq!(
            listener.redirect_uri(),
            format!("http://127.0.0.1:{}/callback", addr.port())
        );
        let cancel = Arc::new(AtomicBool::new(false));
        let flag = Arc::clone(&cancel);
        let (tx, done) = mpsc::channel();
        let thread = std::thread::spawn(move || {
            let _ = tx.send(listener.wait(&expected(), limits, &flag));
        });
        Running {
            addr,
            cancel,
            done,
            thread,
        }
    }

    impl Running {
        /// Waits for the result, then joins the thread and checks that the
        /// listening socket is closed, so nothing outlives the wait.
        fn finish(self, within: Duration) -> Result<String> {
            let result = self
                .done
                .recv_timeout(within)
                .expect("wait did not return in time");
            self.thread.join().expect("listener thread panicked");
            assert!(
                TcpStream::connect(self.addr).is_err(),
                "listener still accepts connections after the wait"
            );
            result
        }

        fn still_waiting(&self) -> bool {
            matches!(self.done.try_recv(), Err(mpsc::TryRecvError::Empty))
        }
    }

    fn request(addr: SocketAddr, raw: &str) -> String {
        let mut stream = TcpStream::connect(addr).unwrap();
        stream.write_all(raw.as_bytes()).unwrap();
        stream
            .set_read_timeout(Some(Duration::from_secs(5)))
            .unwrap();
        let mut response = String::new();
        let _ = stream.read_to_string(&mut response);
        response
    }

    fn get(addr: SocketAddr, target: &str) -> String {
        request(
            addr,
            &format!("GET {target} HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"),
        )
    }

    fn callback(query: &str) -> String {
        format!("/callback?{query}")
    }

    fn valid(code: &str) -> String {
        callback(&format!(
            "code={code}&state={STATE}&iss={}",
            urlencoding::encode(ISSUER)
        ))
    }

    fn quick() -> CallbackLimits {
        CallbackLimits {
            overall: Duration::from_secs(10),
            per_connection: Duration::from_millis(300),
        }
    }

    #[test]
    fn valid_callback_returns_code() {
        let run = start(quick());
        let response = get(run.addr, &valid("abc"));
        assert!(response.starts_with("HTTP/1.1 200"), "{response}");
        assert!(!response.contains("abc"), "the page must not echo the code");
        assert_eq!(run.finish(Duration::from_secs(2)).unwrap(), "abc");
    }

    #[test]
    fn silent_connection_times_out_without_blocking_the_login() {
        let run = start(quick());
        let silent = TcpStream::connect(run.addr).unwrap();
        let started = Instant::now();
        let response = get(run.addr, &valid("after-silence"));
        assert!(response.starts_with("HTTP/1.1 200"), "{response}");
        assert!(
            started.elapsed() < Duration::from_secs(3),
            "silent connection held the listener for {:?}",
            started.elapsed()
        );
        assert_eq!(run.finish(Duration::from_secs(2)).unwrap(), "after-silence");
        drop(silent);
    }

    #[test]
    fn overall_deadline_ends_the_wait() {
        let run = start(CallbackLimits {
            overall: Duration::from_millis(300),
            per_connection: Duration::from_millis(100),
        });
        let err = run.finish(Duration::from_secs(3)).unwrap_err();
        assert!(matches!(err, Error::AuthTimeout), "{err:?}");
    }

    #[test]
    fn overall_deadline_also_bounds_a_trickling_connection() {
        let run = start(CallbackLimits {
            overall: Duration::from_millis(500),
            per_connection: Duration::from_secs(30),
        });
        let mut slow = TcpStream::connect(run.addr).unwrap();
        for _ in 0..5 {
            let _ = slow.write_all(b"G");
            std::thread::sleep(Duration::from_millis(80));
        }
        let err = run.finish(Duration::from_secs(3)).unwrap_err();
        assert!(matches!(err, Error::AuthTimeout), "{err:?}");
    }

    #[test]
    fn cancel_flag_stops_the_wait_even_during_a_silent_connection() {
        let run = start(CallbackLimits {
            overall: Duration::from_secs(60),
            per_connection: Duration::from_secs(60),
        });
        let _silent = TcpStream::connect(run.addr).unwrap();
        std::thread::sleep(Duration::from_millis(150));
        run.cancel.store(true, Ordering::SeqCst);
        let err = run.finish(Duration::from_secs(2)).unwrap_err();
        assert!(matches!(err, Error::AuthCancelled), "{err:?}");
    }

    #[test]
    fn error_response_fails_at_once() {
        let run = start(quick());
        let response = get(
            run.addr,
            &callback(&format!(
                "error=access_denied&error_description=User%20denied%1b%5b31m&state={STATE}&iss={}",
                urlencoding::encode(ISSUER)
            )),
        );
        assert!(response.contains("Sign-in failed"), "{response}");
        match run.finish(Duration::from_secs(2)).unwrap_err() {
            Error::OAuthRedirect { error, description } => {
                assert_eq!(error, "access_denied");
                assert_eq!(description.as_deref(), Some("User denied[31m"));
            }
            other => panic!("expected OAuthRedirect, got {other:?}"),
        }
    }

    #[test]
    fn mismatched_state_is_ignored_and_the_wait_continues() {
        let run = start(quick());
        for target in [
            callback(&format!(
                "code=evil&state=other&iss={}",
                urlencoding::encode(ISSUER)
            )),
            callback(&format!(
                "error=access_denied&state=other&iss={}",
                urlencoding::encode(ISSUER)
            )),
            callback("code=evil&iss=https%3A%2F%2Flogs.example.com"),
            callback(&format!(
                "code=evil&state={STATE}&state=other&iss={}",
                urlencoding::encode(ISSUER)
            )),
        ] {
            let response = get(run.addr, &target);
            assert!(response.starts_with("HTTP/1.1 400"), "{target}: {response}");
            std::thread::sleep(Duration::from_millis(50));
            assert!(run.still_waiting(), "{target} ended the wait");
        }
        get(run.addr, &valid("good"));
        assert_eq!(run.finish(Duration::from_secs(2)).unwrap(), "good");
    }

    #[test]
    fn mismatched_or_missing_iss_is_rejected() {
        for iss in ["&iss=https%3A%2F%2Fevil.example.com", ""] {
            let run = start(quick());
            let response = get(run.addr, &callback(&format!("code=abc&state={STATE}{iss}")));
            assert!(response.starts_with("HTTP/1.1 400"), "{response}");
            let err = run.finish(Duration::from_secs(2)).unwrap_err();
            assert!(
                matches!(&err, Error::OAuth(msg) if msg.contains("issuer")),
                "{err:?}"
            );
        }
    }

    #[test]
    fn missing_code_is_an_error() {
        let run = start(quick());
        get(
            run.addr,
            &callback(&format!(
                "state={STATE}&iss={}",
                urlencoding::encode(ISSUER)
            )),
        );
        let err = run.finish(Duration::from_secs(2)).unwrap_err();
        assert!(matches!(err, Error::OAuth(_)), "{err:?}");
    }

    #[test]
    fn other_paths_methods_and_oversized_lines_are_ignored() {
        let run = start(quick());
        assert!(get(run.addr, "/favicon.ico").starts_with("HTTP/1.1 404"));
        assert!(
            get(run.addr, &format!("/callback/x?code=a&state={STATE}")).starts_with("HTTP/1.1 404")
        );
        let post = request(
            run.addr,
            &format!(
                "POST {} HTTP/1.1\r\nContent-Length: 0\r\n\r\n",
                valid("posted")
            ),
        );
        assert!(post.starts_with("HTTP/1.1 405"), "{post}");
        let absolute = request(
            run.addr,
            &format!("GET http://evil.example{} HTTP/1.1\r\n\r\n", valid("abs")),
        );
        assert!(absolute.starts_with("HTTP/1.1 400"), "{absolute}");
        let huge = format!("{}&pad={}", valid("huge"), "a".repeat(MAX_REQUEST_LINE));
        assert_eq!(
            get(run.addr, &huge),
            "",
            "an oversized line gets no response"
        );
        assert!(run.still_waiting());

        get(run.addr, &valid("final"));
        assert_eq!(run.finish(Duration::from_secs(2)).unwrap(), "final");
    }

    #[tokio::test]
    async fn async_wait_returns_after_the_thread_finishes() {
        let listener = CallbackListener::bind().unwrap();
        let addr = listener.listener.local_addr().unwrap();
        let waiter = tokio::spawn(wait_for_code(listener, expected(), quick()));
        let target = valid("from-async");
        tokio::task::spawn_blocking(move || get(addr, &target))
            .await
            .unwrap();
        assert_eq!(waiter.await.unwrap().unwrap(), "from-async");
        assert!(TcpStream::connect(addr).is_err());
    }
}
