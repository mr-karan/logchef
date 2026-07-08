import { describe, it, expect } from "vitest";
import { buildHistogramChartModel } from "@/utils/histogram-chart";
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
});
