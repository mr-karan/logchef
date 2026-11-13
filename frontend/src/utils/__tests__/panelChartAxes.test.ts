import { expect, it } from 'vitest';
import { resultFrame } from '../panelFrame';
import { chartTicks, chartTimeFormatter } from '../panelChartAxes';
it('uses nice ticks and integer ticks for counts', () => {
  expect(chartTicks(0, 9, true)).toEqual([0, 5, 10]);
  expect(chartTicks(0, 9, false)).toEqual([0, 2.5, 5, 7.5, 10]);
  expect(chartTicks(0, 0, true)).toEqual([0, 1]);
});
it.each([1, 2])('keeps UTC midnight date ticks in negative zones for %s buckets', length => {
  const frame = resultFrame({ columns: [{ name: 't', type: 'DateTime' }], rows: [{ t: '2026-11-09T00:00:00Z' }, { t: '2026-11-10T00:00:00Z' }].slice(0, length) });
  expect(chartTimeFormatter(frame, { type: 'chart', x: 't' }, false, 'America/New_York').format(Date.parse('2026-11-09T00:00:00Z'))).toBe('Nov 9');
});
it('keeps daily ticks across a 23-hour DST day', () => {
  const frame = resultFrame({ columns: [{ name: 't', type: 'DateTime' }], rows: [{ t: '2026-03-07T05:00:00Z' }, { t: '2026-03-08T05:00:00Z' }, { t: '2026-03-09T04:00:00Z' }] });
  expect(chartTimeFormatter(frame, { type: 'chart', x: 't' }, false, 'America/New_York').format(Date.parse('2026-03-09T04:00:00Z'))).toBe('Mar 9');
});
