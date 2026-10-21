import { describe, it, expect } from "vitest";
import type { QueryHistoryRecord } from "@/api/explore";
import {
  buildHistoryRerunQuery,
  formatHistoryTimeAgo,
  formatHistoryDuration,
} from "@/lib/queryHistory";
import { parseZoneId, type ZoneId } from "@/utils/timeWindow";

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

function zone(name: string): ZoneId {
  const parsed = parseZoneId(name);
  if (!parsed) throw new Error(`no zone data for ${name}`);
  return parsed;
}

describe("formatHistoryTimeAgo", () => {
  const utc = zone("UTC");
  const base = new Date("2026-07-14T10:00:00Z").getTime();

  it("renders minutes, hours and days relative to now", () => {
    const relativeTime = new Intl.RelativeTimeFormat("en", { numeric: "auto", style: "short" });
    expect(formatHistoryTimeAgo("2026-07-14 09:59:40", utc, base)).toBe(relativeTime.format(0, "second"));
    expect(formatHistoryTimeAgo("2026-07-14 09:55:00", utc, base)).toBe(relativeTime.format(-5, "minute"));
    expect(formatHistoryTimeAgo("2026-07-14 07:00:00", utc, base)).toBe(relativeTime.format(-3, "hour"));
    expect(formatHistoryTimeAgo("2026-07-12 10:00:00", utc, base)).toBe(relativeTime.format(-2, "day"));
  });

  it("handles already-ISO timestamps", () => {
    const relativeTime = new Intl.RelativeTimeFormat("en", { numeric: "auto", style: "short" });
    expect(formatHistoryTimeAgo("2026-07-14T09:55:00Z", utc, base)).toBe(relativeTime.format(-5, "minute"));
  });

  it("shows an entry older than a week as a date in the given zone", () => {
    expect(formatHistoryTimeAgo("2026-07-01T23:30:00Z", utc, base)).toBe("Jul 1, 2026");
    expect(formatHistoryTimeAgo("2026-07-01T23:30:00Z", zone("Asia/Kolkata"), base)).toBe("Jul 2, 2026");
  });

  it("formats relative time using the selected locale", () => {
    expect(formatHistoryTimeAgo("2026-07-14 09:55:00", utc, base, "zh-CN")).toBe("5分钟前");
  });
});

describe("formatHistoryDuration", () => {
  it("uses ms below a second and seconds above", () => {
    expect(formatHistoryDuration(42)).toBe("42 ms");
    expect(formatHistoryDuration(1500)).toBe("1.50 s");
    expect(formatHistoryDuration(12000)).toBe("12.0 s");
  });
});
