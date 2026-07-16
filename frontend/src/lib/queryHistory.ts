import type { QueryHistoryRecord } from "@/api/explore";
import { getExploreModeForQueryLanguage } from "@/lib/queryMetadata";

/**
 * Build the router query params that re-run a query-history entry in the log
 * explorer. The explorer's URL watcher (useUrlState) picks these params up,
 * switches team/source as needed, loads the query into the editor, and
 * executes it — so re-run works even when the entry belongs to a different
 * team or source than the one currently open.
 *
 * History rows carry no time range or limit, so those are intentionally
 * omitted: the explorer keeps whatever range/limit is currently active.
 */
export function buildHistoryRerunQuery(entry: QueryHistoryRecord): Record<string, string> {
  const mode = getExploreModeForQueryLanguage(entry.query_language);
  const query: Record<string, string> = {
    team: String(entry.team_id),
    source: String(entry.source_id),
  };

  if (mode === "native") {
    // clickhouse-sql and logsql both live in the explorer's "native" editor,
    // which reads its content from the `sql` param.
    query.mode = "native";
    query.sql = entry.query_text;
  } else {
    query.q = entry.query_text;
  }

  return query;
}

/**
 * Human-readable relative time ("just now", "5m ago", "3h ago", "2d ago") for a
 * history entry's created_at. Falls back to a locale date string for anything
 * older than a week. Accepts the backend's space-separated timestamps as well
 * as ISO strings.
 */
export function formatHistoryTimeAgo(createdAt: string, nowMs: number = Date.now()): string {
  const normalized =
    createdAt.includes("T") || createdAt.includes("Z") || createdAt.includes("+")
      ? createdAt
      : createdAt.replace(" ", "T") + "Z";

  const then = new Date(normalized).getTime();
  if (Number.isNaN(then)) {
    return "";
  }

  const diffMs = Math.max(0, nowMs - then);
  const diffMinutes = Math.floor(diffMs / 60_000);
  const diffHours = Math.floor(diffMs / 3_600_000);
  const diffDays = Math.floor(diffMs / 86_400_000);

  if (diffMinutes < 1) return "just now";
  if (diffMinutes < 60) return `${diffMinutes}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return new Date(then).toLocaleDateString();
}

/**
 * Compact duration label for a query's execution time in milliseconds.
 */
export function formatHistoryDuration(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs < 0) return "—";
  if (durationMs < 1000) return `${Math.round(durationMs)} ms`;
  return `${(durationMs / 1000).toFixed(durationMs < 10_000 ? 2 : 1)} s`;
}
