import type { QueryStats } from "@/api/explore";

/**
 * The chip for a result's truncation state, as an i18n key and the plural
 * count to render it with, or null when nothing was cut.
 */
export function truncationMessage(
  stats: Pick<QueryStats, "truncation" | "limit_applied" | "rows_returned"> | null | undefined
): { key: string; count: number } | null {
  switch (stats?.truncation) {
    case "rows":
      return { key: "ui.truncationRows", count: stats.rows_returned ?? stats.limit_applied ?? 0 };
    case "bytes":
      return { key: "ui.truncationBytes", count: 0 };
    case "unknown":
      return { key: "ui.truncationUnknown", count: 0 };
    default:
      return null;
  }
}
