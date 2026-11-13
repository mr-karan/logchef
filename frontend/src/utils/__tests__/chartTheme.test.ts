import { expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { chartColors } from "../panelCharts";
const css = readFileSync("src/assets/index.css", "utf8");
it.each([".light", ".dark"])("provides twelve distinct dashboard colors and preserves Explorer in %s", selector => {
  const block = css.slice(css.indexOf(`${selector} {`)).split("}")[0];
  const colors = [...block.matchAll(/--panel-chart-\d+: (#[0-9a-f]+);/g)].map(m => m[1]);
  expect(colors).toHaveLength(12);
  expect(new Set(colors).size).toBe(12);
  expect(block).toContain("--chart-1: oklch(0.623 0.214 259.815)");
  expect(block).toContain("--panel-chart-other: #6e7074");
});
it("keys colors on series identity and retains semantic colors", () => {
  const first = Array.from({ length: 12 }, (_, i) => chartColors(`group-${i}`, `group-${i}`).color);
  expect(new Set(first).size).toBeGreaterThan(10);
  expect(chartColors("POST", "POST").color).toBe("#91CC75");
  expect(chartColors("error", "error").color).toBe("#EE6666");
  expect(chartColors("other", "Other", true).color).toBe("var(--panel-chart-other)");
});
