//! Shared time-range resolution for CLI commands that build a `start_time`/
//! `end_time` window for a query request.
//!
//! The invariant this module enforces: **the wall-clock string sent to the
//! server must always be formatted in the same timezone that is sent
//! alongside it as the request's `timezone` field.** The server (and the
//! ClickHouse SQL it generates) interprets the wall-clock string *in that
//! timezone* — e.g. `toDateTime('2026-07-14 04:58:42', 'Asia/Kolkata')`. If
//! the string was actually computed in UTC but labeled with a different
//! zone, the whole window silently shifts by that zone's UTC offset.
//!
//! Every command should go through [`resolve_time_range`] rather than
//! formatting timestamps and picking a timezone independently.

use chrono::{DateTime, Utc};
use chrono_tz::Tz;

const WALL_CLOCK_FORMAT: &str = "%Y-%m-%d %H:%M:%S";

/// Resolves the effective timezone for a request: the configured value if
/// it parses as a valid IANA zone name, else the system's local IANA zone,
/// else UTC.
///
/// This is what makes the CLI "do the right thing" with zero config,
/// matching how the web UI defaults to the browser's timezone.
pub fn resolve_timezone(configured: Option<&str>) -> Tz {
    resolve_timezone_with(configured, iana_time_zone::get_timezone().ok().as_deref())
}

/// Same resolution logic as [`resolve_timezone`], but with the "system
/// timezone" fed in explicitly rather than detected — this is what lets the
/// fallback path be unit tested without depending on the host's actual
/// timezone.
fn resolve_timezone_with(configured: Option<&str>, system: Option<&str>) -> Tz {
    parse_tz(configured)
        .or_else(|| parse_tz(system))
        .unwrap_or(Tz::UTC)
}

fn parse_tz(value: Option<&str>) -> Option<Tz> {
    value
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .and_then(|s| s.parse::<Tz>().ok())
}

/// Input to [`resolve_time_range`].
pub enum TimeInput<'a> {
    /// Wall-clock strings already expressed in the effective timezone (e.g.
    /// user-supplied `--from`/`--to`, entered as local wall-clock time).
    /// Passed through unchanged.
    WallClock { start: &'a str, end: &'a str },
    /// A concrete instant range (e.g. `now - since`, or a stored epoch
    /// timestamp) to be formatted as a wall-clock string in the effective
    /// timezone.
    Instant {
        start: DateTime<Utc>,
        end: DateTime<Utc>,
    },
}

/// A time range ready to go on the wire: `start`/`end` are wall-clock
/// strings formatted in `timezone`, and `timezone` is exactly the zone name
/// that must accompany them in the request so the server interprets them
/// correctly.
pub struct ResolvedTimeRange {
    pub start: String,
    pub end: String,
    pub timezone: String,
}

/// Resolves a time range for a query request. Always returns a `timezone`
/// that matches how `start`/`end` were formatted, enforcing the invariant
/// described at the module level.
pub fn resolve_time_range(input: TimeInput<'_>, configured_tz: Option<&str>) -> ResolvedTimeRange {
    let tz = resolve_timezone(configured_tz);
    let (start, end) = match input {
        TimeInput::WallClock { start, end } => (start.to_string(), end.to_string()),
        TimeInput::Instant { start, end } => (
            start
                .with_timezone(&tz)
                .format(WALL_CLOCK_FORMAT)
                .to_string(),
            end.with_timezone(&tz).format(WALL_CLOCK_FORMAT).to_string(),
        ),
    };
    ResolvedTimeRange {
        start,
        end,
        timezone: tz.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn kolkata() -> Tz {
        "Asia/Kolkata".parse().unwrap()
    }

    /// The formatted wall-clock strings, parsed back in the timezone they
    /// were labeled with, must represent the same instants as the UTC
    /// inputs.
    #[test]
    fn instant_formats_wall_clock_that_round_trips_to_same_epoch() {
        let start = Utc.with_ymd_and_hms(2026, 7, 14, 4, 58, 42).unwrap();
        let end = Utc.with_ymd_and_hms(2026, 7, 14, 5, 58, 42).unwrap();

        let range = resolve_time_range(TimeInput::Instant { start, end }, Some("Asia/Kolkata"));

        assert_eq!(range.timezone, "Asia/Kolkata");

        let tz = kolkata();
        let parsed_start = chrono::NaiveDateTime::parse_from_str(&range.start, WALL_CLOCK_FORMAT)
            .expect("start should parse")
            .and_local_timezone(tz)
            .single()
            .expect("unambiguous local time");
        let parsed_end = chrono::NaiveDateTime::parse_from_str(&range.end, WALL_CLOCK_FORMAT)
            .expect("end should parse")
            .and_local_timezone(tz)
            .single()
            .expect("unambiguous local time");

        assert_eq!(parsed_start.with_timezone(&Utc), start);
        assert_eq!(parsed_end.with_timezone(&Utc), end);

        // Sanity check the historical bug: the formatted wall-clock string
        // must NOT equal the naive UTC formatting (IST is UTC+5:30).
        assert_ne!(range.start, start.format(WALL_CLOCK_FORMAT).to_string());
    }

    #[test]
    fn instant_with_utc_zone_is_unchanged_from_naive_utc_formatting() {
        let start = Utc.with_ymd_and_hms(2026, 7, 14, 4, 58, 42).unwrap();
        let end = Utc.with_ymd_and_hms(2026, 7, 14, 5, 58, 42).unwrap();

        let range = resolve_time_range(TimeInput::Instant { start, end }, Some("UTC"));

        assert_eq!(range.timezone, "UTC");
        assert_eq!(range.start, start.format(WALL_CLOCK_FORMAT).to_string());
        assert_eq!(range.end, end.format(WALL_CLOCK_FORMAT).to_string());
    }

    #[test]
    fn wall_clock_input_is_passed_through_unchanged() {
        let range = resolve_time_range(
            TimeInput::WallClock {
                start: "2026-07-14 04:58:42",
                end: "2026-07-14 05:58:42",
            },
            Some("Asia/Kolkata"),
        );

        assert_eq!(range.start, "2026-07-14 04:58:42");
        assert_eq!(range.end, "2026-07-14 05:58:42");
        assert_eq!(range.timezone, "Asia/Kolkata");
    }

    #[test]
    fn unconfigured_falls_back_to_injected_system_zone() {
        // No configured value; a fixed (non-UTC, non-host-dependent) system
        // zone is injected and must win over the UTC default.
        let tz = resolve_timezone_with(None, Some("Asia/Kolkata"));
        assert_eq!(tz, kolkata());
    }

    #[test]
    fn unparseable_configured_zone_falls_back_to_injected_system_zone() {
        let tz = resolve_timezone_with(Some("Not/A/Real/Zone"), Some("Asia/Kolkata"));
        assert_eq!(tz, kolkata());
    }

    #[test]
    fn empty_configured_zone_falls_back_to_injected_system_zone() {
        let tz = resolve_timezone_with(Some(""), Some("Asia/Kolkata"));
        assert_eq!(tz, kolkata());
    }

    #[test]
    fn nothing_resolvable_falls_back_to_utc() {
        let tz = resolve_timezone_with(Some("Not/A/Real/Zone"), None);
        assert_eq!(tz, Tz::UTC);

        let tz = resolve_timezone_with(Some("Not/A/Real/Zone"), Some("also/not/real"));
        assert_eq!(tz, Tz::UTC);
    }

    #[test]
    fn public_resolve_timezone_never_panics_and_yields_a_valid_zone() {
        // Exercises the real system-detection path (host-dependent), only
        // asserting it doesn't panic and always yields a parseable zone.
        let tz = resolve_timezone(None);
        assert!(tz.to_string().parse::<Tz>().is_ok());
    }
}
