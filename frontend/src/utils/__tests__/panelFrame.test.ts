import { describe, expect, it, vi } from "vitest";
import { addExact, averageExact, compareExact, exactString, parseExact, roundExact } from "../exactNumber";
import { countFrame, frameCellText, frameWithKinds, reduceStat, resultFrame, suggestKind } from "../panelFrame";
import { formatNumber } from "../numberFormat";

function exact(value: string) {
  const parsed = parseExact(value);
  if (!parsed) throw new Error(`Invalid test number ${value}`);
  return parsed;
}
function numericFrame(rows: Record<string, unknown>[]) {
  return resultFrame({ columns: [{ name: "n", type: "Decimal(38,10)" }, { name: "day", type: "Date" }], rows });
}
describe("exact numbers", () => {
  it("preserves wide integers and decimal sums, comparisons, scaling and half-away rounding", () => {
    expect(exactString(addExact(exact("9007199254740993"), exact("1")))).toBe("9007199254740994");
    expect(compareExact(exact("9007199254740993"), exact("9007199254740992"))).toBe(1);
    expect(exactString(exact("12345678901234567890.1234567890"))).toBe("12345678901234567890.123456789");
    expect(exactString(roundExact(exact("-1.235"), 2), 2)).toBe("-1.24");
    expect(exactString(averageExact(exact("1"), 3))).toBe("0.3333333333");
    expect(formatNumber(exact("124000000"), { scale: 1e-7, decimals: 2, suffix: " Cr" }, { locale: "en-US" }).text).toBe("12.40 Cr");
  });
  it("checks each side of the coefficient, scale, exponent and literal bounds before BigInt", () => {
    for (const valid of ["0." + "0".repeat(399) + "1", "1" + "0".repeat(400), "1e-400", "9".repeat(100), "1." + "0".repeat(300)]) expect(parseExact(valid)).not.toBeNull();
    for (const invalid of ["0." + "0".repeat(400) + "1", "1" + "0".repeat(401), "1e-401", "9".repeat(101), "1e99999", "0x1f", "1,000", "NaN", "12ms"]) expect(parseExact(invalid)).toBeNull();
    const spy = vi.spyOn(globalThis, "BigInt");
    expect(parseExact("0." + "0".repeat(10000) + "1")).toBeNull();
    expect(parseExact("1" + "0".repeat(10000))).toBeNull();
    expect(spy).not.toHaveBeenCalled();
    spy.mockRestore();
  });
  it("formats full digits by default, grouping by locale and showing an exact compact tooltip", () => {
    expect(formatNumber(exact("237497"), {}, { locale: "en-US" }).text).toBe("237,497");
    expect(formatNumber(exact("9007199254740993"), { notation: "standard" }, { locale: "en-US" }).text).toBe("9,007,199,254,740,993");
    expect(formatNumber(exact("1234.5"), { decimals: 2 }, { locale: "de-DE" }).text).toBe("1.234,50");
    expect(formatNumber(exact("9007199254740993"), { notation: "compact" }, { locale: "en-US" }).exact).toBe("9,007,199,254,740,993");
    expect(formatNumber(exact("-0.005"), { decimals: 2 }, { locale: "en-US" }).text).toBe("-0.01");
  });
});
describe("panel frames", () => {
  it("parses mixed UInt64 values and confirmed number cells independently", () => {
    const frame = resultFrame({ columns: [{ name: "n", type: "LowCardinality(Nullable(UInt64))" }], rows: [{ n: 12 }, { n: "9007199254740993" }] });
    expect(frame.columns[0]).toMatchObject({ kind: "number", repr: "bigint", confidence: "declared" });
    expect(frameCellText(frame.rows[1].n)).toBe("9007199254740993");
    const raw = resultFrame({ columns: [{ name: "n", type: "Float64" }], rows: ["12", "1.5e3", "abc", ""].map((n) => ({ n })), untyped: true });
    expect(raw.columns[0].kind).toBe("category");
    const confirmed = frameWithKinds(raw, { n: { kind: "number", confidence: "confirmed" } });
    expect(confirmed.rows.map((r) => frameCellText(r.n))).toEqual(["12", "1500", "", ""]);
    expect(confirmed.notices).toEqual(['1 invalid cell(s) in "n" shown as NULL.']);
    expect(frameWithKinds(confirmed, { n: { kind: "category", confidence: "confirmed" } }).rows[2].n).toBe("abc");
  });
  it("keeps dates as calendar dates, validates kinds and reports missing columns", () => {
    const frame = numericFrame([{ day: "2026-11-12", n: "2" }]);
    for (const zone of ["Etc/GMT+8", "Pacific/Apia"]) {
      const original = process.env.TZ;
      process.env.TZ = zone;
      expect(frameCellText(frame.rows[0].day, frame.columns[1])).toBe("2026-11-12");
      process.env.TZ = original;
    }
    expect(frameWithKinds(frame, { missing: { kind: "number", confidence: "confirmed" } }).notices[0]).toContain("missing");
    expect(() => reduceStat(frame, { value: "missing", reduce: "last", count: false })).toThrow("missing");
    expect(() => reduceStat(frame, { value: "day", reduce: "last", count: false })).toThrow("must be a number");
    expect(suggestKind(["00123"])).toBe("category");
    expect(suggestKind(["42", "0.5"])).toBe("number");
    expect(suggestKind(["2026-11-12T01:02:03Z"])).toBe("time");
  });
  it("keeps NULL, empty, real Other and synthetic Other groups distinct", () => {
    const frame = countFrame([{ bucket: "2026-11-12T00:00:00Z", log_count: 2, group_value: "Other" }, { bucket: "2026-11-12T00:00:00Z", log_count: 3, is_other: true }, { bucket: "2026-11-12T00:00:00Z", log_count: 1, is_null: true }, { bucket: "2026-11-12T00:00:00Z", log_count: 4, group_value: "" }], "1h", true);
    expect(frame.rows.map((r) => r.series)).toEqual([{ kind: "value", value: "Other" }, { kind: "other" }, { kind: "null" }, { kind: "value", value: "" }]);
  });
});
describe("stat reducers", () => {
  const frame = numericFrame([{ n: "9007199254740993", day: "2026-11-10" }, { n: null, day: "2026-11-12" }, { n: "3", day: "2026-11-09" }, { n: "4", day: "2026-11-10" }, { n: "5", day: null }]);
  it.each([ ["sum", "9007199254741005"], ["avg", "2251799813685251.25"], ["min", "3"], ["max", "9007199254740993"] ] as const)("%s ignores NULL and uses every row", (reduce, expected) => {
    const result = reduceStat(frame, { value: "n", reduce, orderBy: "day", count: false });
    expect(frameCellText(result.value)).toBe(expected);
  });
  it("sorts chronologically, preserves NULL on the latest row and reports invalid order cells", () => {
    const last = reduceStat(frame, { value: "n", reduce: "last", orderBy: "day", count: false });
    expect(last.value).toBeNull();
    expect(last.rowLabel).toBe("2026-11-12");
    expect(last.notices.join()).toContain("1 row(s)");
    expect(frameCellText(reduceStat(frame, { value: "n", reduce: "first", orderBy: "day", count: false }).value)).toBe("3");
  });
  it("uses stable result order on ties and warns about unordered rows", () => {
    const tied = numericFrame([{ n: "1", day: "2026-11-12" }, { n: "2", day: "2026-11-12" }]);
    expect(frameCellText(reduceStat(tied, { value: "n", reduce: "last", orderBy: "day", count: false }).value)).toBe("2");
    expect(reduceStat(tied, { value: "n", reduce: "first", orderBy: "day", count: false }).notices.join()).toContain("2 rows share");
    expect(reduceStat(tied, { value: "n", reduce: "last", count: false }).notices.join()).toContain("without order_by");
  });
  it.each(["sum", "avg", "min", "max", "first", "last"] as const)("%s has defined empty and NULL behavior", (reduce) => {
    expect(reduceStat(numericFrame([]), { value: "n", reduce, count: false }).value).toBeNull();
    expect(reduceStat(numericFrame([{ n: null }]), { value: "n", reduce, count: false }).value).toBeNull();
  });
  it("returns zero for an empty count", () => {
    expect(frameCellText(reduceStat(countFrame([], null, false), { value: "count", reduce: "sum", count: true }).value)).toBe("0");
  });
});
