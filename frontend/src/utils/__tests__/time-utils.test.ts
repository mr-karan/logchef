// IMPORTANT: this test proves the #103 timezone double-shift. `date-fns`
// format() (the previous implementation) renders in the JS runtime's local
// timezone, so we pin a fixed, non-UTC "browser" timezone BEFORE importing any
// module under test. This must run before @internationalized/date / date-fns
// initialise their timezone view.
process.env.TZ = "Asia/Kolkata"; // UTC+05:30, no DST

import { describe, it, expect } from "vitest";
import { CalendarDateTime, getLocalTimeZone } from "@internationalized/date";
import { formatDateForSQL, createTimeRangeCondition } from "@/utils/time-utils";
import type { TimeRange } from "@/types/query";

// Sanity: confirm the harness really believes it is running in a +05:30 zone,
// otherwise the repro below would be vacuous.
describe("test harness timezone", () => {
  it("runs with a non-UTC browser-local timezone (IST)", () => {
    expect(getLocalTimeZone()).toMatch(/Asia\/(Kolkata|Calcutta)/);
    expect(new Date("2026-07-14T00:00:00Z").getTimezoneOffset()).toBe(-330);
  });
});

describe("formatDateForSQL — picker timezone != browser-local (#103)", () => {
  // User picks wall-clock 04:30 on 2026-07-14 and the query timezone is UTC.
  // The intended instant is 2026-07-14 04:30:00Z. Because this string is later
  // embedded as toDateTime('<str>', 'UTC'), the emitted wall-clock string MUST
  // itself be 04:30:00 (its UTC wall-clock), not a browser-local shift of it.
  const picked = new CalendarDateTime(2026, 7, 14, 4, 30, 0);

  it("renders the wall-clock string in the paired timezone, not browser-local", () => {
    // Fixed reference of what the OLD (buggy) implementation produced: it did
    // `format(dt.toDate('UTC'))` which rendered the 04:30Z instant in IST as
    // 10:00:00 — a +05:30 shift that then gets re-interpreted as UTC downstream.
    const buggyBrowserLocal = "'2026-07-14 10:00:00'";

    const result = formatDateForSQL(picked, true, "UTC");

    // The fix: the string is the UTC wall-clock of the intended instant.
    expect(result).toBe("'2026-07-14 04:30:00'");
    // And it is explicitly NOT the browser-local (double-shift) rendering.
    expect(result).not.toBe(buggyBrowserLocal);
  });

  it("round-trips: toDateTime(<str>, <tz>) reconstructs the intended instant", () => {
    const tz = "UTC";
    const str = formatDateForSQL(picked, false, tz);
    // ClickHouse's toDateTime(str, tz) interprets str as wall-clock in tz.
    // Emulate that: parse the emitted wall-clock as if it were in `tz`.
    const reconstructed = new CalendarDateTime(
      2026,
      7,
      14,
      Number(str.slice(11, 13)),
      Number(str.slice(14, 16)),
      Number(str.slice(17, 19))
    ).toDate(tz);
    const intended = picked.toDate(tz); // 2026-07-14T04:30:00Z
    expect(reconstructed.toISOString()).toBe(intended.toISOString());
    expect(intended.toISOString()).toBe("2026-07-14T04:30:00.000Z");
  });

  it("is correct when picker timezone equals browser-local (regression guard)", () => {
    // With tz = IST the wall-clock is unchanged either way; the fix must not
    // break the common case.
    const result = formatDateForSQL(picked, true, "Asia/Kolkata");
    expect(result).toBe("'2026-07-14 04:30:00'");
  });

  it("returns now() for nullish input", () => {
    expect(formatDateForSQL(null)).toBe("now()");
    expect(formatDateForSQL(undefined)).toBe("now()");
  });
});

describe("createTimeRangeCondition — no double-shift end to end (#103)", () => {
  const range: TimeRange = {
    start: new CalendarDateTime(2026, 7, 14, 4, 30, 0),
    end: new CalendarDateTime(2026, 7, 14, 6, 30, 0),
  };

  it("emits UTC wall-clocks paired with the UTC label for a UTC query", () => {
    const cond = createTimeRangeCondition("timestamp", range, true, "UTC");
    expect(cond).toBe(
      "`timestamp` BETWEEN toDateTime('2026-07-14 04:30:00', 'UTC') " +
        "AND toDateTime('2026-07-14 06:30:00', 'UTC')"
    );
    // Guard against the browser-local shifted rendering leaking back in.
    expect(cond).not.toContain("10:00:00");
    expect(cond).not.toContain("12:00:00");
  });

  it("pairs the wall-clock with whichever timezone label is used (America/New_York)", () => {
    const cond = createTimeRangeCondition("ts", range, true, "America/New_York");
    // Wall-clock fields are preserved and paired with the NY label, so the
    // instant is correct regardless of the browser being in IST.
    expect(cond).toBe(
      "`ts` BETWEEN toDateTime('2026-07-14 04:30:00', 'America/New_York') " +
        "AND toDateTime('2026-07-14 06:30:00', 'America/New_York')"
    );
  });
});
