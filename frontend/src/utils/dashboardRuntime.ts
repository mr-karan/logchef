import type { DashboardCachePolicy } from "@/api/meta";
import type { DashboardPanelQuery, DashboardQueryPanel, PanelInterval } from "@/api/dashboards";
import { calculateOptimalGranularity } from "@/services/HistogramService";
import { templateMacros } from "@/utils/template";
import {
  epochMs,
  parseRelativeSpec,
  relativeWindow,
  resolveWindow,
  toRfc3339,
  zoneOffsetMs,
  type EpochMs,
  type ResolvedWindow,
  type TimeWindow,
  type ZoneId,
} from "@/utils/timeWindow";

// Result queries default to 1,000 rows and accept up to 10,000.
const DEFAULT_TABLE_LIMIT = 1000;
const TABLE_LIMIT_MAX = 10000;

// Resolve a dashboard's EFFECTIVE result-cache TTL (seconds), combining the blob
// value with the server's advertised policy. Client and server apply the SAME
// clamp with the SAME max, so the snap bucket, the directive TTL, and the
// server-side cache TTL all coincide.
//
// FAILS CLOSED to 0 (no caching) whenever the policy is missing/disabled/
// malformed — this is deliberate. Guessing enabled/600 on a stale or old server
// would change the query-window semantics (snapping) and risk serving stale
// data against a cache the server may not even keep.
export function resolveEffectiveCacheTtl(
  blobTtl: number | null | undefined,
  policy: DashboardCachePolicy | null | undefined
): number {
  // Old/malformed server, or cache disabled → fail closed (no caching).
  if (!policy || policy.enabled !== true) return 0;
  if (!Number.isSafeInteger(policy.default_ttl_seconds) || policy.default_ttl_seconds < 1) return 0;
  if (!Number.isSafeInteger(policy.max_ttl_seconds) || policy.max_ttl_seconds < 1) return 0;

  if (blobTtl === 0) return 0; // explicit per-dashboard "off"
  const requested =
    blobTtl === undefined || blobTtl === null
      ? policy.default_ttl_seconds // dashboard didn't set one → server default
      : blobTtl;
  if (!Number.isSafeInteger(requested) || requested < 1) return 0; // fail closed on garbage
  return Math.min(requested, policy.max_ttl_seconds); // clamp to server max (same clamp the server applies)
}

// Compute the range actually executed for a refresh, given an injected `nowMs`.
// Pure + fake-clock testable. `baseStart`/`baseEnd` are the unsnapped range for
// this now (from the existing time parsing); `durationMs` is only meaningful for
// "rolling".
//
// - absolute → passthrough (never snapped; its key is naturally stable).
// - calendar (today/yesterday) → passthrough, PRESERVING calendar boundaries —
//   never subtract a rolling duration (which would move the day off its edge).
// - rolling + effTtl>0 → snap the end to the current TTL bucket so successive
//   refreshes within a bucket produce a byte-identical (cacheable) query.
// - rolling + effTtl===0 → no snap; a fresh moving window anchored on nowMs.
export function resolveAppliedRange(input: {
  kind: "rolling" | "calendar" | "absolute";
  baseStart: number;
  baseEnd: number;
  durationMs: number;
  effTtlSeconds: number;
  nowMs: number;
}): { start: number; end: number } {
  const { kind, baseStart, baseEnd, durationMs, effTtlSeconds, nowMs } = input;
  if (kind === "absolute" || kind === "calendar") {
    return { start: baseStart, end: baseEnd };
  }
  // rolling
  if (effTtlSeconds > 0) {
    const ttlMs = effTtlSeconds * 1000;
    const bucketEnd = Math.floor(nowMs / ttlMs) * ttlMs;
    return { start: bucketEnd - durationMs, end: bucketEnd };
  }
  return { start: nowMs - durationMs, end: nowMs };
}

export function classifyPanelError(err: unknown): { locked: boolean; message: string } {
  const asObj = (typeof err === "object" && err !== null ? err : {}) as {
    error_type?: string;
    message?: string;
  };
  const locked = asObj.error_type === "AuthorizationError";
  const message = asObj.message || (err instanceof Error ? err.message : "Failed to load panel");
  return { locked, message };
}

// Clamp a table panel's row cap into [1, TABLE_LIMIT_MAX]; absent/invalid
// falls back to the default. Guards against a legacy/hand-edited blob asking
// to render tens of thousands of DOM rows.
export function clampTableLimit(value: number | undefined): number {
  if (!value || !Number.isFinite(value) || value <= 0) return DEFAULT_TABLE_LIMIT;
  return Math.min(Math.max(Math.floor(value), 1), TABLE_LIMIT_MAX);
}

// Visualization edits reuse the query result.
export function execSignature(p: DashboardQueryPanel): string {
  return JSON.stringify([
    p.query.mode,
    p.query.text,
    p.query.language,
    p.team_id,
    p.source_id,
    p.query.limit ?? null,
    p.query.group_by ?? null,
    p.query.translate_limit ?? null,
    p.query.interval ?? null,
    p.query.range ?? null,
  ]);
}

/** The fixed values of `query.interval`: the histogram windows, shortest first. */
export const PANEL_INTERVALS: readonly { window: PanelInterval; seconds: number }[] = [
  { window: "1s", seconds: 1 }, { window: "5s", seconds: 5 }, { window: "10s", seconds: 10 },
  { window: "15s", seconds: 15 }, { window: "30s", seconds: 30 }, { window: "1m", seconds: 60 },
  { window: "5m", seconds: 300 }, { window: "10m", seconds: 600 }, { window: "15m", seconds: 900 },
  { window: "30m", seconds: 1800 }, { window: "1h", seconds: 3600 }, { window: "2h", seconds: 7200 },
  { window: "3h", seconds: 10800 }, { window: "6h", seconds: 21600 }, { window: "12h", seconds: 43200 },
  { window: "24h", seconds: 86400 },
];

/** A fixed interval may give at most this many buckets over the range. */
export const MAX_INTERVAL_BUCKETS = 1000;

/**
 * The window a panel queries with. `auto` keeps the rule of about 100 buckets.
 * A fixed interval that would give more than 1,000 buckets is raised to the
 * next window that fits, and `raised` says so.
 */
export function resolveInterval(
  interval: DashboardPanelQuery["interval"],
  range: { start: number; end: number },
): { window: PanelInterval; raised: boolean } {
  if (interval === undefined || interval === "auto") {
    return { window: calculateOptimalGranularity(toRfc3339(epochMs(range.start)), toRfc3339(epochMs(range.end))) as PanelInterval, raised: false };
  }
  const spanSeconds = (range.end - range.start) / 1000;
  const chosen = PANEL_INTERVALS.findIndex((entry) => entry.window === interval);
  const fits = PANEL_INTERVALS.find((entry, index) => index >= chosen && spanSeconds / entry.seconds <= MAX_INTERVAL_BUCKETS);
  const window = (fits ?? PANEL_INTERVALS[PANEL_INTERVALS.length - 1]).window;
  return { window, raised: window !== interval };
}

/** Resolves a selection to the range a refresh executes, snapped to the cache bucket when it rolls. */
export function resolveExecutedRange(window: TimeWindow, zone: ZoneId, nowMs: number, effTtlSeconds: number): ResolvedWindow {
  const { start, end } = resolveWindow(window, zone, epochMs(nowMs));
  // Calendar ranges keep their day boundaries and are never snapped.
  const kind = window.kind === "absolute" ? "absolute" : parseRelativeSpec(window.spec)?.kind === "calendar" ? "calendar" : "rolling";
  const applied = resolveAppliedRange({ kind, baseStart: start, baseEnd: end, durationMs: end - start, effTtlSeconds, nowMs });
  return { start: epochMs(applied.start), end: epochMs(applied.end), zone };
}

/**
 * The range a panel queries. A panel that sets `query.range` queries that
 * range in place of the dashboard range. A stored range that no longer parses
 * falls back to the dashboard range.
 */
export function resolvePanelRange(input: {
  panelRange: string | undefined;
  dashboardRange: ResolvedWindow;
  nowMs: number;
  effTtlSeconds: number;
}): ResolvedWindow {
  const own = input.panelRange ? relativeWindow(input.panelRange) : null;
  if (!own) return input.dashboardRange;
  return resolveExecutedRange(own, input.dashboardRange.zone, input.nowMs, input.effTtlSeconds);
}

/**
 * Whether the dashboard range reaches a panel's query. LogchefQL and LogsQL
 * always take the range. ClickHouse SQL takes it only through
 * `{{__time_filter}}`, or `{{__from}}` with `{{__to}}`.
 */
export function panelTimeBound(query: Pick<DashboardPanelQuery, "language" | "text">): boolean {
  if (query.language !== "clickhouse-sql") return true;
  const macros = templateMacros(query.text, "clickhouse-sql");
  return macros.has("__time_filter") || (macros.has("__from") && macros.has("__to"));
}

const DAY_MS = 86_400_000;

/**
 * The first change of the zone's UTC offset inside the range, such as a
 * daylight-saving change. VictoriaLogs count buckets keep the offset of the
 * range start, so after the change they sit off local midnight.
 */
export function zoneOffsetChange(range: ResolvedWindow): { at: EpochMs; beforeMs: number; afterMs: number } | null {
  // Offset changes are weeks apart, so a coarse walk finds the first one.
  const stride = range.end - range.start > 400 * DAY_MS ? 7 * DAY_MS : DAY_MS;
  let previous: number = range.start;
  const startOffset = zoneOffsetMs(range.start, range.zone);
  while (previous < range.end) {
    const next = Math.min(previous + stride, range.end);
    if (zoneOffsetMs(epochMs(next), range.zone) !== startOffset) {
      let low = previous;
      let high = next;
      // `high` ends on the first millisecond with the new offset.
      while (high - low > 1) {
        const middle = Math.floor((low + high) / 2);
        if (zoneOffsetMs(epochMs(middle), range.zone) === startOffset) low = middle;
        else high = middle;
      }
      return { at: epochMs(high), beforeMs: startOffset, afterMs: zoneOffsetMs(epochMs(high), range.zone) };
    }
    previous = next;
  }
  return null;
}
