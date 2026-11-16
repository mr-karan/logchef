import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { NumberFormat } from "@/api/dashboards";
import { parseExact } from "../exactNumber";
import { formatNumber } from "../numberFormat";

// The Go report formatter (internal/reports) runs the same fixture, so a change
// on either side fails the other side's test.
const fixture: { cases: { name: string; value?: string; float?: number; format: NumberFormat; text: string }[] } =
  JSON.parse(readFileSync("../pkg/models/testdata/report_number_cases.json", "utf8"));

describe("report number cases shared with Go", () => {
  it("has cases", () => expect(fixture.cases.length).toBeGreaterThan(0));
  it.each(fixture.cases.map((c) => [c.name, c] as const))("%s", (_name, c) => {
    const value = c.value !== undefined ? parseExact(c.value) : c.float;
    if (value === null || value === undefined) throw new Error(`case ${c.name} has no valid value`);
    expect(formatNumber(value, c.format, { locale: "en-US" }).text).toBe(c.text);
  });
});
