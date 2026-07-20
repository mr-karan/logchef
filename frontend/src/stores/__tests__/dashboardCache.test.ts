import { describe, it, expect } from "vitest";
import { resolveEffectiveCacheTtl, resolveAppliedRange } from "../dashboards";
import type { DashboardCachePolicy } from "@/api/meta";

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
