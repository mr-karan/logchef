import { describe, expect, it } from "vitest";
import { epochMs, parseZoneId, type TimeWindow, type ZoneId } from "../timeWindow";
import { decodeDraftWindow, decodeStoredRange, decodeWindowFromUrl, encodeStoredRange, encodeWindowForUrl } from "../timeWindowCodec";

// The reading browser runs in New York (EDT, -04:00, in October).
process.env.TZ = "America/New_York";

function zone(name: string): ZoneId {
  const parsed = parseZoneId(name);
  if (!parsed) throw new Error(`bad zone ${name}`);
  return parsed;
}

const UTC = zone("UTC");
const NEW_YORK = zone("America/New_York");
const at = (iso: string) => Date.parse(iso);
const absolute = (start: number, end: number): TimeWindow => ({ kind: "absolute", start: epochMs(start), end: epochMs(end) });

// The stored v1 ms: 04:00Z and 05:00Z, whose New York browser fields are 00:00 and 01:00.
const STORED_START = at("2026-10-19T04:00:00Z");
const STORED_END = at("2026-10-19T05:00:00Z");
const range = { absolute: { start: STORED_START, end: STORED_END } };
// The second 01:30 of the New York fall-back day.
const FOLD = absolute(at("2026-11-01T06:30:00Z"), at("2026-11-01T07:15:00Z"));

describe("URL codec", () => {
  it("reads start and end ms as instants, which chips, alerts and the CLI write", () => {
    expect(decodeWindowFromUrl({ start: String(STORED_START), end: String(STORED_END) })).toEqual(absolute(STORED_START, STORED_END));
    expect(decodeWindowFromUrl({ start_time: String(STORED_START), end_time: String(STORED_END) })).toEqual(absolute(STORED_START, STORED_END));
  });

  it("reads a relative spec, and rejects junk or a reversed window", () => {
    expect(decodeWindowFromUrl({ t: "today" })).toEqual({ kind: "relative", spec: "today" });
    expect(decodeWindowFromUrl({ time: "1h" })).toEqual({ kind: "relative", spec: "1h" });
    expect(decodeWindowFromUrl({ t: "soon" })).toBeNull();
    expect(decodeWindowFromUrl({ start: "abc", end: "2" })).toBeNull();
    expect(decodeWindowFromUrl({ start: "5", end: "2" })).toBeNull();
    expect(decodeWindowFromUrl({})).toBeNull();
  });

  it("round-trips a DST fold window to the same instants", () => {
    const encoded = encodeWindowForUrl(FOLD);
    expect(decodeWindowFromUrl(encoded)).toEqual(FOLD);
  });
});

describe("draft codec", () => {
  it("reads a v2 window exactly, including a DST fold", () => {
    expect(decodeDraftWindow({ version: 2, window: FOLD }, UTC)).toEqual(FOLD);
    expect(decodeDraftWindow({ version: 2, window: { kind: "relative", spec: "7d" } }, UTC)).toEqual({ kind: "relative", spec: "7d" });
  });

  it("reads v1 browser-field ms in the draft's zone, as the old reader did", () => {
    // Fields 00:00 and 01:00 read as UTC wall clock.
    expect(decodeDraftWindow({ version: 1, absoluteStart: STORED_START, absoluteEnd: STORED_END, timezone: null }, UTC)).toEqual(
      absolute(at("2026-10-19T00:00:00Z"), at("2026-10-19T01:00:00Z")),
    );
    // Fields 00:00 and 01:00 read as IST wall clock.
    expect(decodeDraftWindow({ version: 1, absoluteStart: STORED_START, absoluteEnd: STORED_END, timezone: "Asia/Kolkata" }, UTC)).toEqual(
      absolute(at("2026-10-18T18:30:00Z"), at("2026-10-18T19:30:00Z")),
    );
    // A local-preference author: the fields are browser wall clock, so the ms come back unchanged.
    expect(decodeDraftWindow({ version: 1, absoluteStart: STORED_START, absoluteEnd: STORED_END, timezone: null }, NEW_YORK)).toEqual(
      absolute(STORED_START, STORED_END),
    );
  });

  it("reads a v1 relative draft, and rejects unknown versions", () => {
    expect(decodeDraftWindow({ version: 1, relativeTime: "yesterday" }, UTC)).toEqual({ kind: "relative", spec: "yesterday" });
    expect(decodeDraftWindow({ version: 3, window: FOLD }, UTC)).toBeNull();
    expect(decodeDraftWindow("nope", UTC)).toBeNull();
  });
});

describe("saved query and share codec", () => {
  it("reads a saved query v1 in the reader's preference zone, as the old reader did", () => {
    expect(decodeStoredRange(range, { format: "saved-query", version: 1, prefZone: UTC })).toEqual(
      absolute(at("2026-10-19T00:00:00Z"), at("2026-10-19T01:00:00Z")),
    );
    expect(decodeStoredRange(range, { format: "saved-query", version: 1, prefZone: NEW_YORK })).toEqual(absolute(STORED_START, STORED_END));
  });

  it("reads a share v1 with a non-UTC timezone as the true instant, for every reader", () => {
    expect(decodeStoredRange(range, { format: "share", version: 1, timezone: "Asia/Kolkata" })).toEqual(absolute(STORED_START, STORED_END));
  });

  it("reads a share v1 with a UTC timezone as the old reader did", () => {
    expect(decodeStoredRange(range, { format: "share", version: 1, timezone: "UTC" })).toEqual(
      absolute(at("2026-10-19T00:00:00Z"), at("2026-10-19T01:00:00Z")),
    );
  });

  it("reads v2 saved queries and shares exactly, including a DST fold", () => {
    const { version, timeRange } = encodeStoredRange(FOLD);
    expect(version).toBe(2);
    expect(decodeStoredRange(timeRange, { format: "saved-query", version, prefZone: UTC })).toEqual(FOLD);
    expect(decodeStoredRange(timeRange, { format: "share", version, timezone: "UTC" })).toEqual(FOLD);
  });

  it("keeps relative ranges and rejects missing or broken ones", () => {
    const relative = encodeStoredRange({ kind: "relative", spec: "today" }).timeRange;
    expect(decodeStoredRange(relative, { format: "share", version: 1, timezone: "UTC" })).toEqual({ kind: "relative", spec: "today" });
    expect(decodeStoredRange(null, { format: "saved-query", version: 2, prefZone: UTC })).toBeNull();
    expect(decodeStoredRange({ relative: "bogus" }, { format: "saved-query", version: 2, prefZone: UTC })).toBeNull();
    expect(decodeStoredRange({ absolute: { start: 9, end: 1 } }, { format: "saved-query", version: 2, prefZone: UTC })).toBeNull();
  });
});
