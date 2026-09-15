import { beforeEach, describe, expect, it, vi } from "vitest";
import { CalendarDateTime, ZonedDateTime } from "@internationalized/date";

const mocks = vi.hoisted(() => ({
  exploreStore: {
    sourceId: 2,
    activeMode: "logchefql" as "logchefql" | "native",
    logchefqlCode: 'lvl="WARN"',
    nativeQuery: "",
    timeRange: {
      start: { year: 2026, month: 8, day: 8, hour: 10, minute: 0, second: 0 },
      end: { year: 2026, month: 8, day: 8, hour: 10, minute: 15, second: 0 },
    },
    limit: 100,
    selectedTimezoneIdentifier: "UTC",
    getTimezoneIdentifier: vi.fn(() => "UTC"),
    lastExecutedState: null,
    isQueryStateDirty: false,
    canExecuteQuery: true,
    isLoadingOperation: vi.fn(() => false),
    setLogchefqlCode: vi.fn(),
    setNativeQuery: vi.fn(),
    setActiveMode: vi.fn(),
    clearError: vi.fn(),
    executeQuery: vi.fn(),
  },
  sourcesStore: {
    currentSourceDetails: {
      id: 2,
      name: "VictoriaLogs Demo",
      source_type: "victorialogs",
      _meta_ts_field: "_time",
      connection: { base_url: "http://victorialogs:9428" },
    },
    getCurrentSourceTableName: null,
  },
  teamsStore: { currentTeamId: 1 },
  validate: vi.fn(),
  translate: vi.fn(),
  convertVariables: vi.fn((query: string) => query),
  getVariablesForApi: vi.fn(() => [
    { name: "trade_id", type: "text" as const, value: "api's" },
  ]),
}));

vi.mock("@/stores/explore", () => ({
  useExploreStore: () => mocks.exploreStore,
}));

vi.mock("@/stores/sources", () => ({
  useSourcesStore: () => mocks.sourcesStore,
}));

vi.mock("@/stores/teams", () => ({
  useTeamsStore: () => mocks.teamsStore,
}));

vi.mock("@/api/logchefql", () => ({
  logchefqlApi: {
    validate: mocks.validate,
    translate: mocks.translate,
  },
}));

vi.mock("@/composables/useVariables", () => ({
  useVariables: () => ({
    convertVariables: mocks.convertVariables,
    getVariablesForApi: mocks.getVariablesForApi,
  }),
}));

import { useQuery } from "../useQuery";

describe("useQuery LogchefQL execution preflight", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.exploreStore.activeMode = "logchefql";
    mocks.exploreStore.logchefqlCode = 'lvl="WARN"';
    mocks.exploreStore.nativeQuery = "";
  });

  it("executes valid LogchefQL for a VictoriaLogs source without requiring a table name", async () => {
    mocks.validate.mockResolvedValue({ data: { valid: true } });
    mocks.exploreStore.executeQuery.mockResolvedValue({
      success: true,
      data: { logs: [] },
      error: null,
    });

    const query = useQuery();
    const result = await query.executeQuery();

    expect(mocks.validate).toHaveBeenCalledWith(1, 2, 'lvl="WARN"');
    expect(mocks.exploreStore.executeQuery).toHaveBeenCalledOnce();
    expect(result.success).toBe(true);
    expect(query.queryError.value).toBe("");
  });

  it("blocks execution when server-side LogchefQL validation fails", async () => {
    mocks.validate.mockResolvedValue({
      data: {
        valid: false,
        error: { code: "PARSE_ERROR", message: "Expected a field value" },
      },
    });

    const query = useQuery();
    const result = await query.executeQuery();

    expect(result.success).toBe(false);
    expect(result.error?.message).toBe("Expected a field value");
    expect(query.queryError.value).toBe("Expected a field value");
    expect(mocks.exploreStore.executeQuery).not.toHaveBeenCalled();
  });

  it("validates a quoted variable without doubling its quotes", async () => {
    mocks.exploreStore.logchefqlCode = 'p.resp.FillNumber="{{ trade_id }}"|p.order_number';
    mocks.validate.mockResolvedValue({ data: { valid: true } });
    mocks.exploreStore.executeQuery.mockResolvedValue({ success: true, data: { logs: [] } });

    const result = await useQuery().executeQuery();

    expect(mocks.validate).toHaveBeenCalledWith(1, 2, 'p.resp.FillNumber="__VAR_trade_id__"|p.order_number');
    expect(mocks.exploreStore.logchefqlCode).toBe('p.resp.FillNumber="{{ trade_id }}"|p.order_number');
    expect(result.success).toBe(true);
  });

  it("sends typed variables to translation and uses returned SQL directly", async () => {
    mocks.exploreStore.logchefqlCode = 'message="prefix {{ trade_id }}"';
    mocks.translate.mockResolvedValue({
      data: {
        valid: true,
        generated_query: "SELECT * FROM logs WHERE message = 'prefix api\\'s'",
      },
    });

    const query = useQuery();
    await query.changeMode("native");

    expect(mocks.translate).toHaveBeenCalledWith(1, 2, expect.objectContaining({
      query: 'message="prefix {{ trade_id }}"',
      variables: [{ name: "trade_id", type: "text", value: "api's" }],
    }));
    expect(mocks.exploreStore.setNativeQuery).toHaveBeenCalledWith(
      "SELECT * FROM logs WHERE message = 'prefix api\\'s'",
    );
    expect(mocks.convertVariables).not.toHaveBeenCalled();
  });

  it.each([
    { timezone: "UTC", start: "2026-08-08 04:30:00", end: "2026-08-08 04:45:00" },
    { timezone: "Asia/Kolkata", start: "2026-08-08 10:00:00", end: "2026-08-08 10:15:00" },
  ] as const)("formats a ZonedDateTime in the paired $timezone timezone", async ({ timezone, start, end }) => {
    mocks.exploreStore.timeRange = {
      start: new ZonedDateTime(2026, 8, 8, "Asia/Kolkata", 19_800_000, 10, 0, 0),
      end: new ZonedDateTime(2026, 8, 8, "Asia/Kolkata", 19_800_000, 10, 15, 0),
    };
    mocks.exploreStore.getTimezoneIdentifier.mockReturnValue(timezone);
    mocks.translate.mockResolvedValue({
      data: {
        valid: true,
        generated_query: "SELECT 1",
      },
    });

    await useQuery().changeMode("native");

    expect(mocks.translate).toHaveBeenCalledWith(1, 2, expect.objectContaining({
      start_time: start,
      end_time: end,
      timezone,
    }));
  });

  it("preserves CalendarDateTime wall-clock values for the selected timezone", async () => {
    mocks.exploreStore.timeRange = {
      start: new CalendarDateTime(2026, 8, 8, 10, 0, 0),
      end: new CalendarDateTime(2026, 8, 8, 10, 15, 0),
    };
    mocks.exploreStore.getTimezoneIdentifier.mockReturnValue("Asia/Kolkata");
    mocks.translate.mockResolvedValue({
      data: {
        valid: true,
        generated_query: "SELECT 1",
      },
    });

    await useQuery().changeMode("native");

    expect(mocks.translate).toHaveBeenCalledWith(1, 2, expect.objectContaining({
      start_time: "2026-08-08 10:00:00",
      end_time: "2026-08-08 10:15:00",
      timezone: "Asia/Kolkata",
    }));
  });
});
