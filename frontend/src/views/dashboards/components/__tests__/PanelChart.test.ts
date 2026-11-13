import { ResizeObserver as PolyfillResizeObserver } from '@juggle/resize-observer';
import { createApp, markRaw, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
import { useThemeStore } from '@/stores/theme';
import { i18n } from '@/i18n';
import { countFrame, resultFrame, type PanelFrame } from '@/utils/panelFrame';
import { suggestViz, type ChartViz } from '@/utils/panelCharts';
import PanelChart from '../PanelChart.vue';
import { readFileSync } from 'node:fs';
import { Crosshair, Scatter, StackedBar, GroupedBar } from '@unovis/ts';
markRaw(PolyfillResizeObserver.prototype);
const cleanups: (() => void)[] = [];
afterEach(() => { cleanups.splice(0).forEach(f => f()); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
async function render(frame: PanelFrame, viz: ChartViz, count = false, barBox = new DOMRect(0, 0, 80, 30)) {
  const css = readFileSync('src/assets/index.css', 'utf8');
  for (const match of css.slice(css.indexOf('.light {')).split('}')[0].matchAll(/(--panel-chart-[\w-]+): (#[0-9a-f]+);/g)) document.documentElement.style.setProperty(match[1], match[2]);
  localStorage.setItem('vueuse-color-scheme', 'light');
  localStorage.setItem('logchef_user_preferences', JSON.stringify({ timezone: 'utc' }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal('IntersectionObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(640);
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 640, 260));
  Object.defineProperty(SVGElement.prototype, 'getComputedTextLength', { configurable: true, value() { return (this.textContent?.length ?? 0) * 6; } });
  Object.defineProperty(SVGElement.prototype, 'getBBox', { configurable: true, value() { return this.tagName === 'text' ? new DOMRect(0, 0, (this.textContent?.length ?? 0) * 6, 12) : barBox; } });
  const host = document.createElement('div');
  document.body.append(host);
  const pinia = createPinia();
  setActivePinia(pinia);
  const app = createApp(PanelChart, { frame, viz, count, height: 260 }).use(pinia).use(i18n);
  app.mount(host);
  cleanups.push(() => { app.unmount(); host.remove(); });
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 40));
  return host;
}
const source = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'a', type: 'Nullable(Int32)' }, { name: 'b', type: 'Int32' }], rows: [{ x: 'a', a: -30, b: 10 }, { x: 'b', a: null, b: 20 }] });
it.each(['line', 'area'] as const)('renders %s with a single keyboard target and no point markers', async mark => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark });
  expect(host.querySelectorAll('[tabindex="0"]')).toHaveLength(1);
  expect(host.querySelectorAll(`.${Scatter.selectors.point}`)).toHaveLength(0);
  const plot = host.querySelector<HTMLElement>('.panel-chart__plot')!;
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 40));
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('-30');
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total-20');
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }));
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 40));
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('NULL');
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('20');
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
  host.querySelector('.panel-chart__plot')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 40));
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('9,007,199,254,740,993');
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain(malicious);
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
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'bar', orientation: 'horizontal', show_values: true });
  await vi.waitFor(() => expect(host.querySelector('path[class$="-bar"]')).toBeTruthy());
  const bars = [...host.querySelectorAll<SVGPathElement>('path[class$="-bar"]')];
  expect(bars.length).toBe(3);
  expect([...host.querySelectorAll('.panel-chart__value')].map(e => e.textContent)).toEqual(['-30', '10', '20']);
  for (const bar of bars) {
    expect(bar.getAttribute('d')).not.toMatch(/NaN|Infinity/);
  }
});


it.each(['stacked', 'none'] as const)('labels the bound series in %s bars with zero and NULL segments', async stack => {
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, ...['a', 'b', 'c'].map(name => ({ name, type: 'Nullable(Int32)' }))], rows: [{ x: 'Group 1', a: 0, b: 5, c: 7 }, { x: 'Group 2', a: null, b: -8, c: 9 }] });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['a', 'b', 'c'], mark: 'bar', stack, show_values: true });
  const selector = stack === 'stacked' ? StackedBar.selectors.barGroup : GroupedBar.selectors.barGroup;
  await vi.waitFor(() => expect([...host.querySelectorAll(`.${selector}`)].map(group => [...group.querySelectorAll('.panel-chart__value')].map(e => e.textContent))).toEqual([['5', '7'], ['-8', '9']]));
});

it('uses one tooltip and moves the actual crosshair with keyboard navigation', async () => {
  const warn = vi.spyOn(console, 'warn');
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'bar' });
  const plot = host.querySelector<HTMLElement>('.panel-chart__plot')!;
  plot.dispatchEvent(new FocusEvent('focus'));
  plot.click();
  await nextTick();
  expect(host.querySelectorAll('[role=tooltip]')).toHaveLength(0);
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await vi.waitFor(() => expect(host.querySelectorAll('[role=tooltip]')).toHaveLength(1));
  const line = host.querySelector(`.${Crosshair.selectors.line}`)!;
  const position = line.getAttribute('x1');
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total-20');
  expect(host.querySelectorAll(`.${Crosshair.selectors.circle}`)).toHaveLength(2);
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }));
  await vi.waitFor(() => expect(line.getAttribute('x1')).not.toBe(position));
  expect(host.querySelectorAll('[role=tooltip]')).toHaveLength(1);
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('NULL');
  expect(warn.mock.calls.flat().join(' ')).not.toContain('Y accessors have not been configured');
});

it('shows the snapped row when hovering in the right half of an interval', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'line' });
  const svg = host.querySelector('svg')!;
  svg.dispatchEvent(new MouseEvent('mousemove', { clientX: 370, clientY: 100, bubbles: true }));
  await vi.waitFor(() => expect(host.querySelector('[role=tooltip]')?.textContent).toContain('NULL'));
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total20');
});

it.each(['line', 'area', 'bar'] as const)('bounds show_values DOM for dense %s charts', async mark => {
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'n', type: 'Int32' }], rows: Array.from({ length: mark === 'bar' ? 100 : 1000 }, (_, i) => ({ x: String(i), n: i + 1 })) });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['n'], mark, show_values: true });
  expect(host.querySelectorAll(`.${Scatter.selectors.point}`)).toHaveLength(0);
  expect(host.querySelectorAll('.panel-chart__value')).toHaveLength(0);
  expect(host.textContent).toContain('Value labels hidden');
});

it('constructs a constant number of time formatters when mounting 1000 rows', async () => {
  const frame = resultFrame({ columns: [{ name: 't', type: 'DateTime64' }, { name: 'n', type: 'Int32' }], rows: Array.from({ length: 1000 }, (_, i) => ({ t: new Date(1700000000000 + i * 60000).toISOString(), n: i })) });
  const formatter = vi.spyOn(Intl, 'DateTimeFormat');
  const host = await render(frame, { type: 'chart', x: 't', y: ['n'], mark: 'line' });
  expect(host.querySelector('svg')).not.toBeNull();
  expect(formatter.mock.calls.length).toBeLessThan(10);
});

it('hides labels that do not fit a narrow bar', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'bar', stack: 'none', show_values: true }, false, new DOMRect(0, 0, 10, 30));
  expect(host.querySelectorAll(`.${GroupedBar.selectors.bar}`)).toHaveLength(4);
  expect(host.querySelectorAll('.panel-chart__value')).toHaveLength(0);
});
it('renders sparse point labels with a halo and retains them during keyboard navigation', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['b'], mark: 'line', show_values: true });
  await vi.waitFor(() => expect(host.querySelectorAll(`.${Scatter.selectors.point} > text`)).toHaveLength(2));
  const labels = [...host.querySelectorAll(`.${Scatter.selectors.point} > text`)];
  expect(labels.map(e => e.textContent)).toEqual(['10', '20']);
  expect(labels.every(e => e.classList.contains('panel-chart__point-value'))).toBe(true);
  host.querySelector('.panel-chart__plot')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await nextTick();
  expect([...host.querySelectorAll(`.${Scatter.selectors.point} > text`)]).toEqual(labels);
});

it('refreshes label contrast after a theme change', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'bar', show_values: true });
  await vi.waitFor(() => expect(host.querySelectorAll('.panel-chart__value')).toHaveLength(3));
  const css = readFileSync('src/assets/index.css', 'utf8');
  for (const match of css.slice(css.indexOf('.dark {')).split('}')[0].matchAll(/(--panel-chart-[\w-]+): (#[0-9a-f]+);/g)) document.documentElement.style.setProperty(match[1], match[2]);
  useThemeStore().setTheme('dark');
  await nextTick();
  await nextTick();
  expect([...host.querySelectorAll<SVGTextElement>('.panel-chart__value')].map(e => e.style.fill)).toEqual(['rgb(0, 0, 0)', 'rgb(0, 0, 0)', 'rgb(0, 0, 0)']);
});

it('uses the shared tooltip for horizontal keyboard selection', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'], mark: 'bar', orientation: 'horizontal' });
  const plot = host.querySelector('.panel-chart__plot')!;
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await vi.waitFor(() => expect(host.querySelectorAll('[role=tooltip]')).toHaveLength(1));
  expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total-20');
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
  await vi.waitFor(() => expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total20'));
  expect(host.querySelectorAll('[role=tooltip]')).toHaveLength(1);
});
