import { expect, it, vi } from 'vitest';
import { resultFrame, countFrame, frameCellText } from '../panelFrame';
import { buildPanelChart, buildPanelList, chartValue, chartStacked, suggestViz, withSuggestedRoles } from '../panelCharts';
const columns = [{ name: 'x', type: 'String' }, { name: 'n', type: 'Decimal(20,4)' }];
it('renders suggested VictoriaLogs roles', () => {
  const frame = resultFrame({ columns: [], untyped: true, rows: [{ _time: '2026-11-10T00:00:00Z', service: 'api', errors: '12' }, { _time: '2026-11-10T01:00:00Z', service: 'web', errors: '15' }] });
  for (const viz of [suggestViz(frame), withSuggestedRoles(frame, { type: 'chart' })]) {
    expect(viz).toMatchObject({ type: 'chart', x: '_time', y: ['errors'], series: 'service', columns: { errors: { kind: 'number', confidence: 'confirmed' } } });
    if (viz.type !== 'chart') throw new Error('Expected chart');
    expect(buildPanelChart(frame, viz, false).series).toHaveLength(2);
  }
});
it('returns empty models', () => {
  const empty = resultFrame({ columns: [], rows: [], untyped: true });
  expect(buildPanelChart(empty, { type: 'chart', x: '_time', y: ['n'] }, false).rows).toEqual([]);
  expect(buildPanelList(empty, { type: 'pie', label: 'x', value: 'n' }, false).items).toEqual([]);
  expect(buildPanelChart(countFrame([], '1h', true), { type: 'chart' }, true).series).toEqual([]);
});
it('preserves sub-millisecond keys and skips NULL duplicate keys', () => {
  const frame = resultFrame({ columns: [{ name: 't', type: 'Nullable(DateTime64(9))' }, columns[1]], rows: [{ t: '2026-11-10T00:00:00.000001Z', n: '1' }, { t: '2026-11-10T00:00:00.000002Z', n: '2' }, { t: null, n: '3' }, { t: null, n: '4' }] });
  const model = buildPanelChart(frame, { type: 'chart', x: 't', y: ['n'] }, false);
  expect(model.rows).toHaveLength(2);
  expect(model.notices.join(' ')).toContain('2 row(s) with NULL');
});
it('folds overflow inside the cap and preserves exact thresholds', () => {
  const exact = buildPanelList(resultFrame({ columns, rows: [{ x: 'small', n: '0.1' }, { x: 'large', n: '0.9' }] }), { type: 'pie', label: 'x', value: 'n', min_slice_percent: 10 }, false);
  expect(exact.items.map(i => i.label)).toEqual(['small', 'large']);
  const capped = buildPanelList(resultFrame({ columns, rows: Array.from({ length: 60 }, (_, i) => ({ x: String(i), n: i === 59 ? '0.001' : '1' })) }), { type: 'pie', label: 'x', value: 'n', min_slice_percent: 0.01 }, false);
  expect(capped.items).toHaveLength(50);
  expect(capped.items.at(-1)?.label).toBe('Other');
  expect(capped.items.reduce((s, i) => s + i.percentage, 0)).toBeCloseTo(100);
  expect(capped.items.reduce((s, i) => s + i.displayPercentage, 0)).toBeCloseTo(100);
});
it('retains small label values', () => {
  const model = buildPanelChart(resultFrame({ columns, rows: [{ x: 'a', n: '0.0042' }] }), { type: 'chart', x: 'x', y: ['n'] }, false);
  expect([...model.rows[0].values.values()][0]?.text).toBe('0.0042');
});
it('builds 10000 x 5 decimals without eager formatting', () => {
  const ys = ['a', 'b', 'c', 'd', 'e'];
  const frame = resultFrame({ columns: [columns[0], ...ys.map(name => ({ name, type: 'Decimal(20,4)' }))], rows: Array.from({ length: 10000 }, (_, i) => ({ x: String(i), a: '1.2345', b: '2.3456', c: '3.4567', d: '4.5678', e: '5.6789' })) });
  const formatter = vi.spyOn(Intl, 'NumberFormat');
  const start = performance.now();
  const model = buildPanelChart(frame, { type: 'chart', x: 'x', y: ys }, false);
  console.info(`10000x5 Decimal model ${(performance.now() - start).toFixed(1)} ms; ${model.rows.length} rows; ${model.series.length} series; ${formatter.mock.calls.length} formatter constructions`);
  expect(model.rows).toHaveLength(10000);
  expect(model.rows[9999].values.size).toBe(5);
  expect(formatter.mock.calls.length).toBeLessThan(10);
  formatter.mockRestore();
}, 60000);

it('keeps group colours stable across count charts and result lists', () => {
  const chart = buildPanelChart(countFrame([{ bucket: '2026-11-10T00:00:00Z', log_count: 2, group_value: 'api' }], '1h', true), { type: 'chart' }, true);
  const list = buildPanelList(resultFrame({ columns, rows: [{ x: 'api', n: '2' }] }), { type: 'toplist', label: 'x', value: 'n' }, false);
  expect(chart.series[0].color).toBe(list.items[0].color);
});

it('shares absent stack defaults for bars, areas and lines', () => {
  expect(chartStacked({ type: 'chart', mark: 'bar' })).toBe(true);
  expect(chartStacked({ type: 'chart', mark: 'area' })).toBe(true);
  expect(chartStacked({ type: 'chart', mark: 'line' })).toBe(false);
  expect(chartStacked({ type: 'chart', mark: 'bar', stack: 'none' })).toBe(false);
});

it('preserves very small nonzero values', () => {
  expect(chartValue(1e-12).text).not.toBe('0.0000000000');
  expect(Number(chartValue(1e-12).text)).toBe(1e-12);
});

it('reuses timestamp formatters across cells in each zone', () => {
  const formatter = vi.spyOn(Intl, 'DateTimeFormat');
  try {
    const frame = resultFrame({ columns: [{ name: 't', type: 'DateTime64(6)' }], rows: [{ t: '2026-11-10T00:00:00.123456Z' }] });
    for (const zone of ['Asia/Kolkata', 'America/New_York']) {
      for (let i = 0; i < 1000; i++) expect(frameCellText(frame.rows[0].t, frame.columns[0], zone)).toContain('.123456');
    }
    expect(formatter.mock.calls.length).toBeLessThanOrEqual(2);
  } finally { formatter.mockRestore(); }
});
