import { ResizeObserver as PolyfillResizeObserver } from '@juggle/resize-observer';
import { createApp, markRaw, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
import { i18n } from '@/i18n';
import { countFrame, resultFrame, type PanelFrame } from '@/utils/panelFrame';
import { suggestViz, type ChartViz } from '@/utils/panelCharts';
import PanelChart from '../PanelChart.vue';
markRaw(PolyfillResizeObserver.prototype);
const cleanups: (() => void)[] = [];
afterEach(() => { cleanups.splice(0).forEach(f => f()); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
async function render(frame: PanelFrame, viz: ChartViz, count = false) {
  localStorage.setItem('logchef_user_preferences', JSON.stringify({ timezone: 'utc' }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal('IntersectionObserver', class { observe() {} unobserve() {} disconnect() {} });
  const host = document.createElement('div');
  document.body.append(host);
  const app = createApp(PanelChart, { frame, viz, count, height: 260 }).use(createPinia()).use(i18n);
  app.mount(host);
  cleanups.push(() => { app.unmount(); host.remove(); });
  await nextTick();
  return host;
}
const source = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'a', type: 'Nullable(Int32)' }, { name: 'b', type: 'Int32' }], rows: [{ x: 'a', a: -30, b: 10 }, { x: 'b', a: null, b: 20 }] });
it.each(['line', 'area'] as const)('renders %s with a single keyboard target and no point markers', async mark => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark });
  expect(host.querySelectorAll('[tabindex="0"]')).toHaveLength(1);
  expect(host.querySelectorAll('.vis-scatter-point')).toHaveLength(0);
  const plot = host.querySelector<HTMLElement>('.panel-chart__plot')!;
  plot.dispatchEvent(new FocusEvent('focus'));
  await nextTick();
  expect(host.querySelector('.panel-chart__keyboard')?.textContent).toContain('-30');
  expect(host.querySelector('.panel-chart__keyboard')?.textContent).toContain('Total-20');
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }));
  await nextTick();
  expect(host.querySelector('.panel-chart__keyboard')?.textContent).toContain('NULL');
  expect(host.querySelector('.panel-chart__keyboard')?.textContent).toContain('20');
});
it('renders suggested untyped roles', async () => {
  const frame = resultFrame({ columns: [], untyped: true, rows: [{ _time: '2026-11-10T00:00:00Z', service: 'web', errors: '12' }, { _time: '2026-11-10T01:00:00Z', service: 'api', errors: '15' }] });
  const viz = suggestViz(frame);
  if (viz.type !== 'chart') throw new Error('Expected chart');
  const host = await render(frame, viz);
  expect(host.querySelector('[role=alert]')).toBeNull();
  expect(host.querySelectorAll('.panel-chart__legend span')).toHaveLength(2);
});
it.each([false, true])('shows empty state for empty frame, count=%s', async count => {
  const frame = count ? countFrame([], '1h', true) : resultFrame({ columns: [], rows: [], untyped: true });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['a'] }, count);
  expect(host.textContent).toContain('No data for this time range');
  expect(host.querySelector('svg')).toBeNull();
});
it('keeps tooltip content inert and exact label text free of approximation', async () => {
  const malicious = '<img src=x onerror="alert(1)">';
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'n', type: 'UInt64' }], rows: [{ x: malicious, n: '9007199254740993' }] });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['n'], mark: 'bar' });
  host.querySelector('.panel-chart__plot')?.dispatchEvent(new FocusEvent('focus'));
  await nextTick();
  expect(host.querySelector('.panel-chart__keyboard')?.textContent).toContain('9,007,199,254,740,993');
  expect(host.querySelector('.panel-chart__keyboard')?.textContent).toContain(malicious);
  expect(host.querySelector('img')).toBeNull();
});
it('rejects duplicate keys', async () => {
  const frame = resultFrame({ columns: source.columns.map(c => ({ name: c.name, type: c.backendType ?? '' })), rows: [...source.rawRows, source.rawRows[0]] });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['a'] });
  expect(host.querySelector('[role=alert]')?.textContent).toContain('1 duplicate key');
});

it('renders finite horizontal stacked bars with negatives and NULLs', async () => {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(640);
  Object.defineProperty(SVGElement.prototype, "getComputedTextLength", { configurable: true, value() { return (this.textContent?.length ?? 0) * 7; } });
  Object.defineProperty(SVGElement.prototype, "getBBox", { configurable: true, value: () => new DOMRect(0, 0, 40, 12) });
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'bar', orientation: 'horizontal' });
  await vi.waitFor(() => expect(host.querySelector('path[class$="-bar"]')).toBeTruthy());
  const bars = [...host.querySelectorAll<SVGPathElement>('path[class$="-bar"]')];
  expect(bars.length).toBeGreaterThan(0);
  for (const bar of bars) {
    expect(bar.getAttribute('d')).not.toMatch(/NaN|Infinity/);
  }
});
