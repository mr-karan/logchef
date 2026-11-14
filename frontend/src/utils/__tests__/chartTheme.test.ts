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
    const label = chartLabelColor(color);
    expect(label).toBeDefined();
    const l = luminance(color), text = luminance(label!);
    expect((Math.max(l, background) + 0.05) / (Math.min(l, background) + 0.05)).toBeGreaterThanOrEqual(3);
    expect((Math.max(l, text) + 0.05) / (Math.min(l, text) + 0.05)).toBeGreaterThanOrEqual(4.5);
  }
  for (const [a, b] of [[2, 7], [2, 10], [7, 10]]) {
    const distance = Math.hypot(...[1, 3, 5].map(i => parseInt(colors[a].slice(i, i + 2), 16) - parseInt(colors[b].slice(i, i + 2), 16)));
    expect(distance).toBeGreaterThan(70);
  }
});

function lab(hex: string) {
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(n => n <= 0.04045 ? n / 12.92 : ((n + 0.055) / 1.055) ** 2.4);
  const f = (v: number) => v > 216 / 24389 ? Math.cbrt(v) : v * 841 / 108 + 4 / 29;
  const x = f((r * 0.4124564 + g * 0.3575761 + b * 0.1804375) / 0.95047);
  const y = f(r * 0.2126729 + g * 0.7151522 + b * 0.0721750);
  const z = f((r * 0.0193339 + g * 0.1191920 + b * 0.9503041) / 1.08883);
  return [116 * y - 16, 500 * (x - y), 200 * (y - z)];
}
it.each(['.light', '.dark'])('separates every palette pair including Other in %s', selector => {
  const block = css.slice(css.indexOf(`${selector} {`)).split('}')[0];
  const colors = [...block.matchAll(/--panel-chart-(?:\d+|other): (#[0-9a-f]+);/g)].map(m => m[1]);
  expect(colors).toHaveLength(13);
  for (let i = 0; i < colors.length; i++) for (let j = i + 1; j < colors.length; j++) {
    const a = lab(colors[i]), b = lab(colors[j]);
    expect(Math.hypot(...a.map((n, k) => n - b[k])), `${colors[i]} vs ${colors[j]}`).toBeGreaterThanOrEqual(18.8);
  }
});
it.each(['.light', '.dark'])('keeps generated colours past twelve series readable in %s', selector => {
  const block = css.slice(css.indexOf(`${selector} {`)).split('}')[0];
  const background = selector === '.light' ? 1 : 0.003;
  const used = new Set<string>();
  const colors = Array.from({ length: 50 }, (_, i) => chartColors(String(i), `series ${i}`, false, used).color).slice(12);
  for (const color of colors) {
    const resolved = color.replace('var(--panel-chart-generated-lightness)', block.match(/--panel-chart-generated-lightness: ([\d.]+%);/)?.[1] ?? '');
    const match = resolved.match(/^hsl\(([\d.]+) ([\d.]+)% ([\d.]+)%\)$/);
    expect(match).not.toBeNull();
    if (!match) throw new Error(`Unresolved color ${resolved}`);
    const hue = Number(match[1]) / 60, saturation = Number(match[2]) / 100, lightness = Number(match[3]) / 100;
    const c = (1 - Math.abs(2 * lightness - 1)) * saturation, x = c * (1 - Math.abs(hue % 2 - 1)), m = lightness - c / 2;
    const channels = hue < 1 ? [c, x, 0] : hue < 2 ? [x, c, 0] : hue < 3 ? [0, c, x] : hue < 4 ? [0, x, c] : hue < 5 ? [x, 0, c] : [c, 0, x];
    const hex = '#' + channels.map(v => Math.round((v + m) * 255).toString(16).padStart(2, '0')).join('');
    const l = luminance(hex);
    expect((Math.max(l, background) + 0.05) / (Math.min(l, background) + 0.05), resolved).toBeGreaterThanOrEqual(3);
  }
});

it('leaves label colour to CSS when the fill is not a hex colour', () => {
  expect(chartLabelColor(undefined)).toBeUndefined();
  expect(chartLabelColor('rgb(0, 0, 0)')).toBeUndefined();
  expect(chartLabelColor('#fafafa')).toBe('#000000');
  expect(chartLabelColor('#101010')).toBe('#ffffff');
});
