import { i18n } from "@/i18n";
import type { DashboardViz, DashboardVizType, NumberFormat } from "@/api/dashboards";
import { addExact, compareExact, exactString, isExact, multiplyExact, parseExact, type FrameNumber } from "./exactNumber";
import { frameCellText, frameWithKinds, suggestKind, type FrameColumn, type PanelFrame } from "./panelFrame";
import { getColorForGroupValue } from "./histogram-chart";
import { formatNumber } from "./numberFormat";

export type ChartViz = Extract<DashboardViz, { type: "chart" }>;
export type ListViz = Extract<DashboardViz, { type: "pie" | "toplist" }>;
export interface ChartValue { raw: FrameNumber | null; coordinate: number; text: string; exact: string; approximate: boolean }
export interface ChartSeries { key: string; label: string; color: string }
export interface ChartRow { key: string; x: number; label: () => string; values: Map<string, ChartValue | null> }
export interface ChartModel { rows: ChartRow[]; series: ChartSeries[]; time: boolean; notices: string[] }
export interface ListItem extends ChartSeries { value: ChartValue; percentage: number; displayPercentage: number }

export function chartColors(key: string, text: string, other = false, used = new Set<string>()): Pick<ChartSeries, "color"> {
  let hash = 0;
  for (const char of key) hash = (Math.imul(hash, 31) + char.charCodeAt(0)) | 0;
  const semantic = /^(error|err|fatal|critical|crit|alert|emerg|emergency|warn|warning|info|information|notice|debug|trace|verbose|get|post|put|delete|patch|options|head)$/i.test(text);
  const preferred = other ? "var(--panel-chart-other)" : semantic ? getColorForGroupValue(text) : `var(--panel-chart-${Math.abs(hash) % 12 + 1})`;
  let color = preferred;
  for (let offset = 1; used.has(color) && offset <= 12; offset++) color = `var(--panel-chart-${(Math.abs(hash) + offset) % 12 + 1})`;
  if (used.has(color)) color = `hsl(${(used.size * 137.508) % 360} 65% var(--panel-chart-generated-lightness))`;
  used.add(color);
  return { color };
}

export function chartLabelColor(hex: string): string {
  const channels = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(c => c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  const luminance = channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
  return luminance > 0.179 ? "#000000" : "#ffffff";
}

export function chartStacked(viz: ChartViz): boolean {
  return (viz.stack ?? ((viz.mark ?? "line") === "line" ? "none" : "stacked")) === "stacked";
}

function suggestionColumns(frame: PanelFrame): FrameColumn[] {
  return frame.columns.map((c) => c.backendType === null && c.confidence !== "confirmed" ? { ...c, kind: suggestKind(frame.rawRows.slice(0, 100).map((r) => r[c.name])) } : c);
}

export function suggestRoles(frame: PanelFrame, type: DashboardVizType): DashboardViz {
  const columns = suggestionColumns(frame);
  const numbers = columns.filter((c) => c.kind === "number");
  const time = columns.find((c) => c.kind === "time" || c.kind === "date" || c.name === "_time");
  const categories = columns.filter((c) => c.kind === "category");
  const roles = (): DashboardViz => { switch (type) {
    case "stat": return { type, value: numbers[0]?.name, order_by: time?.name };
    case "chart": return { type, x: (time ?? categories[0])?.name, y: numbers.map((c) => c.name), ...(time && categories.length === 1 && numbers.length === 1 ? { series: categories[0].name } : {}) };
    case "pie": case "toplist": return { type, label: columns.find((c) => c.kind !== "number")?.name, value: numbers[0]?.name };
    case "table": return { type };
  } };
  const viz = roles();
  return withConfirmedRoles(frame, viz);
}

function withConfirmedRoles(frame: PanelFrame, viz: DashboardViz): DashboardViz {
  const names = viz.type === "chart" ? [viz.x, ...(viz.y ?? []), viz.series] : viz.type === "stat" ? [viz.value, viz.order_by] : viz.type === "table" ? [] : [viz.label, viz.value];
  const confirmed = Object.fromEntries(suggestionColumns(frame).filter(c => c.backendType === null && names.includes(c.name)).map(c => [c.name, { kind: c.name === "_time" ? "time" as const : c.kind, confidence: "confirmed" as const }]));
  return Object.keys(confirmed).length ? { ...viz, columns: { ...confirmed, ...viz.columns } } : viz;
}

export function suggestViz(frame: PanelFrame): DashboardViz {
  const roles = suggestRoles(frame, "chart");
  const columns = suggestionColumns(frame);
  const numbers = columns.filter((c) => c.kind === "number");
  if (frame.rows.length === 1 && numbers.length === 1) return suggestRoles(frame, "stat");
  if (roles.type === "chart" && roles.x && roles.y?.length) {
    const kinds = columns.map((c) => c.kind);
    const time = kinds.some((k) => k === "time" || k === "date") || roles.x === "_time";
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

function colorIdentity(value: unknown, count: boolean): string {
  if (count && typeof value === "object" && value !== null && "kind" in value && value.kind === "value" && "value" in value) return identity(value.value);
  return identity(value);
}

function identity(value: unknown): string {
  if (isExact(value)) return `number:${exactString(value)}`;
  return JSON.stringify(value) ?? "null";
}

function label(value: unknown, col: FrameColumn, count: boolean, zone: string): string {
  if (count && typeof value === "object" && value !== null && "kind" in value) {
    if (value.kind === "other") return i18n.global.t("panelChart.other");
    if (value.kind === "null") return i18n.global.t("panelChart.nullCategory");
    if (value.kind === "value" && "value" in value) return String(value.value) || i18n.global.t("panelChart.emptyCategory");
  }
  if (value === null || value === undefined) return i18n.global.t("panelChart.nullCategory");
  return frameCellText(value, col, zone) || i18n.global.t("panelChart.emptyCategory");
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
  let formatted: ReturnType<typeof formatNumber> | undefined;
  const magnitude = Math.abs(coordinate * (format?.scale ?? 1));
  const decimals = magnitude > 0 && magnitude < 1 ? Math.max(2, 1 - Math.floor(Math.log10(magnitude))) : undefined;
  const text = () => formatted ??= formatNumber(value, { ...format, decimals: format?.decimals ?? decimals });
  return { raw: value, coordinate, get text() { return text().text; }, get exact() { return text().exact; }, approximate };
}

function duplicateError(keys: Map<string, number>, first: string | undefined, roles: string[]): void {
  const duplicates = [...keys.values()].filter((n) => n > 1).length;
  if (duplicates) throw new Error(`${duplicates} duplicate key(s). First key: ${first}. Group the query by ${roles.map((r) => `"${r}"`).join(" and ")}.`);
}

export function buildPanelChart(input: PanelFrame, viz: ChartViz, count: boolean, zone = "UTC"): ChartModel {
  if (!input.rows.length && !input.columns.length) return { rows: [], series: [], time: false, notices: input.notices };
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
  const usedColors = new Set<string>();
  const rows = new Map<string, { row: ChartRow; values: Map<string, FrameNumber | null> }>();
  let nullX = 0;
  for (const source of frame.rows) {
    const xv = source[x.name];
    if (time && xv === null) { nullX++; continue; }
    const groupKey = group ? identity(source[group.name]) : "";
    const rowKey = identity(xv);
    const key = JSON.stringify([rowKey, groupKey]);
    keys.set(key, (keys.get(key) ?? 0) + 1);
    if (keys.get(key) === 2 && firstDuplicate === undefined) firstDuplicate = `${label(xv, x, count, zone)}${group ? `, ${label(source[group.name], group, count, zone)}` : ""}`;
    const entry = rows.get(rowKey) ?? { row: { key: rowKey, x: time ? Date.parse(String(xv)) : rows.size, label: () => label(xv, x, count, zone), values: new Map() }, values: new Map<string, FrameNumber | null>() };
    rows.set(rowKey, entry);
    for (const y of ys) {
      const sk = JSON.stringify([y.name, groupKey]);
      if (!series.has(sk)) {
        const text = group ? label(source[group.name], group, count, zone) : y.name;
        const colorKey = group ? colorIdentity(source[group.name], count) : identity(y.name);
        const color = chartColors(group && ys.length > 1 ? JSON.stringify([colorKey, y.name]) : colorKey, text, count && groupKey === '{"kind":"other"}', usedColors);
        series.set(sk, { key: sk, label: group && ys.length > 1 ? `${text} · ${y.name}` : text, ...color });
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
  const result = [...rows.values()].map((e) => e.row);
  if (time) result.sort((a, b) => a.x - b.x);
  if (result.some((r) => [...r.values.values()].some((v) => v?.approximate))) notices.push("Chart coordinates are approximate. Tooltips show exact values.");
  return { rows: result, series: visibleSeries, time, notices };
}

export function buildPanelList(input: PanelFrame, viz: ListViz, count: boolean, zone = "UTC"): { items: ListItem[]; notices: string[]; total: ChartValue | null } {
  if (!input.rows.length && !input.columns.length) return { items: [], notices: input.notices, total: null };
  const frame = frameWithKinds(input, viz.columns);
  const dimension = column(frame, count ? "series" : viz.label);
  const metric = column(frame, count ? "count" : viz.value, true);
  const notices = [...frame.notices];
  const keys = new Map<string, number>();
  let firstDuplicate: string | undefined;
  const values = new Map<string, { key: string; label: string; value: FrameNumber | null; colorKey: string }>();
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
    values.set(key, { key, label: text, colorKey: colorIdentity(row[dimension.name], count), value: count && previous?.value !== undefined && previous.value !== null && value !== null ? sum(previous.value, value) : value });
  }
  if (!count) duplicateError(keys, firstDuplicate, [dimension.name]);
  if (nulls) notices.push(`${nulls} NULL value(s) omitted.`);
  let entries = [...values.values()];
  const totalValue = entries.reduce<FrameNumber | null>((a, e) => e.value === null ? a : a === null ? e.value : sum(a, e.value), null);
  if (count) entries.sort((a, b) => isExact(a.value) && isExact(b.value) ? compareExact(b.value, a.value) : Number(b.value) - Number(a.value));
  let remainder: FrameNumber | null = null;
  const fold = (value: FrameNumber | null) => { if (value !== null) remainder = remainder === null ? value : sum(remainder, value); };
  if (viz.type === "pie" && viz.min_slice_percent && totalValue !== null) {
    const total = isExact(totalValue) ? totalValue : parseExact(totalValue);
    const threshold = parseExact(viz.min_slice_percent);
    entries = entries.filter(e => {
      const value = isExact(e.value) ? e.value : parseExact(e.value);
      if (value && total && threshold && compareExact(multiplyExact(value, { kind: "exact", coefficient: 100n, scale: 0 }), multiplyExact(threshold, total)) < 0) { fold(e.value); return false; }
      return true;
    });
  }
  if (viz.type === "pie" && entries.length + (remainder === null ? 0 : 1) > 50) {
    notices.push(`50 of ${values.size} slices shown. Overflow folded into Other.`);
    entries.splice(49).forEach(e => fold(e.value));
  }
  if (remainder !== null) entries.push({ key: "pie-remainder", label: i18n.global.t("panelChart.other"), value: remainder, colorKey: "pie-remainder" });
  const maxRows = viz.type === "pie" ? 50 : viz.max_rows ?? 20;
  if (entries.length > maxRows) notices.push(`${maxRows} of ${entries.length} ${viz.type === "pie" ? "slices" : "rows"} shown.`);
  const usedColors = new Set<string>();
  const items = entries.slice(0, maxRows).map((e) => ({ ...e, ...chartColors(e.colorKey, e.label, e.key === "pie-remainder" || e.key === '{"kind":"other"}', usedColors), value: e.value === null ? { raw: null, coordinate: 0, text: i18n.global.t("panelChart.null"), exact: i18n.global.t("panelChart.null"), approximate: false } : chartValue(e.value, viz.format), percentage: 0, displayPercentage: 0 }));
  const max = Math.max(0, ...items.map((i) => Math.abs(i.value.coordinate)));
  const total = items.reduce((s, i) => s + (max ? Math.abs(i.value.coordinate) / max : 0), 0);
  for (const item of items) item.percentage = total ? Math.abs(item.value.coordinate) / max / total * 100 : 0;
  if (total > 0) {
    for (const item of items) item.displayPercentage = Math.floor(item.percentage * 10) / 10;
    const remaining = Math.round(1000 - items.reduce((sum, item) => sum + Math.round(item.displayPercentage * 10), 0));
    const rounded = [...items].sort((a, b) => (b.percentage - b.displayPercentage) - (a.percentage - a.displayPercentage));
    for (let i = 0; i < remaining; i++) rounded[i].displayPercentage += 0.1;
  }
  if (items.some((i) => i.value.approximate)) notices.push("Chart coordinates are approximate. Tooltips show exact values.");
  return { items, notices, total: totalValue === null ? null : chartValue(totalValue, viz.format) };
}

export function withSuggestedRoles(frame: PanelFrame, viz: DashboardViz): DashboardViz {
  if (viz.type === "chart" && viz.x && viz.y?.length) return withConfirmedRoles(frame, viz);
  if (viz.type === "stat" && viz.value) return withConfirmedRoles(frame, viz);
  if ((viz.type === "pie" || viz.type === "toplist") && viz.label && viz.value) return withConfirmedRoles(frame, viz);
  const suggested = suggestRoles(frameWithKinds(frame, viz.columns), viz.type);
  const columns = { ...suggested.columns, ...viz.columns };
  const kinds = Object.keys(columns).length ? { columns } : {};
  if (viz.type === "chart" && suggested.type === "chart") return { ...viz, x: viz.x ?? suggested.x, y: viz.y?.length ? viz.y : suggested.y, series: viz.series ?? suggested.series, ...kinds };
  if (viz.type === "stat" && suggested.type === "stat") return { ...viz, value: viz.value ?? suggested.value, order_by: viz.order_by ?? suggested.order_by, ...kinds };
  if ((viz.type === "pie" || viz.type === "toplist") && (suggested.type === "pie" || suggested.type === "toplist")) return { ...viz, label: viz.label ?? suggested.label, value: viz.value ?? suggested.value, ...kinds };
  return viz;
}
