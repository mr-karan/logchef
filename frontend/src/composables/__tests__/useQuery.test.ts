import { beforeEach, describe, expect, it, vi } from "vitest";

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
  convertVariables: vi.fn((query: string) => query),
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
    translate: vi.fn(),
  },
}));

vi.mock("@/composables/useVariables", () => ({
  useVariables: () => ({ convertVariables: mocks.convertVariables }),
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
});
