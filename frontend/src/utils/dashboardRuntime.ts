import type { DashboardCachePolicy } from "@/api/meta";
import type { DashboardPanel } from "@/api/dashboards";
import { isSuccessResponse, type APIResponse } from "@/api/types";

// Table panel row cap. The builder's limit option maxes at 1000; a hand-edited
// or legacy blob could carry anything (a saved limit:100000 would try to render
// 100k rows), so the runtime clamps into [1, TABLE_LIMIT_MAX] regardless.
const DEFAULT_TABLE_LIMIT = 100;
const TABLE_LIMIT_MAX = 1000;

// Stable, key-order-independent serialization used for the dirty comparison. The
// reflow step re-emits object keys in a different order than the loaded blob, so
// a plain JSON.stringify would report a semantically-identical draft as dirty.
export function stableStringify(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }
  if (value && typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>).sort();
    return `{${keys
      .map((k) => `${JSON.stringify(k)}:${stableStringify((value as Record<string, unknown>)[k])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

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

// Signature of the fields that determine a panel's fetched result. Two panels
// with the same signature render identically, so panelState is reusable.
export function execSignature(p: DashboardPanel): string {
  return JSON.stringify([
    p.type,
    p.query,
    p.query_language,
    p.team_id,
    p.source_id,
    p.options?.limit ?? null,
    p.options?.group_by ?? null,
  ]);
}

export function unwrap<T>(resp: APIResponse<T>): T {
  if (isSuccessResponse(resp)) {
    return resp.data as T;
  }
  // Non-2xx responses reject before this; a 200 with status:"error" lands here.
  throw resp;
}
