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

// Explorer route query for a source, a window in Unix ms, and a query. The
// query is always named, even when empty, so the Explorer opens exactly it
// instead of the user's saved draft.
export function exploreWindowQuery(target: ExploreWindow): Record<string, string> {
  const mode = target.mode ?? "logchefql";
  return {
    team: String(target.teamId),
    source: String(target.sourceId),
    start: String(target.start),
    end: String(target.end),
    mode,
    [mode === "native" ? "sql" : "q"]: target.query?.trim() ?? "",
  };
}

// The window of the evaluation that produced a history entry. Native alerts hold
// an aggregate query, so only a condition alert carries its query over; a
// native alert opens its window with no filter.
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
