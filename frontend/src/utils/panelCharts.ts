import type { DashboardViz, DashboardVizType, NumberFormat } from "@/api/dashboards";
import { addExact, compareExact, exactString, isExact, parseExact, type FrameNumber } from "./exactNumber";
import { frameCellText, frameWithKinds, suggestKind, type FrameColumn, type PanelFrame } from "./panelFrame";
import { parseGranularityToMilliseconds } from "./histogram-chart";
import { formatNumber } from "./numberFormat";

export type ChartViz = Extract<DashboardViz, { type: "chart" }>;
export type ListViz = Extract<DashboardViz, { type: "pie" | "toplist" }>;
export interface ChartValue { coordinate: number; text: string; exact: string; approximate: boolean }
export interface ChartSeries { key: string; label: string; color: string }
export interface ChartRow { key: string; x: number; label: string; values: Map<string, ChartValue | null> }
export interface ChartModel { rows: ChartRow[]; series: ChartSeries[]; time: boolean; notices: string[] }
export interface ListItem extends ChartSeries { value: ChartValue; percentage: number }

function suggestionColumns(frame: PanelFrame): FrameColumn[] {
  return frame.columns.map((c) => c.backendType === null && c.confidence !== "confirmed" ? { ...c, kind: suggestKind(frame.rawRows.slice(0, 100).map((r) => r[c.name])) } : c);
}

export function suggestRoles(frame: PanelFrame, type: DashboardVizType): DashboardViz {
  const columns = suggestionColumns(frame);
  const numbers = columns.filter((c) => c.kind === "number");
  const time = columns.find((c) => c.kind === "time" || c.kind === "date");
  const categories = columns.filter((c) => c.kind === "category");
  switch (type) {
    case "stat": return { type, value: numbers[0]?.name, order_by: time?.name };
    case "chart": return { type, x: (time ?? categories[0])?.name, y: numbers.map((c) => c.name), ...(time && categories.length === 1 && numbers.length === 1 ? { series: categories[0].name } : {}) };
    case "pie": case "toplist": return { type, label: columns.find((c) => c.kind !== "number")?.name, value: numbers[0]?.name };
    case "table": return { type };
  }
}

export function suggestViz(frame: PanelFrame): DashboardViz {
  const roles = suggestRoles(frame, "chart");
  const columns = suggestionColumns(frame);
  const numbers = columns.filter((c) => c.kind === "number");
  if (frame.rows.length === 1 && numbers.length === 1) return suggestRoles(frame, "stat");
  if (roles.type === "chart" && roles.x && roles.y?.length) {
    const kinds = columns.map((c) => c.kind);
    const time = kinds.some((k) => k === "time" || k === "date");
    if (time || kinds.filter((k) => k === "category").length === 1 && frame.rows.length <= 50) return { ...roles, mark: time ? "line" : "bar", stack: "none" };
  }
  return suggestRoles(frame, "table");
}

function column(frame: PanelFrame, name: string | undefined, numeric = false): FrameColumn {
  if (!name) throw new Error("Select the visualization's column roles.");
  const found = frame.columns.find((c) => c.name === name);
  if (!found) throw new Error(`Column "${name}" is missing from the result.`);
  if (numeric && found.kind !== "number") throw new Error(`Column "${name}" must be a number (backend type ${found.backendType ?? "untyped"}).`);
  return found;
}

function identity(value: unknown): string {
  if (isExact(value)) return `number:${exactString(value)}`;
  return JSON.stringify(value) ?? "null";
}

function label(value: unknown, col: FrameColumn, count: boolean, zone: string): string {
  if (count && typeof value === "object" && value !== null && "kind" in value) {
    if (value.kind === "other") return "Other";
    if (value.kind === "null") return "(null)";
    if (value.kind === "value" && "value" in value) return String(value.value) || "(empty)";
  }
  if (value === null || value === undefined) return "(null)";
  return frameCellText(value, col, zone) || "(empty)";
}

function number(value: unknown): FrameNumber | null {
  return isExact(value) || typeof value === "number" && Number.isFinite(value) ? value : null;
}

function sum(a: FrameNumber, b: FrameNumber): FrameNumber {
  if (isExact(a) && isExact(b)) return addExact(a, b);
  return Number(isExact(a) ? exactString(a) : a) + Number(isExact(b) ? exactString(b) : b);
}

export function chartValue(value: FrameNumber, format?: NumberFormat): ChartValue {
  const coordinate = isExact(value) ? Number(exactString(value)) : value;
  if (!Number.isFinite(coordinate)) throw new Error("A value is outside the chart coordinate range. Use a table to see its exact digits.");
  const converted = parseExact(coordinate);
  const approximate = isExact(value) && (!converted || compareExact(converted, value) !== 0);
  const formatted = formatNumber(value, format);
  return { coordinate, text: (approximate ? "≈ " : "") + formatted.text, exact: formatted.exact, approximate };
}

function duplicateError(keys: Map<string, number>, first: string | undefined, roles: string[]): void {
  const duplicates = [...keys.values()].filter((n) => n > 1).length;
  if (duplicates) throw new Error(`${duplicates} duplicate key(s). First key: ${first}. Group the query by ${roles.map((r) => `"${r}"`).join(" and ")}.`);
}

export function buildPanelChart(input: PanelFrame, viz: ChartViz, count: boolean, zone = "UTC"): ChartModel {
  const frame = frameWithKinds(input, viz.columns);
  const x = column(frame, count ? "time" : viz.x);
  const ys = (count ? ["count"] : viz.y ?? []).map((name) => column(frame, name, true));
  if (!ys.length) throw new Error("Select at least one y column.");
  const group = count ? frame.columns.find((c) => c.name === "series") : viz.series ? column(frame, viz.series) : undefined;
  const time = viz.x_scale === "time" || viz.x_scale !== "category" && (x.kind === "time" || x.kind === "date");
  if (time && x.kind !== "time" && x.kind !== "date") throw new Error(`Column "${x.name}" must be a time or date for a time axis.`);
  const notices = [...frame.notices];
  const keys = new Map<string, number>();
  let firstDuplicate: string | undefined;
  const series = new Map<string, ChartSeries>();
  const rows = new Map<string, { row: ChartRow; values: Map<string, FrameNumber | null> }>();
  let nullX = 0;
  for (const source of frame.rows) {
    const xv = source[x.name];
    const groupKey = group ? identity(source[group.name]) : "";
    const rowKey = identity(time && xv !== null ? Date.parse(String(xv)) : xv);
    const key = JSON.stringify([rowKey, groupKey]);
    keys.set(key, (keys.get(key) ?? 0) + 1);
    if (keys.get(key) === 2 && firstDuplicate === undefined) firstDuplicate = `${label(xv, x, count, zone)}${group ? `, ${label(source[group.name], group, count, zone)}` : ""}`;
    if (time && xv === null) { nullX++; continue; }
    const entry = rows.get(rowKey) ?? { row: { key: rowKey, x: time ? Date.parse(String(xv)) : rows.size, label: label(xv, x, count, zone), values: new Map() }, values: new Map<string, FrameNumber | null>() };
    rows.set(rowKey, entry);
    for (const y of ys) {
      const sk = JSON.stringify([y.name, groupKey]);
      if (!series.has(sk)) {
        const text = group ? label(source[group.name], group, count, zone) : y.name;
        series.set(sk, { key: sk, label: group && ys.length > 1 ? `${text} · ${y.name}` : text, color: `var(--chart-${series.size % 5 + 1})` });
      }
      const value = number(source[y.name]);
      const previous = entry.values.get(sk);
      entry.values.set(sk, count && previous !== undefined && previous !== null && value !== null ? sum(previous, value) : value);
    }
  }
  if (!count) duplicateError(keys, firstDuplicate, [x.name, ...(group ? [group.name] : [])]);
  if (nullX) notices.push(`${nullX} row(s) with NULL x values omitted from the time axis.`);
  const visibleSeries = [...series.values()].slice(0, 50);
  if (series.size > 50) notices.push(`50 of ${series.size} series shown.`);
  for (const entry of rows.values()) for (const s of visibleSeries) {
    const value = entry.values.get(s.key);
    entry.row.values.set(s.key, value === undefined || value === null ? count ? chartValue(0, viz.format) : null : chartValue(value, viz.format));
  }
  let result = [...rows.values()].map((e) => e.row);
  if (time) result.sort((a, b) => a.x - b.x);
  const step = count && time ? parseGranularityToMilliseconds(frame.interval) : null;
  if (step && result.length > 1 && (result.at(-1)!.x - result[0].x) / step <= 10000) {
    const filled: ChartRow[] = [];
    for (const row of result) {
      const previous = filled.at(-1);
      if (previous) for (let ts = previous.x + step; ts < row.x; ts += step) filled.push({ key: String(ts), x: ts, label: frameCellText(new Date(ts).toISOString(), x, zone), values: new Map(visibleSeries.map((s) => [s.key, chartValue(0, viz.format)])) });
      filled.push(row);
    }
    result = filled;
  }
  if (result.some((r) => [...r.values.values()].some((v) => v?.approximate))) notices.push("Chart coordinates are approximate. Tooltips show exact values.");
  return { rows: result, series: visibleSeries, time, notices };
}

export function buildPanelList(input: PanelFrame, viz: ListViz, count: boolean, zone = "UTC"): { items: ListItem[]; notices: string[]; total: ChartValue | null } {
  const frame = frameWithKinds(input, viz.columns);
  const dimension = column(frame, count ? "series" : viz.label);
  const metric = column(frame, count ? "count" : viz.value, true);
  const notices = [...frame.notices];
  const keys = new Map<string, number>();
  let firstDuplicate: string | undefined;
  const values = new Map<string, { key: string; label: string; value: FrameNumber | null }>();
  let nulls = 0;
  for (const row of frame.rows) {
    const key = identity(row[dimension.name]);
    const text = label(row[dimension.name], dimension, count, zone);
    keys.set(key, (keys.get(key) ?? 0) + 1);
    if (keys.get(key) === 2 && firstDuplicate === undefined) firstDuplicate = text;
    const value = number(row[metric.name]);
    if (value === null && viz.type === "pie") { nulls++; continue; }
    if (viz.type === "pie" && value !== null && (isExact(value) ? value.coefficient < 0n : value < 0)) throw new Error("Pie values must be nonnegative.");
    const previous = values.get(key);
    values.set(key, { key, label: text, value: count && previous?.value !== undefined && previous.value !== null && value !== null ? sum(previous.value, value) : value });
  }
  if (!count) duplicateError(keys, firstDuplicate, [dimension.name]);
  if (nulls) notices.push(`${nulls} NULL value(s) omitted.`);
  const colors = new Map([...values.keys()].map((key, i) => [key, `var(--chart-${i % 5 + 1})`]));
  let entries = [...values.values()];
  const totalValue = entries.reduce<FrameNumber | null>((a, e) => e.value === null ? a : a === null ? e.value : sum(a, e.value), null);
  if (count) entries.sort((a, b) => isExact(a.value) && isExact(b.value) ? compareExact(b.value, a.value) : Number(b.value) - Number(a.value));
  if (viz.type === "pie" && viz.min_slice_percent && entries.length) {
    const coordinates = entries.map((v) => v.value === null ? 0 : chartValue(v.value).coordinate);
    const max = Math.max(...coordinates);
    const total = coordinates.reduce((n, v) => n + (max ? v / max : 0), 0);
    let remainder: FrameNumber | null = null;
    entries = entries.filter((e, i) => {
      if (total && coordinates[i] / max / total * 100 < viz.min_slice_percent!) { remainder = e.value === null ? remainder : remainder === null ? e.value : sum(remainder, e.value); return false; }
      return true;
    });
    if (remainder !== null) entries.push({ key: "pie-remainder", label: "Other", value: remainder });
  }
  const maxRows = viz.type === "pie" ? 50 : viz.max_rows ?? 20;
  if (entries.length > maxRows) notices.push(`${maxRows} of ${entries.length} ${viz.type === "pie" ? "slices" : "rows"} shown.`);
  const items = entries.map((e) => ({ ...e, color: colors.get(e.key) ?? "var(--muted-foreground)", value: e.value === null ? { coordinate: 0, text: "NULL", exact: "NULL", approximate: false } : chartValue(e.value, viz.format), percentage: 0 }));
  const max = Math.max(0, ...items.map((i) => Math.abs(i.value.coordinate)));
  const total = items.reduce((s, i) => s + (max ? Math.abs(i.value.coordinate) / max : 0), 0);
  for (const item of items) item.percentage = total ? Math.abs(item.value.coordinate) / max / total * 100 : 0;
  if (items.some((i) => i.value.approximate)) notices.push("Chart coordinates are approximate. Tooltips show exact values.");
  return { items: items.slice(0, maxRows), notices, total: totalValue === null ? null : chartValue(totalValue, viz.format) };
}

export function withSuggestedRoles(frame: PanelFrame, viz: DashboardViz): DashboardViz {
  if (viz.type === "chart" && viz.x && viz.y?.length) return viz;
  if (viz.type === "stat" && viz.value) return viz;
  if ((viz.type === "pie" || viz.type === "toplist") && viz.label && viz.value) return viz;
  const suggested = suggestRoles(frameWithKinds(frame, viz.columns), viz.type);
  const columns = { ...suggested.columns, ...viz.columns };
  const kinds = Object.keys(columns).length ? { columns } : {};
  if (viz.type === "chart" && suggested.type === "chart") return { ...viz, x: viz.x ?? suggested.x, y: viz.y?.length ? viz.y : suggested.y, series: viz.series ?? suggested.series, ...kinds };
  if (viz.type === "stat" && suggested.type === "stat") return { ...viz, value: viz.value ?? suggested.value, order_by: viz.order_by ?? suggested.order_by, ...kinds };
  if ((viz.type === "pie" || viz.type === "toplist") && (suggested.type === "pie" || suggested.type === "toplist")) return { ...viz, label: viz.label ?? suggested.label, value: viz.value ?? suggested.value, ...kinds };
  return viz;
}
