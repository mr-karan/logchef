import { describe, expect, it } from "vitest";

import { panelExploreQuery } from "../panelExplore";
import { epochMs } from "../timeWindow";

const range = { start: epochMs(1_760_500_000_000), end: epochMs(1_760_503_600_000) };

describe("panelExploreQuery", () => {
  it("opens a LogchefQL panel as a LogchefQL query", () => {
    const query = panelExploreQuery(
      { type: "stat", team_id: 3, source_id: 7, query: ' level="error" ', query_language: "logchefql" },
      range
    );

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
    const query = panelExploreQuery({ type: "stat", team_id: 3, source_id: 7, query: "SELECT 1", query_language: language }, range);

    expect(query.mode).toBe("native");
    expect(query.sql).toBe("SELECT 1");
    expect(query.q).toBeUndefined();
  });

  it("opens an empty LogsQL panel as the match-all filter", () => {
    const query = panelExploreQuery({ type: "stat", team_id: 3, source_id: 7, query: "", query_language: "logsql" }, range);

    expect(query.mode).toBe("native");
    expect(query.sql).toBe("*");
  });

  it.each([
    ["drops pipes a stat panel ignores", "stat", 'error | stats count()', "error"],
    ["keeps a pipe inside quotes", "timeseries", '_msg:"a|b" | sort by (_time)', '_msg:"a|b"'],
    [
      "keeps a pipe inside a subquery",
      "stat",
      "user_id:in(level:error | fields user_id) | stats count()",
      "user_id:in(level:error | fields user_id)",
    ],
    ["opens a pipes-only filter as match-all", "breakdown", "| stats count()", "*"],
    ["keeps the full query for a table panel", "table", "error | sort by (_time)", "error | sort by (_time)"],
  ] as const)("%s", (_name, type, text, want) => {
    const query = panelExploreQuery({ type, team_id: 3, source_id: 7, query: text, query_language: "logsql" }, range);

    expect(query.sql).toBe(want);
  });

  it("opens an empty query as all logs in the range", () => {
    const query = panelExploreQuery({ type: "stat", team_id: 3, source_id: 7, query: "  ", query_language: "logchefql" }, range);

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
