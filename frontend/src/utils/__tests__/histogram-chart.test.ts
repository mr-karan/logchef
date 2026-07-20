import { describe, it, expect } from "vitest";
import {
  buildHistogramChartModel,
  parseGranularityToMilliseconds,
  getColorForGroupValue,
} from "@/utils/histogram-chart";
import type { HistogramData } from "@/services/HistogramService";

const MIN = 60_000;
// Interval-aligned bucket timestamps (what ClickHouse toStartOfInterval returns).
const iso = (min: number) => new Date(min * MIN).toISOString();

describe("buildHistogramChartModel zero-fill", () => {
  it("fills missing interior buckets with zero (ungrouped)", () => {
    // Buckets at minute 0, 1, and 3 — minute 2 is missing (no matching logs).
    const buckets: HistogramData[] = [
      { bucket: iso(0), log_count: 5 },
      { bucket: iso(1), log_count: 3 },
      { bucket: iso(3), log_count: 8 },
    ];
    const model = buildHistogramChartModel(buckets, "1m");
    expect(model.rows.map((r) => r.ts)).toEqual([0, MIN, 2 * MIN, 3 * MIN]);
    expect(model.rows.map((r) => r.count)).toEqual([5, 3, 0, 8]);
    // A filled row carries a real ISO bucket label and contiguous end.
    const filled = model.rows[2];
    expect(filled.total).toBe(0);
    expect(filled.bucketEndTs).toBe(3 * MIN);
  });

  it("fills gaps for every series (grouped)", () => {
    // Two hosts, each appearing in only some buckets across minutes 0..2.
    const buckets: HistogramData[] = [
      { bucket: iso(0), log_count: 4, group_value: "a" },
      { bucket: iso(0), log_count: 1, group_value: "b" },
      { bucket: iso(2), log_count: 7, group_value: "a" },
    ];
    const model = buildHistogramChartModel(buckets, "1m");
    expect(model.isGrouped).toBe(true);
    expect(model.rows.map((r) => r.ts)).toEqual([0, MIN, 2 * MIN]);
    // The middle bucket (minute 1) is fully zero for both series.
    const mid = model.rows[1];
    for (const s of model.series) {
      expect(mid[s.key]).toBe(0);
    }
    expect(mid.total).toBe(0);
    // Real buckets keep their counts, missing series zero-filled.
    const seriesA = model.series.find((s) => s.label === "a")!;
    const seriesB = model.series.find((s) => s.label === "b")!;
    expect(model.rows[0][seriesA.key]).toBe(4);
    expect(model.rows[0][seriesB.key]).toBe(1);
    expect(model.rows[2][seriesA.key]).toBe(7);
    expect(model.rows[2][seriesB.key]).toBe(0);
  });

  it("leaves a dense series unchanged (no phantom rows)", () => {
    const buckets: HistogramData[] = [
      { bucket: iso(0), log_count: 1 },
      { bucket: iso(1), log_count: 2 },
      { bucket: iso(2), log_count: 3 },
    ];
    const model = buildHistogramChartModel(buckets, "1m");
    expect(model.rows).toHaveLength(3);
    expect(model.rows.map((r) => r.count)).toEqual([1, 2, 3]);
  });

  it("does not fill a single bucket", () => {
    const model = buildHistogramChartModel([{ bucket: iso(0), log_count: 9 }], "1m");
    expect(model.rows).toHaveLength(1);
  });

  it("refuses to expand beyond the safety cap (bad step vs huge span)", () => {
    // First and last 10000 minutes apart at a 1m step → 10000 buckets > 5000 cap.
    const buckets: HistogramData[] = [
      { bucket: iso(0), log_count: 1 },
      { bucket: iso(10_000), log_count: 1 },
    ];
    const model = buildHistogramChartModel(buckets, "1m");
    expect(model.rows).toHaveLength(2);
  });

  // B5: a pathological granularity digit-string overflows Number() to Infinity;
  // parseGranularityToMilliseconds must reject it so fillBucketGaps never sees
  // an unbounded/non-finite step (which would loop `ts += Infinity` forever).
  it("does not hang on a pathological (overflowing) granularity", () => {
    const pathological = `${"9".repeat(400)}s`;
    const buckets: HistogramData[] = [
      { bucket: iso(0), log_count: 1 },
      { bucket: iso(1), log_count: 2 },
      { bucket: iso(2), log_count: 3 },
    ];
    expect(() => buildHistogramChartModel(buckets, pathological)).not.toThrow();
    const model = buildHistogramChartModel(buckets, pathological);
    // The bad granularity is rejected; cadence falls back to data-inference
    // instead of hanging, and the already-dense series is left unchanged.
    expect(model.rows).toHaveLength(3);
    expect(model.rows.map((r) => r.count)).toEqual([1, 2, 3]);
  });

  // B6: cadence inference must use the minimum positive gap across all
  // buckets, not just the first pair — a sparse head shouldn't set a
  // too-large step for the rest of the (denser) series.
  it("infers cadence from the minimum gap, not a sparse first gap", () => {
    // First gap is 5 minutes (0 -> 5); the rest of the data is dense at 1m.
    const buckets: HistogramData[] = [
      { bucket: iso(0), log_count: 1 },
      { bucket: iso(5), log_count: 2 },
      { bucket: iso(6), log_count: 3 },
      { bucket: iso(7), log_count: 4 },
    ];
    // No granularity passed — cadence must be inferred from the data.
    const model = buildHistogramChartModel(buckets);
    expect(model.bucketWidthMs).toBe(MIN);
    expect(model.rows.map((r) => r.ts)).toEqual(
      [0, 1, 2, 3, 4, 5, 6, 7].map((m) => m * MIN),
    );
    expect(model.rows.map((r) => r.count)).toEqual([1, 0, 0, 0, 0, 2, 3, 4]);
  });
});

describe("parseGranularityToMilliseconds", () => {
  it("parses well-formed granularities", () => {
    expect(parseGranularityToMilliseconds("5m")).toBe(5 * MIN);
    expect(parseGranularityToMilliseconds("30s")).toBe(30_000);
    expect(parseGranularityToMilliseconds("2h")).toBe(2 * 3_600_000);
  });

  it("rejects an overflowing (non-finite) result", () => {
    expect(parseGranularityToMilliseconds(`${"9".repeat(400)}s`)).toBeNull();
  });

  it("rejects a zero step", () => {
    expect(parseGranularityToMilliseconds("0s")).toBeNull();
  });

  it("returns null for missing/malformed input", () => {
    expect(parseGranularityToMilliseconds(undefined)).toBeNull();
    expect(parseGranularityToMilliseconds(null)).toBeNull();
    expect(parseGranularityToMilliseconds("bogus")).toBeNull();
  });
});

describe("getColorForGroupValue", () => {
  // B10: no own-property check meant "__proto__"/"constructor" resolved
  // through the prototype chain to a non-string (an object/function),
  // breaking the function's string-only contract.
  it("always returns a string, even for prototype-polluting values", () => {
    expect(typeof getColorForGroupValue("__proto__")).toBe("string");
    expect(typeof getColorForGroupValue("constructor")).toBe("string");
  });

  it("still resolves a real known severity", () => {
    expect(getColorForGroupValue("error")).toBe("#EE6666");
  });

  it("falls back to a default color for an empty value", () => {
    expect(getColorForGroupValue("")).toBe("#5470C6");
  });
});
