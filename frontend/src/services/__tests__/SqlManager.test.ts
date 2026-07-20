// Pin a fixed non-UTC "browser" timezone before importing modules under test
// so the #103 double-shift would surface if it regressed. See time-utils tests.
process.env.TZ = "Asia/Kolkata"; // UTC+05:30, no DST

import { describe, it, expect } from "vitest";
import { CalendarDateTime } from "@internationalized/date";
import { SqlManager } from "@/services/SqlManager";
import type { TimeRange } from "@/types/query";

const range: TimeRange = {
  start: new CalendarDateTime(2026, 7, 14, 4, 30, 0),
  end: new CalendarDateTime(2026, 7, 14, 6, 30, 0),
};

describe("SqlManager.generateDefaultSql — query timezone (#103)", () => {
  it("emits wall-clock strings paired with the requested (non-browser) timezone", () => {
    const { success, sql } = SqlManager.generateDefaultSql({
      tableName: "logs.app",
      tsField: "timestamp",
      timeRange: range,
      limit: 100,
      timezone: "UTC",
    });

    expect(success).toBe(true);
    expect(sql).toContain(
      "`timestamp` BETWEEN toDateTime('2026-07-14 04:30:00', 'UTC') " +
        "AND toDateTime('2026-07-14 06:30:00', 'UTC')"
    );
    // The old bug rendered the 04:30Z instant in browser-local IST (10:00).
    expect(sql).not.toContain("10:00:00");
    expect(sql).not.toContain("12:00:00");
  });
});

describe("SqlManager.updateTimeRange — replaces existing window without shift (#103)", () => {
  it("swaps the toDateTime BETWEEN window using the requested timezone", () => {
    const original =
      "SELECT * FROM logs.app\n" +
      "WHERE `timestamp` BETWEEN toDateTime('2020-01-01 00:00:00', 'UTC') " +
      "AND toDateTime('2020-01-01 01:00:00', 'UTC')\n" +
      "ORDER BY `timestamp` DESC\nLIMIT 100";

    const updated = SqlManager.updateTimeRange({
      sql: original,
      tsField: "timestamp",
      timeRange: range,
      timezone: "UTC",
    });

    expect(updated).toContain("toDateTime('2026-07-14 04:30:00', 'UTC')");
    expect(updated).toContain("toDateTime('2026-07-14 06:30:00', 'UTC')");
    expect(updated).not.toContain("2020-01-01");
    expect(updated).not.toContain("10:00:00");
  });
});
