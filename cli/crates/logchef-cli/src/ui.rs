//! Shared human-output helpers: TTY/`--quiet` gating, number formatting, a
//! small SQL/LogsQL syntax highlighter, an elapsed spinner, and the top-level
//! error reporter with actionable hints.
//!
//! Everything here is for human `text` output only. Machine output
//! (`--output json`/`jsonl`) and pipes must stay clean and stable, so callers
//! gate every affordance below through [`human`] / [`stderr_human`] (which are
//! false when stdout/stderr is not a TTY, or when `--quiet` is set).

use std::io::{IsTerminal, Write};

/// True when human "chrome" tied to stdout (stats lines, colored/highlighted
/// stdout, tables) should be shown: stdout is a TTY and `--quiet` is unset.
/// Piping stdout or passing `--quiet` makes this false, keeping json/jsonl and
/// redirected output byte-for-byte clean.
pub fn human(quiet: bool) -> bool {
    !quiet && std::io::stdout().is_terminal()
}

/// True when a stderr-only affordance (spinner, `--show-sql` trace, error
/// hint) is appropriate: stderr is a TTY and `--quiet` is unset. Independent
/// of stdout, so these never contaminate piped stdout.
pub fn stderr_human(quiet: bool) -> bool {
    !quiet && std::io::stderr().is_terminal()
}

/// Formats an integer with thousands separators: `1234567` → `"1,234,567"`.
pub fn thousands(n: i64) -> String {
    let digits = n.unsigned_abs().to_string();
    let len = digits.len();
    let mut out = String::with_capacity(len + len / 3 + 1);
    if n < 0 {
        out.push('-');
    }
    for (i, ch) in digits.chars().enumerate() {
        if i != 0 && (len - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(ch);
    }
    out
}

/// Compact human count: `1234` → `"1.2k"`, `3_400_000` → `"3.4M"`. Values
/// under 1000 are returned as plain integers.
pub fn compact(n: i64) -> String {
    let sign = if n < 0 { "-" } else { "" };
    let v = n.unsigned_abs() as f64;
    let (num, suffix) = if v >= 1e12 {
        (v / 1e12, "T")
    } else if v >= 1e9 {
        (v / 1e9, "B")
    } else if v >= 1e6 {
        (v / 1e6, "M")
    } else if v >= 1e3 {
        (v / 1e3, "k")
    } else {
        return format!("{}{}", sign, n.unsigned_abs());
    };
    if num >= 100.0 {
        format!("{}{:.0}{}", sign, num, suffix)
    } else {
        format!("{}{:.1}{}", sign, num, suffix)
    }
}

/// Prints the standard `N logs | Xms | R rows read` summary to stderr with
/// counts humanized. No-op unless [`human`] (so it never appears in piped
/// output or under `--quiet`).
pub fn print_stats(quiet: bool, count: usize, exec_ms: i64, rows_read: i64) {
    if !human(quiet) {
        return;
    }
    eprintln!(
        "\n{} logs | {}ms | {} rows read",
        thousands(count as i64),
        thousands(exec_ms),
        thousands(rows_read)
    );
}

// ANSI styles for the tiny query highlighter. Kept local so machine output
// never touches them.
const RESET: &str = "\x1b[0m";
const KW: &str = "\x1b[1;36m"; // bold cyan — keywords
const STR: &str = "\x1b[32m"; // green — string literals
const NUM: &str = "\x1b[33m"; // yellow — numbers
const DIM: &str = "\x1b[2m"; // dim — pipes/operators

const SQL_KEYWORDS: &[&str] = &[
    "SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "GROUP", "BY", "ORDER", "LIMIT", "HAVING", "AS",
    "BETWEEN", "IN", "LIKE", "ILIKE", "ASC", "DESC", "ON", "JOIN", "LEFT", "RIGHT", "INNER",
    "OUTER", "SETTINGS", "FORMAT", "WITH", "DISTINCT", "NULL", "IS", "CASE", "WHEN", "THEN",
    "ELSE", "END", "INTERVAL", "UNION", "ALL", "USING", "PREWHERE", "ARRAY",
];

const LOGSQL_KEYWORDS: &[&str] = &[
    "stats",
    "count",
    "count_uniq",
    "uniq",
    "sum",
    "avg",
    "min",
    "max",
    "sort",
    "by",
    "limit",
    "fields",
    "filter",
    "head",
    "offset",
    "keep",
    "delete",
    "rename",
    "and",
    "or",
    "not",
];

/// Syntax-highlights a generated backend query for human display. `language`
/// is the server's `generated_query_language` (`"clickhouse-sql"`, `"logsql"`,
/// or `None`). Returns the input unchanged when `enabled` is false so piped /
/// `--quiet` output stays plain.
pub fn highlight_query(query: &str, language: Option<&str>, enabled: bool) -> String {
    if !enabled {
        return query.to_string();
    }
    match language {
        Some("logsql") => highlight_tokens(query, LOGSQL_KEYWORDS),
        _ => highlight_tokens(query, SQL_KEYWORDS),
    }
}

/// Character-scanning highlighter shared by SQL and LogsQL: colors single- and
/// double-quoted string literals, numbers, `|` pipe operators, and any word
/// matching `keywords` (case-insensitive). Everything else is passed through
/// unchanged, so it degrades gracefully on unfamiliar syntax.
fn highlight_tokens(s: &str, keywords: &[&str]) -> String {
    let chars: Vec<char> = s.chars().collect();
    let mut out = String::with_capacity(s.len() + 16);
    let mut i = 0;
    while i < chars.len() {
        let c = chars[i];
        if c == '\'' || c == '"' {
            let quote = c;
            let start = i;
            i += 1;
            while i < chars.len() {
                if chars[i] == '\\' && i + 1 < chars.len() {
                    i += 2;
                    continue;
                }
                if chars[i] == quote {
                    i += 1;
                    break;
                }
                i += 1;
            }
            out.push_str(STR);
            out.extend(chars[start..i].iter());
            out.push_str(RESET);
            continue;
        }
        if c.is_ascii_alphabetic() || c == '_' {
            let start = i;
            while i < chars.len() && (chars[i].is_ascii_alphanumeric() || chars[i] == '_') {
                i += 1;
            }
            let word: String = chars[start..i].iter().collect();
            if keywords.iter().any(|k| k.eq_ignore_ascii_case(&word)) {
                out.push_str(KW);
                out.push_str(&word);
                out.push_str(RESET);
            } else {
                out.push_str(&word);
            }
            continue;
        }
        if c.is_ascii_digit() {
            let start = i;
            while i < chars.len() && (chars[i].is_ascii_digit() || chars[i] == '.') {
                i += 1;
            }
            out.push_str(NUM);
            out.extend(chars[start..i].iter());
            out.push_str(RESET);
            continue;
        }
        if c == '|' {
            out.push_str(DIM);
            out.push('|');
            out.push_str(RESET);
            i += 1;
            continue;
        }
        out.push(c);
        i += 1;
    }
    out
}

/// A minimal stderr spinner for long-running queries. It runs a background
/// task that repaints a braille frame + elapsed seconds on stderr, and clears
/// the line on [`finish`](Spinner::finish). It is inert (prints nothing)
/// unless [`stderr_human`], so it never corrupts piped stdout or `--quiet`
/// runs. Always call `finish()` before printing results.
pub struct Spinner {
    handle: Option<tokio::task::JoinHandle<()>>,
}

impl Spinner {
    pub fn start(quiet: bool, message: &'static str) -> Self {
        if !stderr_human(quiet) {
            return Self { handle: None };
        }
        let handle = tokio::spawn(async move {
            const FRAMES: [char; 10] = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
            let start = std::time::Instant::now();
            let mut frame = 0usize;
            loop {
                eprint!(
                    "\r{}{} {} ({:.1}s){}\x1b[K",
                    DIM,
                    FRAMES[frame % FRAMES.len()],
                    message,
                    start.elapsed().as_secs_f64(),
                    RESET
                );
                let _ = std::io::stderr().flush();
                frame += 1;
                tokio::time::sleep(std::time::Duration::from_millis(90)).await;
            }
        });
        Self {
            handle: Some(handle),
        }
    }

    /// Stops the spinner and clears its line. Safe to call when inert.
    pub fn finish(mut self) {
        if let Some(handle) = self.handle.take() {
            handle.abort();
            eprint!("\r\x1b[K");
            let _ = std::io::stderr().flush();
        }
    }
}

impl Drop for Spinner {
    fn drop(&mut self) {
        if let Some(handle) = &self.handle {
            handle.abort();
        }
    }
}

/// Renders a failed command to stderr the way anyhow's default `main` would
/// (`Error:` + cause chain), then appends a single actionable `→` hint when
/// stderr is an interactive terminal (and not `--quiet`). Hints are pure human
/// chrome: stdout is untouched, so machine consumers reading stdout are
/// unaffected and the process still exits non-zero.
pub fn report_error(err: &anyhow::Error, quiet: bool) {
    eprintln!("Error: {err:?}");
    if stderr_human(quiet)
        && let Some(hint) = hint_for_error(err)
    {
        eprintln!("\n  → {hint}");
    }
}

/// Maps a failure to a one-line, copy-pasteable next step. Prefers the
/// server's structured `error_type`/status (via the `logchef_core::Error` in
/// the cause chain), then falls back to matching the rendered message text for
/// the locally-generated errors (missing team/source, bad time flags).
pub fn hint_for_error(err: &anyhow::Error) -> Option<String> {
    for cause in err.chain() {
        if let Some(core) = cause.downcast_ref::<logchef_core::Error>()
            && let Some(hint) = hint_for_core(core)
        {
            return Some(hint);
        }
    }

    let text = format!("{err:#}").to_lowercase();

    if text.contains("not authenticated")
        || text.contains("no context configured")
        || text.contains("run 'logchef auth'")
        || text.contains("token required")
    {
        return Some(
            "not signed in — run `logchef auth --server <url>`, then `logchef doctor` to verify"
                .into(),
        );
    }
    if text.contains("token") && text.contains("expired") {
        return Some("token expired — run `logchef auth` to sign in again".into());
    }
    if text.contains("does not have access")
        || text.contains("forbidden")
        || text.contains("access to this source")
    {
        return Some("no access to that team/source — check `logchef whoami`".into());
    }
    if text.contains("team") && text.contains("not found") {
        return Some(
            "unknown team — list them with `logchef teams`, then pass -t <id|name>".into(),
        );
    }
    if text.contains("source") && text.contains("not found") {
        return Some(
            "unknown source — list them with `logchef sources -t <team>`, then pass -S <id|name>"
                .into(),
        );
    }
    if text.contains("team not specified") {
        return Some("pass -t <team> or set a default: `logchef config set team <id|name>`".into());
    }
    if text.contains("source not specified") {
        return Some(
            "pass -S <source> or set a default: `logchef config set source <id|name>`".into(),
        );
    }
    if text.contains("invalid duration") {
        return Some("--since takes a relative window like 15m, 1h, 24h, 7d, 2w".into());
    }
    if text.contains("yyyy-mm-dd") || text.contains("invalid time format") {
        return Some(
            "--from/--to must be \"YYYY-MM-DD HH:MM:SS\" in your effective timezone".into(),
        );
    }
    if text.contains("--from requires --to") || text.contains("--to requires --from") {
        return Some("--from and --to must be passed together".into());
    }
    None
}

fn hint_for_core(err: &logchef_core::Error) -> Option<String> {
    use logchef_core::Error;
    match err {
        Error::NotAuthenticated => {
            Some("run `logchef auth` to sign in, then `logchef doctor` to verify".into())
        }
        Error::Api {
            status: Some(401), ..
        } => Some("token invalid or expired — run `logchef auth`".into()),
        Error::Api {
            status: Some(403), ..
        } => Some("no access to that team/source — check `logchef whoami`".into()),
        Error::Api {
            error_type: Some(kind),
            ..
        } => hint_for_error_type(kind),
        Error::Network(_) => {
            Some("can't reach the server — check the URL/connection, then `logchef doctor`".into())
        }
        _ => None,
    }
}

fn hint_for_error_type(kind: &str) -> Option<String> {
    match kind {
        "unauthorized" | "authentication_required" | "invalid_token" => {
            Some("run `logchef auth` to sign in again".into())
        }
        "forbidden" => Some("no access to that team/source — check `logchef whoami`".into()),
        "not_found" => {
            Some("that team/source/query doesn't exist — double-check the id/name".into())
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn thousands_groups_digits() {
        assert_eq!(thousands(0), "0");
        assert_eq!(thousands(42), "42");
        assert_eq!(thousands(1234), "1,234");
        assert_eq!(thousands(1234567), "1,234,567");
        assert_eq!(thousands(-1234567), "-1,234,567");
    }

    #[test]
    fn compact_scales_units() {
        assert_eq!(compact(999), "999");
        assert_eq!(compact(1234), "1.2k");
        assert_eq!(compact(3_400_000), "3.4M");
        assert_eq!(compact(150_000), "150k");
    }

    #[test]
    fn highlight_disabled_is_identity() {
        let sql = "SELECT * FROM logs.app WHERE level = 'error'";
        assert_eq!(highlight_query(sql, Some("clickhouse-sql"), false), sql);
    }

    #[test]
    fn highlight_enabled_wraps_keywords_and_strings() {
        let out = highlight_query("SELECT x WHERE y = 'z'", Some("clickhouse-sql"), true);
        assert!(out.contains(KW));
        assert!(out.contains(STR));
        // The plain text must survive untouched between the escapes.
        assert!(out.contains("SELECT"));
        assert!(out.contains("'z'"));
    }
}
