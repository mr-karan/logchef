import type { Alert, AlertHistoryEntry } from "@/api/alerts";

type AlertQueryFields = Pick<Alert, "source_id" | "editor_mode" | "condition_json" | "lookback_seconds">;

// Explorer query for the logs in the evaluation window that produced a history entry.
// Native alerts hold an aggregate query, so only the source and window carry over.
export function alertHistoryExploreQuery(
  alert: AlertQueryFields,
  entry: Pick<AlertHistoryEntry, "triggered_at">,
  teamId: number
): Record<string, string> {
  const end = Date.parse(entry.triggered_at);
  const query: Record<string, string> = {
    team: String(teamId),
    source: String(alert.source_id),
    start: String(end - alert.lookback_seconds * 1000),
    end: String(end),
  };

  const condition = alert.editor_mode === "condition" ? alert.condition_json?.trim() : "";
  if (condition) {
    query.mode = "logchefql";
    query.q = condition;
  }

  return query;
}
