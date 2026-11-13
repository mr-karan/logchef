import { describe, expect, it } from "vitest";

import { panelExploreQuery } from "../panelExplore";
import { epochMs } from "../timeWindow";
import type { DashboardPanelQuery, PanelQueryLanguage } from "@/api/dashboards";

const range = { start: epochMs(1_760_500_000_000), end: epochMs(1_760_503_600_000) };

function count(text: string, language: PanelQueryLanguage = "logchefql"): DashboardPanelQuery {
  return { mode: "count", language, text, interval: "auto" };
}

function result(text: string, language: PanelQueryLanguage): DashboardPanelQuery {
  return { mode: "result", language, text, limit: 100 };
}

describe("panelExploreQuery", () => {
  it("opens a LogchefQL panel as a LogchefQL query", () => {
    const query = panelExploreQuery({ team_id: 3, source_id: 7, query: count(' level="error" ') }, range);

    expect(query).toEqual({
      team: "3",
      source: "7",
      start: String(range.start),
      end: String(range.end),
      mode: "logchefql",
      q: 'level="error"',
    });
  });

  it.each(["clickhouse-sql", "logsql"] as const)("opens a %s panel in native mode", (language) => {
    const query = panelExploreQuery({ team_id: 3, source_id: 7, query: count("SELECT 1", language) }, range);

    expect(query.mode).toBe("native");
    expect(query.sql).toBe("SELECT 1");
    expect(query.q).toBeUndefined();
  });

  it("opens an empty LogsQL panel as the match-all filter", () => {
    const query = panelExploreQuery({ team_id: 3, source_id: 7, query: count("", "logsql") }, range);

    expect(query.mode).toBe("native");
    expect(query.sql).toBe("*");
  });

  it.each([
    ["drops pipes a count panel ignores", count("error | stats count()", "logsql"), "error"],
    ["keeps a pipe inside quotes", count('_msg:"a|b" | sort by (_time)', "logsql"), '_msg:"a|b"'],
    [
      "keeps a pipe inside a subquery",
      count("user_id:in(level:error | fields user_id) | stats count()", "logsql"),
      "user_id:in(level:error | fields user_id)",
    ],
    ["opens a pipes-only filter as match-all", count("| stats count()", "logsql"), "*"],
    ["keeps the full query for a result panel", result("error | sort by (_time)", "logsql"), "error | sort by (_time)"],
  ] as const)("%s", (_name, query, want) => {
    const explore = panelExploreQuery({ team_id: 3, source_id: 7, query }, range);

    expect(explore.sql).toBe(want);
  });

  it("opens an empty query as all logs in the range", () => {
    const query = panelExploreQuery({ team_id: 3, source_id: 7, query: count("  ") }, range);

    expect(query).toEqual({
      team: "3",
      source: "7",
      start: String(range.start),
      end: String(range.end),
      mode: "logchefql",
      q: "",
    });
  });
});
