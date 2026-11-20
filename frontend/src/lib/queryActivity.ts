import type { RouteLocationRaw } from "vue-router";
import type { DateWindow, Percentile, QueryTotals, ShapeFilterItem, UserQueryStat } from "@/api/admin";
import { formatHistoryDuration } from "@/lib/queryHistory";
import { formatNumber } from "@/utils/numberFormat";

/** Average latency in ms, or null when nothing ran. */
export function averageMs(totals: QueryTotals): number | null {
  return totals.query_count > 0 ? totals.total_duration_ms / totals.query_count : null;
}

// Past this rise a change reads as a multiple, such as 26×.
const MAX_PERCENT = 999;

/**
 * How a value moved against the prior window. "new" when the prior window
 * had none, a multiple past a 999% rise, null when there is nothing to
 * compare.
 */
export type Change = { kind: "new" } | { kind: "percent"; percent: number } | { kind: "multiple"; factor: number } | null;

export function changeOf(current: number | null, prior: number | null): Change {
  if (current === null || prior === null) return null;
  if (prior === 0) return current > 0 ? { kind: "new" } : null;
  const percent = ((current - prior) / prior) * 100;
  return percent > MAX_PERCENT ? { kind: "multiple", factor: current / prior } : { kind: "percent", percent };
}

/** A percentile in the open bucket reads as more than its bound. */
export function formatPercentile(p: Percentile, locale: string): string {
  const text = formatHistoryDuration(p.ms, locale);
  return p.open ? `>${text}` : text;
}

/** A 0..1 rate as a percent with one decimal. */
export function formatRate(rate: number, locale: string): string {
  return `${(rate * 100).toLocaleString(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`;
}

export function formatBytes(bytes: number, locale: string): string {
  return formatNumber(Math.round(bytes), { unit: "bytes" }, { locale }).text;
}

/** A user's display name: a service account's synthetic email never shows. */
export function userName(user: Pick<UserQueryStat, "full_name" | "email" | "account_type">): string {
  if (user.account_type === "service") return user.full_name;
  return user.full_name || user.email;
}

export function shapeColumn(item: ShapeFilterItem): string {
  if (!item.path?.length) return item.column;
  return item.access === "map" ? `${item.column}['${item.path.join("']['")}']` : [item.column, ...item.path].join(".");
}

const DAY_MS = 86_400_000;

/**
 * Audit Log links for query.run events over the same whole UTC days as
 * window: from its first midnight to the midnight after its last day.
 */
export function auditLink(window: DateWindow, filter: { actor_id?: number | null; source_id?: number | null; outcome?: "failure" } = {}): RouteLocationRaw {
  const query: Record<string, string> = {
    action: "query.run",
    start: String(Date.parse(window.since)),
    end: String(Date.parse(window.until) + DAY_MS),
  };
  if (filter.actor_id) query.actor_id = String(filter.actor_id);
  if (filter.source_id) {
    query.target_type = "source";
    query.target_id = String(filter.source_id);
  }
  if (filter.outcome) query.outcome = filter.outcome;
  return { name: "AdminAuditLog", query };
}

export function advisorLink(sourceId: number): RouteLocationRaw {
  return { name: "SourceInspection", query: { sourceId: String(sourceId) } };
}

/** A lookback such as 30d, 6h or 15m, rounded to its largest unit. */
export function formatLookback(seconds: number): string {
  for (const [unit, size] of [["d", 86_400], ["h", 3_600], ["m", 60]] as const) {
    if (seconds >= size) return `${Math.round(seconds / size)}${unit}`;
  }
  return `${Math.round(seconds)}s`;
}

/** A summed duration: minutes or hours past a minute, else null. */
export function formatTotalTime(ms: number, locale: string): string | null {
  const format = (value: number, unit: string) => `${value.toLocaleString(locale, { maximumFractionDigits: 1 })} ${unit}`;
  if (ms >= 3_600_000) return format(ms / 3_600_000, "h");
  if (ms >= 60_000) return format(ms / 60_000, "min");
  return null;
}
