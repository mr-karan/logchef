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
import { useExploreRuns } from "../exploreRuns";
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

const ok = <T>(data: T) => ({ status: "success" as const, data });
const NO_LOGS = { logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 } };

// Explorer showing `source` with a team, as the navigator leaves it before a run.
function exploreSource(source: Source, window: TimeWindow = relative("15m")) {
  mocks.teamsState.currentTeamId = 1;
  mocks.sourcesState.currentSourceDetails = source;
  mocks.sourcesState.teamSources = [source];
  const contextStore = useContextStore();
  contextStore.selectTeam(1);
  contextStore.selectSource(source.id);
  mocks.logchefqlQuery.mockResolvedValue(ok(NO_LOGS));
  const store = useExploreStore();
  store.setWindow(window);
  return { store, runs: useExploreRuns() };
}

const settled = (runs: ReturnType<typeof useExploreRuns>) => vi.waitFor(() => expect(runs.isRunning).toBe(false));

// The LogchefQL requests that ran `text`. Stores left by earlier tests may also query.
const requestsFor = (text: string) => mocks.logchefqlQuery.mock.calls.filter((call) => call[2].query === text).map((call) => call[2]);

describe("explore store", () => {
  // Shares the editor's query and selects the share, as Explorer's Share button does.
  async function shareEditorQuery(token: string) {
    useContextStore().selectTeam(1);
    useContextStore().selectSource(10);
    mocks.teamsState.currentTeamId = 1;
    mocks.exploreCreateQueryShare.mockResolvedValue({ data: { token } });
    const store = useExploreStore();
    const share = await store.createQueryShare();
    if (!share) throw new Error("share was not created");
    store.markShare(share);
    return store;
  }

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
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = await shareEditorQuery("share-token");
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

    it("leaves the share selection untouched on a no-op mode switch", async () => {
      mocks.sourcesState.currentSourceDetails = CH_SOURCE;
      const store = await shareEditorQuery("share-token");
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
    it("resets the query content when it opens another source", () => {
      const contextStore = useContextStore();
      contextStore.selectTeam(1);
      contextStore.selectSource(10);

      const store = useExploreStore();
      store.setLogchefqlCode('level="error"');

      openUrl({ team: "1", source: "20" });

      expect(contextStore.sourceId).toBe(20);
      expect(store.logchefqlCode).toBe("");
      expect(store.nativeQuery).toBe("");
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

  describe("run windows", () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("resolves a relative window when each run starts", async () => {
      const { runs } = exploreSource(VL_SOURCE, relative("15m"));
      runs.run({ mode: "logchefql", text: 'at="each run"' });
      await settled(runs);
      vi.setSystemTime(new Date("2026-01-01T00:10:00Z"));
      runs.run({ mode: "logchefql", text: 'at="each run"' });
      await settled(runs);

      expect(requestsFor('at="each run"').map((request) => request.end_time)).toEqual(["2026-01-01T00:00:00.000Z", "2026-01-01T00:10:00.000Z"]);
    });

    it("leaves an absolute window as it is across runs", async () => {
      const { runs } = exploreSource(VL_SOURCE, absolute(0, 1_000_000));
      runs.run({ mode: "logchefql", text: 'at="absolute"' });
      await settled(runs);
      runs.run({ mode: "logchefql", text: 'at="absolute"' });
      await settled(runs);

      expect(requestsFor('at="absolute"').map((request) => [request.start_time, request.end_time])).toEqual([
        ["1970-01-01T00:00:00.000Z", "1970-01-01T00:16:40.000Z"],
        ["1970-01-01T00:00:00.000Z", "1970-01-01T00:16:40.000Z"],
      ]);
    });
  });

  describe("Run validation", () => {
    async function validatedRun(code: string) {
      const { runs } = exploreSource(VL_SOURCE);
      runs.run({ mode: "logchefql", text: code }, { validate: true });
      await settled(runs);
      return runs;
    }

    it("accepts valid LogchefQL for a VictoriaLogs source without requiring a table name", async () => {
      mocks.logchefqlValidate.mockResolvedValue(ok({ valid: true }));
      const runs = await validatedRun('lvl="WARN"');

      expect(mocks.logchefqlValidate).toHaveBeenCalledWith(1, 2, 'lvl="WARN"');
      expect(requestsFor('lvl="WARN"')).toHaveLength(1);
      expect(runs.error).toBeNull();
    });

    it("rejects LogchefQL that the server says is invalid and shows why", async () => {
      mocks.logchefqlValidate.mockResolvedValue(ok({ valid: false, error: { code: "PARSE_ERROR", message: "Expected a field value" } }));
      const runs = await validatedRun('lvl="WARN"');

      expect(runs.error).toBe("Expected a field value");
      expect(mocks.logchefqlQuery).not.toHaveBeenCalled();
    });

    it("validates a quoted variable without doubling its quotes", async () => {
      mocks.logchefqlValidate.mockResolvedValue(ok({ valid: true }));
      await validatedRun('p.resp.FillNumber="{{ trade_id }}"|p.order_number');

      expect(mocks.logchefqlValidate).toHaveBeenCalledWith(1, 2, 'p.resp.FillNumber="__VAR_trade_id__"|p.order_number');
      expect(requestsFor('p.resp.FillNumber="{{ trade_id }}"|p.order_number')).toHaveLength(1);
    });
  });

  describe("LogchefQL execution", () => {
    it("sends a VictoriaLogs query through the source-aware LogchefQL endpoint", async () => {
      const { runs } = exploreSource(VL_SOURCE);
      const execution = runs.run({ mode: "logchefql", text: 'lvl="WARN"' });
      await settled(runs);

      expect(execution?.status).toBe("succeeded");
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

    it("sends the window as RFC3339 instants with the preference zone", async () => {
      const { runs } = exploreSource(VL_SOURCE, absolute(Date.UTC(2026, 7, 8, 4, 30), Date.UTC(2026, 7, 8, 4, 45)));
      runs.run({ mode: "logchefql", text: 'at="rfc3339"' });
      await settled(runs);

      expect(requestsFor('at="rfc3339"')[0]).toMatchObject({
        start_time: "2026-08-08T04:30:00.000Z",
        end_time: "2026-08-08T04:45:00.000Z",
        timezone: usePreferencesStore().zone,
      });
    });

    it("resolves today at midnight in the preference zone, not the browser's", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-10-19T20:45:00Z")); // already Oct 20 in the IST browser
      try {
        const { runs } = exploreSource(VL_SOURCE);
        await usePreferencesStore().updatePreferences({ timezone: "utc" });
        openUrl({ team: "1", source: String(VL_SOURCE.id), t: "today", mode: "logchefql", q: 'at="today"' });
        runs.run({ mode: "logchefql", text: 'at="today"' });
        await settled(runs);

        expect(requestsFor('at="today"')[0]).toMatchObject({
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
      const { runs } = exploreSource(CH_SOURCE);
      await usePreferencesStore().updatePreferences({ timezone: "utc" });
      mocks.logchefqlQuery.mockResolvedValue(ok({ ...NO_LOGS, generated_sql: "SELECT 1" }));

      openUrl({ team: "1", source: String(CH_SOURCE.id), start: String(start), end: String(end), mode: "logchefql", q: 'at="evidence"' });
      runs.run({ mode: "logchefql", text: 'at="evidence"' });
      await settled(runs);

      expect(requestsFor('at="evidence"')[0]).toMatchObject({
        start_time: new Date(start).toISOString(),
        end_time: new Date(end).toISOString(),
        timezone: "UTC",
      });
      expect(mocks.histogramFetch.mock.lastCall?.[0]).toMatchObject({ queryText: "SELECT 1", window: { start, end, zone: "UTC" } });
    });
  });

  describe("Ask Chef context window", () => {
    const QUERY = 'ask="chef"';

    // The window of the log query that runs right after Ask Chef opened.
    async function logQueryWindow(runs: ReturnType<typeof useExploreRuns>): Promise<{ start: number; end: number; zone: string }> {
      runs.run({ mode: "logchefql", text: QUERY });
      await settled(runs);
      const request = requestsFor(QUERY).at(-1);
      return { start: Date.parse(request.start_time), end: Date.parse(request.end_time), zone: request.timezone };
    }

    it("is the window the log query reads for an absolute range", async () => {
      const { store, runs } = exploreSource(VL_SOURCE, absolute(Date.UTC(2026, 7, 8, 4, 30), Date.UTC(2026, 7, 8, 4, 45)));
      store.setLogchefqlCode(QUERY);

      const askChef = useAskChefStore();
      askChef.openForExplorer();

      expect(askChef.context).toMatchObject(await logQueryWindow(runs));
    });

    it("is the window the log query reads for a relative range", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(new Date("2026-08-08T10:00:00Z"));
      try {
        const { store, runs } = exploreSource(VL_SOURCE, relative("1h"));
        store.setLogchefqlCode(QUERY);

        const askChef = useAskChefStore();
        askChef.openForExplorer();

        expect(askChef.context).toMatchObject(await logQueryWindow(runs));
      } finally {
        vi.useRealTimers();
      }
    });

    it("is the window of the results Explorer shows once a query ran", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      vi.setSystemTime(new Date("2026-08-08T10:00:00Z"));
      try {
        const { runs } = exploreSource(VL_SOURCE, relative("1h"));
        const ran = await logQueryWindow(runs);
        vi.setSystemTime(new Date("2026-08-08T10:30:00Z"));

        const askChef = useAskChefStore();
        askChef.openForExplorer();

        expect(askChef.context).toMatchObject(ran);
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

    it("prefers the share token over time/limit params", async () => {
      const store = useExploreStore();
      store.setWindow(relative("15m"));
      await shareEditorQuery("tok123");

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
  // #101: a query in flight when Explorer enters another location is aborted,
  // and its late response never shows there.
  // ---------------------------------------------------------------------------
  describe("async races on source switch (#101)", () => {
    function armInFlightLogchefql() {
      const { store, runs } = exploreSource(CH_SOURCE);
      store.setLogchefqlCode('level="error"');
      let resolveQuery: (value: unknown) => void = () => {};
      mocks.logchefqlQuery.mockImplementationOnce(() => new Promise((resolve) => { resolveQuery = resolve; }));
      runs.run(store.editorQuery);
      return { store, runs, resolveQuery: (value: unknown) => resolveQuery(value) };
    }

    it("aborts the in-flight query when Explorer enters another location", async () => {
      const { runs, resolveQuery } = armInFlightLogchefql();
      const signal: AbortSignal = mocks.logchefqlQuery.mock.lastCall?.[3].signal;

      runs.enter(null);
      expect(signal.aborted).toBe(true);
      expect(runs.isRunning).toBe(false);
      resolveQuery(ok(NO_LOGS));
    });

    it("drops a stale response that resolves after Explorer entered another location", async () => {
      const { runs, resolveQuery } = armInFlightLogchefql();

      runs.enter(null);
      resolveQuery(ok({
        logs: [{ msg: "stale" }],
        columns: [{ name: "msg", type: "String" }],
        stats: {},
        conditions: [{ field: 'msg', operator: '~', value: 'stale', is_regex: true }],
        fields_used: ['msg'],
      }));
      await nextTick();
      await Promise.resolve();

      expect(runs.shown).toBeNull();
      expect(runs.results).toBeNull();
    });

    it("pairs compiled metadata with the executed query rather than later editor changes", async () => {
      const { store, runs, resolveQuery } = armInFlightLogchefql();
      store.setLogchefqlCode('level="warn"');
      store.setLimit(500);
      store.setActiveMode('native');
      const conditions = [{ field: 'level', operator: '=', value: 'error', is_regex: false }];
      resolveQuery(ok({
        logs: [{ level: 'error' }], columns: [], stats: {},
        generated_query: "SELECT * FROM logs WHERE level = 'error'",
        conditions, fields_used: ['level'],
      }));
      await settled(runs);
      expect(runs.shown).toMatchObject({ query: { mode: 'logchefql', text: 'level="error"' }, limit: 100 });
      expect(runs.results?.compilation).toEqual({ conditions, fieldsUsed: ['level'] });
      expect(store.logchefqlCode).toBe('level="warn"');

      store.setNativeQuery('SELECT 1');
      mocks.exploreGetLogs.mockResolvedValue(ok({ data: [{ n: 1 }], columns: [], stats: {} }));
      runs.run(store.editorQuery);
      await settled(runs);
      expect(runs.shown?.query.mode).toBe('native');
      expect(runs.results?.compilation).toBeNull();
    });

    it("clears compiled filters after executing an empty LogchefQL query", async () => {
      const { store, runs, resolveQuery } = armInFlightLogchefql();
      resolveQuery(ok({ logs: [], columns: [], stats: {}, fields_used: ['level'],
        conditions: [{ field: 'level', operator: '=', value: 'error', is_regex: false }],
      }));
      await settled(runs);
      store.setLogchefqlCode('');
      mocks.logchefqlQuery.mockResolvedValue(ok({ logs: [], columns: [], stats: {}, conditions: null, fields_used: null }));
      runs.run(store.editorQuery);
      await settled(runs);
      expect(runs.results?.compilation).toEqual({ conditions: [], fieldsUsed: [] });
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
