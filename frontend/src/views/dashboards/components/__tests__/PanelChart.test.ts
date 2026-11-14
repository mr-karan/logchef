import { ResizeObserver as PolyfillResizeObserver } from '@juggle/resize-observer';
import { createApp, defineComponent, h, markRaw, nextTick, shallowRef } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { afterEach, expect, it, vi } from 'vitest';
import { useThemeStore } from '@/stores/theme';
import { i18n } from '@/i18n';
import { countFrame, resultFrame, type PanelFrame } from '@/utils/panelFrame';
import { suggestViz, type ChartViz } from '@/utils/panelCharts';
import PanelChart from '../PanelChart.vue';
import { readFileSync } from 'node:fs';
import { Plotline, Crosshair, Scatter, StackedBar, GroupedBar, Line, Area } from '@unovis/ts';
import { show as tooltipShown } from '@unovis/ts/components/tooltip/style';
markRaw(PolyfillResizeObserver.prototype);
const cleanups: (() => void)[] = [];
const height = shallowRef(260);
afterEach(() => { cleanups.splice(0).forEach(f => f()); height.value = 260; vi.unstubAllGlobals(); vi.restoreAllMocks(); });
async function render(frame: PanelFrame, viz: ChartViz, count = false, barBox = new DOMRect(0, 0, 80, 30), width = 640, current = shallowRef(frame)) {
  const css = readFileSync('src/assets/index.css', 'utf8');
  for (const match of css.slice(css.indexOf('.light {')).split('}')[0].matchAll(/(--panel-chart-[\w-]+): (#[0-9a-f]+);/g)) document.documentElement.style.setProperty(match[1], match[2]);
  localStorage.setItem('vueuse-color-scheme', 'light');
  localStorage.setItem('logchef_user_preferences', JSON.stringify({ timezone: 'utc' }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal('IntersectionObserver', class { observe() {} unobserve() {} disconnect() {} });
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(width);
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, width, 260));
  Object.defineProperty(SVGElement.prototype, 'getComputedTextLength', { configurable: true, value() { return (this.textContent?.length ?? 0) * 6; } });
  Object.defineProperty(SVGElement.prototype, 'getBBox', { configurable: true, value() { return this.tagName === 'text' ? new DOMRect(0, 0, (this.textContent?.length ?? 0) * 6, 12) : barBox; } });
  const host = document.createElement('div');
  document.body.append(host);
  const pinia = createPinia();
  setActivePinia(pinia);
  const app = createApp(defineComponent({ setup: () => () => h(PanelChart, { frame: current.value, viz, count, height: height.value }) })).use(pinia).use(i18n);
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
  const points = host.querySelectorAll(`.${Scatter.selectors.point}`);
  if (mark === 'bar') expect(points).toHaveLength(0);
  else { expect(points.length).toBeGreaterThan(0); expect(points.length).toBeLessThanOrEqual(14); }
  expect(host.querySelectorAll('.panel-chart__value')).toHaveLength(0);
  expect(host.textContent).toContain('Some value labels are hidden');
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
  expect(host.textContent).not.toContain('Some value labels are hidden');
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

it('announces keyboard buckets and formatted series outside the chart image only', async () => {
  const frame = resultFrame({ columns: source.columns.map(c => ({ name: c.name, type: c.backendType ?? '' })), rows: source.rawRows.map((row, i) => ({ ...row, x: i === 0 ? 'First bucket' : 'Second bucket' })) });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['a', 'b'], format: { unit: 'percent' } });
  const live = host.querySelector('[aria-live="polite"]')!;
  expect(live).not.toBeNull();
  expect(live.closest('[role="img"]')).toBeNull();
  expect(live.classList.contains('sr-only')).toBe(true);
  expect(live.textContent?.trim()).toBe('');
  const plot = host.querySelector('.panel-chart__plot')!;
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await nextTick();
  expect(live.textContent).toContain('First bucket');
  expect(live.textContent).toContain('a -30%');
  expect(live.textContent).toContain('b 10%');
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }));
  await nextTick();
  expect(live.textContent).toContain('Second bucket');
  expect(live.textContent).not.toContain('First bucket');
  expect(live.textContent).toContain('a NULL');
  expect(live.textContent).toContain('b 20%');
  plot.dispatchEvent(new PointerEvent('pointermove', { bubbles: true }));
  host.querySelector('svg')!.dispatchEvent(new MouseEvent('mousemove', { clientX: 370, clientY: 100, bubbles: true }));
  await vi.waitFor(() => expect(host.querySelector('[role=tooltip]')?.textContent).toContain('20%'));
  expect(live.textContent?.trim()).toBe('');
});

it.each(['pointerdown', 'pointermove', 'pointerenter'])('returns keyboard inspection to the mouse on %s', async event => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'] });
  const plot = host.querySelector('.panel-chart__plot')!;
  plot.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await vi.waitFor(() => expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total-20'));
  plot.dispatchEvent(new PointerEvent(event, { bubbles: true }));
  await nextTick();
  host.querySelector('svg')!.dispatchEvent(new MouseEvent('mousemove', { clientX: 370, clientY: 100, bubbles: true }));
  await vi.waitFor(() => expect(host.querySelector('[role=tooltip]')?.textContent).toContain('Total20'));
});

it.each([['area', undefined, 150], ['area', 'none', 50], ['line', undefined, 50]] as const)('places %s %s labels on the rendered series top', async (mark, stack, top) => {
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'a', type: 'Int32' }, { name: 'b', type: 'Int32' }], rows: [{ x: 'bucket', a: 100, b: 50 }] });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['a', 'b'], mark, stack, show_values: true });
  const groups = [...host.querySelectorAll(`.${Scatter.selectors.point}`)];
  expect(groups.map(g => g.querySelector('text')?.textContent)).toEqual(['100', '50']);
  const y = (g: Element) => Number(g.getAttribute('transform')?.match(/translate\([^,]+,\s*([^)]+)\)/)?.[1]);
  const a = y(groups[0]), b = y(groups[1]);
  expect(Number.isFinite(b)).toBe(true);
  if (top === 150) expect(b).toBeLessThan(a);
  else expect(b).toBeGreaterThan(a);
});

it.each(['line', 'area'] as const)('thins dense %s labels across rows and series', async mark => {
  const ys = ['a', 'b', 'c', 'd', 'e'];
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, ...ys.map(name => ({ name, type: 'Int32' }))], rows: Array.from({ length: 10 }, (_, i) => ({ x: String(i), a: 10, b: 20, c: 30, d: 40, e: 50 })) });
  const host = await render(frame, { type: 'chart', x: 'x', y: ys, mark, show_values: true });
  const labels = host.querySelectorAll(`.${Scatter.selectors.point} > text`);
  expect(labels.length).toBeGreaterThan(0);
  expect(labels.length).toBeLessThanOrEqual(14);
  expect(new Set([...labels].map(e => e.textContent)).size).toBe(5);
});

it('waits for plot width before announcing hidden labels', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['b'], show_values: true }, false, new DOMRect(), 0);
  expect(host.textContent).not.toContain('Some value labels are hidden');
});

it('hides the hover readout of the previous frame when the data refreshes', async () => {
  const frameWith = (q: number) => resultFrame({ columns: [{ name: 'x', type: 'String' }, { name: 'a', type: 'Int32' }], rows: [{ x: 'p', a: 1 }, { x: 'q', a: q }] });
  const current = shallowRef(frameWith(20));
  const host = await render(current.value, { type: 'chart', x: 'x', y: ['a'] }, false, undefined, 640, current);
  host.querySelector('svg')!.dispatchEvent(new MouseEvent('mousemove', { clientX: 470, clientY: 100, bubbles: true }));
  await vi.waitFor(() => expect(host.querySelector('[role=tooltip]')?.textContent).toContain('qa20'));
  const wrapper = host.querySelector('[role=tooltip]')!.parentElement!;
  expect(wrapper.classList.contains(tooltipShown)).toBe(true);
  current.value = frameWith(99);
  await vi.waitFor(() => expect(wrapper.classList.contains(tooltipShown)).toBe(false));
});

it('falls back to the computed bar fill when the series colour cannot be resolved', async () => {
  const ys = Array.from({ length: 13 }, (_, i) => `s${i}`);
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, ...ys.map(name => ({ name, type: 'Int32' }))], rows: [Object.fromEntries([['x', 'g'], ...ys.map(name => [name, 5])])] });
  const computed = window.getComputedStyle;
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element, pseudo) => {
    const style = computed(element, pseudo);
    if (element.tagName !== 'path') return style;
    return new Proxy(style, {
      get: (target, key) => key === 'fill' ? 'rgb(250, 250, 250)' : typeof Reflect.get(target, key) === 'function' ? Reflect.get(target, key).bind(target) : Reflect.get(target, key),
      set: (target, key, value) => Reflect.set(target, key, value),
    });
  });
  const host = await render(frame, { type: 'chart', x: 'x', y: ys, mark: 'bar', stack: 'none', show_values: true }, false, new DOMRect(0, 0, 40, 30), 2000);
  await vi.waitFor(() => expect(host.querySelectorAll('.panel-chart__value')).toHaveLength(13));
  // The 13th series gets a generated hsl() colour with a nested CSS variable, which Unovis cannot parse.
  expect([...host.querySelectorAll<SVGTextElement>('.panel-chart__value')].at(-1)!.style.fill).toBe('rgb(0, 0, 0)');
});

it('stacks negative area values downward from the baseline', async () => {
  const frame = resultFrame({ columns: [{ name: 'x', type: 'String' }, ...['a', 'b', 'c'].map(name => ({ name, type: 'Int32' }))], rows: [{ x: 'bucket', a: 100, b: -50, c: 30 }] });
  const host = await render(frame, { type: 'chart', x: 'x', y: ['a', 'b', 'c'], mark: 'area', show_values: true });
  await vi.waitFor(() => expect(host.querySelectorAll(`.${Scatter.selectors.point}`)).toHaveLength(3));
  const groups = [...host.querySelectorAll(`.${Scatter.selectors.point}`)];
  expect(groups.map(g => g.querySelector('text')?.textContent)).toEqual(['100', '-50', '30']);
  const [a, b, c] = groups.map(g => Number(g.getAttribute('transform')?.match(/translate\([^,]+,\s*([^)]+)\)/)?.[1]));
  const tickY = (label: string) => Number([...host.querySelectorAll('.tick')].find(t => t.textContent === label)?.getAttribute('transform')?.match(/translate\([^,]+,\s*([^)]+)\)/)?.[1]);
  // The axis can render after the marks under load; wait for the ticks this test reads.
  await vi.waitFor(() => expect(['0', '50', '-50'].every(label => Number.isFinite(tickY(label)))).toBe(true));
  const zero = tickY('0'), fifty = tickY('50'), minusFifty = tickY('-50');
  // SVG y grows downward: c stacks on a (130), b sits at -50, below the zero baseline.
  expect(c).toBeLessThan(a);
  expect(b).toBeGreaterThan(zero);
  expect(Math.abs(b - minusFifty)).toBeLessThan(Math.abs(b - fifty));
});

it.each([['line', undefined], ['area', undefined], ['area', 'none']] as const)('draws %s %s paths from the full rows', async (mark, stack) => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['b'], mark, stack });
  const selector = mark === 'line' ? Line.selectors.linePath : Area.selectors.area;
  const paths = [...host.querySelectorAll(`.${selector}`)];
  expect(paths.length).toBeGreaterThan(0);
  // Without row data Unovis draws a flat placeholder; a real path reaches the top of the y domain for b=20.
  const ys = paths.flatMap(p => [...(p.getAttribute('d') ?? '').matchAll(/[ML][\d.-]+,([\d.-]+)/g)].map(m => Number(m[1])));
  expect(Math.min(...ys)).toBeLessThan(50);
});

it('keeps the live region stable across re-renders of the same bucket', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a', 'b'] });
  host.querySelector('.panel-chart__plot')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home' }));
  await nextTick();
  const live = host.querySelector('[aria-live="polite"]')!;
  const changes: MutationRecord[] = [];
  const observer = new MutationObserver(records => changes.push(...records));
  observer.observe(live, { childList: true, subtree: true, characterData: true });
  height.value = 300;
  await nextTick();
  height.value = 320;
  await nextTick();
  await new Promise(resolve => setTimeout(resolve, 40));
  observer.disconnect();
  expect(live.textContent).toContain('a -30');
  expect(changes).toHaveLength(0);
});

it('draws threshold lines and stretches the axis to include them', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a'], thresholds: [{ value: null, color: 'green' }, { value: '100', color: 'red' }] });
  const line = host.querySelector(`.${Plotline.selectors.plotline}`);
  expect(line).toBeTruthy();
  expect(line?.getAttribute('style')).toContain('var(--panel-red)');
  const y = Number(line?.getAttribute('y1'));
  expect(y).toBeGreaterThanOrEqual(0);
  expect(y).toBeLessThan(260);
});

it('keeps the chart when an exact threshold cannot be plotted', async () => {
  const host = await render(source, { type: 'chart', x: 'x', y: ['a'], thresholds: [{ value: null, color: 'green' }, { value: '1e400', color: 'red' }] });
  expect(host.querySelector('[role=alert]')).toBeNull();
  expect(host.querySelector('svg')).toBeTruthy();
  expect(host.textContent).toContain('Threshold 1e400 is outside the chart coordinate range and was not drawn.');
});
