// Pin a fixed non-UTC "browser" timezone before importing modules under test
// so the #103 double-shift would surface if it regressed. See time-utils tests.
process.env.TZ = "Asia/Kolkata"; // UTC+05:30, no DST

import { describe, it, expect } from "vitest";
import { generateDefaultSql } from "@/services/SqlManager";
import { epochMs, zoneFromPreference, type ResolvedWindow } from "@/utils/timeWindow";

const range: ResolvedWindow = {
  start: epochMs(Date.UTC(2026, 6, 14, 4, 30)),
  end: epochMs(Date.UTC(2026, 6, 14, 6, 30)),
  zone: zoneFromPreference("utc"),
};

describe("generateDefaultSql — query timezone (#103)", () => {
  it("emits wall-clock strings paired with the window's (non-browser) zone", () => {
    const sql = generateDefaultSql({
      tableName: "logs.app",
      tsField: "timestamp",
      window: range,
      limit: 100,
    });

    expect(sql).toContain(
      "`timestamp` BETWEEN toDateTime('2026-07-14 04:30:00', 'UTC') " +
        "AND toDateTime('2026-07-14 06:30:00', 'UTC')"
    );
    // The old bug rendered the 04:30Z instant in browser-local IST (10:00).
    expect(sql).not.toContain("10:00:00");
    expect(sql).not.toContain("12:00:00");
  });
});
