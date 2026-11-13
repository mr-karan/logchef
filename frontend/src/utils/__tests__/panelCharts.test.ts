import { describe, expect, it } from "vitest";
import { countFrame, resultFrame } from "../panelFrame";
import { buildPanelChart, buildPanelList, suggestRoles, suggestViz } from "../panelCharts";

const frame = resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "a", type: "Decimal(20,2)" }, { name: "b", type: "UInt64" }], rows: [{ day: "2026-11-10", a: "12.50", b: "9007199254740993" }, { day: "2026-11-08", a: null, b: "3" }] });

describe("result chart frames", () => {
  it("preserves category query order and NULL while time axes sort without filling", () => {
    const category = buildPanelChart(frame, { type: "chart", x: "day", y: ["b", "a"], x_scale: "category" }, false);
    expect(category.rows.map((r) => r.label)).toEqual(["2026-11-10", "2026-11-08"]);
    expect(category.series.map((s) => s.label)).toEqual(["b", "a"]);
    expect(category.rows[1].values.get(category.series[1].key)).toBeNull();
    expect(category.rows[0].values.get(category.series[0].key)).toMatchObject({ exact: "9,007,199,254,740,993", approximate: true });
    const time = buildPanelChart(frame, { type: "chart", x: "day", y: ["a"] }, false);
    expect(time.rows.map((r) => r.label)).toEqual(["2026-11-08", "2026-11-10"]);
    expect(time.rows).toHaveLength(2);
  });
  it("checks unique keys over the full result before applying the 50-series cap", () => {
    const columns = [{ name: "x", type: "String" }, { name: "s", type: "String" }, { name: "n", type: "UInt32" }];
    const rows = Array.from({ length: 55 }, (_, i) => ({ x: "x", s: String(i), n: i }));
    const model = buildPanelChart(resultFrame({ columns, rows }), { type: "chart", x: "x", series: "s", y: ["n"] }, false);
    expect(model.series).toHaveLength(50);
    expect(model.notices).toContain("50 of 55 series shown.");
    expect(() => buildPanelChart(resultFrame({ columns, rows: [...rows, ...rows.slice(53)] }), { type: "chart", x: "x", series: "s", y: ["n"] }, false)).toThrow('2 duplicate key(s). First key: x, 53. Group the query by "x" and "s".');
  });
  it("reports invalid roles on empty data", () => {
    const empty = resultFrame({ columns: [{ name: "x", type: "String" }], rows: [] });
    expect(() => buildPanelChart(empty, { type: "chart", x: "x", y: ["missing"] }, false)).toThrow('Column "missing"');
    expect(() => buildPanelChart(empty, { type: "chart", x: "x", y: ["x"] }, false)).toThrow("must be a number");
    expect(() => buildPanelList(empty, { type: "pie", label: "x", value: "missing" }, false)).toThrow('Column "missing"');
  });
  it("suggests persisted roles and a visualization from preview columns", () => {
    expect(suggestViz(frame)).toMatchObject({ type: "chart", x: "day", y: ["a", "b"], mark: "line" });
    const grouped = resultFrame({ columns: [{ name: "time", type: "DateTime" }, { name: "service", type: "String" }, { name: "n", type: "UInt32" }], rows: [] });
    expect(suggestRoles(grouped, "chart")).toMatchObject({ x: "time", y: ["n"], series: "service" });
    const native = resultFrame({ columns: [{ name: "service", type: "String" }, { name: "n", type: "String" }], rows: [{ service: "api", n: "12" }, { service: "web", n: "13" }], untyped: true });
    expect(suggestViz(native).columns).toMatchObject({ n: { kind: "number", confidence: "confirmed" } });
    expect(suggestViz(native)).toMatchObject({ type: "chart", mark: "bar", y: ["n"] });
  });
});

describe("list and pie frames", () => {
  const columns = [{ name: "label", type: "String" }, { name: "n", type: "Decimal(20,2)" }];
  it("folds only requested small slices and preserves a real Other label", () => {
    const source = resultFrame({ columns, rows: [{ label: "Other", n: "990" }, { label: "a", n: "2" }, { label: "b", n: "3" }, { label: "c", n: null }] });
    expect(buildPanelList(source, { type: "pie", label: "label", value: "n" }, false).items.map((i) => i.label)).toEqual(["Other", "a", "b"]);
    const folded = buildPanelList(source, { type: "pie", label: "label", value: "n", min_slice_percent: 1 }, false);
    expect(folded.items.map((i) => [i.label, i.value.exact])).toEqual([["Other", "990"], ["Other", "5"]]);
    expect(new Set(folded.items.map((i) => i.key)).size).toBe(2);
    expect(folded.total?.exact).toBe("995");
    expect(folded.notices).toContain("1 NULL value(s) omitted.");
    expect(buildPanelList(source, { type: "toplist", label: "label", value: "n", max_rows: 2 }, false).notices).toContain("2 of 4 rows shown.");
  });
  it("rejects negative pie values and duplicate labels without adding them", () => {
    expect(() => buildPanelList(resultFrame({ columns, rows: [{ label: "bad", n: "-1" }] }), { type: "pie", label: "label", value: "n" }, false)).toThrow("nonnegative");
    const duplicates = resultFrame({ columns, rows: [{ label: "a", n: "10" }, { label: "a", n: "20" }] });
    for (const type of ["pie", "toplist"] as const) expect(() => buildPanelList(duplicates, { type, label: "label", value: "n" }, false)).toThrow('1 duplicate key(s). First key: a. Group the query by "label".');
  });
  it("keeps count identities apart, adds duplicate counts and fills only count time gaps", () => {
    const source = countFrame([
      { bucket: "2026-11-10T00:00:00Z", log_count: 1, group_value: "Other" },
      { bucket: "2026-11-10T00:00:00Z", log_count: 2, is_null: true },
      { bucket: "2026-11-10T00:00:00Z", log_count: 3, group_value: "" },
      { bucket: "2026-11-10T00:00:00Z", log_count: 4, is_other: true },
      { bucket: "2026-11-10T00:00:00Z", log_count: 5, group_value: "Other" },
      { bucket: "2026-11-12T00:00:00Z", log_count: 1, group_value: "Other" },
    ], "24h", true);
    const chart = buildPanelChart(source, { type: "chart" }, true);
    expect(chart.series.map((s) => s.label)).toEqual(["Other", "(null)", "(empty)", "Other"]);
    expect(chart.rows).toHaveLength(3);
    expect(chart.rows[0].values.get(chart.series[0].key)?.exact).toBe("6");
    expect([...chart.rows[1].values.values()].map((v) => v?.coordinate)).toEqual([0, 0, 0, 0]);
    const pie = buildPanelList(source, { type: "pie" }, true);
    expect(pie.items).toHaveLength(4);
    expect(pie.items.map((i) => i.value.exact)).toEqual(["7", "4", "3", "2"]);
  });
});

it("suggests table for multiple category columns and preserves explicit roles", async () => {
  const { withSuggestedRoles } = await import("../panelCharts");
  const source = resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "n", type: "UInt32" }, { name: "s", type: "String" }], rows: [] });
  expect(withSuggestedRoles(source, { type: "chart", x: "s", y: ["n"], x_scale: "category" })).toMatchObject({ x: "s", y: ["n"], x_scale: "category" });
  expect(withSuggestedRoles(source, { type: "chart", x: "day", y: ["n"] })).toEqual({ type: "chart", x: "day", y: ["n"] });
  const categories = resultFrame({ columns: [{ name: "a", type: "String" }, { name: "b", type: "String" }, { name: "n", type: "UInt32" }], rows: [{ a: "a", b: "b", n: 1 }, { a: "b", b: "c", n: 2 }] });
  expect(suggestViz(categories).type).toBe("table");
});

it("caps pies at 50 query-ordered slices without changing their shares", () => {
  const source = resultFrame({ columns: [{ name: "label", type: "String" }, { name: "n", type: "UInt32" }], rows: Array.from({ length: 52 }, (_, i) => ({ label: `label ${i}`, n: 1 })) });
  const pie = buildPanelList(source, { type: "pie", label: "label", value: "n" }, false);
  expect(pie.items).toHaveLength(50);
  expect(pie.items[0].label).toBe("label 0");
  expect(pie.items[49].label).toBe("Other");
  expect(pie.items[0].percentage).toBeCloseTo(100 / 52);
  expect(pie.notices).toContain("50 of 52 slices shown. Overflow folded into Other.");
});

it("retains full timestamp strings as result keys", () => {
  const source = resultFrame({ columns: [{ name: "t", type: "DateTime64" }, { name: "n", type: "UInt32" }], rows: [{ t: "2026-11-10T00:00:00Z", n: 1 }, { t: "2026-11-10T01:00:00+01:00", n: 2 }] });
  expect(buildPanelChart(source, { type: "chart", x: "t", y: ["n"] }, false).rows).toHaveLength(2);
});

it("retains the exact real tiny remainder with a categorical color", () => {
  const source = resultFrame({ columns: [{ name: "method", type: "String" }, { name: "n", type: "UInt64" }], rows: [{ method: "POST", n: "59500" }, { method: "GET", n: "57800" }, { method: "PUT", n: "1" }] });
  const pie = buildPanelList(source, { type: "pie", label: "method", value: "n", min_slice_percent: 1 }, false);
  expect(pie.total?.exact).toBe("117,301");
  expect(pie.items.map((i) => [i.label, i.value.exact])).toEqual([["POST", "59,500"], ["GET", "57,800"], ["Other", "1"]]);
  expect(pie.items[2].percentage).toBeCloseTo(100 / 117301);
  expect(pie.items[2].color).toBe("var(--panel-chart-other)");
  expect(new Set(pie.items.map((i) => i.color)).size).toBe(3);
});
it("does not invent a remainder when all slices meet the threshold", () => {
  const source = resultFrame({ columns: [{ name: "method", type: "String" }, { name: "n", type: "Decimal(20,2)" }], rows: [{ method: "GET", n: "0.10" }, { method: "POST", n: "0.20" }] });
  const pie = buildPanelList(source, { type: "pie", label: "method", value: "n", min_slice_percent: 1 }, false);
  expect(pie.items.map((i) => i.label)).toEqual(["GET", "POST"]);
  expect(pie.total?.exact).toBe("0.3");
});
