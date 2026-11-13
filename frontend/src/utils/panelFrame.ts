import { histogramBucketBounds, type BucketAlignment, type ResolvedWindow } from "./timeWindow";
import { fillBucketGaps, parseGranularityToMilliseconds } from "./histogram-chart";
import type { ColumnKind, ConfirmedColumn, StatReduce } from "@/api/dashboards";
import type { HistogramDataPoint, QueryStats, Truncation, LimitSource } from "@/api/explore";
import { addExact, averageExact, compareExact, exactString, isExact, parseExact, type FrameNumber, type ExactNumber } from "./exactNumber";

export interface FrameColumn {
  name: string;
  backendType: string | null;
  kind: ColumnKind;
  confidence: "declared" | "confirmed" | "guessed";
  repr: "int" | "bigint" | "decimal" | "float" | null;
}
export type SeriesKey = { kind: "value"; value: string } | { kind: "null" } | { kind: "other" };
export interface PanelFrame {
  columns: FrameColumn[];
  rows: Record<string, unknown>[];
  rawRows: Record<string, unknown>[];
  interval: string | null;
  limit: { source: LimitSource; rows: number } | null;
  truncation: Truncation;
  notices: string[];
}

export function declaredColumn(name: string, backendType: string | null): FrameColumn {
  const type = (backendType ?? "").replace(/(?:Nullable|LowCardinality)\(/g, "").replace(/\)+$/, "");
  let kind: ColumnKind = "category";
  let repr: FrameColumn["repr"] = null;
  if (/^Date(?:32)?$/.test(type)) kind = "date";
  else if (/^DateTime/.test(type)) kind = "time";
  else if (/^(?:U?Int)(?:8|16|32)$/.test(type)) { kind = "number"; repr = "int"; }
  else if (/^U?Int(?:64|128|256)$/.test(type)) { kind = "number"; repr = "bigint"; }
  else if (/^Decimal/.test(type)) { kind = "number"; repr = "decimal"; }
  else if (/^Float/.test(type)) { kind = "number"; repr = "float"; }
  else if (type === "Bool") kind = "bool";
  else if (/^(Array|Map|Tuple|JSON|Object|Nested)/.test(type)) kind = "other";
  return { name, backendType, kind, repr, confidence: "declared" };
}

export function resultFrame(input: {
  columns: { name: string; type: string }[];
  rows: Record<string, unknown>[];
  untyped?: boolean;
  stats?: QueryStats;
}): PanelFrame {
  const names = new Set(input.columns.map((c) => c.name));
  const columns = input.columns.map((c) => declaredColumn(c.name, input.untyped ? null : c.type));
  for (const row of input.rows) for (const name of Object.keys(row)) {
    if (!names.has(name)) { names.add(name); columns.push(declaredColumn(name, null)); }
  }
  const frame: PanelFrame = {
    columns, rawRows: input.rows, rows: [], interval: null,
    limit: input.stats?.limit_applied !== undefined && input.stats.limit_source ? { source: input.stats.limit_source, rows: input.stats.limit_applied } : null,
    truncation: input.stats?.truncation ?? "none", notices: [],
  };
  return frameWithKinds(frame);
}

export function countFrame(buckets: HistogramDataPoint[], interval: string | null, grouped: boolean, notice?: string | null, range?: ResolvedWindow, alignment: BucketAlignment = "calendar"): PanelFrame {
  if (buckets.length) {
    const step = parseGranularityToMilliseconds(interval);
    if (!step) throw new Error("Count results require a valid bucket interval.");
    const byTime = new Map<number, HistogramDataPoint[]>();
    for (const bucket of buckets) {
      const ts = Date.parse(bucket.bucket);
      byTime.set(ts, [...(byTime.get(ts) ?? []), bucket]);
    }
    const rows = [...byTime].map(([ts, buckets]) => ({ ts, buckets })).sort((a, b) => a.ts - b.ts);
    const series = new Map(buckets.map((bucket) => [JSON.stringify([bucket.is_other ?? false, bucket.is_null ?? false, bucket.group_value ?? ""]), bucket]));
    const makeZero = (ts: number) => ({
      ts,
      buckets: [...(grouped && series.size ? series.values() : [{}])].map((series) => ({
        ...series, bucket: new Date(ts).toISOString().replace(".000Z", "Z"), log_count: 0,
      })),
    });
    const filled = fillBucketGaps(rows, step, makeZero, range ? histogramBucketBounds(range, step, alignment) : undefined);
    buckets = filled.flatMap((row) => row.buckets);
  }
  const rows = buckets.map((b) => ({
    time: b.bucket, count: b.log_count,
    ...(grouped ? { series: b.is_other ? { kind: "other" } : b.is_null ? { kind: "null" } : { kind: "value", value: b.group_value ?? "" } } : {}),
  }));
  const frame = resultFrame({ columns: [{ name: "time", type: "DateTime64" }, { name: "count", type: "UInt64" }, ...(grouped ? [{ name: "series", type: "String" }] : [])], rows });
  return { ...frame, interval, notices: notice ? [notice] : [] };
}

const rfc3339 = /^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
function validDate(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0, 10) === value;
}
function readCell(value: unknown, column: FrameColumn): unknown {
  if (value === null || value === undefined) return null;
  if (column.kind === "category" || column.kind === "other") return value;
  if (value === "") return null;
  if (column.kind === "number") {
    if (column.repr === "float") return typeof value === "number" && Number.isFinite(value) ? value : null;
    return parseExact(value);
  }
  if (column.kind === "bool") return value === true || value === "true" ? true : value === false || value === "false" ? false : null;
  if (typeof value !== "string") return null;
  if (column.kind === "date") {
    const date = declaredColumn(column.name, column.backendType).kind === "date" ? value.slice(0, 10) : value;
    return validDate(date) ? date : null;
  }
  const timestamp = column.backendType && /DateTime/.test(column.backendType) ? value.replace(" ", "T") + (/Z$|[+-]\d\d:\d\d$/.test(value) ? "" : "Z") : value;
  return rfc3339.test(timestamp) && validDate(timestamp.slice(0, 10)) && Number.isFinite(Date.parse(timestamp)) ? timestamp : null;
}

export function frameWithKinds(frame: PanelFrame, overrides: Record<string, ConfirmedColumn> = {}): PanelFrame {
  if (frame.rows.length === frame.rawRows.length && Object.keys(overrides).every((name) => frame.columns.some((c) => c.name === name)) && frame.columns.every((c) => Object.hasOwn(overrides, c.name) ? c.confidence === "confirmed" && c.kind === overrides[c.name].kind : c.confidence === "declared")) return frame;
  const columns = frame.columns.map((c): FrameColumn => {
    const declared = declaredColumn(c.name, c.backendType);
    const confirmed = Object.hasOwn(overrides, c.name) ? overrides[c.name] : undefined;
    return confirmed ? { ...declared, kind: confirmed.kind, confidence: "confirmed", repr: confirmed.kind === "number" ? declared.repr ?? "decimal" : null } : declared;
  });
  const notices: string[] = [];
  for (const name of Object.keys(overrides)) if ((frame.rawRows.length || columns.length) && !columns.some((c) => c.name === name)) notices.push(`Column "${name}" is missing from the result.`);
  const invalid = new Map<string, number>();
  const previousColumns = new Map(frame.columns.map((column) => [column.name, column]));
  const rows = frame.rawRows.map((raw, index) => {
    const row: Record<string, unknown> = Object.create(null);
    for (const c of columns) {
      const previous = previousColumns.get(c.name);
      const rawValue = Object.hasOwn(raw, c.name) ? raw[c.name] : undefined;
      const value = index < frame.rows.length && previous?.kind === c.kind && previous.repr === c.repr ? frame.rows[index][c.name] : readCell(rawValue, c);
      row[c.name] = value;
      if (value === null && rawValue !== null && rawValue !== undefined && rawValue !== "" && !(typeof rawValue === "string" && /^(?:NaN|[+-]?Inf)$/.test(rawValue))) invalid.set(c.name, (invalid.get(c.name) ?? 0) + 1);
    }
    return row;
  });
  for (const [name, count] of invalid) notices.push(`${count} invalid cell(s) in "${name}" shown as NULL.`);
  return { ...frame, columns, rows, notices: [...frame.notices.filter((n) => !n.includes("invalid cell(s)") && !n.startsWith("Column \"")), ...notices] };
}

export function suggestKind(values: unknown[]): ColumnKind {
  const cells = values.filter((v) => v !== "" && v !== null && v !== undefined);
  if (!cells.length) return "category";
  if (cells.every((v) => typeof v === "string" && rfc3339.test(v) && Number.isFinite(Date.parse(v)))) return "time";
  if (cells.every((v) => typeof v === "string" && /^-?(?:0|[1-9]\d*)(?:\.\d+)?$/.test(v) && (v.replace(/[^\d]/g, "").replace(/^0+/, "").length <= 15))) return "number";
  return "category";
}

const timeFormats = new Map<string, Intl.DateTimeFormat>();

export function frameCellText(value: unknown, column?: FrameColumn, zone = "UTC"): string {
  if (value === null || value === undefined) return "";
  if (isExact(value)) {
    const scale = column?.repr === "decimal" ? column.backendType?.match(/Decimal(?:\(\d+,\s*|(?:32|64|128|256)\(\s*)(\d+)/)?.[1] : undefined;
    return exactString(value, scale === undefined ? undefined : Number(scale));
  }
  if (column?.kind === "time" && typeof value === "string") {
    let formatter = timeFormats.get(zone);
    if (!formatter) {
      formatter = new Intl.DateTimeFormat("en", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
      if (timeFormats.size >= 32) timeFormats.clear();
      timeFormats.set(zone, formatter);
    }
    const parts = new Map(formatter.formatToParts(new Date(value)).map((p) => [p.type, p.value]));
    const fraction = value.match(/\.(\d+)/)?.[1];
    return `${parts.get("year")}-${parts.get("month")}-${parts.get("day")} ${parts.get("hour")}:${parts.get("minute")}:${parts.get("second")}${fraction ? "." + fraction : ""}`;
  }
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

export interface ReducedStat {
  value: FrameNumber | null;
  rowLabel: string | null;
  notices: string[];
}
function compareCells(a: unknown, b: unknown, column: FrameColumn): number {
  if (isExact(a) && isExact(b)) return compareExact(a, b);
  if (typeof a === "number" && typeof b === "number") return a - b;
  if (column.kind === "time") {
    const millis = Date.parse(String(a)) - Date.parse(String(b));
    if (millis) return millis;
    const fraction = (v: unknown) => String(v).match(/\.(\d+)/)?.[1] ?? "";
    const left = fraction(a), right = fraction(b);
    const length = Math.max(left.length, right.length);
    return compareText(left.padEnd(length, "0"), right.padEnd(length, "0"));
  }
  return compareText(String(a), String(b));
}

function compareText(a: string, b: string): number {
  const left = Array.from(a, (c) => c.codePointAt(0)!);
  const right = Array.from(b, (c) => c.codePointAt(0)!);
  for (let i = 0; i < Math.min(left.length, right.length); i++) if (left[i] !== right[i]) return left[i] - right[i];
  return left.length - right.length;
}

export function reduceStat(frame: PanelFrame, options: { value: string; reduce: StatReduce; orderBy?: string; count: boolean; zone?: string }): ReducedStat {
  if (!frame.rows.length && !frame.columns.length) return { value: options.count ? parseExact(0) : null, rowLabel: null, notices: frame.notices };
  const column = frame.columns.find((c) => c.name === options.value);
  if (!column) throw new Error(`Column "${options.value}" is missing from the result.`);
  if (column.kind !== "number") throw new Error(`Column "${column.name}" must be a number (backend type ${column.backendType ?? "untyped"}).`);
  const notices = [...frame.notices];
  let rows = frame.rows;
  if (options.count) {
    const totals = new Map<string, ExactNumber>();
    for (const row of rows) {
      const time = new Date(String(row.time)).toISOString().replace(".000Z", "Z");
      const count = row.count;
      if (isExact(count)) {
        const previous = totals.get(time);
        totals.set(time, previous ? addExact(previous, count) : count);
      }
    }
    rows = [...totals].map(([time, count]) => ({ time, count }));
  }
  let order: FrameColumn | undefined;
  const orderBy = options.count ? "time" : options.orderBy;
  if (orderBy) {
    order = frame.columns.find((c) => c.name === orderBy);
    if (!order) throw new Error(`Column "${options.orderBy}" is missing from the result.`);
  }
  if (!rows.length && options.count) return { value: parseExact(0), rowLabel: null, notices };
  if (options.reduce === "first" || options.reduce === "last") {
    if (order) {
      const key = order;
      const before = rows.length;
      rows = rows.filter((r) => r[key.name] !== null && r[key.name] !== undefined);
      if (rows.length !== before) notices.push(`${before - rows.length} row(s) have no valid order value.`);
      rows = [...rows].sort((a, b) => compareCells(a[key.name], b[key.name], key));
    }
    const selected = options.reduce === "first" ? rows[0] : rows.at(-1);
    if (!order && rows.length > 1) notices.push("Multiple rows without order_by. Result order selects the value.");
    if (order && selected) {
      const tied = rows.filter((r) => compareCells(r[order.name], selected[order.name], order) === 0).length;
      if (tied > 1) notices.push(`${tied} rows share the selected order value. Group the query by "${order.name}".`);
    }
    const value = selected?.[column.name];
    return { value: isExact(value) || typeof value === "number" ? value : null, rowLabel: selected && order ? frameCellText(selected[order.name], order, options.zone) : null, notices };
  }
  const values = rows.map((r) => r[column.name]).filter((v): v is FrameNumber => isExact(v) || typeof v === "number");
  if (!values.length) return { value: options.count && !rows.length ? parseExact(0) : null, rowLabel: null, notices };
  let value = values[0];
  if (isExact(value)) {
    let result = value;
    for (const next of values.slice(1)) {
      if (!isExact(next)) throw new Error("Mixed numeric representations in a frame column.");
      if (options.reduce === "sum" || options.reduce === "avg") result = addExact(result, next);
      else if ((options.reduce === "min" && compareExact(next, result) < 0) || (options.reduce === "max" && compareExact(next, result) > 0)) result = next;
    }
    value = options.reduce === "avg" ? averageExact(result, values.length) : result;
  } else {
    const floats = values.filter((v): v is number => typeof v === "number");
    value = options.reduce === "min" ? Math.min(...floats) : options.reduce === "max" ? Math.max(...floats) : floats.reduce((a, b) => a + b, 0) / (options.reduce === "avg" ? floats.length : 1);
  }
  return { value, rowLabel: null, notices };
}

export function histogramBuckets(frame: PanelFrame): HistogramDataPoint[] {
  return frame.rawRows.map((row) => {
    const series = row.series;
    const point: HistogramDataPoint = { bucket: String(row.time), log_count: typeof row.count === "number" ? row.count : 0 };
    if (typeof series === "object" && series !== null && "kind" in series) {
      if (series.kind === "null") point.is_null = true;
      else if (series.kind === "other") point.is_other = true;
      else if (series.kind === "value" && "value" in series && typeof series.value === "string") point.group_value = series.value;
    }
    return point;
  });
}
