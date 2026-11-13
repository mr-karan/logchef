import { describe, expect, it } from "vitest";
import { parseExact } from "../exactNumber";
import { formatNumber } from "../numberFormat";
function format(value: string, options: Parameters<typeof formatNumber>[1] = {}, locale = "en-US") {
  const n = parseExact(value);
  if (!n) throw new Error("Invalid fixture");
  return formatNumber(n, options, { locale });
}
describe("number format review regressions", () => {
  it("chooses compact magnitude after rounding", () => {
    expect(format("999999", { notation: "compact" }).text).toBe("1M");
    expect(format("999999999", { notation: "compact", decimals: 0 }).text).toBe("1B");
  });
  it("chooses byte units after rounding and never compacts bytes", () => {
    expect(format("1048575", { unit: "bytes" }).text).toBe("1 MiB");
    expect(format("1048575", { unit: "bytes", notation: "compact" }).text).toBe("1 MiB");
    expect(format("1048575", { unit: "bytes" }).exact).toBe("1,048,575 B");
  });
  it.each([
    ["0.5", "duration_ms", "0.5 ms"], ["999.999", "duration_ms", "1 s"],
    ["59999.9", "duration_ms", "1 min"], ["3599.999", "duration_s", "1 h"],
    ["86399.999", "duration_s", "1 d"], ["0.001", "duration_s", "1 ms"],
  ] as const)("formats %s %s as %s", (value, unit, expected) => {
    expect(format(value, { unit }).text).toBe(expected);
    expect(format(value, { unit }).exact).toContain(unit === "duration_ms" ? " ms" : " s");
  });
  it("localizes fractional digits with integer digits", () => {
    expect(format("1234.5", { decimals: 2 }, "ar-EG").text).toBe(new Intl.NumberFormat("ar-EG", { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(1234.5));
  });
  it("shows overflow after floating point scaling", () => {
    expect(formatNumber(Number.MAX_VALUE, { scale: 1e100 }).text).toBe("Overflow");
  });
});
