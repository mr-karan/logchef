import type { Alert, AlertHistoryEntry } from "@/api/alerts";
import type { EvidenceRecord, QueryMode } from "@/api/askChef";

type AlertQueryFields = Pick<Alert, "source_id" | "editor_mode" | "condition_json" | "lookback_seconds">;

export interface ExploreWindow {
  teamId: number;
  sourceId: number;
  start: number;
  end: number;
  mode?: QueryMode;
  query?: string;
}

// Explorer route query for a source, a window in Unix ms, and an optional query.
export function exploreWindowQuery(target: ExploreWindow): Record<string, string> {
  const query: Record<string, string> = {
    team: String(target.teamId),
    source: String(target.sourceId),
    start: String(target.start),
    end: String(target.end),
  };
  const text = target.query?.trim();
  if (target.mode && text) {
    query.mode = target.mode;
    query[target.mode === "native" ? "sql" : "q"] = text;
  }
  return query;
}

// The window of the evaluation that produced a history entry. Native alerts hold
// an aggregate query, so only a condition alert carries its query over.
export function alertHistoryWindow(alert: AlertQueryFields, entry: Pick<AlertHistoryEntry, "triggered_at">, teamId: number): ExploreWindow {
  const end = Date.parse(entry.triggered_at);
  const condition = alert.editor_mode === "condition" ? alert.condition_json?.trim() : "";
  return {
    teamId,
    sourceId: alert.source_id,
    start: end - alert.lookback_seconds * 1000,
    end,
    ...(condition ? { mode: "logchefql", query: condition } : {}),
  };
}

export function alertHistoryExploreQuery(
  alert: AlertQueryFields,
  entry: Pick<AlertHistoryEntry, "triggered_at">,
  teamId: number
): Record<string, string> {
  return exploreWindowQuery(alertHistoryWindow(alert, entry, teamId));
}

export function evidenceExploreQuery(evidence: EvidenceRecord): Record<string, string> {
  return exploreWindowQuery({
    teamId: evidence.team_id,
    sourceId: evidence.source_id,
    start: evidence.start,
    end: evidence.end,
    mode: evidence.mode,
    query: evidence.query,
  });
}
