import { describe, expect, it } from "vitest";

import { alertHistoryExploreQuery, evidenceExploreQuery } from "../alertExplore";

const entry = { triggered_at: "2026-10-15T06:08:00Z" };
const end = Date.parse(entry.triggered_at);

describe("alertHistoryExploreQuery", () => {
  it("opens the condition over the evaluation window", () => {
    const query = alertHistoryExploreQuery(
      { source_id: 7, editor_mode: "condition", condition_json: ' level="error" ', lookback_seconds: 300 },
      entry,
      3
    );

    expect(query).toEqual({
      team: "3",
      source: "7",
      start: String(end - 300_000),
      end: String(end),
      mode: "logchefql",
      q: 'level="error"',
    });
  });

  it("opens a native alert's window with an explicitly empty filter", () => {
    const query = alertHistoryExploreQuery(
      { source_id: 7, editor_mode: "native", condition_json: 'level="error"', lookback_seconds: 60 },
      entry,
      3
    );

    expect(query).toEqual({
      team: "3",
      source: "7",
      start: String(end - 60_000),
      end: String(end),
      mode: "logchefql",
      q: "",
    });
  });
});

describe("evidenceExploreQuery", () => {
  const evidence = {
    id: "E2" as const,
    tool: "top_values",
    team_id: 4,
    source_id: 9,
    summary: "19 of 23 from checkout-api",
    start: 1000,
    end: 2000,
  };

  it("opens a LogchefQL query in Search mode", () => {
    expect(evidenceExploreQuery({ ...evidence, mode: "logchefql", query: 'service="checkout-api"' })).toEqual({
      team: "4",
      source: "9",
      start: "1000",
      end: "2000",
      mode: "logchefql",
      q: 'service="checkout-api"',
    });
  });

  it("opens a native query in the SQL editor", () => {
    expect(evidenceExploreQuery({ ...evidence, mode: "native", query: "SELECT count() FROM logs" })).toEqual({
      team: "4",
      source: "9",
      start: "1000",
      end: "2000",
      mode: "native",
      sql: "SELECT count() FROM logs",
    });
  });

  it("names an empty query so the Explorer does not restore the draft", () => {
    expect(evidenceExploreQuery({ ...evidence, mode: "logchefql", query: "  " })).toEqual({
      team: "4",
      source: "9",
      start: "1000",
      end: "2000",
      mode: "logchefql",
      q: "",
    });
  });
});
