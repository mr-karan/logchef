import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { nextTick } from "vue";
import type { Source } from "@/api/sources";

// ---------------------------------------------------------------------------
// Mocks
//
// The explore store pulls in a large dependency graph (sources/teams stores,
// the histogram/AI sub-stores, the logs + logchefql API modules). We stub the
// pieces that would otherwise hit the network or add unrelated noise, but
// keep the real context store (already covered by context.test.ts) so the
// source/team-switching watchers under test run against real reactivity.
//
// `sourcesState`/`teamsState` are plain `reactive()` objects rather than
// vi.fn()-returned plain objects: the store watches `sourcesStore.currentSourceDetails`
// and reads `teamsStore.currentTeamId` inside computeds, so mutations here must
// be tracked by Vue's reactivity system for those watchers/computeds to react.
// ---------------------------------------------------------------------------

// `vi.mock` factories are hoisted above ordinary imports, so they cannot close
// over a top-level `reactive(...)` built from a statically-imported `vue`
// binding (TDZ). Building the reactive state via a dynamic `import("vue")`
// inside each (async) factory sidesteps that, while still giving the explore
// store's `watch(() => sourcesStore.currentSourceDetails, ...)` and the
// `urlQueryParameters` computed real Vue reactivity to track.
const mocks = vi.hoisted(() => ({
  sourcesState: null as unknown as { currentSourceDetails: Source | null; teamSources: Source[]; isLoadingTeamSources: boolean },
  teamsState: null as unknown as { currentTeamId: number | null },
  logchefqlQuery: vi.fn(),
  exploreGetLogs: vi.fn(),
  exploreCancelQuery: vi.fn(),
  exploreCreateQueryShare: vi.fn(),
}));

vi.mock("@/stores/sources", async () => {
  const { reactive } = await import("vue");
  mocks.sourcesState = reactive({
    currentSourceDetails: null as Source | null,
    teamSources: [] as Source[],
    isLoadingTeamSources: false,
  });
  return { useSourcesStore: () => mocks.sourcesState };
});

vi.mock("@/stores/teams", async () => {
  const { reactive } = await import("vue");
  mocks.teamsState = reactive({ currentTeamId: null as number | null });
  return { useTeamsStore: () => mocks.teamsState };
});

vi.mock("@/stores/exploreHistogram", () => ({
  useExploreHistogramStore: () => ({
    clearHistogramData: vi.fn(),
    setGroupByField: vi.fn(),
    fetchHistogramData: vi.fn().mockResolvedValue({ success: true }),
    histogramData: [],
    isLoadingHistogram: false,
    histogramError: null,
    histogramGranularity: null,
    groupByField: null,
  }),
}));

vi.mock("@/stores/exploreAI", () => ({
  useExploreAIStore: () => ({
    generateAiSql: vi.fn(),
    clearState: vi.fn(),
    isGeneratingAISQL: false,
    aiSqlError: null,
    generatedAiSql: null,
  }),
}));

// Silence the toast side-effect (vue-sonner) triggered by state.handleError.
vi.mock("@/composables/useToast", () => ({
  useToast: () => ({ toast: vi.fn() }),
}));

vi.mock("@/api/explore", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/explore")>();
  return {
    ...actual,
    exploreApi: {
      ...actual.exploreApi,
      getLogs: mocks.exploreGetLogs,
      cancelQuery: mocks.exploreCancelQuery,
      createQueryShare: mocks.exploreCreateQueryShare,
    },
    buildTailUrl: vi.fn(() => "ws://mock-tail"),
    subscribeToTail: vi.fn(() => Promise.resolve()),
  };
});

vi.mock("@/api/logchefql", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/logchefql")>();
  return {
    ...actual,
    logchefqlApi: { ...actual.logchefqlApi, query: mocks.logchefqlQuery },
  };
});

import { useExploreStore } from "../explore";
import { useContextStore } from "../context";
import { timestampToCalendarDateTime } from "@/utils/time";

function makeSource(overrides: Partial<Source> = {}): Source {
  return {
    id: 1,
    name: "source-1",
    _meta_is_auto_created: false,
    source_type: "clickhouse",
    _meta_ts_field: "timestamp",
    connection: { database: "default", table_name: "logs" } as unknown as Source["connection"],
    ttl_days: 30,
    created_at: "",
    updated_at: "",
    is_connected: true,
    ...overrides,
  };
}

// A ClickHouse source: supports both logchefql and clickhouse-sql (default).
const CH_SOURCE = makeSource({ id: 1, source_type: "clickhouse" });
// A VictoriaLogs source: supports logchefql and logsql.
const VL_SOURCE = makeSource({ id: 2, source_type: "victorialogs" });
// A source explicitly restricted to SQL only (no LogchefQL support).
const SQL_ONLY_SOURCE = makeSource({ id: 3, query_languages: ["clickhouse-sql"] });

describe("explore store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
    vi.clearAllMocks();
    mocks.sourcesState.currentSourceDetails = null;
    mocks.sourcesState.teamSources = [];
    mocks.sourcesState.isLoadingTeamSources = false;
    mocks.teamsState.currentTeamId = null;
  });

  describe("mode switching (logchefql <-> native)", () => {
    it("setActiveMode switches to the requested mode when the source supports it", () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = useExploreStore();
      expect(store.activeMode).toBe("logchefql");
      store.setActiveMode("native");
      expect(store.activeMode).toBe("native");
    });

    it("coerces to native when the source does not support logchefql", () => {
      mocks.sourcesState.currentSourceDetails = SQL_ONLY_SOURCE;
      const store = useExploreStore();
      store.setActiveMode("logchefql");
      expect(store.activeMode).toBe("native");
    });

    it("clears an active share selection when the mode actually changes", async () => {
      // A share selection is only considered "dirty" (and thus cleared) once
      // a snapshot has been recorded, which happens via createQueryShare —
      // setActiveShareToken alone (no snapshot) is not enough to arm this.
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      mocks.exploreCreateQueryShare.mockResolvedValue({ data: { token: "share-token" } });

      const store = useExploreStore();
      await store.createQueryShare();
      expect(store.activeShareToken).toBe("share-token");

      store.setActiveMode("native");
      expect(store.activeShareToken).toBeNull();
    });

    it("leaves the share selection untouched on a no-op mode switch", () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = useExploreStore();
      store.setActiveShareToken("share-token");
      store.setActiveMode("logchefql"); // already the active mode -> no-op
      expect(store.activeShareToken).toBe("share-token");
    });

    it("auto-migrates logchefql content to native when the source loses logchefql support", async () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      mocks.sourcesState.currentSourceDetails = SQL_ONLY_SOURCE;
      await nextTick();

      expect(store.activeMode).toBe("native");
      expect(store.logchefqlCode).toBe("");
      expect(store.nativeQuery).toBe('level="error"');
    });

    it("does not clobber an existing native query when auto-migrating", async () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');
      store.setNativeQuery("SELECT 1");

      mocks.sourcesState.currentSourceDetails = SQL_ONLY_SOURCE;
      await nextTick();

      expect(store.nativeQuery).toBe("SELECT 1");
      expect(store.logchefqlCode).toBe("");
    });

    it("re-normalizes the active mode when the new source regains logchefql support", async () => {
      mocks.sourcesState.currentSourceDetails = VL_SOURCE;
      const store = useExploreStore();
      expect(store.activeMode).toBe("logchefql");

      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      await nextTick();
      expect(store.activeMode).toBe("logchefql");
    });
  });

  describe("source switching", () => {
    it("resets logs, columns and query content when the source id changes", async () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);

      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      contextStore.selectSource(20);
      await nextTick(); // the sourceId watcher runs on the next flush

      expect(store.logchefqlCode).toBe("");
      expect(store.nativeQuery).toBe("");
      expect(store.logs).toEqual([]);
      expect(store.hasExecutedQuery).toBe(false);
    });

    it("suppressNextSourceReset skips exactly one reset for the given source", async () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);

      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      store.suppressNextSourceReset(20);
      contextStore.selectSource(20);
      await nextTick();
      expect(store.logchefqlCode).toBe('level="error"'); // reset suppressed

      contextStore.selectSource(10);
      await nextTick();
      expect(store.logchefqlCode).toBe(""); // suppression was one-shot; this reset fires
    });

    it("switching teams cascades into a source reset via the context store", async () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);

      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      contextStore.selectTeam(2); // context store clears sourceId when the team changes
      await nextTick();
      expect(contextStore.sourceId).toBeNull();
      expect(store.logchefqlCode).toBe("");
    });
  });

  describe("relative time re-resolution on executeQuery", () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("re-resolves the relative time window on every execution instead of reusing a stale one", async () => {
      const store = useExploreStore();
      store.setRelativeTimeRange("15m");
      const firstEnd = store.timeRange?.end.toString();

      vi.setSystemTime(new Date("2026-01-01T00:10:00Z"));
      // No team is selected, so this fails fast inside withLoading — but the
      // relative-time refresh at the top of executeQuery runs unconditionally
      // before that guard, which is exactly the behavior being pinned here.
      await store.executeQuery();
      const secondEnd = store.timeRange?.end.toString();

      expect(store.selectedRelativeTime).toBe("15m");
      expect(secondEnd).not.toEqual(firstEnd);
    });

    it("leaves an absolute time range untouched across executions", async () => {
      const store = useExploreStore();
      store.setTimeConfiguration({
        absoluteRange: {
          start: timestampToCalendarDateTime(0),
          end: timestampToCalendarDateTime(1_000_000),
        },
      });
      const before = store.timeRange;
      await store.executeQuery();
      expect(store.timeRange).toEqual(before);
      expect(store.selectedRelativeTime).toBeNull();
    });
  });

  describe("urlQueryParameters", () => {
    it("serializes team, source, absolute time range, and limit by default", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setTimeConfiguration({
        absoluteRange: {
          start: timestampToCalendarDateTime(0),
          end: timestampToCalendarDateTime(60_000),
        },
      });
      store.setLimit(250);

      const params = store.urlQueryParameters;
      expect(params.team).toBe("1");
      expect(params.source).toBe("10");
      expect(params.start).toBe("0");
      expect(params.end).toBe("60000");
      expect(params.limit).toBe("250");
      expect(params.mode).toBeUndefined();
    });

    it('serializes relative time as "t" instead of start/end', () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setRelativeTimeRange("1h");

      const params = store.urlQueryParameters;
      expect(params.t).toBe("1h");
      expect(params.start).toBeUndefined();
      expect(params.end).toBeUndefined();
    });

    it("adds mode=native only for the native editor", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;

      const store = useExploreStore();
      expect(store.urlQueryParameters.mode).toBeUndefined();

      store.setActiveMode("native");
      expect(store.urlQueryParameters.mode).toBe("native");
    });

    it("collapses to just team/source/id when a saved query is selected and not diverged", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setRelativeTimeRange("15m");
      store.setSelectedQueryId("42");

      // hasDivergedFromSavedQuery is false here because no savedQuerySnapshot
      // was hydrated (selectedQueryId was set directly) — so the "id" param
      // takes priority and every other param is dropped.
      const params = store.urlQueryParameters;
      expect(params).toEqual({ team: "1", source: "10", id: "42" });
    });

    it("prefers the share token over time/limit params", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setRelativeTimeRange("15m");
      store.setActiveShareToken("tok123");

      const params = store.urlQueryParameters;
      expect(params).toEqual({ team: "1", source: "10", share: "tok123" });
    });

    it("omits team/source when none is selected", () => {
      const store = useExploreStore();
      const params = store.urlQueryParameters;
      expect(params.team).toBeUndefined();
      expect(params.source).toBeUndefined();
    });
  });

  describe("initializeFromUrl", () => {
    it("defaults the time range to 15m when nothing is present", () => {
      const store = useExploreStore();
      const result = store.initializeFromUrl({});
      expect(store.selectedRelativeTime).toBe("15m");
      expect(result.shouldExecute).toBe(false); // no source selected yet
    });

    it('selects the source from the "source" param', () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      const store = useExploreStore();
      store.initializeFromUrl({ source: "10" });
      expect(contextStore.sourceId).toBe(10);
    });

    it("legacy q param without an explicit mode selects logchefql mode", () => {
      const store = useExploreStore();
      store.initializeFromUrl({ q: 'level="error"' });
      expect(store.activeMode).toBe("logchefql");
      expect(store.logchefqlCode).toBe('level="error"');
    });

    it("legacy sql param without an explicit mode selects native mode", () => {
      const store = useExploreStore();
      store.initializeFromUrl({ sql: "SELECT 1" });
      expect(store.activeMode).toBe("native");
      expect(store.nativeQuery).toBe("SELECT 1");
    });

    it("an explicit id param defers to saved-query resolution instead of executing immediately", () => {
      const store = useExploreStore();
      const result = store.initializeFromUrl({ id: "99" });
      expect(result).toMatchObject({ needsResolve: true, queryId: "99", shouldExecute: false });
      expect(store.selectedQueryId).toBe("99");
    });

    it("a share param requests share resolution instead of executing immediately", () => {
      const store = useExploreStore();
      const result = store.initializeFromUrl({ share: "tok" });
      expect(result).toMatchObject({ needsShareResolve: true, shareToken: "tok", shouldExecute: false });
      expect(store.activeShareToken).toBe("tok");
    });
  });
});
