//! "New version available" notifier, modeled on npm/gh: when a newer stable
//! CLI release exists on GitHub, print a small boxed notice to **stderr** at
//! the very end of a command. It is gated to interactive use only and MUST
//! never delay or break a command — the network call is time-boxed to 1.5s and
//! every error is swallowed silently.
//!
//! stdout is never touched, so `| jq` and other pipes stay clean.

use std::cmp::Ordering;
use std::io::IsTerminal;
use std::path::PathBuf;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use logchef_core::Config;
use serde::{Deserialize, Serialize};

const CACHE_FILE: &str = "update-check.json";
const CACHE_TTL_SECS: u64 = 24 * 60 * 60; // 24h, mirroring cache.rs' TTL pattern.
const HTTP_TIMEOUT: Duration = Duration::from_millis(1500);
const RELEASES_API: &str = "https://api.github.com/repos/mr-karan/logchef/releases?per_page=30";
const RELEASES_URL: &str = "https://github.com/mr-karan/logchef/releases";
const TAG_PREFIX: &str = "cli-v";
const USER_AGENT_VALUE: &str = concat!("logchef-cli/", env!("CARGO_PKG_VERSION"));

// Brand purple + dim, matching the raw-ANSI approach used elsewhere in the CLI.
const PURPLE: &str = "\x1b[38;2;139;92;246m";
const DIM: &str = "\x1b[2m";
const RESET: &str = "\x1b[0m";

/// Disk cache of the latest-known release, stored in the config dir.
#[derive(Debug, Serialize, Deserialize)]
struct UpdateCache {
    latest: String,
    checked_at: u64,
}

/// Entry point wired at the tail of a successful command. Self-gates on config,
/// TTY, quiet, and env kill-switches, then checks (cache or a time-boxed fetch)
/// and prints a notice if a newer stable release exists. Never returns an
/// error and never blocks beyond the 1.5s HTTP timeout.
pub async fn check_and_notify(quiet: bool) {
    if !gate(quiet) {
        return;
    }
    if let Some(latest) = latest_version().await
        && let Some(current) = Version::parse(env!("CARGO_PKG_VERSION"))
        && latest > current
    {
        print_notice(&current.to_string(), &latest.to_string());
    }
}

/// All the non-network gating: config flag, stderr TTY, not quiet, no env
/// kill-switch. Config-load failures fall back to enabled (default true).
fn gate(quiet: bool) -> bool {
    if quiet || crate::env_flags::env_off("LOGCHEF_NO_UPDATE_CHECK") || crate::env_flags::ci() {
        return false;
    }
    if !std::io::stderr().is_terminal() {
        return false;
    }
    Config::load().map(|c| c.check_updates).unwrap_or(true)
}

/// Returns the latest stable version, using the fresh cache when available or
/// otherwise a time-boxed network fetch. Any failure yields `None` silently.
async fn latest_version() -> Option<Version> {
    let path = cache_path()?;

    if let Some(cache) = load_cache(&path)
        && !is_expired(cache.checked_at)
    {
        return Version::parse(&cache.latest);
    }

    let latest = fetch_latest_stable().await?;
    save_cache(&path, &latest.to_string());
    Some(latest)
}

/// Time-boxed GitHub fetch. Returns the max stable `cli-v*` release, or `None`
/// on any timeout/error/parse failure (never propagates).
async fn fetch_latest_stable() -> Option<Version> {
    let fetch = async {
        let client = reqwest::Client::builder().build().ok()?;
        let resp = client
            .get(RELEASES_API)
            .header(reqwest::header::USER_AGENT, USER_AGENT_VALUE)
            .send()
            .await
            .ok()?;
        if !resp.status().is_success() {
            return None;
        }
        let releases: Vec<Release> = resp.json().await.ok()?;
        latest_stable_from_tags(releases.iter().map(|r| r.tag_name.as_str()))
    };

    tokio::time::timeout(HTTP_TIMEOUT, fetch)
        .await
        .ok()
        .flatten()
}

#[derive(Debug, Deserialize)]
struct Release {
    tag_name: String,
}

/// Picks the maximum stable version among tags that start with `cli-v`.
/// Prereleases and unparseable tags are skipped, so a `-alpha` never nags a
/// stable user.
fn latest_stable_from_tags<'a>(tags: impl Iterator<Item = &'a str>) -> Option<Version> {
    tags.filter_map(|t| t.strip_prefix(TAG_PREFIX))
        .filter_map(Version::parse)
        .filter(|v| v.prerelease.is_none())
        .max()
}

fn cache_path() -> Option<PathBuf> {
    Config::config_dir().ok().map(|dir| dir.join(CACHE_FILE))
}

fn load_cache(path: &PathBuf) -> Option<UpdateCache> {
    let content = std::fs::read_to_string(path).ok()?;
    serde_json::from_str(&content).ok()
}

fn save_cache(path: &PathBuf, latest: &str) {
    let cache = UpdateCache {
        latest: latest.to_string(),
        checked_at: now(),
    };
    if let (Some(parent), Ok(content)) = (path.parent(), serde_json::to_string_pretty(&cache)) {
        std::fs::create_dir_all(parent).ok();
        std::fs::write(path, content).ok();
    }
}

fn is_expired(checked_at: u64) -> bool {
    now().saturating_sub(checked_at) > CACHE_TTL_SECS
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

/// Renders the boxed update notice to stderr. Plain (no box color) under
/// `NO_COLOR`.
fn print_notice(current: &str, latest: &str) {
    let l1 = format!("Update available  {current} → {latest}");
    let l2 = RELEASES_URL.to_string();
    let width = l1.chars().count().max(l2.chars().count());
    let border = "─".repeat(width + 4);

    let color = crate::env_flags::color_enabled();
    let (open, close) = if color { (PURPLE, RESET) } else { ("", "") };
    let (dim, dim_close) = if color { (DIM, RESET) } else { ("", "") };

    eprintln!("{open}╭{border}╮{close}");
    for (i, line) in [l1, l2].iter().enumerate() {
        let pad = " ".repeat(width - line.chars().count());
        // First line uses normal weight, the URL is dimmed.
        if i == 1 {
            eprintln!("{open}│{close}  {dim}{line}{dim_close}{pad}  {open}│{close}");
        } else {
            eprintln!("{open}│{close}  {line}{pad}  {open}│{close}");
        }
    }
    eprintln!("{open}╰{border}╯{close}");
}

/// A hand-parsed `MAJOR.MINOR.PATCH` version with an optional `-prerelease`
/// suffix. Deliberately tiny: no semver crate dependency.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Version {
    major: u64,
    minor: u64,
    patch: u64,
    prerelease: Option<String>,
}

impl Version {
    /// Parses `MAJOR.MINOR.PATCH[-prerelease]`. Requires exactly three numeric
    /// core components; anything else returns `None` (skip).
    fn parse(s: &str) -> Option<Self> {
        let s = s.trim();
        let (core, prerelease) = match s.split_once('-') {
            Some((core, pre)) if !pre.is_empty() => (core, Some(pre.to_string())),
            Some(_) => return None,
            None => (s, None),
        };
        let mut parts = core.split('.');
        let major = parts.next()?.parse().ok()?;
        let minor = parts.next()?.parse().ok()?;
        let patch = parts.next()?.parse().ok()?;
        if parts.next().is_some() {
            return None;
        }
        Some(Self {
            major,
            minor,
            patch,
            prerelease,
        })
    }
}

impl std::fmt::Display for Version {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}.{}.{}", self.major, self.minor, self.patch)?;
        if let Some(pre) = &self.prerelease {
            write!(f, "-{pre}")?;
        }
        Ok(())
    }
}

impl Ord for Version {
    fn cmp(&self, other: &Self) -> Ordering {
        (self.major, self.minor, self.patch)
            .cmp(&(other.major, other.minor, other.patch))
            .then_with(|| match (&self.prerelease, &other.prerelease) {
                // A stable release (no prerelease) outranks any prerelease with
                // the same core: 0.3.0 > 0.3.0-alpha.
                (None, None) => Ordering::Equal,
                (None, Some(_)) => Ordering::Greater,
                (Some(_), None) => Ordering::Less,
                (Some(a), Some(b)) => a.cmp(b),
            })
    }
}

impl PartialOrd for Version {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn v(s: &str) -> Version {
        Version::parse(s).unwrap()
    }

    #[test]
    fn parses_core_and_prerelease() {
        let ver = v("1.2.3");
        assert_eq!((ver.major, ver.minor, ver.patch), (1, 2, 3));
        assert!(ver.prerelease.is_none());

        let pre = v("0.3.0-alpha.1");
        assert_eq!(pre.prerelease.as_deref(), Some("alpha.1"));
    }

    #[test]
    fn rejects_malformed() {
        assert!(Version::parse("").is_none());
        assert!(Version::parse("1.2").is_none());
        assert!(Version::parse("1.2.3.4").is_none());
        assert!(Version::parse("v1.2.3").is_none());
        assert!(Version::parse("1.x.3").is_none());
        assert!(Version::parse("1.2.3-").is_none());
    }

    #[test]
    fn compares_core_fields() {
        assert!(v("0.2.0") < v("0.3.0"));
        assert!(v("0.2.10") > v("0.2.9"));
        assert!(v("1.0.0") > v("0.99.99"));
        assert_eq!(v("0.2.0"), v("0.2.0"));
    }

    #[test]
    fn same_version_does_not_notify() {
        // Strict-greater is the notify rule; equal must not.
        assert_eq!(v("0.2.0").cmp(&v("0.2.0")), Ordering::Equal);
    }

    #[test]
    fn stable_outranks_prerelease_of_same_core() {
        assert!(v("0.3.0") > v("0.3.0-alpha.1"));
        assert!(v("0.3.0-alpha.1") < v("0.3.0"));
        // A prerelease of a higher core still beats a lower stable core.
        assert!(v("0.3.0-alpha") > v("0.2.0"));
    }

    #[test]
    fn prerelease_never_selected_as_latest_stable() {
        let tags = ["cli-v0.3.0-alpha.1", "cli-v0.2.5", "cli-v0.2.9"];
        let latest = latest_stable_from_tags(tags.iter().copied()).unwrap();
        assert_eq!(latest.to_string(), "0.2.9");
    }

    #[test]
    fn filters_non_cli_and_unparseable_tags() {
        let tags = [
            "v1.0.0",         // wrong prefix (server release) -> skip
            "cli-v0.3.0",     // stable cli release
            "cli-vgarbage",   // unparseable -> skip
            "cli-v0.2.0",     // older stable
            "some-other-tag", // skip
        ];
        let latest = latest_stable_from_tags(tags.iter().copied()).unwrap();
        assert_eq!(latest.to_string(), "0.3.0");
    }

    #[test]
    fn no_cli_tags_yields_none() {
        let tags = ["v1.0.0", "v2.0.0"];
        assert!(latest_stable_from_tags(tags.iter().copied()).is_none());
    }
}
