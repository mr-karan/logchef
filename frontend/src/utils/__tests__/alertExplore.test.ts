import { describe, expect, it } from "vitest";

import { alertHistoryExploreQuery } from "../alertExplore";

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

  it("carries only the source and window for native alerts", () => {
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
    });
  });
});
