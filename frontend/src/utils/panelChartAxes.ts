import type { PanelFrame } from './panelFrame';
import type { ChartViz } from './panelCharts';
import { parseGranularityToMilliseconds } from './histogram-chart';

export function chartTicks(low: number, high: number, count: boolean): number[] {
  const span = high - low;
  if (!Number.isFinite(span)) throw new Error("The chart's numeric range is too wide. Use a table to see exact values.");
  const rough = (span || 1) / 4;
  const power = 10 ** Math.floor(Math.log10(rough));
  const factor = [1, 2, 2.5, 5, 10].find(n => n * power >= rough && (!count || Number.isInteger(n * power))) ?? 10;
  const step = count ? Math.max(1, factor * power) : factor * power;
  const first = Math.floor(low / step), last = Math.ceil((high || step) / step);
  return Array.from({ length: last - first + 1 }, (_, i) => (first + i) * step);
}

export function chartTimeFormatter(frame: PanelFrame, viz: ChartViz, count: boolean, zone: string): Intl.DateTimeFormat {
  const name = count ? 'time' : viz.x;
  const kind = viz.columns?.[name ?? '']?.kind ?? frame.columns.find(c => c.name === name)?.kind;
  const times = [...new Set(frame.rows.map(r => String(r[name ?? ''])).filter(v => v !== 'null'))].sort();
  const cadence = parseGranularityToMilliseconds(frame.interval);
  const midnightUTC = times.length > 0 && times.every(t => /^\d{4}-\d{2}-\d{2}(?:T00:00:00(?:\.0+)?Z)?$/.test(t));
  const calendarDays = times.length > 1 && times.slice(1).every((t, i) => Date.parse(t) - Date.parse(times[i]) >= 23 * 3600000);
  const dateOnly = kind === 'date' || (cadence !== null && cadence >= 86400000) || calendarDays || (cadence === null && midnightUTC);
  const timeZone = kind === 'date' || (dateOnly && midnightUTC) ? 'UTC' : zone;
  const start = Date.parse(times[0] ?? '1970-01-01'), end = Date.parse(times.at(-1) ?? '1970-01-01');
  const day = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric', month: 'numeric', day: 'numeric' });
  const year = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric' });
  return new Intl.DateTimeFormat('en-US', {
    timeZone,
    ...(dateOnly || day.format(start) !== day.format(end) ? { month: 'short', day: 'numeric' } : {}),
    ...(year.format(start) !== year.format(end) ? { year: 'numeric' } : {}),
    ...(!dateOnly ? { hour: '2-digit', minute: '2-digit', ...(cadence !== null && cadence < 60000 ? { second: '2-digit' } : {}), hourCycle: 'h23' } : {}),
  });
}
