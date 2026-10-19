import { describe, expect, it } from "vitest";
import { generateCliCommand } from "../cliCommand";
import { epochMs, relativeWindow, resolveWindow, zoneFromPreference, type TimeWindow } from "../timeWindow";

// A non-UTC browser, so a browser-zone wall clock in the command shows up.
process.env.TZ = "Asia/Kolkata";

const utc = zoneFromPreference("utc");
const anchor = epochMs(Date.UTC(2026, 9, 19, 20, 45));

function command(window: TimeWindow, language: "logchefql" | "clickhouse-sql" | "logsql" = "logchefql"): string {
  return generateCliCommand({
    teamId: 1,
    sourceId: 3,
    language,
    query: 'level="error"',
    window,
    resolved: resolveWindow(window, utc, anchor),
    limit: 100,
    timeout: 30,
  });
}

function relative(spec: string): TimeWindow {
  const window = relativeWindow(spec);
  if (!window) throw new Error(`bad spec ${spec}`);
  return window;
}

describe("generateCliCommand", () => {
  it("copies an absolute window as RFC3339 instants, whatever the browser zone", () => {
    const window: TimeWindow = { kind: "absolute", start: epochMs(Date.UTC(2026, 9, 19, 4)), end: epochMs(Date.UTC(2026, 9, 19, 5)) };
    expect(command(window)).toBe(
      `logchef query 'level="error"' -t 1 -S 3 --from "2026-10-19T04:00:00.000Z" --to "2026-10-19T05:00:00.000Z"`,
    );
  });

  it("copies today and yesterday as the instants the Explorer resolved", () => {
    expect(command(relative("today"))).toContain(`--from "2026-10-19T00:00:00.000Z" --to "2026-10-19T20:45:00.000Z"`);
    expect(command(relative("yesterday"))).toContain(`--from "2026-10-18T00:00:00.000Z" --to "2026-10-19T00:00:00.000Z"`);
    expect(command(relative("today"))).not.toContain("-s ");
  });

  it("keeps a duration relative", () => {
    expect(command(relative("15m"))).toBe(`logchef query 'level="error"' -t 1 -S 3 -s 15m`);
  });

  it("adds the window to native VictoriaLogs, but not to native ClickHouse SQL", () => {
    expect(command(relative("today"), "logsql")).toBe(
      `logchef sql 'level="error"' -t 1 -S 3 --from "2026-10-19T00:00:00.000Z" --to "2026-10-19T20:45:00.000Z" --timeout 30`,
    );
    expect(command(relative("today"), "clickhouse-sql")).toBe(`logchef sql 'level="error"' -t 1 -S 3 --timeout 30`);
  });
});
