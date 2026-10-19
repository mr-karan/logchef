import { describe, expect, it } from "vitest";
import {
  epochMs,
  formatInZone,
  formatWallClock,
  isSameZonedDay,
  parseWallClock,
  parseZoneId,
  relativeWindow,
  resolveWindow,
  sameWindow,
  sqlTimeCondition,
  toRfc3339,
  windowLabel,
  zoneFromPreference,
  zoneOffsetMs,
  type ZoneId,
} from "../timeWindow";

// A non-UTC browser zone, so any browser-zone wall clock shows up as a shift.
process.env.TZ = "Asia/Kolkata";

function zone(name: string): ZoneId {
  const parsed = parseZoneId(name);
  if (!parsed) throw new Error(`bad zone ${name}`);
  return parsed;
}

function relative(spec: string) {
  const window = relativeWindow(spec);
  if (!window) throw new Error(`bad spec ${spec}`);
  return window;
}

const UTC = zone("UTC");
const NEW_YORK = zone("America/New_York");
const at = (iso: string) => epochMs(Date.parse(iso));

describe("resolveWindow", () => {
  it("resolves today and yesterday at day boundaries in the given zone, not the browser's", () => {
    const anchor = at("2026-10-19T20:45:00Z");

    expect(resolveWindow(relative("today"), UTC, anchor)).toEqual({ start: at("2026-10-19T00:00:00Z"), end: anchor, zone: UTC });
    expect(resolveWindow(relative("yesterday"), UTC, anchor)).toEqual({
      start: at("2026-10-18T00:00:00Z"),
      end: at("2026-10-19T00:00:00Z"),
      zone: UTC,
    });
  });

  it("keeps start before end when the zone's date differs from the browser's", () => {
    // 20:45Z is already Oct 20 in the browser zone (IST), but Oct 19 in New York.
    const anchor = at("2026-10-20T02:45:00Z");
    const today = resolveWindow(relative("today"), NEW_YORK, anchor);
    expect(today.start).toBe(at("2026-10-19T04:00:00Z"));
    expect(today.start).toBeLessThanOrEqual(today.end);
  });

  it("moves a midnight that falls in a DST gap forward to the first valid instant", () => {
    // Chile starts DST on 2026-09-06: 00:00 local does not exist, the day starts at 01:00 (-03).
    const santiago = zone("America/Santiago");
    const anchor = at("2026-09-06T15:00:00Z");
    expect(resolveWindow(relative("today"), santiago, anchor).start).toBe(at("2026-09-06T04:00:00Z"));
    expect(resolveWindow(relative("yesterday"), santiago, at("2026-09-07T15:00:00Z"))).toMatchObject({
      start: at("2026-09-06T04:00:00Z"),
      end: at("2026-09-07T03:00:00Z"),
    });
  });

  it("gives yesterday a 25 hour span on a DST fall-back day", () => {
    const window = resolveWindow(relative("yesterday"), NEW_YORK, at("2026-11-02T15:00:00Z"));
    expect(window.end - window.start).toBe(25 * 3_600_000);
  });

  it("treats durations as fixed spans back from the anchor", () => {
    const anchor = at("2026-11-01T12:00:00Z");
    expect(resolveWindow(relative("15m"), NEW_YORK, anchor).start).toBe(anchor - 15 * 60_000);
    expect(resolveWindow(relative("1d"), NEW_YORK, anchor).start).toBe(anchor - 86_400_000);
    expect(resolveWindow(relative("2w"), NEW_YORK, anchor).start).toBe(anchor - 14 * 86_400_000);
  });

  it("returns an absolute window's instants unchanged, with the zone attached", () => {
    const window = { kind: "absolute" as const, start: at("2026-10-19T04:00:00Z"), end: at("2026-10-19T05:00:00Z") };
    expect(resolveWindow(window, NEW_YORK, at("2030-01-01T00:00:00Z"))).toEqual({ start: window.start, end: window.end, zone: NEW_YORK });
  });
});

describe("relative specs", () => {
  it("accepts durations and calendar days, and rejects anything else", () => {
    expect(relativeWindow("15m")).toEqual({ kind: "relative", spec: "15m" });
    expect(relativeWindow("yesterday")).toEqual({ kind: "relative", spec: "yesterday" });
    for (const bad of ["", "0m", "15", "15s", "now-1h", "Today"]) expect(relativeWindow(bad), bad).toBeNull();
  });

  it("labels durations and leaves calendar and absolute windows to their range", () => {
    expect(windowLabel(relative("15m"))).toEqual({ key: "time.lastMinutes", count: 15 });
    expect(windowLabel(relative("24h"))).toEqual({ key: "time.lastHours", count: 24 });
    expect(windowLabel(relative("1w"))).toEqual({ key: "time.lastDays", count: 7 });
    expect(windowLabel(relative("today"))).toBeNull();
    expect(windowLabel({ kind: "absolute", start: epochMs(0), end: epochMs(1) })).toBeNull();
  });

  it("compares windows by value", () => {
    expect(sameWindow(relative("1h"), relative("1h"))).toBe(true);
    expect(sameWindow(relative("1h"), relative("2h"))).toBe(false);
    const a = { kind: "absolute" as const, start: epochMs(1), end: epochMs(2) };
    expect(sameWindow(a, { ...a })).toBe(true);
    expect(sameWindow(a, { ...a, end: epochMs(3) })).toBe(false);
  });
});

describe("wall clock text", () => {
  // 2026-11-01 01:30 occurs twice in New York: 05:30Z (EDT) and 06:30Z (EST).
  const firstFold = at("2026-11-01T05:30:00Z");
  const secondFold = at("2026-11-01T06:30:00Z");

  it("round-trips both occurrences of a repeated hour to the same instant", () => {
    expect(formatWallClock(firstFold, NEW_YORK)).toBe("2026-11-01 01:30:00 -04:00");
    expect(formatWallClock(secondFold, NEW_YORK)).toBe("2026-11-01 01:30:00 -05:00");
    expect(parseWallClock(formatWallClock(firstFold, NEW_YORK), NEW_YORK)).toBe(firstFold);
    expect(parseWallClock(formatWallClock(secondFold, NEW_YORK), NEW_YORK)).toBe(secondFold);
  });

  it("writes no offset outside a fold, and parses plain text in the given zone", () => {
    const instant = at("2026-10-19T04:00:00Z");
    expect(formatWallClock(instant, UTC)).toBe("2026-10-19 04:00:00");
    expect(formatWallClock(instant, NEW_YORK)).toBe("2026-10-19 00:00:00");
    expect(parseWallClock("2026-10-19 04:00:00", UTC)).toBe(instant);
    expect(parseWallClock("2026-10-19T00:00", NEW_YORK)).toBe(instant);
  });

  it("takes the earlier instant for a fold without an offset, and moves a gap time forward", () => {
    expect(parseWallClock("2026-11-01 01:30:00", NEW_YORK)).toBe(firstFold);
    // 2026-03-08 02:30 does not exist in New York; it becomes 03:30 EDT.
    expect(parseWallClock("2026-03-08 02:30:00", NEW_YORK)).toBe(at("2026-03-08T07:30:00Z"));
  });

  it("reads text with an explicit offset as that exact instant, in any zone", () => {
    for (const z of [UTC, NEW_YORK, zone("Asia/Kolkata")]) {
      expect(parseWallClock("2026-10-19T04:00:00Z", z)).toBe(at("2026-10-19T04:00:00Z"));
      expect(parseWallClock("2026-10-19T09:30:00.250+05:30", z)).toBe(at("2026-10-19T04:00:00.250Z"));
      expect(parseWallClock("2026-11-01 01:30:00 +02:00", z)).toBe(at("2026-10-31T23:30:00Z"));
    }
  });

  it("keeps fractional seconds of plain text in the given zone", () => {
    expect(parseWallClock("2026-10-19 00:00:00.5", NEW_YORK)).toBe(at("2026-10-19T04:00:00.500Z"));
    expect(parseWallClock("2026-10-19 04:00:00.123456789", UTC)).toBe(at("2026-10-19T04:00:00.123Z"));
  });

  it("rejects malformed text and impossible dates", () => {
    for (const bad of ["", "yesterday", "2026-02-30 00:00:00", "2026-13-01 00:00", "2026-10-19 24:00", "2026-10-19 04:00:00.", "2026-10-19 04:00:00 +24:00"]) {
      expect(parseWallClock(bad, NEW_YORK), bad).toBeNull();
    }
  });
});

describe("zone-explicit helpers", () => {
  it("builds zones from the preference and rejects unknown names", () => {
    expect(zoneFromPreference("utc")).toBe("UTC");
    expect(zoneOffsetMs(epochMs(0), zoneFromPreference("local"))).toBe(5.5 * 3_600_000);
    expect(parseZoneId("Not/AZone")).toBeNull();
  });

  it("formats and compares days in the given zone, not the browser's", () => {
    const instant = at("2026-10-19T20:00:00Z"); // Oct 20 01:30 in IST
    expect(formatInZone(instant, UTC, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }, "en-US")).toBe("20:00");
    expect(isSameZonedDay(instant, at("2026-10-19T00:00:00Z"), UTC)).toBe(true);
    expect(isSameZonedDay(instant, at("2026-10-19T00:00:00Z"), zone("Asia/Kolkata"))).toBe(false);
    expect(zoneOffsetMs(instant, NEW_YORK)).toBe(-4 * 3_600_000);
    expect(toRfc3339(instant)).toBe("2026-10-19T20:00:00.000Z");
  });

  it("writes the SQL window in its zone, and a fold bound in UTC", () => {
    expect(sqlTimeCondition("timestamp", { start: at("2026-10-19T04:00:00Z"), end: at("2026-10-19T05:00:00Z"), zone: NEW_YORK })).toBe(
      "`timestamp` BETWEEN toDateTime('2026-10-19 00:00:00', 'America/New_York') AND toDateTime('2026-10-19 01:00:00', 'America/New_York')",
    );
    expect(sqlTimeCondition("`ts`", { start: at("2026-11-01T06:30:00Z"), end: at("2026-11-01T08:00:00Z"), zone: NEW_YORK })).toBe(
      "`ts` BETWEEN toDateTime('2026-11-01 06:30:00', 'UTC') AND toDateTime('2026-11-01 03:00:00', 'America/New_York')",
    );
  });
});
