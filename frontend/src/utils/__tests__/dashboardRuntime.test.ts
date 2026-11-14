import { describe, it, expect } from "vitest";
import {
  classifyPanelError,
  clampTableLimit,
  execSignature,
  panelTimeBound,
  PANEL_INTERVALS,
  resolveAppliedRange,
  resolveEffectiveCacheTtl,
  resolveInterval,
  resolvePanelRange,
  zoneOffsetChange,
} from "../dashboardRuntime";
import type { DashboardCachePolicy } from "@/api/meta";
import type { DashboardQueryPanel } from "@/api/dashboards";
import { epochMs, parseZoneId, type ResolvedWindow, type ZoneId } from "@/utils/timeWindow";

// A well-formed server policy: caching on, 10m default, 1h max.
const POLICY: DashboardCachePolicy = {
  enabled: true,
  default_ttl_seconds: 600,
  max_ttl_seconds: 3600,
};

describe("resolveEffectiveCacheTtl", () => {
  // [name, blobTtl, policy, expected]
  const cases: Array<[string, number | null | undefined, DashboardCachePolicy | null | undefined, number]> = [
    // Fail-closed on missing/disabled/malformed policy (old server).
    ["policy null → 0", 600, null, 0],
    ["policy undefined → 0", 600, undefined, 0],
    ["enabled=false → 0", 600, { ...POLICY, enabled: false }, 0],
    ["enabled missing (coerced) → 0", 600, { ...POLICY, enabled: undefined as any }, 0],
    ["default < 1 → 0", 600, { ...POLICY, default_ttl_seconds: 0 }, 0],
    ["default negative → 0", 600, { ...POLICY, default_ttl_seconds: -1 }, 0],
    ["default float → 0", 600, { ...POLICY, default_ttl_seconds: 1.5 }, 0],
    ["default NaN → 0", 600, { ...POLICY, default_ttl_seconds: NaN }, 0],
    ["max < 1 → 0", 600, { ...POLICY, max_ttl_seconds: 0 }, 0],
    ["max negative → 0", 600, { ...POLICY, max_ttl_seconds: -5 }, 0],
    ["max float → 0", 600, { ...POLICY, max_ttl_seconds: 3600.5 }, 0],
    ["max NaN → 0", 600, { ...POLICY, max_ttl_seconds: NaN }, 0],

    // Explicit per-dashboard "off".
    ["blob 0 → 0", 0, POLICY, 0],

    // Blob unset → server default.
    ["blob undefined → default", undefined, POLICY, 600],
    ["blob null → default", null, POLICY, 600],

    // Clamp to server max.
    ["blob > max → max", 100000, POLICY, 3600],
    ["blob == max → max", 3600, POLICY, 3600],

    // Valid blob below max passes through.
    ["blob valid < max → blob", 300, POLICY, 300],
    ["blob 1 → 1", 1, POLICY, 1],

    // Garbage blob fails closed.
    ["blob NaN → 0", NaN, POLICY, 0],
    ["blob negative → 0", -10, POLICY, 0],
    ["blob float → 0", 12.5, POLICY, 0],
    ["blob Infinity → 0", Infinity, POLICY, 0],
  ];

  it.each(cases)("%s", (_name, blobTtl, policy, expected) => {
    expect(resolveEffectiveCacheTtl(blobTtl, policy)).toBe(expected);
  });
});

describe("resolveAppliedRange", () => {
  const TTL = 600; // seconds
  const TTL_MS = TTL * 1000; // 600_000
  const DURATION = 900_000; // 15m window

  describe("rolling + caching on (snaps to bucket)", () => {
    it("snaps end down to the current TTL bucket and preserves duration", () => {
      const nowMs = 1_000_000_000; // not on a bucket edge
      const bucketEnd = Math.floor(nowMs / TTL_MS) * TTL_MS; // 999_600_000
      const r = resolveAppliedRange({
        kind: "rolling",
        baseStart: nowMs - DURATION,
        baseEnd: nowMs,
        durationMs: DURATION,
        effTtlSeconds: TTL,
        nowMs,
      });
      expect(r.end).toBe(bucketEnd);
      expect(r.start).toBe(bucketEnd - DURATION);
      expect(r.end - r.start).toBe(DURATION);
    });

    it("nowMs exactly on a bucket edge → end === nowMs", () => {
      const nowMs = 1666 * TTL_MS; // 999_600_000, divisible by TTL_MS
      const r = resolveAppliedRange({
        kind: "rolling",
        baseStart: nowMs - DURATION,
        baseEnd: nowMs,
        durationMs: DURATION,
        effTtlSeconds: TTL,
        nowMs,
      });
      expect(r.end).toBe(nowMs);
      expect(r.start).toBe(nowMs - DURATION);
    });

    it("edge - 1ms → previous bucket", () => {
      const edge = 1666 * TTL_MS; // 999_600_000
      const nowMs = edge - 1;
      const r = resolveAppliedRange({
        kind: "rolling",
        baseStart: nowMs - DURATION,
        baseEnd: nowMs,
        durationMs: DURATION,
        effTtlSeconds: TTL,
        nowMs,
      });
      expect(r.end).toBe(edge - TTL_MS); // 999_000_000
      expect(r.start).toBe(edge - TTL_MS - DURATION);
    });

    it("edge + 1ms → same bucket as the edge", () => {
      const edge = 1666 * TTL_MS;
      const nowMs = edge + 1;
      const r = resolveAppliedRange({
        kind: "rolling",
        baseStart: nowMs - DURATION,
        baseEnd: nowMs,
        durationMs: DURATION,
        effTtlSeconds: TTL,
        nowMs,
      });
      expect(r.end).toBe(edge);
      expect(r.start).toBe(edge - DURATION);
    });
  });

  describe("rolling + caching off (moving window)", () => {
    it("returns {now - duration, now} and moves as now moves", () => {
      const now1 = 1_000_000_000;
      const r1 = resolveAppliedRange({
        kind: "rolling",
        baseStart: now1 - DURATION,
        baseEnd: now1,
        durationMs: DURATION,
        effTtlSeconds: 0,
        nowMs: now1,
      });
      expect(r1).toEqual({ start: now1 - DURATION, end: now1 });

      // A later refresh with a fresh now advances the window (no freeze).
      const now2 = now1 + 5_000;
      const r2 = resolveAppliedRange({
        kind: "rolling",
        baseStart: now2 - DURATION,
        baseEnd: now2,
        durationMs: DURATION,
        effTtlSeconds: 0,
        nowMs: now2,
      });
      expect(r2).toEqual({ start: now2 - DURATION, end: now2 });
      expect(r2.end).toBeGreaterThan(r1.end);
    });
  });

  describe("calendar (today/yesterday) is NEVER snapped", () => {
    // Boundaries must be preserved regardless of effTtl — subtracting a rolling
    // duration would move the day off its calendar edge (the bug being fixed).
    const baseStart = 1_700_000_000_000;
    const baseEnd = 1_700_086_400_000;
    it.each([0, 600, 3600])("effTtl=%i → passthrough", (effTtl) => {
      const r = resolveAppliedRange({
        kind: "calendar",
        baseStart,
        baseEnd,
        durationMs: baseEnd - baseStart,
        effTtlSeconds: effTtl,
        nowMs: 1_700_050_000_123, // deliberately not on any bucket edge
      });
      expect(r).toEqual({ start: baseStart, end: baseEnd });
    });
  });

  describe("absolute is NEVER snapped", () => {
    const baseStart = 1_699_999_999_001;
    const baseEnd = 1_700_003_599_999;
    it.each([0, 600, 3600])("effTtl=%i → passthrough", (effTtl) => {
      const r = resolveAppliedRange({
        kind: "absolute",
        baseStart,
        baseEnd,
        durationMs: 0,
        effTtlSeconds: effTtl,
        nowMs: 1_700_050_000_123,
      });
      expect(r).toEqual({ start: baseStart, end: baseEnd });
    });
  });
});

describe("classifyPanelError", () => {
  it("marks an AuthorizationError as locked", () => {
    expect(classifyPanelError({ error_type: "AuthorizationError", message: "no access" })).toEqual({
      locked: true,
      message: "no access",
    });
  });

  it("uses the message of a plain error", () => {
    expect(classifyPanelError(new Error("boom"))).toEqual({ locked: false, message: "boom" });
  });

  it("keeps an authorization error locked when its message is empty", () => {
    expect(classifyPanelError({ error_type: "AuthorizationError", message: "" })).toEqual({
      locked: true,
      message: "Failed to load panel",
    });
  });

  it.each([undefined, null, "str", 42, {}])("falls back for %j", (err) => {
    expect(classifyPanelError(err)).toEqual({ locked: false, message: "Failed to load panel" });
  });
});

describe("clampTableLimit", () => {
  it.each<[string, number | undefined, number]>([
    ["undefined → default", undefined, 1000],
    ["0 → default", 0, 1000],
    ["negative → default", -5, 1000],
    ["NaN → default", NaN, 1000],
    ["Infinity → default", Infinity, 1000],
    ["fraction below 1 clamps up to 1", 0.5, 1],
    ["1.9 floors to 1", 1.9, 1],
    ["in range passes through", 250, 250],
    ["max passes through", 1000, 1000],
    ["above max clamps", 100000, 10000],
  ])("%s", (_name, value, expected) => {
    expect(clampTableLimit(value)).toBe(expected);
  });
});

describe("execSignature", () => {
  const base: DashboardQueryPanel = {
    id: "p1",
    kind: "query",
    title: "A",
    team_id: 1,
    source_id: 2,
    query: { mode: "result", language: "logchefql", text: "level:error", limit: 50 },
    viz: { type: "table" },
  };

  it("ignores fields that do not affect the fetched result", () => {
    expect(execSignature({ ...base, id: "p2", title: "B" })).toBe(execSignature(base));
    expect(execSignature({ ...base, viz: { type: "stat", value: "n", reduce: "avg", format: { scale: 1e-7 } } })).toBe(execSignature(base));
    expect(execSignature({ ...base, viz: { type: "table", visible_columns: ["a"] } })).toBe(execSignature(base));
  });

  it.each<[string, Partial<DashboardQueryPanel>]>([
    ["mode", { query: { ...base.query, mode: "count" } }],
    ["query text", { query: { ...base.query, text: "level:warn" } }],
    ["query language", { query: { ...base.query, language: "clickhouse-sql" } }],
    ["team", { team_id: 9 }],
    ["source", { source_id: 9 }],
    ["limit", { query: { ...base.query, limit: 10 } }],
    ["group by", { query: { ...base.query, group_by: "host" } }],
    ["translate limit", { query: { ...base.query, translate_limit: 5 } }],
  ])("changes when %s changes", (_name, patch) => {
    expect(execSignature({ ...base, ...patch })).not.toBe(execSignature(base));
  });
});

function resolved(start: string, end: string, zone = "UTC"): ResolvedWindow {
  return { start: epochMs(Date.parse(start)), end: epochMs(Date.parse(end)), zone: parseZoneId(zone) as ZoneId };
}

// RFC 0003 "Macros": `auto` keeps about 100 buckets, and a fixed interval
// that would give more than 1,000 buckets is raised to the next window.
describe("resolveInterval", () => {
  const sevenDays = { start: Date.parse("2026-11-05T18:30:00Z"), end: Date.parse("2026-11-12T18:30:00Z") };
  const oneHour = { start: Date.parse("2026-11-12T10:00:00Z"), end: Date.parse("2026-11-12T11:00:00Z") };

  it("keeps a fixed interval that fits: 24h buckets on a 7-day range", () => {
    expect(resolveInterval("24h", sevenDays)).toEqual({ window: "24h", raised: false });
    expect(resolveInterval("24h", oneHour)).toEqual({ window: "24h", raised: false });
  });

  it("resolves auto and an absent interval to about 100 buckets", () => {
    // 7 days / 100 = 6048 s, and the next window up is 2h.
    expect(resolveInterval("auto", sevenDays)).toEqual({ window: "2h", raised: false });
    expect(resolveInterval(undefined, sevenDays)).toEqual({ window: "2h", raised: false });
    // 1 hour / 100 = 36 s, and the next window up is 1m.
    expect(resolveInterval("auto", oneHour)).toEqual({ window: "1m", raised: false });
  });

  it("raises a fixed interval to the first window with at most 1,000 buckets", () => {
    // 7 days is 604,800 s: 10m gives 1,008 buckets and 15m gives 672.
    expect(resolveInterval("10m", sevenDays)).toEqual({ window: "15m", raised: true });
    expect(resolveInterval("1s", sevenDays)).toEqual({ window: "15m", raised: true });
    expect(resolveInterval("15m", sevenDays)).toEqual({ window: "15m", raised: false });
    // 1,000 buckets exactly is allowed: 1,000 s at 1s.
    const thousandSeconds = { start: 0, end: 1_000_000 };
    expect(resolveInterval("1s", thousandSeconds)).toEqual({ window: "1s", raised: false });
    expect(resolveInterval("1s", { start: 0, end: 1_001_000 })).toEqual({ window: "5s", raised: true });
  });

  it("stops at the largest window for a range no window fits", () => {
    const tenYears = { start: 0, end: 10 * 365 * 86_400_000 };
    expect(resolveInterval("1h", tenYears)).toEqual({ window: "24h", raised: true });
    expect(resolveInterval("24h", tenYears)).toEqual({ window: "24h", raised: false });
  });

  it("lists the sixteen histogram windows", () => {
    expect(PANEL_INTERVALS.map((entry) => entry.window)).toEqual(["1s", "5s", "10s", "15s", "30s", "1m", "5m", "10m", "15m", "30m", "1h", "2h", "3h", "6h", "12h", "24h"]);
  });
});

// A panel that sets query.range queries it in place of the dashboard range.
describe("resolvePanelRange", () => {
  const nowMs = Date.parse("2026-11-12T10:15:30Z");
  const dashboardRange = resolved("2026-11-12T10:00:30Z", "2026-11-12T10:15:30Z", "Asia/Kolkata");

  it("follows the dashboard range when the panel sets none", () => {
    expect(resolvePanelRange({ panelRange: undefined, dashboardRange, nowMs, effTtlSeconds: 0 })).toBe(dashboardRange);
  });

  it("resolves a calendar range in the dashboard's zone, unsnapped", () => {
    expect(resolvePanelRange({ panelRange: "today-6d", dashboardRange, nowMs, effTtlSeconds: 600 })).toEqual({
      start: Date.parse("2026-11-05T18:30:00Z"), end: nowMs, zone: "Asia/Kolkata",
    });
  });

  it("resolves a rolling range from now, snapped to the cache bucket when caching is on", () => {
    expect(resolvePanelRange({ panelRange: "7d", dashboardRange, nowMs, effTtlSeconds: 0 })).toEqual({
      start: nowMs - 7 * 86_400_000, end: nowMs, zone: "Asia/Kolkata",
    });
    const bucketEnd = Date.parse("2026-11-12T10:10:00Z");
    expect(resolvePanelRange({ panelRange: "7d", dashboardRange, nowMs, effTtlSeconds: 600 })).toEqual({
      start: bucketEnd - 7 * 86_400_000, end: bucketEnd, zone: "Asia/Kolkata",
    });
  });

  it("falls back to the dashboard range for a stored range that does not parse", () => {
    expect(resolvePanelRange({ panelRange: "last week", dashboardRange, nowMs, effTtlSeconds: 0 })).toBe(dashboardRange);
  });
});

// RFC 0003 "Time binding".
describe("panelTimeBound", () => {
  it.each([
    ["LogchefQL", "logchefql", 'level="error"', true],
    ["LogsQL", "logsql", "error | stats count()", true],
    ["ClickHouse SQL with its own window", "clickhouse-sql", "SELECT count() FROM logs WHERE ts >= today() - 4", false],
    ["ClickHouse SQL with the time filter", "clickhouse-sql", "SELECT count() FROM logs WHERE {{__time_filter}}", true],
    ["ClickHouse SQL with both bounds", "clickhouse-sql", "SELECT count() FROM logs WHERE ts >= {{__from}} AND ts < {{__to}}", true],
    ["ClickHouse SQL with one bound", "clickhouse-sql", "SELECT count() FROM logs WHERE ts >= {{__from}}", false],
    ["ClickHouse SQL with only an interval", "clickhouse-sql", "SELECT toStartOfInterval(ts, {{__interval}}) FROM logs", false],
    ["ClickHouse SQL with the filter in a comment", "clickhouse-sql", "SELECT count() FROM logs -- {{__time_filter}}", false],
    ["ClickHouse SQL with the filter in a string", "clickhouse-sql", "SELECT '{{__time_filter}}' FROM logs", false],
  ] as const)("%s", (_name, language, text, bound) => {
    expect(panelTimeBound({ language, text })).toBe(bound);
  });
});

// VictoriaLogs count buckets keep the offset of the range start.
describe("zoneOffsetChange", () => {
  it("finds the end of summer time in Berlin", () => {
    expect(zoneOffsetChange(resolved("2026-10-22T22:00:00Z", "2026-10-27T23:00:00Z", "Europe/Berlin"))).toEqual({
      at: Date.parse("2026-10-25T01:00:00Z"), beforeMs: 7_200_000, afterMs: 3_600_000,
    });
  });

  it("finds the start of summer time in New York", () => {
    expect(zoneOffsetChange(resolved("2026-03-07T17:00:00Z", "2026-03-10T16:00:00Z", "America/New_York"))).toEqual({
      at: Date.parse("2026-03-08T07:00:00Z"), beforeMs: -18_000_000, afterMs: -14_400_000,
    });
  });

  it("finds the first change of a range that crosses two", () => {
    expect(zoneOffsetChange(resolved("2026-01-01T00:00:00Z", "2026-12-31T00:00:00Z", "Europe/Berlin"))?.at).toBe(Date.parse("2026-03-29T01:00:00Z"));
    expect(zoneOffsetChange(resolved("2024-06-01T00:00:00Z", "2026-06-01T00:00:00Z", "Europe/Berlin"))?.at).toBe(Date.parse("2024-10-27T01:00:00Z"));
  });

  it("finds none in a zone without daylight saving, or in a range between changes", () => {
    expect(zoneOffsetChange(resolved("2026-10-22T22:00:00Z", "2026-10-27T23:00:00Z", "Asia/Kolkata"))).toBeNull();
    expect(zoneOffsetChange(resolved("2026-10-22T22:00:00Z", "2026-10-27T23:00:00Z", "UTC"))).toBeNull();
    expect(zoneOffsetChange(resolved("2026-11-01T00:00:00Z", "2026-11-12T00:00:00Z", "Europe/Berlin"))).toBeNull();
    expect(zoneOffsetChange(resolved("2026-10-25T01:00:00Z", "2026-10-27T23:00:00Z", "Europe/Berlin"))).toBeNull();
  });
});

describe("execSignature and the panel range", () => {
  it("changes when the range changes, so a preview runs again", () => {
    const panel: DashboardQueryPanel = { id: "p", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1" }, viz: { type: "table" } };
    const withRange = { ...panel, query: { ...panel.query, range: "today-6d" } };
    expect(execSignature(withRange)).not.toBe(execSignature(panel));
    expect(execSignature({ ...panel, query: { ...panel.query, interval: "24h" } })).not.toBe(execSignature(panel));
  });
});
