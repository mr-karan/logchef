import { beforeEach, describe, expect, it, vi } from "vitest";
import { epochMs, parseZoneId, zoneFromPreference, type ResolvedWindow } from "@/utils/timeWindow";

const mocks = vi.hoisted(() => ({
  exploreStore: {
    sourceId: 2,
    activeMode: "logchefql" as "logchefql" | "native",
    logchefqlCode: 'lvl="WARN"',
    nativeQuery: "",
    resolved: null as unknown as ResolvedWindow,
    limit: 100,
    lastExecutedState: null,
    isQueryStateDirty: false,
    canExecuteQuery: true,
    setLogchefqlCode: vi.fn(),
    setNativeQuery: vi.fn(),
    setActiveMode: vi.fn(),
    clearError: vi.fn(),
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

describe("useQuery SQL translation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.exploreStore.activeMode = "logchefql";
    mocks.exploreStore.logchefqlCode = 'lvl="WARN"';
    mocks.exploreStore.nativeQuery = "";
    mocks.exploreStore.resolved = { start: epochMs(Date.UTC(2026, 7, 8, 4, 30)), end: epochMs(Date.UTC(2026, 7, 8, 4, 45)), zone: zoneFromPreference("utc") };
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

  it.each([zoneFromPreference("utc"), parseZoneId("Asia/Kolkata") ?? zoneFromPreference("utc")])(
    "sends the resolved window as RFC3339 instants with its %s zone",
    async (zone) => {
      mocks.exploreStore.resolved = { start: epochMs(Date.UTC(2026, 7, 8, 4, 30)), end: epochMs(Date.UTC(2026, 7, 8, 4, 45)), zone };
      mocks.translate.mockResolvedValue({
        data: {
          valid: true,
          generated_query: "SELECT 1",
        },
      });

      await useQuery().changeMode("native");

      expect(mocks.translate).toHaveBeenCalledWith(1, 2, expect.objectContaining({
        start_time: "2026-08-08T04:30:00.000Z",
        end_time: "2026-08-08T04:45:00.000Z",
        timezone: zone,
      }));
    },
  );
});
