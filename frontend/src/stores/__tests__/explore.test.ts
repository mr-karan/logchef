import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { nextTick } from "vue";
import type { Source } from "@/api/sources";
import type { ResolvedSavedQuery } from "@/api/savedQueries";
import { parseExploreLocation } from "@/lib/exploreLocation";
import { epochMs, relativeWindow, type TimeWindow } from "@/utils/timeWindow";

// A non-UTC browser zone, so any browser-zone wall clock shows up as a shift.
process.env.TZ = "Asia/Kolkata";

// ---------------------------------------------------------------------------
// Mocks
//
// The explore store pulls in a large dependency graph (sources/teams stores,
// the histogram/AI sub-stores, the logs + logchefql API modules). We stub the
// pieces that would otherwise hit the network or add unrelated noise, but
// keep the real context store (already covered by context.test.ts) so open()
// and the source watchers under test run against real reactivity.
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
  logchefqlValidate: vi.fn(),
  exploreGetLogs: vi.fn(),
  exploreCancelQuery: vi.fn(),
  exploreCreateQueryShare: vi.fn(),
  histogramFetch: vi.fn(),
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
    fetchHistogramData: mocks.histogramFetch,
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
    generatedAiQuery: null,
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
    logchefqlApi: { ...actual.logchefqlApi, query: mocks.logchefqlQuery, validate: mocks.logchefqlValidate },
  };
});

import { useExploreStore, type OpenedLocation } from "../explore";
import { useAskChefStore } from "../askChef";
import { useContextStore } from "../context";
import { usePreferencesStore } from "../preferences";
import { useVariableStore } from "../variables";

function relative(spec: string): TimeWindow {
  const window = relativeWindow(spec);
  if (!window) throw new Error(`bad spec ${spec}`);
  return window;
}

function absolute(start: number, end: number): TimeWindow {
  return { kind: "absolute", start: epochMs(start), end: epochMs(end) };
}

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

// Opens what an Explorer URL names, as the navigator does once the team and source are loaded.
function openUrl(params: Record<string, string>): boolean {
  const { teamId, sourceId, target } = parseExploreLocation(params);
  if (target.kind === "saved" || target.kind === "share") throw new Error("openUrl opens drafts and queries only");
  return useExploreStore().open({ teamId: teamId ?? 1, sourceId, content: target });
}

function savedQuery(overrides: Partial<ResolvedSavedQuery>): ResolvedSavedQuery {
  return {
    id: 7, source_id: 1, resolved_team_id: 1, name: "Q", description: "", query_language: "logchefql",
    editor_mode: "builder", query_content: "{}", created_at: "", updated_at: "", ...overrides,
  };
}

function savedContent(saved: ResolvedSavedQuery): Extract<OpenedLocation["content"], { kind: "saved" }> {
  return { kind: "saved", saved, queryContent: JSON.parse(saved.query_content) };
}

// A ClickHouse source: supports both logchefql and clickhouse-sql (default).
const CH_SOURCE = makeSource({ id: 1, source_type: "clickhouse" });
// A VictoriaLogs source: supports logchefql and logsql.
const VL_SOURCE = makeSource({
  id: 2,
  source_type: "victorialogs",
  connection: { base_url: "http://victorialogs:9428" },
});
// A source explicitly restricted to SQL only (no LogchefQL support).
const SQL_ONLY_SOURCE = makeSource({ id: 3, query_languages: ["clickhouse-sql"] });

describe("explore store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
    vi.clearAllMocks();
    mocks.histogramFetch.mockResolvedValue({ success: true });
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
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      mocks.exploreCreateQueryShare.mockResolvedValue({ data: { token: "share-token" } });

      const store = useExploreStore();
      const share = await store.createQueryShare();
      store.setActiveShareToken(share?.token ?? null);
      expect(store.activeShareToken).toBe("share-token");

      store.setActiveMode("native");
      expect(store.activeShareToken).toBeNull();
    });

    it("shares a native query in the server's sql mode", async () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      mocks.exploreCreateQueryShare.mockResolvedValue({ data: { token: "share-token" } });

      const store = useExploreStore();
      store.setActiveMode("native");
      store.setNativeQuery("SELECT 1");
      await store.createQueryShare();

      expect(mocks.exploreCreateQueryShare.mock.lastCall?.[1]).toMatchObject({ mode: "sql", query: "SELECT 1" });
    });

    it("leaves the share selection untouched on a no-op mode switch", () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = useExploreStore();
      store.setActiveShareToken("share-token");
      store.setActiveMode("logchefql"); // already the active mode -> no-op
      expect(store.activeShareToken).toBe("share-token");
    });

    it("drops LogchefQL from a link opened on a source without LogchefQL", () => {
      mocks.sourcesState.currentSourceDetails = SQL_ONLY_SOURCE;
      const store = useExploreStore();
      store.setNativeQuery("SELECT 1");

      openUrl({ team: "1", source: String(SQL_ONLY_SOURCE.id), mode: "logchefql", q: 'level="error"' });

      expect(store.activeMode).toBe("native");
      expect(store.logchefqlCode).toBe("");
      // Raw LogchefQL must NOT leak into nativeQuery without translation.
      // The default SQL generator in executeQuery will produce a valid query.
      expect(store.nativeQuery).toBe("");
    });
  });

  describe("source switching", () => {
    it("resets logs, columns and query content when it opens another source", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);

      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      openUrl({ team: "1", source: "20" });

      expect(contextStore.sourceId).toBe(20);
      expect(store.logchefqlCode).toBe("");
      expect(store.nativeQuery).toBe("");
      expect(store.logs).toEqual([]);
      expect(store.hasExecutedQuery).toBe(false);
    });

    it("resets the query when it opens a team without sources", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);

      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      expect(store.open({ teamId: 2, sourceId: null, content: { kind: "draft" } })).toBe(false);
      expect(contextStore.teamId).toBe(2);
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
      store.setWindow(relative("15m"));
      const firstEnd = store.resolved.end;

      vi.setSystemTime(new Date("2026-01-01T00:10:00Z"));
      // No team is selected, so this fails fast inside withLoading — but the
      // relative-time refresh at the top of executeQuery runs unconditionally
      // before that guard, which is exactly the behavior being pinned here.
      await store.executeQuery();

      expect(store.window).toEqual(relative("15m"));
      expect(store.resolved.end).toBe(Date.parse("2026-01-01T00:10:00Z"));
      expect(store.resolved.end).not.toBe(firstEnd);
    });

    it("leaves an absolute time range untouched across executions", async () => {
      const store = useExploreStore();
      store.setWindow(absolute(0, 1_000_000));
      await store.executeQuery();
      expect(store.window).toEqual(absolute(0, 1_000_000));
      expect(store.resolved).toMatchObject({ start: 0, end: 1_000_000 });
    });
  });

  describe("Run validation", () => {
    function validatedStore(code: string) {
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = VL_SOURCE;
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(VL_SOURCE.id);
      const store = useExploreStore();
      store.setLogchefqlCode(code);
      return store;
    }

    it("accepts valid LogchefQL for a VictoriaLogs source without requiring a table name", async () => {
      mocks.logchefqlValidate.mockResolvedValue({ data: { valid: true } });
      const store = validatedStore('lvl="WARN"');

      expect(await store.validateQuery()).toBe(true);
      expect(mocks.logchefqlValidate).toHaveBeenCalledWith(1, 2, 'lvl="WARN"');
      expect(store.error).toBeNull();
    });

    it("rejects LogchefQL that the server says is invalid and shows why", async () => {
      mocks.logchefqlValidate.mockResolvedValue({ data: { valid: false, error: { code: "PARSE_ERROR", message: "Expected a field value" } } });
      const store = validatedStore('lvl="WARN"');

      expect(await store.validateQuery()).toBe(false);
      expect(store.error?.message).toBe("Expected a field value");
    });

    it("validates a quoted variable without doubling its quotes", async () => {
      mocks.logchefqlValidate.mockResolvedValue({ data: { valid: true } });
      const store = validatedStore('p.resp.FillNumber="{{ trade_id }}"|p.order_number');

      expect(await store.validateQuery()).toBe(true);
      expect(mocks.logchefqlValidate).toHaveBeenCalledWith(1, 2, 'p.resp.FillNumber="__VAR_trade_id__"|p.order_number');
      expect(store.logchefqlCode).toBe('p.resp.FillNumber="{{ trade_id }}"|p.order_number');
    });
  });

  describe("LogchefQL execution", () => {
    it("sends a VictoriaLogs query through the source-aware LogchefQL endpoint", async () => {
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = VL_SOURCE;
      mocks.sourcesState.teamSources = [VL_SOURCE];

      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(VL_SOURCE.id);

      const store = useExploreStore();
      store.setWindow(relative("15m"));
      store.setLogchefqlCode('lvl="WARN"');
      mocks.logchefqlQuery.mockResolvedValue({
        data: {
          logs: [],
          columns: [],
          stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 },
        },
      });

      const result = await store.executeQuery();

      expect(result.success).toBe(true);
      expect(mocks.logchefqlQuery).toHaveBeenCalledWith(
        1,
        VL_SOURCE.id,
        expect.objectContaining({
          query: 'lvl="WARN"',
          start_time: expect.any(String),
          end_time: expect.any(String),
          timezone: expect.any(String),
          limit: 100,
          query_timeout: expect.any(Number),
        }),
        expect.objectContaining({
          signal: expect.any(AbortSignal),
          timeout: expect.any(Number),
        }),
      );
    });

    function exploreVictoriaLogsWith(window: TimeWindow) {
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = VL_SOURCE;
      mocks.sourcesState.teamSources = [VL_SOURCE];
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(VL_SOURCE.id);
      const store = useExploreStore();
      store.setWindow(window);
      mocks.logchefqlQuery.mockResolvedValue({
        data: { logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 } },
      });
      return store;
    }

    it("sends the window as RFC3339 instants with the preference zone", async () => {
      const store = exploreVictoriaLogsWith(absolute(Date.UTC(2026, 7, 8, 4, 30), Date.UTC(2026, 7, 8, 4, 45)));
      await store.executeQuery();

      expect(mocks.logchefqlQuery).toHaveBeenCalledWith(
        1,
        VL_SOURCE.id,
        expect.objectContaining({
          start_time: "2026-08-08T04:30:00.000Z",
          end_time: "2026-08-08T04:45:00.000Z",
          timezone: store.resolved.zone,
        }),
        expect.anything(),
      );
    });

    it("resolves today at midnight in the preference zone, not the browser's", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-10-19T20:45:00Z")); // already Oct 20 in the IST browser
      try {
        mocks.teamsState.currentTeamId = 1;
        mocks.sourcesState.currentSourceDetails = VL_SOURCE;
        mocks.sourcesState.teamSources = [VL_SOURCE];
        useContextStore().selectTeam(1);
        mocks.logchefqlQuery.mockResolvedValue({
          data: { logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 } },
        });
        const preferences = usePreferencesStore();
        const store = useExploreStore();
        await preferences.updatePreferences({ timezone: "utc" });
        openUrl({ team: "1", source: String(VL_SOURCE.id), t: "today", mode: "logchefql", q: "" });
        await store.executeQuery();

        expect(mocks.logchefqlQuery.mock.lastCall?.[2]).toMatchObject({
          start_time: "2026-10-19T00:00:00.000Z",
          end_time: "2026-10-19T20:45:00.000Z",
          timezone: "UTC",
        });
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe("evidence links", () => {
    it("opens a link's start and end ms as instants for the query and the histogram", async () => {
      const start = Date.UTC(2026, 9, 19, 4);
      const end = Date.UTC(2026, 9, 19, 5);
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      mocks.sourcesState.teamSources = [CH_SOURCE];
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      const preferences = usePreferencesStore();
      const store = useExploreStore();
      await preferences.updatePreferences({ timezone: "utc" });
      mocks.logchefqlQuery.mockResolvedValue({
        data: { logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 }, generated_sql: "SELECT 1" },
      });

      openUrl({ team: "1", source: String(CH_SOURCE.id), start: String(start), end: String(end), mode: "logchefql", q: "" });
      await store.executeQuery();

      expect(mocks.logchefqlQuery.mock.lastCall?.[2]).toMatchObject({
        start_time: new Date(start).toISOString(),
        end_time: new Date(end).toISOString(),
        timezone: "UTC",
      });
      expect(mocks.histogramFetch.mock.lastCall?.[0]).toMatchObject({ window: { start, end, zone: "UTC" } });
    });
  });

  describe("Ask Chef context window", () => {
    // Ask Chef reads its window before the query runs: watchers left by
    // earlier tests reset the context store while the query is awaited.
    const QUERY = 'ask="chef"';

    function exploreVictoriaLogs() {
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = VL_SOURCE;
      mocks.sourcesState.teamSources = [VL_SOURCE];
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(VL_SOURCE.id);
      mocks.logchefqlQuery.mockResolvedValue({
        data: { logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 } },
      });
      const store = useExploreStore();
      store.setLogchefqlCode(QUERY);
      return store;
    }

    // Stores left by earlier tests may also query, so read this store's request by its query.
    async function logQueryWindow(): Promise<{ start: number; end: number; zone: string }> {
      await useExploreStore().executeQuery();
      const request = mocks.logchefqlQuery.mock.calls.filter((call) => call[2].query === QUERY).at(-1)?.[2];
      return { start: Date.parse(request.start_time), end: Date.parse(request.end_time), zone: request.timezone };
    }

    it("is the window the log query reads for an absolute range", async () => {
      exploreVictoriaLogs().setWindow(absolute(Date.UTC(2026, 7, 8, 4, 30), Date.UTC(2026, 7, 8, 4, 45)));

      const askChef = useAskChefStore();
      askChef.openForExplorer();

      expect(askChef.context).toMatchObject(await logQueryWindow());
    });

    it("is the window the log query reads for a relative range", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-08-08T10:00:00Z"));
      try {
        exploreVictoriaLogs().setWindow(relative("1h"));

        const askChef = useAskChefStore();
        askChef.openForExplorer();

        expect(askChef.context).toMatchObject(await logQueryWindow());
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe("urlQueryParameters", () => {
    it("serializes team, source, absolute time range, and limit by default", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setWindow(absolute(0, 60_000));
      store.setLimit(250);

      const params = store.urlQueryParameters;
      expect(params.team).toBe("1");
      expect(params.source).toBe("10");
      expect(params.start).toBe("0");
      expect(params.end).toBe("60000");
      expect(params.limit).toBe("250");
    });

    it('serializes relative time as "t" instead of start/end', () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setWindow(relative("1h"));

      const params = store.urlQueryParameters;
      expect(params.t).toBe("1h");
      expect(params.start).toBeUndefined();
      expect(params.end).toBeUndefined();
    });

    it("always writes the mode and its query, even when the query is empty", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;

      const store = useExploreStore();
      expect(store.urlQueryParameters).toMatchObject({ mode: "logchefql", q: "" });

      store.setLogchefqlCode('level="error"');
      expect(store.urlQueryParameters).toMatchObject({ mode: "logchefql", q: 'level="error"' });

      store.setActiveMode("native");
      store.setNativeQuery("SELECT 1");
      expect(store.urlQueryParameters).toMatchObject({ mode: "native", sql: "SELECT 1" });
      expect(store.urlQueryParameters.q).toBeUndefined();
    });

    it("collapses to just team/source/id when a saved query is selected and not diverged", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setWindow(relative("15m"));
      store.markSavedQuery(savedQuery({ id: 42, name: "errors", query_content: JSON.stringify({ version: 1, content: "", limit: 100 }) }));

      const params = store.urlQueryParameters;
      expect(params).toEqual({ team: "1", source: "10", id: "42" });
    });

    it("prefers the share token over time/limit params", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);
      mocks.teamsState.currentTeamId = 1;

      const store = useExploreStore();
      store.setWindow(relative("15m"));
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

  describe("open", () => {
    it("defaults the time range to 15m and runs nothing without a source", () => {
      const store = useExploreStore();
      expect(openUrl({})).toBe(false);
      expect(store.window).toEqual(relative("15m"));
    });

    it('selects the source from the "source" param', () => {
      const contextStore = useContextStore();
      openUrl({ team: "1", source: "10" });
      expect(contextStore.sourceId).toBe(10);
    });

    it("legacy q param without an explicit mode selects logchefql mode", () => {
      const store = useExploreStore();
      openUrl({ q: 'level="error"' });
      expect(store.activeMode).toBe("logchefql");
      expect(store.logchefqlCode).toBe('level="error"');
    });

    it("legacy sql param without an explicit mode selects native mode", () => {
      const store = useExploreStore();
      openUrl({ sql: "SELECT 1" });
      expect(store.activeMode).toBe("native");
      expect(store.nativeQuery).toBe("SELECT 1");
    });
  });

  // ---------------------------------------------------------------------------
  // #102 — draft persistence must survive a reload. Opening a location must
  // not persist a draft before it restores the saved one.
  // ---------------------------------------------------------------------------
  describe("draft round-trip on reload (#102)", () => {
    it("preserves an unsaved query across a simulated reload", () => {
      mocks.teamsState.currentTeamId = 1;
      const ctx1 = useContextStore();
      ctx1.selectTeam(1);
      ctx1.selectSource(10);

      const store1 = useExploreStore();
      store1.setLogchefqlCode('level="error"'); // persists the draft

      // Simulate an F5: fresh Pinia + stores, same team/source, re-init from URL.
      setActivePinia(createPinia());
      mocks.teamsState.currentTeamId = 1;
      const ctx2 = useContextStore();
      ctx2.selectTeam(1);

      const store2 = useExploreStore();
      openUrl({ team: "1", source: "10" });

      expect(store2.logchefqlCode).toBe('level="error"');
      expect(store2.activeMode).toBe("logchefql");
    });

    it("saves no window the user never chose", () => {
      mocks.teamsState.currentTeamId = 1;
      const ctx = useContextStore();
      ctx.selectTeam(1);
      ctx.selectSource(10);

      useExploreStore().setLogchefqlCode('level="error"');

      const draft = JSON.parse(localStorage.getItem("logchef.explore.draft.1.10") ?? "{}");
      expect(draft).toMatchObject({ version: 2, logchefqlCode: 'level="error"', window: null });
    });
  });

  describe("links that name their query", () => {
    function saveDraft() {
      mocks.teamsState.currentTeamId = 1;
      const ctx = useContextStore();
      ctx.selectTeam(1);
      ctx.selectSource(10);
      const store = useExploreStore();
      store.setWindow(relative("7d"));
      store.setLogchefqlCode('level="debug"');
      setActivePinia(createPinia());
      mocks.teamsState.currentTeamId = 1;
      useContextStore().selectTeam(1);
    }

    it("opens an explicitly empty query and the link's window, not the saved draft", () => {
      saveDraft();
      const store = useExploreStore();

      const result = openUrl({ team: "1", source: "10", start: "1000", end: "2000", mode: "logchefql", q: "" });

      expect(store.logchefqlCode).toBe("");
      expect(store.activeMode).toBe("logchefql");
      expect(store.window).toEqual(absolute(1000, 2000));
      expect(store.urlQueryParameters).toMatchObject({ start: "1000", end: "2000" });
      expect(result).toBe(true);
    });

    it("opens a link that names only a time or a mode without the draft's query", () => {
      saveDraft();
      const store = useExploreStore();
      openUrl({ team: "1", source: "10", t: "1h" });
      expect(store.logchefqlCode).toBe("");
      expect(store.window).toEqual(relative("1h"));

      saveDraft();
      const next = useExploreStore();
      openUrl({ team: "1", source: "10", mode: "logchefql" });
      expect(next.logchefqlCode).toBe("");
      expect(next.window).not.toEqual(relative("7d"));
    });

    it("brings back a source's unrun draft after switching away and back", () => {
      mocks.teamsState.currentTeamId = 1;
      const ctx = useContextStore();
      ctx.selectTeam(1);
      ctx.selectSource(10);
      const store = useExploreStore();
      store.setLogchefqlCode('level="debug"');

      openUrl({ team: "1", source: "11" });
      expect(store.logchefqlCode).toBe("");

      openUrl({ team: "1", source: "10" });
      expect(store.logchefqlCode).toBe('level="debug"');
      expect(JSON.parse(localStorage.getItem("logchef.explore.draft.1.10") ?? "{}")).toMatchObject({ logchefqlCode: 'level="debug"' });
    });

    it("still restores the draft when the link names no query", () => {
      saveDraft();
      const store = useExploreStore();

      openUrl({ team: "1", source: "10" });

      expect(store.logchefqlCode).toBe('level="debug"');
      expect(store.window).toEqual(relative("7d"));
    });
  });

  // ---------------------------------------------------------------------------
  // #105 — hydrated saved-query content must land in the editor slot matching
  // the resolved (possibly coerced) active mode, not the query's raw language,
  // and the opposite slot must be cleared so nothing lingers behind a hidden
  // editor.
  // ---------------------------------------------------------------------------
  describe("saved-query hydration slot (#105)", () => {
    it("routes logchefql content to the logchefql slot and clears native", () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = useExploreStore();
      store.setNativeQuery("SELECT old"); // stale content in the other slot

      store.open({ teamId: 1, sourceId: CH_SOURCE.id, content: savedContent(savedQuery({
        name: "My Query",
        query_content: JSON.stringify({
          content: 'level="error"',
          limit: 100,
          timeRange: { relative: "15m" },
        }),
      })) });

      expect(store.activeMode).toBe("logchefql");
      expect(store.logchefqlCode).toBe('level="error"');
      expect(store.nativeQuery).toBe(""); // opposite slot cleared
    });

    it("coerces to native and routes content there when the source can't honour logchefql", () => {
      mocks.sourcesState.currentSourceDetails = SQL_ONLY_SOURCE;
      const store = useExploreStore();

      store.open({ teamId: 1, sourceId: SQL_ONLY_SOURCE.id, content: savedContent(savedQuery({
        id: 8,
        query_content: JSON.stringify({
          content: "some content",
          limit: 100,
          timeRange: { relative: "15m" },
        }),
      })) });

      // Mode coerced to native → content lands in the visible native slot,
      // NOT in the hidden logchefql editor.
      expect(store.activeMode).toBe("native");
      expect(store.nativeQuery).toBe("some content");
      expect(store.logchefqlCode).toBe("");
    });
  });

  // ---------------------------------------------------------------------------
  // #101 — a query in flight when the source changes must be aborted, and its
  // late response must never populate results under the new source.
  // ---------------------------------------------------------------------------
  describe("async races on source switch (#101)", () => {
    function armInFlightLogchefql() {
      mocks.teamsState.currentTeamId = 1;
      mocks.sourcesState.currentSourceDetails = CH_SOURCE; // id 1
      mocks.sourcesState.teamSources = [CH_SOURCE];
      const ctx = useContextStore();
      ctx.selectTeam(1);
      ctx.selectSource(1);

      const store = useExploreStore();
      store.setWindow(relative("15m"));
      store.setLogchefqlCode('level="error"');

      // The FIRST query call (this store's executeQuery, invoked synchronously
      // below) gets a controllable deferred. Setup-store watchers from earlier
      // tests are never disposed and share this reactive mock state, so they can
      // fire their own executeQuery when we mutate the source — those extra
      // calls fall through to an already-resolved default and must NOT hijack
      // our deferred resolver.
      let resolveQuery: (value: any) => void = () => {};
      mocks.logchefqlQuery.mockReset();
      mocks.logchefqlQuery.mockImplementationOnce(
        () => new Promise((resolve) => { resolveQuery = resolve; })
      );
      mocks.logchefqlQuery.mockImplementation(() =>
        Promise.resolve({ data: { logs: [], columns: [], stats: {} } })
      );

      return { ctx, store, resolveQuery: (v: any) => resolveQuery(v) };
    }

    it("aborts the in-flight query when it opens another source", async () => {
      const { resolveQuery, store } = armInFlightLogchefql();

      const abortSpy = vi.spyOn(AbortController.prototype, "abort");
      const exec = store.executeQuery();
      await Promise.resolve();
      await nextTick();

      // Measure only the aborts triggered by the source switch (other layers
      // may create/abort their own controllers).
      const abortsBeforeSwitch = abortSpy.mock.calls.length;

      mocks.sourcesState.currentSourceDetails = VL_SOURCE; // id 2
      mocks.sourcesState.teamSources = [VL_SOURCE];
      openUrl({ team: "1", source: "2" });
      expect(abortSpy.mock.calls.length).toBeGreaterThan(abortsBeforeSwitch);

      resolveQuery({ data: { logs: [], columns: [], stats: {} } });
      await exec;
      abortSpy.mockRestore();
    });

    it("drops a stale response that resolves after the source switched", async () => {
      const { ctx, resolveQuery, store } = armInFlightLogchefql();

      const exec = store.executeQuery();
      await Promise.resolve();
      await nextTick();

      mocks.sourcesState.currentSourceDetails = VL_SOURCE; // id 2
      mocks.sourcesState.teamSources = [VL_SOURCE];
      ctx.selectSource(2);
      await nextTick();

      // Response for the OLD source (1) arrives after the switch to 2.
      resolveQuery({
        data: {
          logs: [{ msg: "stale" }],
          columns: [{ name: "msg", type: "String" }],
          stats: {},
          conditions: [{ field: 'msg', operator: '~', value: 'stale', is_regex: true }],
          fields_used: ['msg'],
        },
      });
      const result = await exec;

      expect(store.logs).toEqual([]); // stale result never applied
      expect(result.error?.error_type).toBe("StaleResponse");
      expect(store.lastExecutedState?.compilation).toBeUndefined();
    });

    it("pairs compiled metadata with the executed query rather than later editor changes", async () => {
      const { store, resolveQuery } = armInFlightLogchefql();
      const exec = store.executeQuery();
      await nextTick();
      store.setLogchefqlCode('level="warn"');
      store.setLimit(500);
      store.setActiveMode('native');
      const conditions = [{ field: 'level', operator: '=', value: 'error', is_regex: false }];
      resolveQuery({ data: {
        logs: [{ level: 'error' }], columns: [], stats: {},
        generated_query: "SELECT * FROM logs WHERE level = 'error'",
        conditions, fields_used: ['level'],
      } });
      await exec;
      expect(store.lastExecutedState).toMatchObject({
        mode: 'logchefql', logchefqlQuery: 'level="error"', limit: 100,
        compilation: { conditions, fieldsUsed: ['level'] },
      });
      expect(store.logchefqlCode).toBe('level="warn"');

      store.setNativeQuery('SELECT 1');
      mocks.exploreGetLogs.mockResolvedValue({ status: 'success', data: { data: [{ n: 1 }], columns: [], stats: {} } });
      await store.executeQuery();
      expect(store.lastExecutedState?.mode).toBe('native');
      expect(store.lastExecutedState?.compilation).toBeUndefined();
    });

    it("clears compiled filters after executing an empty LogchefQL query", async () => {
      const { store, resolveQuery } = armInFlightLogchefql();
      const exec = store.executeQuery();
      await nextTick();
      resolveQuery({ data: { logs: [], columns: [], stats: {}, fields_used: ['level'],
        conditions: [{ field: 'level', operator: '=', value: 'error', is_regex: false }],
      } });
      await exec;
      store.setLogchefqlCode('');
      mocks.logchefqlQuery.mockResolvedValue({ data: { logs: [], columns: [], stats: {}, conditions: null, fields_used: null } });
      await store.executeQuery();
      expect(store.lastExecutedState?.compilation).toEqual({ conditions: [], fieldsUsed: [] });
    });
  });
});

describe("explore template dialect on load", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
    mocks.sourcesState.currentSourceDetails = VL_SOURCE;
    mocks.teamsState.currentTeamId = 1;
    useContextStore().selectTeam(1);
    useContextStore().selectSource(2);
  });

  const cases = [
    { mode: "native", language: "logsql", query: "service:my--svc [[ level:={{level}} ]]" },
    { mode: "logchefql", language: "logchefql", query: "service=my#svc [[ AND level={{level}} ]]" },
  ] as const;

  for (const { mode, language, query } of cases) {
    it.each(["url", "draft", "saved", "share"])(`detects ${language} variables from %s`, (path) => {
      const store = useExploreStore();
      if (path === "url") {
        store.initializeFromUrl({ mode, ...(mode === "native" ? { sql: query } : { q: query }) });
      } else if (path === "draft") {
        localStorage.setItem("logchef.explore.draft.1.2", JSON.stringify({
          version: 2, mode, nativeQuery: query, logchefqlCode: query,
        }));
        store.initializeFromUrl({});
      } else if (path === "saved") {
        store.hydrateFromResolvedQuery({
          id: 1, name: "Template", query_language: language,
          query_content: JSON.stringify({ content: query }),
        });
      } else {
        store.hydrateFromQueryShare({
          token: "template", source_id: 2, team_id: 1, share_url: "/s/template",
          created_at: "", expires_at: "", created_by: 1,
          payload: { version: 1, mode: mode === "native" ? "sql" : mode, query, limit: 100, time_range: { relative: "15m" } },
        });
      }
      expect(useVariableStore().allVariables).toMatchObject([{ name: "level", isOptional: true }]);
    });
  }
});

describe("explore template dialect after a mode switch", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
    mocks.sourcesState.currentSourceDetails = CH_SOURCE;
    mocks.teamsState.currentTeamId = 1;
    useContextStore().selectTeam(1);
    useContextStore().selectSource(1);
  });

  it("scans native SQL with the source dialect after a LogchefQL saved query", () => {
    const store = useExploreStore();
    store.hydrateFromResolvedQuery({
      id: 1, name: "Template", query_language: "logchefql",
      query_content: JSON.stringify({ content: "level={{a}}" }),
    });
    store.setActiveMode("native");
    expect(store.templateDialect).toBe("clickhouse-sql");
  });
});
