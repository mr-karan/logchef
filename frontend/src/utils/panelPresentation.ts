import { z } from "zod";
import type { ColumnFormat, DashboardViz, PaletteColor, TableRule, ThresholdStep, TimeFormat } from "@/api/dashboards";
import { compareExact, isExact, parseExact, type FrameNumber } from "./exactNumber";
import { formatNumber } from "./numberFormat";
import { frameCellText, type FrameColumn } from "./panelFrame";

export const paletteColors: PaletteColor[] = ["green", "amber", "red", "blue", "purple", "gray"];
export function paletteStyle(color?: PaletteColor, background = false) {
  return color ? background ? { color: `var(--panel-${color})`, backgroundColor: `var(--panel-${color}-background)` } : { color: `var(--panel-${color})` } : {};
}
export function thresholdColor(value: FrameNumber | null | undefined, steps: ThresholdStep[] = []): PaletteColor | undefined {
  if (value === null || value === undefined) return undefined;
  const exact = isExact(value) ? value : parseExact(value);
  if (!exact) return undefined;
  let color: PaletteColor | undefined;
  for (const step of steps) {
    const bound = step.value === null ? null : parseExact(step.value);
    if (step.value === null || (bound && compareExact(exact, bound) >= 0)) color = step.color;
  }
  return color;
}
export function ruleMatches(value: unknown, rule: TableRule): boolean {
  const empty = value === null || value === undefined || value === "";
  if (rule.op === "empty") return empty;
  if (rule.op === "not_empty") return !empty;
  if (empty) return false;
  if (rule.op === "contains" || rule.op === "not_contains") {
    const contains = String(value).includes(String(rule.value));
    return rule.op === "contains" ? contains : !contains;
  }
  const left = isExact(value) ? value : parseExact(value), right = parseExact(rule.value);
  const cmp = left && right ? compareExact(left, right) : null;
  switch (rule.op) {
    case "=": return cmp === null ? String(value) === String(rule.value) : cmp === 0;
    case "!=": return cmp === null ? String(value) !== String(rule.value) : cmp !== 0;
    case ">": return cmp !== null && cmp > 0;
    case ">=": return cmp !== null && cmp >= 0;
    case "<": return cmp !== null && cmp < 0;
    case "<=": return cmp !== null && cmp <= 0;
  }
}
export function tableRuleColor(row: Record<string, unknown>, rules: TableRule[] = [], column?: string): PaletteColor | undefined {
  return rules.find(rule => column === undefined
    ? rule.scope === "row" && rule.columns.some(name => ruleMatches(row[name], rule))
    : rule.scope === "cell" && rule.columns.includes(column) && ruleMatches(row[column], rule))?.color;
}
export function formatPanelTime(value: string, kind: "time" | "date", format: TimeFormat = { kind: "time" }, zone = "UTC", locale?: string): string {
  const dateMode = format.date ?? "iso", timeMode = format.time ?? (kind === "date" ? "none" : "seconds");
  const instant = new Date(kind === "date" ? `${value}T00:00:00Z` : value);
  const timeZone = kind === "date" ? "UTC" : zone;
  const parts = new Map(new Intl.DateTimeFormat("en", { timeZone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", fractionalSecondDigits: 3, hourCycle: "h23" }).formatToParts(instant).map(p => [p.type, p.value]));
  const date = dateMode === "none" ? "" : dateMode === "medium" ? new Intl.DateTimeFormat(locale, { timeZone, dateStyle: "medium" }).format(instant) : `${parts.get("year")}-${parts.get("month")}-${parts.get("day")}`;
  const time = timeMode === "none" ? "" : `${parts.get("hour")}:${parts.get("minute")}${timeMode !== "minutes" ? `:${parts.get("second")}` : ""}${timeMode === "millis" ? `.${parts.get("fractionalSecond")}` : ""}`;
  return [date, time].filter(Boolean).join(" ");
}
export function formattedTableCell(value: unknown, column?: FrameColumn, format?: ColumnFormat, zone = "UTC", locale?: string): string {
  if (value === null || value === undefined) return "";
  if ((column?.kind === "time" || column?.kind === "date") && typeof value === "string" && (!format || format.kind === "time")) return formatPanelTime(value, column.kind, format, zone, locale);
  if (format && format.kind !== "time" && (isExact(value) || typeof value === "number")) return formatNumber(value, format, { locale }).text;
  return frameCellText(value, column, zone);
}
const colorSchema = z.enum(["green", "amber", "red", "blue", "purple", "gray"]);
const exactSchema = z.union([z.number().finite(), z.string()]).refine(v => parseExact(v) !== null);
const nameSchema = z.string().min(1).refine(v => Array.from(v).length <= 128);
const namesSchema = z.array(nameSchema).min(1).max(100).refine(v => new Set(v).size === v.length);
const numberFields = {
 notation: z.enum(["auto", "standard", "compact"]).optional(), decimals: z.number().int().min(0).max(10).optional(),
 grouping: z.boolean().optional(), scale: z.number().finite().refine(v => v !== 0).optional(), unit: z.enum(["none", "percent", "bytes", "duration_ms", "duration_s"]).optional(),
 prefix: z.string().refine(v => Array.from(v).length <= 16).optional(), suffix: z.string().refine(v => Array.from(v).length <= 16).optional(),
};
export const columnFormatSchema = z.union([
 z.strictObject({ kind: z.literal("number").optional(), ...numberFields }),
 z.strictObject({ kind: z.literal("time"), date: z.enum(["iso", "medium", "none"]).optional(), time: z.enum(["none", "minutes", "seconds", "millis"]).optional() }),
]);
export const rulesSchema = z.array(z.strictObject({ columns: namesSchema, op: z.enum([">", ">=", "<", "<=", "=", "!=", "contains", "not_contains", "empty", "not_empty"]), value: z.union([z.string(), z.number().finite()]).optional(), color: colorSchema, scope: z.enum(["cell", "row"]) }).refine(rule => {
 if (rule.op === "empty" || rule.op === "not_empty") return rule.value === undefined;
 if (rule.op === "contains" || rule.op === "not_contains") return typeof rule.value === "string";
 if (rule.op === "=" || rule.op === "!=") return typeof rule.value === "string" || exactSchema.safeParse(rule.value).success;
 return exactSchema.safeParse(rule.value).success;
})).max(20);
export const thresholdsSchema = z.array(z.strictObject({ value: exactSchema.nullable(), color: colorSchema })).min(1).max(10).refine(steps => steps.length > 0 && steps[0].value === null && steps.slice(1).every((step,i) => {
 const current = parseExact(step.value), previous = i === 0 ? null : parseExact(steps[i].value);
 return current && (i === 0 || (previous && compareExact(current, previous) > 0));
}));
export function validatePresentation(viz: DashboardViz): string | null {
 if ((viz.type === "stat" || viz.type === "chart") && viz.thresholds !== undefined && !thresholdsSchema.safeParse(viz.thresholds).success) return "Invalid thresholds.";
 if (viz.type === "stat" && viz.color_mode !== undefined && !["value", "background"].includes(viz.color_mode)) return "Invalid color mode.";
 if (viz.type === "table") {
  if (viz.rules !== undefined && !rulesSchema.safeParse(viz.rules).success) return "Invalid table rules.";
  if (viz.column_formats !== undefined && !z.record(nameSchema, columnFormatSchema).refine(v => Object.keys(v).length <= 100).safeParse(viz.column_formats).success) return "Invalid column formats.";
 }
 return null;
}
