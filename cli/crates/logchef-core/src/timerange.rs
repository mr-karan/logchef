//! Shared query time ranges and effective timezone resolution.
//!
//! Wall-clock bounds are formatted in the accompanying timezone.
//! RFC3339 bounds preserve instants, including ambiguous local DST hours.

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
    value.and_then(parse_timezone)
}

/// Parses an IANA zone name such as `Asia/Kolkata`. Returns `None` for empty
/// input and for abbreviations like `IST`, which are not IANA names.
pub fn parse_timezone(value: &str) -> Option<Tz> {
    Some(value.trim())
        .filter(|s| !s.is_empty())
        .and_then(|s| s.parse::<Tz>().ok())
}

/// Input to [`resolve_time_range`].
pub enum TimeInput<'a> {
    /// Wall-clock strings already expressed in the effective timezone (e.g.
    /// user-supplied `--from`/`--to`, entered as local wall-clock time).
    /// Passed through unchanged.
    WallClock { start: &'a str, end: &'a str },
    /// Instants serialized as RFC3339 UTC bounds.
    Rfc3339 {
        start: DateTime<Utc>,
        end: DateTime<Utc>,
    },
}

/// Query bounds with the effective timezone used for wall-clock values.
pub struct ResolvedTimeRange {
    pub start: String,
    pub end: String,
    pub timezone: String,
}

/// Formats query bounds and includes the effective timezone.
pub fn resolve_time_range(input: TimeInput<'_>, configured_tz: Option<&str>) -> ResolvedTimeRange {
    let tz = resolve_timezone(configured_tz);
    let (start, end) = match input {
        TimeInput::Rfc3339 { start, end } => (
            start.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true),
            end.to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true),
        ),
        TimeInput::WallClock { start, end } => (start.to_string(), end.to_string()),
    };
    ResolvedTimeRange {
        start,
        end,
        timezone: tz.to_string(),
    }
}

/// Parse a wall-clock `YYYY-MM-DD HH:MM:SS` string, interpreted in the
/// effective timezone, into epoch **milliseconds** (UTC). Returns `None` if
/// the string does not parse or the local time is invalid/ambiguous (e.g. a
/// DST gap).
///
/// Used by `open` to build the web explorer's absolute-time `start`/`end`
/// query params, which the frontend reads as epoch-ms integers
/// (`parseInt` -> `timestampToCalendarDateTime`). Going through this shared
/// helper keeps the wall-clock-in-effective-timezone invariant consistent
/// with the rest of the CLI's time handling.
pub fn wall_clock_to_epoch_millis(wall_clock: &str, configured_tz: Option<&str>) -> Option<i64> {
    let tz = resolve_timezone(configured_tz);
    chrono::NaiveDateTime::parse_from_str(wall_clock.trim(), WALL_CLOCK_FORMAT)
        .ok()?
        .and_local_timezone(tz)
        .single()
        .map(|dt| dt.with_timezone(&Utc).timestamp_millis())
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn kolkata() -> Tz {
        "Asia/Kolkata".parse().unwrap()
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
    fn parse_timezone_accepts_iana_names_only() {
        assert_eq!(parse_timezone(" Asia/Kolkata "), Some(kolkata()));
        assert_eq!(parse_timezone("UTC"), Some(Tz::UTC));
        assert_eq!(parse_timezone("IST"), None);
        assert_eq!(parse_timezone(""), None);
    }

    #[test]
    fn empty_configured_zone_falls_back_to_injected_system_zone() {
        let tz = resolve_timezone_with(Some(""), Some("Asia/Kolkata"));
        assert_eq!(tz, kolkata());
    }

    #[test]
    fn wall_clock_to_epoch_millis_interprets_string_in_effective_zone() {
        // 04:58:42 in Asia/Kolkata (UTC+5:30) is 23:28:42 UTC the day before.
        let ms = wall_clock_to_epoch_millis("2026-07-14 04:58:42", Some("Asia/Kolkata"))
            .expect("valid wall clock");
        let expected = Utc.with_ymd_and_hms(2026, 7, 13, 23, 28, 42).unwrap();
        assert_eq!(ms, expected.timestamp_millis());
    }

    #[test]
    fn wall_clock_to_epoch_millis_utc_is_naive() {
        let ms = wall_clock_to_epoch_millis("2026-07-14 04:58:42", Some("UTC"))
            .expect("valid wall clock");
        let expected = Utc.with_ymd_and_hms(2026, 7, 14, 4, 58, 42).unwrap();
        assert_eq!(ms, expected.timestamp_millis());
    }

    #[test]
    fn wall_clock_to_epoch_millis_rejects_garbage() {
        assert!(wall_clock_to_epoch_millis("not a time", Some("UTC")).is_none());
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

pub fn calendar_range(
    relative: &str,
    now: DateTime<Utc>,
    configured_tz: Option<&str>,
) -> Option<crate::error::Result<(DateTime<Utc>, DateTime<Utc>)>> {
    use chrono::TimeZone;
    if relative != "today" && relative != "yesterday" {
        return None;
    }
    let tz = resolve_timezone(configured_tz);
    let date = now.with_timezone(&tz).date_naive();
    let midnight = |date: chrono::NaiveDate| {
        let local = date.and_hms_opt(0, 0, 0).unwrap();
        tz.from_local_datetime(&local)
            .earliest()
            .or_else(|| chrono_tz::GapInfo::new(&local, &tz).and_then(|gap| gap.end))
            .filter(|dt| dt.date_naive() == date)
            .map(|dt| dt.with_timezone(&Utc))
            .ok_or_else(|| {
                crate::error::Error::other(format!("Calendar day has no valid instant in {tz}"))
            })
    };
    Some((|| {
        let end = midnight(date)?;
        if relative == "today" {
            Ok((end, now))
        } else {
            Ok((
                midnight(
                    date.pred_opt()
                        .ok_or_else(|| crate::error::Error::other("Invalid previous day"))?,
                )?,
                end,
            ))
        }
    })())
}

#[cfg(test)]
mod calendar_tests {
    use super::*;
    use chrono::TimeZone;

    #[test]
    fn midnight_gaps_use_first_valid_instant_on_transition_and_following_day() {
        for (zone, date, expected) in [
            ("America/Havana", "2026-03-08", "2026-03-08T05:00:00Z"),
            ("Asia/Beirut", "2026-03-29", "2026-03-28T22:00:00Z"),
            ("Africa/Cairo", "2026-04-24", "2026-04-23T22:00:00Z"),
            ("America/Santiago", "2026-09-06", "2026-09-06T04:00:00Z"),
        ] {
            let date = chrono::NaiveDate::parse_from_str(date, "%Y-%m-%d").unwrap();
            let tz: Tz = zone.parse().unwrap();
            let first = DateTime::parse_from_rfc3339(expected)
                .unwrap()
                .with_timezone(&Utc);
            for day in [date, date.succ_opt().unwrap()] {
                let now = tz
                    .from_local_datetime(&day.and_hms_opt(12, 0, 0).unwrap())
                    .single()
                    .unwrap()
                    .with_timezone(&Utc);
                let (today, end) = calendar_range("today", now, Some(zone)).unwrap().unwrap();
                assert_eq!(end, now);
                let (yesterday, yesterday_end) = calendar_range("yesterday", now, Some(zone))
                    .unwrap()
                    .unwrap();
                assert_eq!(yesterday_end, today);
                if day == date {
                    assert_eq!(today, first, "{zone}");
                    assert_eq!(today.with_timezone(&tz).date_naive(), date);
                    assert!(yesterday < today);
                } else {
                    assert_eq!(yesterday, first, "{zone}");
                    assert_eq!(
                        today,
                        tz.from_local_datetime(&day.and_hms_opt(0, 0, 0).unwrap())
                            .earliest()
                            .unwrap()
                            .with_timezone(&Utc)
                    );
                }
            }
        }
    }

    #[test]
    fn calendar_days_follow_zone_and_dst_instead_of_fixed_durations() {
        let now = Utc.with_ymd_and_hms(2026, 11, 2, 4, 30, 0).unwrap();
        let (start, end) = calendar_range("today", now, Some("America/New_York"))
            .unwrap()
            .unwrap();
        assert_eq!(start, Utc.with_ymd_and_hms(2026, 11, 1, 4, 0, 0).unwrap());
        assert_eq!(end, now);
        let now = Utc.with_ymd_and_hms(2026, 11, 2, 12, 0, 0).unwrap();
        let (start, end) = calendar_range("yesterday", now, Some("America/New_York"))
            .unwrap()
            .unwrap();
        assert_eq!(start, Utc.with_ymd_and_hms(2026, 11, 1, 4, 0, 0).unwrap());
        assert_eq!(end, Utc.with_ymd_and_hms(2026, 11, 2, 5, 0, 0).unwrap());
        assert_eq!(end - start, chrono::Duration::hours(25));
    }
}
