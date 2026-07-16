import { describe, it, expect } from "vitest";
import type { QueryHistoryRecord } from "@/api/explore";
import {
  buildHistoryRerunQuery,
  formatHistoryTimeAgo,
  formatHistoryDuration,
} from "@/lib/queryHistory";

function makeEntry(overrides: Partial<QueryHistoryRecord> = {}): QueryHistoryRecord {
  return {
    id: 1,
    team_id: 3,
    source_id: 7,
    query_text: "level=error",
    query_language: "logchefql",
    duration_ms: 42,
    row_count: 10,
    created_at: "2026-07-14 10:00:00",
    ...overrides,
  };
}

describe("buildHistoryRerunQuery", () => {
  it("maps a LogchefQL entry to the q param without a mode", () => {
    const query = buildHistoryRerunQuery(makeEntry({ query_language: "logchefql", query_text: "foo=bar" }));
    expect(query).toEqual({ team: "3", source: "7", q: "foo=bar" });
    expect(query.mode).toBeUndefined();
    expect(query.sql).toBeUndefined();
  });

  it("maps a ClickHouse SQL entry to native mode + sql param", () => {
    const query = buildHistoryRerunQuery(
      makeEntry({ query_language: "clickhouse-sql", query_text: "SELECT 1" })
    );
    expect(query).toEqual({ team: "3", source: "7", mode: "native", sql: "SELECT 1" });
  });

  it("maps a LogsQL entry to native mode + sql param", () => {
    const query = buildHistoryRerunQuery(
      makeEntry({ query_language: "logsql", query_text: "_time:5m" })
    );
    expect(query).toEqual({ team: "3", source: "7", mode: "native", sql: "_time:5m" });
  });
});

describe("formatHistoryTimeAgo", () => {
  const base = new Date("2026-07-14T10:00:00Z").getTime();

  it("renders minutes, hours and days relative to now", () => {
    expect(formatHistoryTimeAgo("2026-07-14 09:59:40", base)).toBe("just now");
    expect(formatHistoryTimeAgo("2026-07-14 09:55:00", base)).toBe("5m ago");
    expect(formatHistoryTimeAgo("2026-07-14 07:00:00", base)).toBe("3h ago");
    expect(formatHistoryTimeAgo("2026-07-12 10:00:00", base)).toBe("2d ago");
  });

  it("handles already-ISO timestamps", () => {
    expect(formatHistoryTimeAgo("2026-07-14T09:55:00Z", base)).toBe("5m ago");
  });
});

describe("formatHistoryDuration", () => {
  it("uses ms below a second and seconds above", () => {
    expect(formatHistoryDuration(42)).toBe("42 ms");
    expect(formatHistoryDuration(1500)).toBe("1.50 s");
    expect(formatHistoryDuration(12000)).toBe("12.0 s");
  });
});
