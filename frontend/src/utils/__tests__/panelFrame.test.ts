import { describe, expect, it, vi } from "vitest";
import { addExact, averageExact, compareExact, exactString, parseExact, roundExact } from "../exactNumber";
import { countFrame, frameCellText, frameWithKinds, reduceStat, resultFrame, suggestKind } from "../panelFrame";
import { epochMs, parseZoneId } from "../timeWindow";
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
    for (const valid of ["0." + "0".repeat(399) + "1", "1" + "0".repeat(400), "1e-400", "9".repeat(100), "1." + "0".repeat(300), "1e0000", "1." + "0".repeat(510)]) expect(parseExact(valid)).not.toBeNull();
    for (const invalid of ["0." + "0".repeat(400) + "1", "1" + "0".repeat(401), "1e-401", "9".repeat(101), "1e99999", "1e00000", "1." + "0".repeat(511), "0x1f", "1,000", "NaN", "12ms"]) expect(parseExact(invalid)).toBeNull();
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
      try {
        process.env.TZ = zone;
        const dated = numericFrame([{ day: "2026-11-12", n: "2" }]);
        expect(frameCellText(dated.rows[0].day, dated.columns[1])).toBe("2026-11-12");
      } finally {
        if (original === undefined) delete process.env.TZ;
        else process.env.TZ = original;
      }
    }
    expect(frameWithKinds(frame, { missing: { kind: "number", confidence: "confirmed" } }).notices[0]).toContain("missing");
    expect(() => reduceStat(frame, { value: "missing", reduce: "last", count: false })).toThrow("missing");
    expect(() => reduceStat(frame, { value: "day", reduce: "last", count: false })).toThrow("must be a number");
    expect(suggestKind(["00123"])).toBe("category");
    expect(suggestKind(["42", "0.5"])).toBe("number");
    expect(suggestKind(["2026-11-12T01:02:03Z"])).toBe("time");
  });
  it("shows timestamps in the explicit viewer zone and rejects impossible dates", () => {
    const frame = resultFrame({ columns: [{ name: "at", type: "DateTime64(3)" }], rows: [{ at: "2026-11-12T23:30:01Z" }, { at: "2026-02-30T10:00:00Z" }] });
    expect(frameCellText(frame.rows[0].at, frame.columns[0], "UTC")).toBe("2026-11-12 23:30:01");
    expect(frameCellText(frame.rows[0].at, frame.columns[0], "Asia/Kolkata")).toBe("2026-11-13 05:00:01");
    expect(frame.rows[1].at).toBeNull();
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
  it.each(["sum", "avg", "min", "max", "first", "last"] as const)("%s returns zero for an empty count", (reduce) => {
    expect(frameCellText(reduceStat(countFrame([], null, false), { value: "count", reduce, count: true }).value)).toBe("0");
  });
});


describe("review regressions", () => {
  it.each(["constructor", "toString", "valueOf", "__proto__"])("keeps the numeric column %s", (name) => {
    const frame = resultFrame({ columns: [{ name, type: "UInt64" }], rows: [{ [name]: "42" }] });
    expect(frameCellText(frame.rows[0][name], frame.columns[0])).toBe("42");
    expect(frame.columns[0].confidence).toBe("declared");
  });
  it("keeps decimal scale and timestamp fractional digits in table cells", () => {
    const frame = resultFrame({ columns: [{ name: "n", type: "Nullable(Decimal(12,2))" }, { name: "at", type: "DateTime64(6)" }], rows: [{ n: "12.50", at: "2026-11-12T00:00:00.123456Z" }] });
    expect(frameCellText(frame.rows[0].n, frame.columns[0])).toBe("12.50");
    expect(frameCellText(frame.rows[0].at, frame.columns[1], "Asia/Kolkata")).toBe("2026-11-12 05:30:00.123456");
  });
  it("treats quiet untyped results and nonfinite strings as ordinary NULL", () => {
    expect(reduceStat(resultFrame({ columns: [], rows: [], untyped: true }), { value: "n", reduce: "last", count: false }).value).toBeNull();
    const frame = frameWithKinds(resultFrame({ columns: [], rows: ["NaN", "Inf", "+Inf", "-Inf"].map((n) => ({ n })), untyped: true }), { n: { kind: "number", confidence: "confirmed" } });
    expect(frame.rows.map((r) => r.n)).toEqual([null, null, null, null]);
    expect(frame.notices).toEqual([]);
    expect(frameWithKinds(resultFrame({ columns: [], rows: [], untyped: true }), { n: { kind: "number", confidence: "confirmed" } }).notices).toEqual([]);
    expect(() => reduceStat(resultFrame({ columns: [{ name: "other", type: "String" }], rows: [] }), { value: "n", reduce: "last", count: false })).toThrow("missing");
  });
  it("rejects trailing garbage and unzoned String time confirmations", () => {
    const frame = resultFrame({ columns: [{ name: "at", type: "String" }], rows: ["2026-11-12junk", "2026-11-12 01:02:03", "2026-11-12T01:02:03Zjunk"].map((at) => ({ at })) });
    for (const kind of ["time", "date"] as const) expect(frameWithKinds(frame, { at: { kind, confidence: "confirmed" } }).rows.map((r) => r.at)).toEqual([null, null, null]);
  });
  it("orders microsecond instants and text by code point", () => {
    const frame = resultFrame({ columns: [{ name: "n", type: "UInt64" }, { name: "at", type: "DateTime64(6)" }], rows: [{ n: 2, at: "2026-11-12T00:00:00.000900Z" }, { n: 1, at: "2026-11-12T01:00:00.000100+01:00" }] });
    const last = reduceStat(frame, { value: "n", reduce: "last", orderBy: "at", count: false });
    expect(frameCellText(last.value)).toBe("2");
    expect(last.notices).toEqual([]);
    const text = resultFrame({ columns: [{ name: "n", type: "UInt64" }, { name: "at", type: "String" }], rows: [{ n: 2, at: "a" }, { n: 1, at: "Z" }] });
    expect(frameCellText(reduceStat(text, { value: "n", reduce: "first", orderBy: "at", count: false }).value)).toBe("1");
  });
});


describe("sparse count buckets", () => {
  const range = { start: epochMs(Date.parse("2026-11-12T00:00:20Z")), end: epochMs(Date.parse("2026-11-12T00:04:00Z")), zone: parseZoneId("UTC")! };
  const sparse = [{ bucket: "2026-11-12T00:02:00Z", log_count: 6 }, { bucket: "2026-11-12T00:00:00Z", log_count: 2 }];
  const filled = [2, 0, 6, 0, 0].map((log_count, i) => ({ bucket: `2026-11-12T00:0${i}:00Z`, log_count }));
  it.each([["min", "0"], ["max", "6"], ["avg", "1.6"], ["sum", "8"], ["first", "2"], ["last", "0"]] as const)("%s agrees on sparse ClickHouse and zero-filled VictoriaLogs", (reduce, expected) => {
    for (const buckets of [sparse, filled]) {
      const frame = countFrame(buckets, "1m", false, undefined, range);
      const reduced = reduceStat(frame, { value: "count", reduce, count: true });
      expect(frameCellText(reduced.value)).toBe(expected);
      if (reduce === "last") expect(reduced.rowLabel).toBe("2026-11-12 00:04:00");
      if (reduce === "first") expect(reduced.rowLabel).toBe("2026-11-12 00:00:00");
    }
  });
  it("totals all groups per bucket before reducing", () => {
    const frame = countFrame([...sparse.map((b) => ({ ...b, group_value: "a" })), { bucket: "2026-11-12T00:00:00Z", log_count: 3, is_other: true }], "1m", true, undefined, range);
    expect(frameCellText(reduceStat(frame, { value: "count", reduce: "max", count: true }).value)).toBe("6");
    expect(frameCellText(reduceStat(frame, { value: "count", reduce: "avg", count: true }).value)).toBe("2.2");
    expect(frameCellText(reduceStat(frame, { value: "count", reduce: "first", count: true }).value)).toBe("5");
  });
});


describe("count bucket boundaries", () => {
  it("fills local daily buckets across daylight saving without inventing UTC buckets", () => {
    const range = { start: epochMs(Date.parse("2026-03-07T17:00:00Z")), end: epochMs(Date.parse("2026-03-10T16:00:00Z")), zone: parseZoneId("America/New_York")! };
    const frame = countFrame([{ bucket: "2026-03-08T05:00:00Z", log_count: 8 }], "24h", false, undefined, range);
    expect(frame.rawRows.map((r) => r.time)).toEqual(["2026-03-07T05:00:00Z", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z", "2026-03-10T04:00:00Z"]);
    expect(frameCellText(reduceStat(frame, { value: "count", reduce: "avg", count: true }).value)).toBe("2");
  });
  it("does not invent a new grouped series while filling empty buckets", () => {
    const range = { start: epochMs(Date.parse("2026-11-12T00:00:00Z")), end: epochMs(Date.parse("2026-11-12T00:02:00Z")), zone: parseZoneId("UTC")! };
    const frame = countFrame([{ bucket: "2026-11-12T00:01:00Z", log_count: 3, is_other: true }], "1m", true, undefined, range);
    expect(frame.rawRows.map((r) => r.series)).toEqual([{ kind: "other" }, { kind: "other" }, { kind: "other" }]);
  });
});


it("keeps multi-hour count buckets aligned after a skipped DST hour", () => {
  const range = { start: epochMs(Date.parse("2026-03-08T05:30:00Z")), end: epochMs(Date.parse("2026-03-08T10:30:00Z")), zone: parseZoneId("America/New_York")! };
  const frame = countFrame([{ bucket: "2026-03-08T08:00:00Z", log_count: 8 }], "2h", false, undefined, range);
  expect(frame.rawRows.map((r) => r.time)).toEqual(["2026-03-08T05:00:00Z", "2026-03-08T07:00:00Z", "2026-03-08T08:00:00Z", "2026-03-08T10:00:00Z"]);
  expect(frameCellText(reduceStat(frame, { value: "count", reduce: "avg", count: true }).value)).toBe("2");
});


describe("declared date wire values", () => {
  it.each(["Date", "Date32", "Nullable(Date)", "LowCardinality(Date32)"])("keeps %s RFC 3339 values as calendar dates in non-UTC zones", (type) => {
    const original = process.env.TZ;
    try {
      for (const zone of ["Etc/GMT+8", "Pacific/Apia"]) {
        process.env.TZ = zone;
        const frame = resultFrame({ columns: [{ name: "day", type }], rows: [{ day: "2026-11-12T00:00:00Z" }, { day: "2026-11-12T00:00:00+13:00" }] });
        expect(frame.rows.map((row) => frameCellText(row.day, frame.columns[0], zone))).toEqual(["2026-11-12", "2026-11-12"]);
        expect(frame.notices).toEqual([]);
      }
    } finally {
      if (original === undefined) delete process.env.TZ;
      else process.env.TZ = original;
    }
  });
  it("keeps confirmed String dates strict even when typed dates accept timestamps", () => {
    const raw = resultFrame({ columns: [{ name: "day", type: "String" }], rows: ["2026-11-12", "2026-11-12T00:00:00Z", "2026-11-12junk"].map((day) => ({ day })) });
    expect(frameWithKinds(raw, { day: { kind: "date", confidence: "confirmed" } }).rows.map((row) => row.day)).toEqual(["2026-11-12", null, null]);
  });
});

it.each(["constructor", "toString", "valueOf", "__proto__"])("treats absent %s cells as NULL without an invalid-cell notice", (name) => {
  const frame = resultFrame({ columns: [{ name, type: "UInt64" }], rows: [{}] });
  expect(frame.rows[0][name]).toBeNull();
  expect(frame.notices).toEqual([]);
});

it.each([
  ["10m", ["2026-11-12T00:10:00Z", "2026-11-12T00:20:00Z", "2026-11-12T00:30:00Z", "2026-11-12T00:40:00Z"]],
  ["30m", ["2026-11-12T00:00:00Z", "2026-11-12T00:30:00Z"]],
])("keeps ClickHouse %s Kathmandu buckets on the epoch grid", (interval, times) => {
  const range = { start: epochMs(Date.parse("2026-11-12T00:17:00Z")), end: epochMs(Date.parse("2026-11-12T00:47:00Z")), zone: parseZoneId("Asia/Kathmandu")! };
  const frame = countFrame(times.map((bucket) => ({ bucket, log_count: 4 })), interval, false, undefined, range);
  expect(frame.rawRows.map((row) => row.time)).toEqual(times);
  expect(frameCellText(reduceStat(frame, { value: "count", reduce: "avg", count: true }).value)).toBe("4");
  expect(frameCellText(reduceStat(frame, { value: "count", reduce: "min", count: true }).value)).toBe("4");
});
