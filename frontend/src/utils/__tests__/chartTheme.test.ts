import { expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { buildPanelChart, chartColors, chartLabelColor } from "../panelCharts";
import { resultFrame } from "../panelFrame";
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
  expect(chartColors("POST", "POST").color).toBe("#91CC75");
  expect(chartColors("error", "error").color).toBe("#EE6666");
  expect(chartColors("other", "Other", true).color).toBe("var(--panel-chart-other)");
});

it.each([
  ['api', 'worker', 'web', 'db', 'cache', 'auth', 'gateway', 'scheduler'],
  ['200', '201', '202', '204', '301', '302', '400', '401', '403', '404', '500', '503'],
])('allocates distinct colours for realistic series %j', (...names) => {
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'group', type: 'String' }, { name: 'n', type: 'Int32' }], rows: names.map(group => ({ x: 'one', group, n: 1 })) });
  const model = buildPanelChart(frame, { type: 'chart', x: 'x', y: ['n'], series: 'group' }, false);
  expect(new Set(model.series.map(s => s.color)).size).toBe(names.length);
});
it('distinguishes y columns within a group', () => {
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'group', type: 'String' }, ...['a', 'b'].map(name => ({ name, type: 'Int32' }))], rows: [{ x: 'one', group: 'api', a: 1, b: 2 }] });
  const model = buildPanelChart(frame, { type: 'chart', x: 'x', y: ['a', 'b'], series: 'group' }, false);
  expect(new Set(model.series.map(s => s.color)).size).toBe(2);
});

function luminance(hex: string) {
  const c = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(n => n <= 0.04045 ? n / 12.92 : ((n + 0.055) / 1.055) ** 2.4);
  return c[0] * 0.2126 + c[1] * 0.7152 + c[2] * 0.0722;
}
it.each(['.light', '.dark'])('keeps palette marks and their labels readable in %s', selector => {
  const block = css.slice(css.indexOf(`${selector} {`)).split('}')[0];
  const colors = [...block.matchAll(/--panel-chart-\d+: (#[0-9a-f]+);/g)].map(m => m[1]);
  const background = selector === '.light' ? 1 : 0.003;
  for (const color of colors) {
    const l = luminance(color), text = luminance(chartLabelColor(color));
    expect((Math.max(l, background) + 0.05) / (Math.min(l, background) + 0.05)).toBeGreaterThanOrEqual(3);
    expect((Math.max(l, text) + 0.05) / (Math.min(l, text) + 0.05)).toBeGreaterThanOrEqual(4.5);
  }
  for (const [a, b] of [[2, 7], [2, 10], [7, 10]]) {
    const distance = Math.hypot(...[1, 3, 5].map(i => parseInt(colors[a].slice(i, i + 2), 16) - parseInt(colors[b].slice(i, i + 2), 16)));
    expect(distance).toBeGreaterThan(70);
  }
});
