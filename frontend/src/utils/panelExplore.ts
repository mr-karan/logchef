import type { DashboardQueryPanel } from "@/api/dashboards";
import type { ResolvedWindow } from "@/utils/timeWindow";
import { exploreWindowQuery } from "@/utils/alertExplore";

type PanelQueryFields = Pick<DashboardQueryPanel, "team_id" | "source_id" | "query">;

// Explorer query for the logs a panel shows, over the window the panel ran.
export function panelExploreQuery(panel: PanelQueryFields, window: Pick<ResolvedWindow, "start" | "end">): Record<string, string> {
  let text = panel.query.text.trim();
  if (panel.query.language === "logsql") {
    // Count panels run through the histogram endpoint, which ignores LogsQL
    // pipes, so they count only the filter.
    if (panel.query.mode === "count") text = logsqlFilter(text);
    // The backend runs an empty LogsQL query as "*", but Explorer will not run
    // an empty native query, so send the match-all filter it stands for.
    text ||= "*";
  }
  return exploreWindowQuery({
    teamId: panel.team_id,
    sourceId: panel.source_id,
    start: window.start,
    end: window.end,
    mode: panel.query.language === "logchefql" ? "logchefql" : "native",
    query: text,
  });
}

// The part of a LogsQL query before its first top-level pipe. A "|" inside a
// quoted string, a regex, or a parenthesized subquery is part of the filter.
function logsqlFilter(query: string): string {
  let quote: string | null = null;
  let depth = 0;
  for (let i = 0; i < query.length; i++) {
    const ch = query[i];
    if (quote) {
      if (ch === "\\" && quote !== "`") i++;
      else if (ch === quote) quote = null;
    } else if (ch === '"' || ch === "'" || ch === "`") {
      quote = ch;
    } else if (ch === "(") {
      depth++;
    } else if (ch === ")") {
      depth = Math.max(0, depth - 1);
    } else if (ch === "|" && depth === 0) {
      return query.slice(0, i).trim();
    }
  }
  return query.trim();
}
