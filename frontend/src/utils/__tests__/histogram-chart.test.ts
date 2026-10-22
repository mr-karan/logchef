import { describe, it, expect } from "vitest";
import {
  buildHistogramChartModel,
  buildHistogramTooltip,
  formatHistogramTimestamp,
  formatTooltipTimestamp,
  parseGranularityToMilliseconds,
  getColorForGroupValue,
  sanitizeChartColor,
} from "@/utils/histogram-chart";
import type { HistogramDataPoint } from "@/api/explore";
import { zoneFromPreference } from "@/utils/timeWindow";

// A browser zone that differs from the label zone, so browser-zone labels show up.
process.env.TZ = "Asia/Kolkata";

const MIN = 60_000;
// Interval-aligned bucket timestamps (what ClickHouse toStartOfInterval returns).
const iso = (min: number) => new Date(min * MIN).toISOString();

describe("buildHistogramChartModel zero-fill", () => {
  it("fills missing interior buckets with zero (ungrouped)", () => {
    // Buckets at minute 0, 1, and 3 — minute 2 is missing (no matching logs).
    const buckets: HistogramDataPoint[] = [
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
    const buckets: HistogramDataPoint[] = [
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
    const buckets: HistogramDataPoint[] = [
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
    const buckets: HistogramDataPoint[] = [
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
    const buckets: HistogramDataPoint[] = [
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
    const buckets: HistogramDataPoint[] = [
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

describe("buildHistogramChartModel grouped series identity", () => {
  it("uses is_other for synthetic Other without collapsing real marker values", () => {
    const model = buildHistogramChartModel([
      { bucket: iso(0), log_count: 4, group_value: "__other__", is_other: true },
      { bucket: iso(0), log_count: 3, group_value: "Other" },
      { bucket: iso(0), log_count: 2, group_value: "__other__" },
      { bucket: iso(0), log_count: 1, group_value: "" },
    ], "1m");

    expect(model.isGrouped).toBe(true);
    expect(model.series.map((series) => series.label)).toEqual([
      "Other",
      "Other",
      "__other__",
      "(empty)",
    ]);
    expect(model.series.filter((series) => series.label === "Other")).toHaveLength(2);
    expect(model.series[0].color).toBe("#6E7074");
    expect(model.series[1].color).not.toBe("#6E7074");
    expect(model.rows[0].total).toBe(10);
    for (const series of model.series) {
      expect(model.rows[0][series.key]).toBeGreaterThan(0);
    }
  });

  it("renders an all-empty group as grouped when the caller knows group-by was requested", () => {
    const model = buildHistogramChartModel([
      { bucket: iso(0), log_count: 5 },
    ], "1m", true);

    expect(model.isGrouped).toBe(true);
    expect(model.series).toHaveLength(1);
    expect(model.series[0].label).toBe("(empty)");
    expect(model.rows[0][model.series[0].key]).toBe(5);
  });

  it("keeps an empty group separate from a literal (empty) value at one timestamp", () => {
    const model = buildHistogramChartModel([
      { bucket: iso(0), log_count: 2, group_value: "" },
      { bucket: iso(0), log_count: 3, group_value: "(empty)" },
    ], "1m");

    expect(model.series.map((series) => series.label)).toEqual(["(empty)", "(empty)"]);
    expect(model.series.map((series) => model.rows[0][series.key])).toEqual([2, 3]);
    expect(model.rows[0].total).toBe(5);
  });

  it("keeps a structural null separate from literal null display values at one timestamp", () => {
    const model = buildHistogramChartModel([
      { bucket: iso(0), log_count: 2, group_value: "", is_null: true },
      { bucket: iso(0), log_count: 3, group_value: "(null)" },
      { bucket: iso(0), log_count: 4, group_value: "null" },
    ], "1m");

    expect(model.series.map((series) => series.label)).toEqual(["(null)", "(null)", "null"]);
    expect(model.series.map((series) => model.rows[0][series.key])).toEqual([2, 3, 4]);
    expect(model.rows[0].total).toBe(9);
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

describe("histogram labels", () => {
  const utc = zoneFromPreference("utc");

  it("render ticks and tooltips in the window's zone, not the browser's", () => {
    const range = { start: Date.UTC(2026, 9, 19, 3), end: Date.UTC(2026, 9, 19, 5), zone: utc };
    expect(formatHistogramTimestamp(Date.UTC(2026, 9, 19, 4), range, "1m")).toBe("04:00");
    expect(formatTooltipTimestamp(Date.UTC(2026, 9, 19, 4), range)).toBe("04:00:00");
  });

  it("show the date when the window crosses midnight in its zone", () => {
    // 17:00Z to 20:00Z stays on Oct 19 in UTC, but crosses midnight in IST.
    const range = { start: Date.UTC(2026, 9, 19, 17), end: Date.UTC(2026, 9, 19, 20), zone: utc };
    expect(formatHistogramTimestamp(Date.UTC(2026, 9, 19, 18), range, "1m")).toBe("18:00");
    const crossing = { ...range, end: Date.UTC(2026, 9, 20, 1) };
    expect(formatHistogramTimestamp(Date.UTC(2026, 9, 19, 18), crossing, "1m")).toMatch(/^(19 Oct|Oct 19), 18:00$/);
  });
});

describe("buildHistogramTooltip", () => {
  it("renders escaped labels, a safe color, precise time, and grouped counts", () => {
    const html = buildHistogramTooltip({
      datum: { ts: 0, bucket: iso(0), bucketEndTs: MIN, total: 12, first: 5, second: 7 },
      series: [
        { key: "first", label: '<img src=x onerror="alert(1)">', color: 'red;"><img src=x>' },
        { key: "second", label: "info", color: "#5470C6" },
      ],
      range: { start: 0, end: MIN, zone: zoneFromPreference("utc") },
      totalLabel: "Total <events>",
    });
    const host = document.createElement("div");
    host.innerHTML = html;
    expect(host.querySelector("img")).toBeNull();
    expect(html).toContain("background:var(--chart-1)");
    expect(html).toContain("background:#5470C6");
    expect(host.textContent).toContain('Total <events>12');
    expect(host.textContent).toContain('00:00:00');
    expect([...host.querySelectorAll("strong")].map((element) => element.textContent)).toEqual(["12", "5", "7"]);
  });

  it("omits the redundant total for a single series and defaults a missing value to zero", () => {
    const html = buildHistogramTooltip({
      datum: { ts: 0, bucket: iso(0), bucketEndTs: MIN, total: 0 },
      series: [{ key: "count", label: "Logs", color: "#5470C6" }],
      range: { start: 0, end: MIN, zone: zoneFromPreference("utc") },
      totalLabel: "Total",
    });
    expect(html).not.toContain("Total");
    const host = document.createElement("div");
    host.innerHTML = html;
    expect(host.querySelector("strong")?.textContent).toBe("0");
  });
});

describe("sanitizeChartColor", () => {
  it("allows known-safe color formats through unchanged", () => {
    expect(sanitizeChartColor("#EE6666")).toBe("#EE6666");
    expect(sanitizeChartColor("#fff")).toBe("#fff");
    expect(sanitizeChartColor("rgb(10, 20, 30)")).toBe("rgb(10, 20, 30)");
    expect(sanitizeChartColor("rgba(10, 20, 30, 0.5)")).toBe("rgba(10, 20, 30, 0.5)");
    expect(sanitizeChartColor("hsl(200, 50%, 50%)")).toBe("hsl(200, 50%, 50%)");
    expect(sanitizeChartColor("var(--chart-1)")).toBe("var(--chart-1)");
  });

  it("falls back to the default safe color for anything else, including an injection attempt", () => {
    expect(sanitizeChartColor(`red;"></span><img src=x onerror=alert(1)>`)).toBe("var(--chart-1)");
    expect(sanitizeChartColor("javascript:alert(1)")).toBe("var(--chart-1)");
    expect(sanitizeChartColor("")).toBe("var(--chart-1)");
    expect(sanitizeChartColor(null)).toBe("var(--chart-1)");
    expect(sanitizeChartColor(undefined)).toBe("var(--chart-1)");
  });
});
