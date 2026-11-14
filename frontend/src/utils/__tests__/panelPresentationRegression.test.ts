import { describe, expect, it } from "vitest";
import { thresholdColor, ruleMatches, tableRuleColor, formatPanelTime, formattedTableCell, validatePresentation } from "../panelPresentation";
import { parseExact } from "../exactNumber";
import { resultFrame } from "../panelFrame";
import type { TableRule } from "@/api/dashboards";

const rule: TableRule = { columns: ["n"], op: "contains", value: "345", color: "red", scope: "cell" };
describe("table text rules", () => {
  it("contains on an exact number cell", () => {
    const frame = resultFrame({ columns: [{ name: "n", type: "UInt64" }], rows: [{ n: "123456" }] });
    expect(ruleMatches(frame.rows[0].n, rule)).toBe(true);
  });
  it("not_contains on an exact number cell", () => {
    const frame = resultFrame({ columns: [{ name: "n", type: "Decimal(10,2)" }], rows: [{ n: "1.50" }] });
    expect(ruleMatches(frame.rows[0].n, { ...rule, op: "not_contains", value: "1.5" })).toBe(false);
  });
  it("text rules use the table's native decimal scale in both scopes", () => {
    const frame = resultFrame({ columns: [{ name: "n", type: "Decimal(10,2)" }], rows: [{ n: "1.50" }] });
    const match: TableRule = { ...rule, value: "50" };
    expect(formattedTableCell(frame.rows[0].n, frame.columns[0])).toBe("1.50");
    expect(tableRuleColor(frame.rows[0], [match], "n", frame.columns)).toBe("red");
    expect(tableRuleColor(frame.rows[0], [{ ...match, scope: "row" }], undefined, frame.columns)).toBe("red");
  });
  it("first matching rule wins within a scope", () => {
    const row = { msg: "retry failed" };
    const a: TableRule = { columns: ["msg"], op: "contains", value: "retry", color: "amber", scope: "row" };
    const b: TableRule = { ...a, value: "failed", color: "red" };
    expect(tableRuleColor(row, [a, b])).toBe("amber");
    expect(tableRuleColor(row, [b, a])).toBe("red");
  });
  it("contains is case-sensitive", () => {
    expect(ruleMatches("Retry", { ...rule, value: "retry" })).toBe(false);
  });
  it("= on text compares numerically when both parse", () => {
    expect(ruleMatches("007", { ...rule, op: "=", value: "7" })).toBe(true);
  });
});
describe("threshold boundaries", () => {
  const steps = [{ value: null, color: "green" as const }, { value: "-10", color: "amber" as const }, { value: "0", color: "red" as const }];
  it("negative steps and boundaries", () => {
    expect(thresholdColor(parseExact("-10"), steps)).toBe("amber");
    expect(thresholdColor(parseExact("-10.0000000001"), steps)).toBe("green");
    expect(thresholdColor(parseExact("-0"), steps)).toBe("red");
    expect(thresholdColor(-5, steps)).toBe("amber");
  });
  it("equal steps are rejected", () => {
    expect(validatePresentation({ type: "stat", thresholds: [{ value: null, color: "green" }, { value: "5", color: "amber" }, { value: "5.0", color: "red" }] })).not.toBeNull();
  });
});
describe("native timestamp precision", () => {
  it("DateTime64(6) default keeps sub-second digits", () => {
    const frame = resultFrame({ columns: [{ name: "ts", type: "DateTime64(6)" }], rows: [{ ts: "2026-11-12 23:05:09.123456" }] });
    expect(formattedTableCell(frame.rows[0].ts, frame.columns[0], undefined, "UTC")).toBe("2026-11-12 23:05:09.123456");
  });
  it("millis on DateTime64(6)", () => {
    expect(formatPanelTime("2026-11-12T23:05:09.123456Z", "time", { kind: "time", time: "millis" }, "UTC")).toBe("2026-11-12 23:05:09.123");
  });
});
