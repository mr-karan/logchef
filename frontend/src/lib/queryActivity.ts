import type { RouteLocationRaw } from "vue-router";
import type { QueryTotals, ShapeFilterItem, UserQueryStat } from "@/api/admin";

/** Average latency in ms, or null when nothing ran. */
export function averageMs(totals: QueryTotals): number | null {
  return totals.query_count > 0 ? totals.total_duration_ms / totals.query_count : null;
}

/**
 * How a value moved against the prior window. "new" when the prior window
 * had none, null when there is nothing to compare.
 */
export type Change = { kind: "new" } | { kind: "percent"; percent: number } | null;

export function changeOf(current: number | null, prior: number | null): Change {
  if (current === null || prior === null) return null;
  if (prior === 0) return current > 0 ? { kind: "new" } : null;
  return { kind: "percent", percent: ((current - prior) / prior) * 100 };
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

/** Audit Log links for query.run events over the same number of days. */
export function auditLink(days: number, filter: { actor_id?: number; source_id?: number; outcome?: "failure" } = {}): RouteLocationRaw {
  const query: Record<string, string> = { action: "query.run", t: `${days}d` };
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
