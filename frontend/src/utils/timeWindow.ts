// The Explorer's time model. A window is plain data (a relative spec or two
// UTC instants). Wall clock exists only at the edges (picker text, SQL text,
// labels), and every wall-clock function takes its zone explicitly.
import { quoteClickHouseIdentifier } from "./clickhouse";
import {
  CalendarDateTime,
  fromAbsolute,
  getLocalTimeZone,
  toCalendarDate,
  toCalendarDateTime,
  toZoned,
} from "@internationalized/date";

declare const epochBrand: unique symbol;
/** A UTC instant in milliseconds. It has no zone and no ambiguity. */
export type EpochMs = number & { readonly [epochBrand]: true };
declare const zoneBrand: unique symbol;
/** A valid IANA zone name. Build it with zoneFromPreference or parseZoneId. */
export type ZoneId = string & { readonly [zoneBrand]: true };

type DurationUnit = "m" | "h" | "d" | "w";
/**
 * A calendar range covers whole days in the viewer's zone. `back` widens
 * "today" to start that many days earlier: `today-6d` is today and the six
 * dates before it, until now.
 */
type RelativeSpec =
  | { kind: "duration"; amount: number; unit: DurationUnit }
  | { kind: "calendar"; day: "today"; back: number }
  | { kind: "calendar"; day: "yesterday" };

/** What the user chose. Plain JSON, so dirty checks and keys can stringify it. */
export type TimeWindow =
  | { kind: "relative"; spec: string }
  | { kind: "absolute"; start: EpochMs; end: EpochMs };

/** A window pinned to instants, plus the zone every wall-clock rendering of it uses. */
export interface ResolvedWindow {
  readonly start: EpochMs;
  readonly end: EpochMs;
  readonly zone: ZoneId;
}

/** An i18n message for a relative window's label, such as "Last 15 minutes". */
interface WindowLabelMessage {
  key: "time.lastMinutes" | "time.lastHours" | "time.lastDays";
  count: number;
}

const UTC = "UTC" as ZoneId;
const UNIT_MS: Record<DurationUnit, number> = { m: 60_000, h: 3_600_000, d: 86_400_000, w: 604_800_000 };
const DURATION_PATTERN = /^(\d+)([mhdw])$/;
const CALENDAR_DAYS_PATTERN = /^today-([1-9]\d{0,3})d$/;
// A century back from any anchor is still a valid Date.
const MAX_DURATION_MS = 100 * 365.25 * UNIT_MS.d;
const WALL_CLOCK_PATTERN =
  /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,9}))?)?(?:\s*(Z|[+-]\d{2}:?\d{2}))?$/i;

export function epochMs(value: number | Date): EpochMs {
  const ms = typeof value === "number" ? value : value.getTime();
  if (!Number.isFinite(ms)) throw new RangeError(`invalid instant: ${ms}`);
  return ms as EpochMs;
}

export function parseZoneId(value: string): ZoneId | null {
  if (!value) return null;
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: value });
    return value as ZoneId;
  } catch {
    return null;
  }
}

export function zoneFromPreference(pref: "utc" | "local"): ZoneId {
  return pref === "utc" ? UTC : (getLocalTimeZone() as ZoneId);
}

export function parseRelativeSpec(text: string): RelativeSpec | null {
  if (text === "today") return { kind: "calendar", day: "today", back: 0 };
  if (text === "yesterday") return { kind: "calendar", day: "yesterday" };
  const days = CALENDAR_DAYS_PATTERN.exec(text);
  if (days) return { kind: "calendar", day: "today", back: Number(days[1]) };
  const match = DURATION_PATTERN.exec(text);
  if (!match) return null;
  const amount = Number(match[1]);
  const unit = match[2] as DurationUnit;
  if (amount < 1 || amount * UNIT_MS[unit] > MAX_DURATION_MS) return null;
  return { kind: "duration", amount, unit };
}

/** Checks the text, then builds a relative window with its canonical spec. Returns null for unknown text. */
export function relativeWindow(text: string): TimeWindow | null {
  const spec = parseRelativeSpec(text);
  if (!spec) return null;
  if (spec.kind === "duration") return { kind: "relative", spec: `${spec.amount}${spec.unit}` };
  return { kind: "relative", spec: spec.day === "today" && spec.back > 0 ? `today-${spec.back}d` : spec.day };
}

/** The whole days a calendar spec covers, counting today. Null for a rolling duration or yesterday. */
export function calendarDayCount(text: string): number | null {
  const spec = parseRelativeSpec(text);
  return spec?.kind === "calendar" && spec.day === "today" ? spec.back + 1 : null;
}

/** The label message for a duration window. Calendar and absolute windows return null; show their range instead. */
export function windowLabel(window: TimeWindow): WindowLabelMessage | null {
  if (window.kind !== "relative") return null;
  const spec = parseRelativeSpec(window.spec);
  if (spec?.kind !== "duration") return null;
  switch (spec.unit) {
    case "m": return { key: "time.lastMinutes", count: spec.amount };
    case "h": return { key: "time.lastHours", count: spec.amount };
    case "d": return { key: "time.lastDays", count: spec.amount };
    case "w": return { key: "time.lastDays", count: spec.amount * 7 };
  }
}

export function sameWindow(a: TimeWindow, b: TimeWindow): boolean {
  if (a.kind === "relative") return b.kind === "relative" && a.spec === b.spec;
  return b.kind === "absolute" && a.start === b.start && a.end === b.end;
}

function startOfDay(day: ReturnType<typeof toCalendarDate>, zone: ZoneId): EpochMs {
  // 'compatible' moves a midnight that falls in a DST gap forward.
  return epochMs(toZoned(day, zone, "compatible").toDate());
}

/** Pure. Invariant: start <= end. A bad relative spec cannot get here, because relativeWindow rejected it. */
export function resolveWindow(window: TimeWindow, zone: ZoneId, anchor: EpochMs): ResolvedWindow {
  if (window.kind === "absolute") return { start: window.start, end: window.end, zone };
  const spec = parseRelativeSpec(window.spec);
  if (!spec) throw new RangeError(`invalid relative spec: ${window.spec}`);
  if (spec.kind === "duration") {
    return { start: epochMs(anchor - spec.amount * UNIT_MS[spec.unit]), end: anchor, zone };
  }
  const today = toCalendarDate(fromAbsolute(anchor, zone));
  const startOfToday = startOfDay(today, zone);
  if (spec.day === "today") return { start: startOfDay(today.subtract({ days: spec.back }), zone), end: anchor, zone };
  return { start: startOfDay(today.subtract({ days: 1 }), zone), end: startOfToday, zone };
}

export function toRfc3339(ms: EpochMs): string {
  return new Date(ms).toISOString();
}

function pad(value: number, width = 2): string {
  return String(value).padStart(width, "0");
}

function formatOffset(offsetMs: number): string {
  const sign = offsetMs < 0 ? "-" : "+";
  const minutes = Math.abs(offsetMs) / 60_000;
  return `${sign}${pad(Math.floor(minutes / 60))}:${pad(minutes % 60)}`;
}

function wallClock(ms: EpochMs, zone: ZoneId): { text: string; offset: number; folded: boolean } {
  const zoned = fromAbsolute(ms, zone);
  const text =
    `${pad(zoned.year, 4)}-${pad(zoned.month)}-${pad(zoned.day)} ` +
    `${pad(zoned.hour)}:${pad(zoned.minute)}:${pad(zoned.second)}`;
  const fields = toCalendarDateTime(zoned);
  const folded = toZoned(fields, zone, "earlier").compare(toZoned(fields, zone, "later")) !== 0;
  return { text, offset: zoned.offset, folded };
}

/** "YYYY-MM-DD HH:MM:SS" in the zone. Appends " ±HH:MM" only when that wall clock occurs twice (DST fold). */
export function formatWallClock(ms: EpochMs, zone: ZoneId): string {
  const { text, offset, folded } = wallClock(ms, zone);
  return folded ? `${text} ${formatOffset(offset)}` : text;
}

function parseOffset(text: string): number | null {
  if (text.toUpperCase() === "Z") return 0;
  const sign = text.startsWith("-") ? -1 : 1;
  const digits = text.slice(1).replace(":", "");
  const [hours, minutes] = [Number(digits.slice(0, 2)), Number(digits.slice(2))];
  if (hours > 23 || minutes > 59) return null;
  return sign * (hours * 60 + minutes) * 60_000;
}

/**
 * The inverse of formatWallClock. Text with an offset (a fold suffix, Z or a
 * pasted RFC3339 time) is that exact instant in any zone. Plain text is wall
 * clock in the zone: a gap time moves forward, and a fold time takes the earlier instant.
 */
export function parseWallClock(text: string, zone: ZoneId): EpochMs | null {
  const match = WALL_CLOCK_PATTERN.exec(text.trim());
  if (!match) return null;
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map((part) => Number(part ?? 0));
  const millisecond = Number((match[7] ?? "").padEnd(3, "0").slice(0, 3));
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return null;
  const fields = new CalendarDateTime(year, month, day, hour, minute, second, millisecond);
  if (fields.day !== day) return null;
  const offsetText = match[8];
  if (offsetText === undefined) return epochMs(toZoned(fields, zone, "compatible").toDate());
  const offset = parseOffset(offsetText);
  if (offset === null) return null;
  return epochMs(toZoned(fields, UTC).toDate().getTime() - offset);
}

export function formatInZone(ms: EpochMs, zone: ZoneId, opts: Intl.DateTimeFormatOptions, locale?: string): string {
  return new Intl.DateTimeFormat(locale, { ...opts, timeZone: zone }).format(ms);
}

export function isSameZonedDay(a: EpochMs, b: EpochMs, zone: ZoneId): boolean {
  return toCalendarDate(fromAbsolute(a, zone)).compare(toCalendarDate(fromAbsolute(b, zone))) === 0;
}

export function zoneOffsetMs(ms: EpochMs, zone: ZoneId): number {
  return fromAbsolute(ms, zone).offset;
}

function sqlBound(ms: EpochMs, zone: ZoneId): string {
  const { text, folded } = wallClock(ms, zone);
  // A fold wall clock is ambiguous in its zone, so write that bound in UTC.
  if (folded) return `toDateTime('${wallClock(ms, UTC).text}', 'UTC')`;
  return `toDateTime('${text}', '${zone}')`;
}

/** The ClickHouse BETWEEN clause for the resolved window, in its own zone. A fold bound is written in UTC. */
export function sqlTimeCondition(tsField: string, window: ResolvedWindow): string {
  const field = quoteClickHouseIdentifier(tsField);
  return `${field} BETWEEN ${sqlBound(window.start, window.zone)} AND ${sqlBound(window.end, window.zone)}`;
}

export type BucketAlignment = "calendar" | "fixed-offset";

export function histogramBucketBounds(range: ResolvedWindow, stepMs: number, alignment: BucketAlignment = "calendar"): { first: number; last: number; next: (ts: number) => number } {
  if (alignment === "fixed-offset") {
    // VictoriaLogs /hits keeps the range-start offset even across DST changes.
    const offset = zoneOffsetMs(range.start, range.zone);
    const align = (ts: number) => Math.floor((ts + offset) / stepMs) * stepMs - offset;
    return { first: align(range.start), last: align(range.end), next: (ts) => ts + stepMs };
  }
  const align = (ts: number) => {
    // ClickHouse toStartOfInterval uses the epoch for 10m, 15m and 30m.
    if ([600_000, 900_000, 1_800_000].includes(stepMs)) return Math.floor(ts / stepMs) * stepMs;
    const date = fromAbsolute(ts, range.zone);
    const wall = toCalendarDateTime(date);
    if (stepMs > 3_600_000) {
      const hours = stepMs / 3_600_000;
      return toZoned(wall.set({ hour: Math.floor(date.hour / hours) * hours, minute: 0, second: 0, millisecond: 0 }), range.zone).toDate().getTime();
    }
    return ts - (date.minute * 60_000 + date.second * 1000 + date.millisecond) % stepMs;
  };
  return {
    first: align(range.start), last: align(range.end),
    next: (ts) => {
      if (stepMs <= 3_600_000) return ts + stepMs;
      const wall = toCalendarDateTime(fromAbsolute(ts, range.zone));
      const hours = stepMs / 3_600_000;
      const next = wall.set({ minute: 0, second: 0, millisecond: 0 }).add({ hours: hours - wall.hour % hours });
      return toZoned(next, range.zone).toDate().getTime();
    },
  };
}
