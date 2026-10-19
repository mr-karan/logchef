// Stored shapes (URL, draft, saved query, share) to and from TimeWindow.
// Legacy decoding lives only here.
import { CalendarDateTime, toZoned } from "@internationalized/date";
import type { SavedQueryTimeRange } from "@/api/savedQueries";
import { epochMs, parseZoneId, relativeWindow, zoneFromPreference, type EpochMs, type TimeWindow, type ZoneId } from "./timeWindow";

export type DraftTime = { version: 2; window: TimeWindow };

type StoredRangeOrigin =
  | { format: "saved-query"; version: number; prefZone: ZoneId }
  | { format: "share"; version: number; timezone: string | undefined };

function isMs(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function absoluteWindow(start: number, end: number): TimeWindow | null {
  return start <= end ? { kind: "absolute", start: epochMs(start), end: epochMs(end) } : null;
}

// What the v1 readers did: take the browser-zone fields of ms and read them as wall clock in zone.
function legacyFieldsMs(ms: number, zone: ZoneId): EpochMs {
  const d = new Date(ms);
  const fields = new CalendarDateTime(d.getFullYear(), d.getMonth() + 1, d.getDate(), d.getHours(), d.getMinutes(), d.getSeconds());
  return epochMs(toZoned(fields, zone).toDate());
}

function legacyWindow(start: number, end: number, zone: ZoneId): TimeWindow | null {
  return absoluteWindow(legacyFieldsMs(start, zone), legacyFieldsMs(end, zone));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseWindow(raw: unknown): TimeWindow | null {
  if (!isRecord(raw)) return null;
  if (raw.kind === "relative") return typeof raw.spec === "string" ? relativeWindow(raw.spec) : null;
  if (raw.kind === "absolute" && isMs(raw.start) && isMs(raw.end)) return absoluteWindow(raw.start, raw.end);
  return null;
}

export function encodeWindowForUrl(w: TimeWindow): { t: string } | { start: string; end: string } {
  return w.kind === "relative" ? { t: w.spec } : { start: String(w.start), end: String(w.end) };
}

/** Reads t, or start and end, as written by the Explorer, Ask Chef chips, alerts and the CLI. Ms are instants. */
export function decodeWindowFromUrl(query: Readonly<Record<string, string | undefined>>): TimeWindow | null {
  const spec = query.t ?? query.time;
  if (spec) return relativeWindow(spec);
  const start = query.start ?? query.start_time;
  const end = query.end ?? query.end_time;
  if (!start || !end || !/^\d+$/.test(start) || !/^\d+$/.test(end)) return null;
  const [startMs, endMs] = [Number(start), Number(end)];
  return isMs(startMs) && isMs(endMs) ? absoluteWindow(startMs, endMs) : null;
}

/** Reads the time of a stored Explorer draft: v2 holds a window, v1 holds browser-field ms. */
export function decodeDraftWindow(raw: unknown, prefZone: ZoneId): TimeWindow | null {
  if (!isRecord(raw)) return null;
  if (raw.version === 2) return parseWindow(raw.window);
  if (raw.version !== 1) return null;
  if (typeof raw.relativeTime === "string" && raw.relativeTime) return relativeWindow(raw.relativeTime);
  if (!isMs(raw.absoluteStart) || !isMs(raw.absoluteEnd)) return null;
  const zone = (typeof raw.timezone === "string" && parseZoneId(raw.timezone)) || prefZone;
  return legacyWindow(raw.absoluteStart, raw.absoluteEnd, zone);
}

export function encodeStoredRange(w: TimeWindow): { version: 2; timeRange: SavedQueryTimeRange } {
  return {
    version: 2,
    timeRange: w.kind === "relative" ? { relative: w.spec } : { absolute: { start: w.start, end: w.end } },
  };
}

/** The saved-query and share decoder. `origin` carries what the old writer stored next to the ms. */
export function decodeStoredRange(range: SavedQueryTimeRange | null | undefined, origin: StoredRangeOrigin): TimeWindow | null {
  if (!range) return null;
  if (range.relative) return relativeWindow(range.relative);
  const start = range.absolute?.start;
  const end = range.absolute?.end;
  if (!isMs(start) || !isMs(end)) return null;
  if (origin.version >= 2) return absoluteWindow(start, end);
  if (origin.format === "saved-query") return legacyWindow(start, end, origin.prefZone);
  // A v1 share's non-UTC timezone was its author's browser zone, so its ms is the true instant.
  if (origin.timezone !== "UTC") return absoluteWindow(start, end);
  return legacyWindow(start, end, zoneFromPreference("utc"));
}
