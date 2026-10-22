import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { teamsApi, type UserTeamMembership } from "@/api/teams";
import { sourcesApi, type Source } from "@/api/sources";
import { logchefqlApi } from "@/api/logchefql";
import { exploreApi } from "@/api/explore";
import { savedQueriesApi } from "@/api/savedQueries";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useExploreStore } from "@/stores/explore";
import { useSourcesStore } from "@/stores/sources";
import { epochMs } from "@/utils/timeWindow";
import { exploreWindowQuery } from "@/utils/alertExplore";
import { contextRouterGuard } from "@/router/contextGuard";
import { i18n } from "@/i18n";
import FieldSideBar from "@/components/field-sidebar/FieldSideBar.vue";

import { useExplorerNavigator } from "../useExplorerNavigator";
import { useQuery } from "../useQuery";
import { useSavedQueries } from "../useSavedQueries";

// Real router, history and stores. Only the HTTP API boundaries are stubbed.
const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });
const team: UserTeamMembership = { id: 1, name: "payments", description: "", created_at: "", updated_at: "", member_count: 1, role: "member" };
const source: Source = {
  id: 3,
  name: "checkout-logs",
  _meta_is_auto_created: false,
  source_type: "clickhouse",
  _meta_ts_field: "timestamp",
  connection: { host: "", database: "logs", table_name: "checkout" },
  ttl_days: 30,
  created_at: "",
  updated_at: "",
  is_connected: true,
};
const otherSource: Source = { ...source, id: 4, name: "cart-logs", connection: { host: "", database: "logs", table_name: "cart" } };
const sqlOnlySource: Source = { ...source, id: 5, name: "audit-logs", query_languages: ["clickhouse-sql"] };
const otherTeam: UserTeamMembership = { ...team, id: 2, name: "search" };
const otherTeamSource: Source = { ...source, id: 7, name: "search-logs" };
const offlineSource: Source = { ...source, id: 6, name: "legacy-search-logs", is_connected: false };

const sourceById = (id: number) => [source, otherSource, sqlOnlySource, otherTeamSource, offlineSource].find((s) => s.id === id) ?? source;
const sourcesOfTeam = (teamId: number) => (teamId === otherTeam.id ? [otherTeamSource] : [source, otherSource, sqlOnlySource]);

const savedOnOtherSource = {
  id: 42, source_id: otherSource.id, resolved_team_id: team.id, name: "cart errors", description: "",
  query_language: "logchefql", editor_mode: "builder", created_at: "", updated_at: "",
};
const savedContent = (content: string) =>
  JSON.stringify({ version: 1, sourceId: otherSource.id, timeRange: { relative: "1h" }, limit: 50, content });
const shareOnOtherSource = {
  token: "tok", share_url: "", team_id: team.id, source_id: otherSource.id, expires_at: "", created_at: "", created_by: 1,
  payload: { version: 2, mode: "logchefql", query: 'shared="yes"', limit: 50, time_range: { relative: "1h" } },
};
const shareForm = { token: "tok2", share_url: "http://logchef.test/share/tok2" };
const saveForm = {
  name: "cart errors", description: "", source_id: source.id, created_from_team_id: team.id, query_language: "logchefql" as const,
  editor_mode: "builder" as const, query_content: JSON.stringify({ version: 1, sourceId: source.id, limit: 100, content: 'saved="yes"' }),
};
const savedFromForm = { ...savedOnOtherSource, id: 77, source_id: source.id, query_content: saveForm.query_content };
const noLogs = { logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 }, generated_sql: "SELECT 1" };
const listFailure = { status: "error", message: "Sources are unavailable", error_type: "ServerError" };

function deferred() {
  let release: () => void = () => {};
  const done = new Promise<void>((resolve) => { release = resolve; });
  return { done, release };
}

// Two evidence chips, as Ask Chef pushes them, with whole-second instants.
const evidenceA = exploreWindowQuery({ teamId: 1, sourceId: 3, start: Date.UTC(2026, 9, 19, 4), end: Date.UTC(2026, 9, 19, 5), query: 'level="error"' });
const evidenceB = exploreWindowQuery({ teamId: 1, sourceId: 3, start: Date.UTC(2026, 9, 18, 9), end: Date.UTC(2026, 9, 18, 10), query: 'service="cart"' });
const evidenceOther = exploreWindowQuery({ teamId: 1, sourceId: 4, start: Date.UTC(2026, 9, 19, 6), end: Date.UTC(2026, 9, 19, 7), query: 'status=500' });

describe("Explorer history navigation", () => {
  let unmount: (() => void) | undefined;

  afterEach(() => {
    unmount?.();
    unmount = undefined;
    vi.restoreAllMocks();
    localStorage.clear();
  });

  // `stub` replaces API stubs before Explorer reads its first location.
  async function mountExplorer(location: Record<string, string> = { team: "1", source: "3" }, stub?: () => void) {
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([team, otherTeam]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockImplementation((teamId: number) => ok(sourcesOfTeam(teamId)) as never);
    vi.spyOn(sourcesApi, "getTeamSource").mockImplementation(
      (_teamId: number, sourceId: number) => ok(sourceById(sourceId)) as never,
    );
    const query = vi.spyOn(logchefqlApi, "query").mockReturnValue(ok(noLogs) as never);
    vi.spyOn(logchefqlApi, "validate").mockReturnValue(ok({ valid: true }) as never);
    vi.spyOn(exploreApi, "getHistogramData").mockReturnValue(ok({ data: [], granularity: "1m" }) as never);

    // A conflicting draft for the same team and source.
    localStorage.setItem(
      "logchef.explore.draft.1.3",
      JSON.stringify({ version: 1, mode: "logchefql", nativeQuery: "", logchefqlCode: 'draft="yes"', limit: 100, relativeTime: "7d", absoluteStart: null, absoluteEnd: null, timezone: null, variables: [] }),
    );

    stub?.();

    const pinia = createPinia();
    setActivePinia(pinia);
    // App.vue creates the explore store before the first navigation.
    const store = useExploreStore();
    let nav: ReturnType<typeof useExplorerNavigator> | undefined;
    let saved: ReturnType<typeof useSavedQueries> | undefined;
    let editor: ReturnType<typeof useQuery> | undefined;
    const Explorer = defineComponent({
      setup() {
        editor = useQuery();
        nav = useExplorerNavigator();
        saved = useSavedQueries();
        return () => null;
      },
    });
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/logs/explore", component: Explorer }] });
    router.beforeEach(contextRouterGuard);
    await router.push({ path: "/logs/explore", query: location });
    const app = createApp({ render: () => h(RouterView) }).use(pinia).use(router);
    app.mount(document.createElement("div"));
    unmount = () => app.unmount();
    const opened = nav;
    const shownEditor = editor;
    if (!opened || !saved || !shownEditor) throw new Error("explorer did not mount");
    await vi.waitFor(() => expect(opened.status.value).not.toBe("loading"));
    // What Explorer's query error banner shows.
    const banner = () => store.error?.message ?? null;
    return { router, store, query, nav: opened, saved, editor: shownEditor, banner, pinia };
  }

  async function settle(store: ReturnType<typeof useExploreStore>) {
    await vi.waitFor(() => expect(store.isExecutingQuery).toBe(false));
    await new Promise((resolve) => setImmediate(resolve));
  }

  function expectOpen(
    router: ReturnType<typeof createRouter>,
    store: ReturnType<typeof useExploreStore>,
    query: ReturnType<typeof vi.spyOn>,
    evidence: Record<string, string>,
  ) {
    expect(router.currentRoute.value.query).toMatchObject({ q: evidence.q, mode: "logchefql", start: evidence.start, end: evidence.end });
    expect(store.logchefqlCode).toBe(evidence.q);
    expect(store.urlQueryParameters).toMatchObject({ q: evidence.q, start: evidence.start, end: evidence.end });
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidence.q });
  }

  // Each navigation runs exactly one query, and nothing aborts it.
  async function expectOneRun(query: ReturnType<typeof vi.spyOn>, navigate: () => Promise<unknown>, store: ReturnType<typeof useExploreStore>) {
    const before = query.mock.calls.length;
    await navigate();
    await settle(store);
    const calls = query.mock.calls.slice(before);
    expect(calls).toHaveLength(1);
    expect((calls[0][3] as { signal: AbortSignal }).signal.aborted).toBe(false);
  }

  async function expectNoRun(query: ReturnType<typeof vi.spyOn>, navigate: () => Promise<unknown>, store: ReturnType<typeof useExploreStore>) {
    const before = query.mock.calls.length;
    await navigate();
    await settle(store);
    expect(query.mock.calls.length).toBe(before);
  }

  // Runs a navigation race, then runs out the timers, so a late superseded
  // navigation would show up here.
  async function raceAndOutwait(
    query: ReturnType<typeof vi.spyOn>,
    store: ReturnType<typeof useExploreStore>,
    race: () => Promise<unknown>,
    expectRuns = expectOneRun,
  ) {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      await expectRuns(query, race, store);
      const before = query.mock.calls.length;
      await vi.advanceTimersByTimeAsync(6000);
      await settle(store);
      expect(query.mock.calls.length).toBe(before);
    } finally {
      vi.useRealTimers();
    }
  }

  it("reopens each evidence link on Back and Forward instead of the saved draft", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    expectOpen(router, store, query, evidenceA);

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);
    expectOpen(router, store, query, evidenceB);

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceA.start));
    }, store);
    expectOpen(router, store, query, evidenceA);

    await expectOneRun(query, async () => {
      router.forward();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceB.start));
    }, store);
    expectOpen(router, store, query, evidenceB);
  });

  it("runs one query when Explorer opens", async () => {
    const { store, query } = await mountExplorer();
    await settle(store);
    expect(query).toHaveBeenCalledTimes(1);
  });

  it("opens a link to another source with the link's query and window", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceOther }), store);
    expect(store.sourceId).toBe(otherSource.id);
    expectOpen(router, store, query, evidenceOther);
  });

  it("goes Back to the previous source's query after picking another source", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);
    store.setLogchefqlCode('unrun="edit"');

    await expectOneRun(query, () => nav.selectSource(otherSource.id), store);
    expect(router.currentRoute.value.query.source).toBe(String(otherSource.id));

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.source).toBe(String(source.id)));
    }, store);
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceB);
  });

  it("keeps the URL in step with a window chosen after picking a source", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => nav.selectSource(otherSource.id), store);

    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "15m" }), store);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), t: "15m" });
  });

  it("runs nothing when Back returns to the shown query before the next source loads", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    let releaseOther: () => void = () => {};
    const otherLoaded = new Promise<void>((resolve) => { releaseOther = resolve; });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      if (sourceId === otherSource.id) await otherLoaded;
      return ok(sourceById(sourceId)) as never;
    });

    await raceAndOutwait(query, store, async () => {
      await router.push({ path: "/logs/explore", query: evidenceOther });
      await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(team.id, otherSource.id));
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.source).toBe(String(source.id)));
      releaseOther();
    }, expectNoRun);
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceA);
  });

  it("opens the last of two quick links when the first source is still loading", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    let releaseOther: () => void = () => {};
    const otherLoaded = new Promise<void>((resolve) => { releaseOther = resolve; });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      if (sourceId === otherSource.id) await otherLoaded;
      return ok(sourceById(sourceId)) as never;
    });

    await raceAndOutwait(query, store, async () => {
      await router.push({ path: "/logs/explore", query: evidenceOther });
      await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(team.id, otherSource.id));
      await router.push({ path: "/logs/explore", query: evidenceB });
      releaseOther();
      await vi.waitFor(() => expect(store.sourceId).toBe(source.id));
    });
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceB);
  });

  it("goes Back to the previous team's query after picking another team", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    await expectOneRun(query, () => nav.selectTeam(otherTeam.id), store);
    expect(router.currentRoute.value.query).toMatchObject({ team: String(otherTeam.id), source: String(otherTeamSource.id) });

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.team).toBe(String(team.id)));
    }, store);
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceA);
  });

  it("keeps Back and Forward working after re-picking the current source", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);

    await nav.selectSource(source.id);
    await settle(store);

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceA.start));
    }, store);
    await expectOneRun(query, async () => {
      router.forward();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceB.start));
    }, store);
    expectOpen(router, store, query, evidenceB);
  });

  it("pushes the mode the picked source supports", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      await new Promise((resolve) => setTimeout(resolve, 80));
      return ok(sourceById(sourceId)) as never;
    });

    await nav.selectSource(sqlOnlySource.id);
    await vi.waitFor(() => expect(store.activeMode).toBe("native"));
    await settle(store);
    expect(query).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(sqlOnlySource.id), mode: "native" });
  });

  it("re-runs when the user chooses the same relative range again", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "15m" }), store);
    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "15m" }), store);
    expect(router.currentRoute.value.query).toMatchObject({ t: "15m", q: evidenceA.q });
  });
  it("runs one query on a fresh load whose source details arrive after the link is read", async () => {
    const { store, query } = await mountExplorer(evidenceA, () => {
      vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
        await new Promise((resolve) => setTimeout(resolve, 80));
        return ok(sourceById(sourceId)) as never;
      });
    });
    await settle(store);
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
  });

  it("runs one query for a saved query link on another source", async () => {
    const { router, store, query } = await mountExplorer({ team: "1", source: "3", id: "42" }, () => {
      vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('saved="yes"') }) as never);
    });
    await settle(store);
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'saved="yes"', limit: 50 });
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), id: "42" });
  });

  it("opens a saved query picked on another source with one run", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('picked="yes"') }) as never);

    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }), store);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'picked="yes"' });
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), id: "42" });
  });

  it("runs one query for a share link on another source", async () => {
    const { router, store, query } = await mountExplorer({ team: "1", source: "3", share: "tok" }, () => {
      vi.spyOn(exploreApi, "getQueryShare").mockReturnValue(ok(shareOnOtherSource) as never);
    });
    await settle(store);
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'shared="yes"', limit: 50 });
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), share: "tok" });
  });

  it("shows the server's error for a share link it cannot open, and runs nothing", async () => {
    const { store, query, nav } = await mountExplorer({ share: "gone" }, () => {
      vi.spyOn(exploreApi, "getQueryShare").mockRejectedValue({ status: "error", message: "Share link expired", error_type: "NotFoundError" });
    });
    await settle(store);
    expect(nav.status.value).toBe("error");
    expect(nav.error.value).toBe("Share link expired");
    expect(query).not.toHaveBeenCalled();
  });

  it("opens the first connected source of a picked team", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(
      (teamId: number) => ok(teamId === otherTeam.id ? [offlineSource, otherTeamSource] : sourcesOfTeam(teamId)) as never,
    );

    const before = query.mock.calls.length;
    await nav.selectTeam(otherTeam.id);
    await settle(store);
    expect(store.sourceId).toBe(otherTeamSource.id);
    expect(query.mock.calls.length - before).toBe(1);
    expect(router.currentRoute.value.query).toMatchObject({ team: String(otherTeam.id), source: String(otherTeamSource.id) });
  });

  it("writes the restored draft of a bare team and source link to the URL", async () => {
    const { router, store } = await mountExplorer();
    await settle(store);
    expect(store.logchefqlCode).toBe('draft="yes"');
    expect(router.currentRoute.value.query).toMatchObject({ team: "1", source: "3", mode: "logchefql", q: 'draft="yes"', t: "7d", limit: "100" });
  });

  it("records the default SQL a SQL-only source runs, and reopening that URL runs it", async () => {
    const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
    const { router, store } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) }, () => localStorage.clear());
    await settle(store);
    expect(native).toHaveBeenCalledTimes(1);
    const sent = native.mock.lastCall?.[1].query_text;
    expect(sent).toContain("FROM logs.checkout");
    expect(router.currentRoute.value.query).toMatchObject({ mode: "native", sql: sent });

    const recorded = Object.fromEntries(Object.entries(router.currentRoute.value.query).map(([key, value]) => [key, String(value)]));
    unmount?.();
    native.mockClear();
    const reopened = await mountExplorer(recorded);
    await settle(reopened.store);
    expect(native).toHaveBeenCalledTimes(1);
    expect(native.mock.lastCall?.[1].query_text).toBe(sent);

    reopened.store.setNativeQuery("");
    await reopened.nav.run();
    await settle(reopened.store);
    expect(native).toHaveBeenCalledTimes(2);
    expect(reopened.router.currentRoute.value.query.sql).toBe(native.mock.lastCall?.[1].query_text);
    expect(reopened.store.nativeQuery).toBe(native.mock.lastCall?.[1].query_text);
  });

  it("runs nothing for a Run that a link supersedes while its query is validated", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    const validation = deferred();
    vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
      await validation.done;
      return ok({ valid: true }) as never;
    });
    store.setLogchefqlCode('typed="yes"');

    await expectOneRun(query, async () => {
      const running = nav.run();
      await vi.waitFor(() => expect(logchefqlApi.validate).toHaveBeenCalled());
      await router.push({ path: "/logs/explore", query: evidenceA });
      await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q }));
      validation.release();
      await running;
    }, store);
    expectOpen(router, store, query, evidenceA);
  });

  it("releases validation immediately on Stop and never runs its late result", async () => {
    const { store, query, nav, editor } = await mountExplorer();
    await settle(store);
    const validation = deferred();
    vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
      await validation.done;
      return ok({ valid: true });
    });
    store.setLogchefqlCode('stopped="yes"');
    const calls = query.mock.calls.length;
    const running = nav.run();
    await vi.waitFor(() => expect(editor.isExecutingQuery.value).toBe(true));
    await nav.cancel();
    expect(editor.isExecutingQuery.value).toBe(false);
    validation.release();
    await running;
    expect(query).toHaveBeenCalledTimes(calls);
  });

  it("opens a picked source when Stop cancels the query that ran before the pick", async () => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle(store);
    const response = deferred();
    query.mockImplementationOnce(async () => {
      await response.done;
      return ok(noLogs) as never;
    });
    const details = deferred();
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      if (sourceId === otherSource.id) await details.done;
      return ok(sourceById(sourceId)) as never;
    });
    store.setLogchefqlCode('slow="yes"');
    const running = nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));

    await router.push({ path: "/logs/explore", query: { team: "1", source: String(otherSource.id) } });
    expect(nav.status.value).toBe("navigating");
    await nav.cancel();
    details.release();
    response.release();
    await running;

    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    await settle(store);
    expect(store.sourceId).toBe(otherSource.id);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), mode: "logchefql" });
  });

  it("releases superseded validation without letting it clear a newer Run's loading state", async () => {
    const { router, store, nav, editor } = await mountExplorer();
    await settle(store);
    const old = deferred();
    const newer = deferred();
    vi.mocked(logchefqlApi.validate)
      .mockImplementationOnce(async () => { await old.done; return ok({ valid: false, error: { message: "Old error", code: "PARSE_ERROR" } }); })
      .mockImplementationOnce(async () => { await newer.done; return ok({ valid: true }); });
    store.setLogchefqlCode('old="yes"');
    const oldRun = nav.run();
    await vi.waitFor(() => expect(editor.isExecutingQuery.value).toBe(true));
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", mode: "native", sql: "" } });
    await vi.waitFor(() => expect(store.activeMode).toBe("native"));
    expect(editor.isExecutingQuery.value).toBe(false);
    store.setActiveMode("logchefql");
    store.setLogchefqlCode('newer="yes"');
    const newRun = nav.run();
    await vi.waitFor(() => expect(logchefqlApi.validate).toHaveBeenCalledTimes(2));
    old.release();
    await oldRun;
    expect(editor.isExecutingQuery.value).toBe(true);
    expect(store.error).toBeNull();
    newer.release();
    await newRun;
    await settle(store);
  });

  it("pushes one history entry with the query a Run ran, not the edits typed while it ran", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    const response = deferred();
    query.mockImplementationOnce(async () => {
      await response.done;
      return ok(noLogs) as never;
    });
    store.setLogchefqlCode('ran="yes"');

    const calls = query.mock.calls.length;
    await expectOneRun(query, async () => {
      const running = nav.run();
      await vi.waitFor(() => expect(query).toHaveBeenCalledTimes(calls + 1));
      store.setLogchefqlCode('typed="later"');
      response.release();
      await running;
    }, store);
    expect(router.currentRoute.value.query.q).toBe('ran="yes"');
    expect(store.logchefqlCode).toBe('typed="later"');

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe(evidenceA.q));
    }, store);
  });

  it("runs nothing and records nothing for a Run whose LogchefQL is invalid", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    vi.mocked(logchefqlApi.validate).mockReturnValue(
      ok({ valid: false, error: { code: "PARSE_ERROR", message: "Expected a field value" } }) as never,
    );
    store.setLogchefqlCode("level=");

    await expectNoRun(query, () => nav.run(), store);
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
  });

  it("runs once and replaces the URL when the limit changes", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    await expectOneRun(query, async () => nav.setLimit(500), store);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q, limit: 500 });
    expect(router.currentRoute.value.query).toMatchObject({ q: evidenceA.q, limit: "500" });

    router.back();
    await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe('draft="yes"'));
  });

  it.each([
    ["window", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["zoom", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "absolute", start: epochMs(1792346400000), end: epochMs(1792350000000) })],
    ["limit", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setLimit(500)],
  ])("replays the last-run text on a %s change and preserves an invalid draft", async (_change, change) => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle(store);
    store.setLogchefqlCode('level="error" and');
    await expectOneRun(query, async () => change(nav), store);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    expect(store.logchefqlCode).toBe('level="error" and');
    expect(store.isQueryStateDirty).toBe(true);
    expect(JSON.parse(localStorage.getItem('logchef.explore.draft.1.3') ?? '{}').logchefqlCode).toBe('level="error" and');
    expect(logchefqlApi.validate).not.toHaveBeenCalled();
  });

  it.each(["stopped", "failed"])("keeps the last successful query after a %s Run", async (outcome) => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle(store);
    const successful = store.lastExecutedState;
    const response = deferred();
    query.mockImplementationOnce(async (_team, _source, _request, options) => {
      await response.done;
      if (options?.signal?.aborted) throw new DOMException("Request canceled", "AbortError");
      throw new Error("Query failed");
    });
    store.setLogchefqlCode('service="cart"');
    const running = nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'service="cart"' }));
    if (outcome === "stopped") await nav.cancel();
    response.release();
    await running;
    await settle(store);

    expect(store.lastExecutedState).toEqual(successful);
    expect(store.isQueryStateDirty).toBe(true);
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "1h" }), store);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    expect(store.logchefqlCode).toBe('service="cart"');
    expect(store.isQueryStateDirty).toBe(true);
  });

  it.each([
    ["window", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["limit", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setLimit(500)],
  ])("replays the in-flight query on a %s change without marking it successful early", async (_change, change) => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle(store);
    const successful = store.lastExecutedState;
    const response = deferred();
    query.mockImplementationOnce(async () => {
      await response.done;
      return ok(noLogs);
    });
    store.setLogchefqlCode('service="cart"');
    const running = nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'service="cart"' }));
    const pending = store.lastExecutedState;
    store.setLogchefqlCode('service="cart" and');
    await expectOneRun(query, async () => change(nav), store);
    response.release();
    await running;

    expect(pending).toEqual(successful);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'service="cart"' });
    expect(router.currentRoute.value.query.q).toBe('service="cart"');
    expect(store.logchefqlCode).toBe('service="cart" and');
    expect(store.isQueryStateDirty).toBe(true);
  });

  it("replays the opened query when its first execution fails and the editor has unrun text", async () => {
    const { store, query, nav } = await mountExplorer(evidenceA, () => {
      vi.mocked(logchefqlApi.query).mockRejectedValueOnce(new Error("Query failed"));
    });
    await settle(store);
    expect(store.lastExecutedState).toBeUndefined();
    store.setLogchefqlCode('level="error" and');
    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "1h" }), store);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
    expect(store.logchefqlCode).toBe('level="error" and');
  });

  it("replays the last-run native query even after an unrun mode change", async () => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle(store);
    const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs) as never);
    store.setActiveMode("native");
    store.setNativeQuery("SELECT 1");
    await nav.run();
    await settle(store);
    store.setNativeQuery("SELECT broken");
    store.setActiveMode("logchefql");
    store.setLogchefqlCode('invalid="draft" and');
    const before = query.mock.calls.length;
    nav.setWindow({ kind: "relative", spec: "1h" });
    await settle(store);
    expect(native).toHaveBeenCalledTimes(2);
    expect(native.mock.lastCall?.[1]).toMatchObject({ query_text: "SELECT 1" });
    expect(query).toHaveBeenCalledTimes(before);
    expect(router.currentRoute.value.query).toMatchObject({ mode: "native", sql: "SELECT 1", t: "1h" });
    expect(router.currentRoute.value.query.q).toBeUndefined();
    expect(store.activeMode).toBe("logchefql");
    expect(store.nativeQuery).toBe("SELECT broken");
    expect(store.logchefqlCode).toBe('invalid="draft" and');
  });

  it("runs New query once and replaces the URL with its defaults", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    await expectOneRun(query, async () => nav.newQuery(), store);
    expect(store.logchefqlCode).toBe("");
    expect(router.currentRoute.value.query).toMatchObject({ q: "", t: "15m", limit: "100" });

    router.back();
    await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe('draft="yes"'));
  });

  it("opens the URL again on Retry after it failed to open", async () => {
    const { router, store, query, nav } = await mountExplorer({ share: "tok" }, () => {
      vi.spyOn(exploreApi, "getQueryShare")
        .mockRejectedValueOnce({ status: "error", message: "Share service is down", error_type: "ServerError" })
        .mockReturnValue(ok(shareOnOtherSource) as never);
    });
    expect(nav.status.value).toBe("error");

    await expectOneRun(query, () => nav.retry(), store);
    expect(nav.status.value).toBe("ready");
    expect(nav.error.value).toBeNull();
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), share: "tok" });
  });

  it("applies a window chosen while a team pick loads to that team", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    const teamLoaded = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async (teamId: number) => {
      if (teamId === otherTeam.id) await teamLoaded.done;
      return ok(sourcesOfTeam(teamId)) as never;
    });

    await expectOneRun(query, async () => {
      const picking = nav.selectTeam(otherTeam.id);
      await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id));
      expect(nav.status.value).toBe("navigating");
      nav.setWindow({ kind: "relative", spec: "1h" });
      teamLoaded.release();
      await picking;
      await vi.waitFor(() => expect(store.sourceId).toBe(otherTeamSource.id));
    }, store);
    expect(nav.status.value).toBe("ready");
    expect(router.currentRoute.value.query).toMatchObject({ team: String(otherTeam.id), source: String(otherTeamSource.id), t: "1h" });
  });

  it.each([
    ["the server rejects it", () => vi.spyOn(savedQueriesApi, "resolve").mockRejectedValue({ status: "error", message: "Saved query not found", error_type: "NotFoundError" })],
    ["its content is not JSON", () => vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: "{broken" }) as never)],
  ])("keeps the shown query when a saved query fails to open because %s", async (_reason, stub) => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    stub();

    await expectNoRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }), store);
    expect(nav.status.value).toBe("ready");
    expect(nav.error.value).toBeNull();
    expect(store.sourceId).toBe(source.id);
    expect(store.logchefqlCode).toBe(evidenceA.q);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), id: "42" });
  });

  it("keeps the shown query and the link when the linked team's sources fail to load", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    vi.mocked(sourcesApi.listTeamSources).mockImplementation((teamId: number) =>
      teamId === otherTeam.id ? Promise.reject(listFailure) : (ok(sourcesOfTeam(teamId)) as never),
    );
    const link = { team: String(otherTeam.id), source: String(otherTeamSource.id), q: 'a="b"', t: "1h" };

    await expectNoRun(query, () => router.push({ path: "/logs/explore", query: link }), store);
    expect(router.currentRoute.value.query).toEqual(link);
    expect(nav.status.value).toBe("ready");
    expect(store.sourceId).toBe(source.id);
    expect(store.logchefqlCode).toBe(evidenceA.q);
  });

  it("shows the error and keeps the link when the first team's sources fail to load", async () => {
    const { router, query, nav } = await mountExplorer(evidenceA, () => {
      vi.mocked(sourcesApi.listTeamSources).mockRejectedValue(listFailure);
    });
    expect(nav.status.value).toBe("error");
    expect(nav.error.value).toBe(listFailure.message);
    expect(router.currentRoute.value.query).toEqual(evidenceA);
    expect(query).not.toHaveBeenCalled();
  });

  it("clears a failed query's error when the next link opens", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    query.mockRejectedValueOnce(new Error("Connection reset"));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    expect(store.error?.message).toContain("Connection reset");

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);
    expect(store.error).toBeNull();
  });

  it("drops an older query's late results when a link on the same source runs nothing", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    const response = deferred();
    query.mockImplementationOnce(async () => {
      await response.done;
      return ok({ ...noLogs, logs: [{ msg: "evidence A" }] }) as never;
    });
    await router.push({ path: "/logs/explore", query: evidenceA });
    await vi.waitFor(() => expect(store.isExecutingQuery).toBe(true));

    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", mode: "native", sql: "" } });
    await vi.waitFor(() => expect(store.activeMode).toBe("native"));
    response.release();
    await settle(store);
    expect(store.logs).toEqual([]);
  });

  it("opens a link to the source another page picked while Explorer stayed open", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    useContextStore().selectSource(otherSource.id);
    const link = { ...store.urlQueryParameters };
    expect(link.source).toBe(String(otherSource.id));

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: link }), store);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
  });

  it("keeps an edited saved query selected so Update remains available", async () => {
    const { store, query, nav, saved } = await mountExplorer();
    await settle(store);
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('picked="yes"') }) as never);
    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }), store);
    expect(saved.isEditingExistingQuery.value).toBe(true);

    store.setLogchefqlCode('picked="edited"');
    expect(saved.isEditingExistingQuery.value).toBe(true);
    expect(store.selectedQueryId).toBe('42');
    expect(store.hasDivergedFromSavedQuery).toBe(true);
  });

  it("records an edited saved Run inline while keeping Update available until an inline URL opens", async () => {
    const { router, store, query, nav, saved } = await mountExplorer();
    await settle(store);
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('picked="yes"') }) as never);
    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }), store);
    store.setLogchefqlCode('picked="edited"');
    await expectOneRun(query, () => nav.run(), store);
    expect(router.currentRoute.value.query).toMatchObject({ mode: "logchefql", q: 'picked="edited"', limit: "50", t: "1h" });
    expect(router.currentRoute.value.query.id).toBeUndefined();
    expect(store.selectedQueryId).toBe("42");
    expect(saved.isEditingExistingQuery.value).toBe(true);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);
    router.back();
    await vi.waitFor(() => expect(store.logchefqlCode).toBe('picked="edited"'));
    expect(store.selectedQueryId).toBeNull();
  });

  it("updates the selected query with the editor text and clears its modified state", async () => {
    const { store, query, nav, saved } = await mountExplorer();
    await settle(store);
    const original = { ...savedOnOtherSource, query_content: savedContent('picked="yes"') };
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok(original) as never);
    vi.spyOn(savedQueriesApi, "list").mockReturnValue(ok([]) as never);
    const update = vi.spyOn(savedQueriesApi, "update").mockImplementation((_id, data) => ok({ ...original, ...data }) as never);
    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }), store);
    store.setLogchefqlCode('picked="updated"');
    const content = savedContent('picked="updated"');
    const result = await nav.recordAfter(() => saved.handleSaveQuery({ ...saveForm, source_id: otherSource.id, query_content: content }), store.markSavedQuery);
    expect(update).toHaveBeenCalledWith("42", expect.objectContaining({ query_content: content }));
    expect(result?.id).toBe(42);
    expect(store.selectedQueryId).toBe("42");
    expect(store.hasDivergedFromSavedQuery).toBe(false);
  });

  it("detects edits after Save as new and records their Run inline", async () => {
    const explorer = await mountExplorer();
    const { router, store, query, nav } = explorer;
    await settle(store);
    stubShareAndSave();
    store.setLogchefqlCode('saved="yes"');
    await save(explorer);
    expect(store.selectedQueryId).toBe("77");
    expect(store.hasDivergedFromSavedQuery).toBe(false);
    store.setLogchefqlCode('saved="edited"');
    expect(store.hasDivergedFromSavedQuery).toBe(true);
    expect(store.selectedQueryId).toBe("77");
    await expectOneRun(query, () => nav.run(), store);
    expect(router.currentRoute.value.query.q).toBe('saved="edited"');
    expect(router.currentRoute.value.query.id).toBeUndefined();
    expect(store.selectedQueryId).toBe("77");
  });

  it("keeps a removed team source out of a navigation that was resolving it", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    const sources = useSourcesStore();
    router.addRoute({ path: "/manage", component: defineComponent({ render: () => null }) });
    await router.push({ path: "/manage", query: { team: "1", source: "3" } });
    const removal = deferred();
    vi.spyOn(teamsApi, "removeTeamSource").mockImplementation(async () => {
      await removal.done;
      return ok(null) as never;
    });
    const removing = useTeamsStore().removeTeamSource(1, 4);
    const oldDetails = deferred();
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_team, id) => {
      if (id === 4) await oldDetails.done;
      return ok(sourceById(id)) as never;
    });
    await router.push({ path: "/logs/explore", query: { team: "1", source: "4" } });
    await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(1, 4));
    vi.mocked(sourcesApi.listTeamSources).mockReturnValue(ok([source, sqlOnlySource]) as never);
    removal.release();
    await removing;
    await vi.waitFor(() => expect(sources.teamSources.some(s => s.id === 4)).toBe(false));
    const queriesBefore = query.mock.calls.length;
    oldDetails.release();
    await vi.waitFor(() => expect(query.mock.calls.length).toBeGreaterThan(queriesBefore));
    await settle(store);
    expect({
      path: router.currentRoute.value.path,
      selected: store.sourceId,
      cached: sources.teamSources.map(s => s.id),
      queried: query.mock.lastCall?.[1],
    }).toEqual({ path: "/logs/explore", selected: source.id, cached: [source.id, sqlOnlySource.id], queried: source.id });
  });

  it("fetches a disconnected pick once, then retries it on the next navigation", async () => {
    const { router, store, query, nav } = await mountExplorer(undefined, () => {
      vi.mocked(sourcesApi.listTeamSources).mockImplementation(teamId => ok(teamId === team.id ? [source, otherSource, offlineSource] : sourcesOfTeam(teamId)));
    });
    await settle(store);
    const details = vi.mocked(sourcesApi.getTeamSource);
    details.mockClear();
    const before = query.mock.calls.length;
    await nav.selectSource(offlineSource.id);
    await vi.waitFor(() => expect(store.sourceId).toBe(offlineSource.id));
    await new Promise(resolve => setImmediate(resolve));
    expect(details).toHaveBeenCalledTimes(1);
    expect(query).toHaveBeenCalledTimes(before);
    details.mockImplementation((_team, id) => ok({ ...sourceById(id), is_connected: true }));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: { team: "1", source: String(offlineSource.id), q: 'retry="yes"' } }), store);
    expect(details).toHaveBeenCalledTimes(2);
  });

  it("loads only the requested team's metadata with empty localStorage", async () => {
    const { store, query } = await mountExplorer({ team: String(otherTeam.id), source: String(otherTeamSource.id), q: 'first="yes"' }, () => localStorage.clear());
    await settle(store);
    expect({ lists: vi.mocked(sourcesApi.listTeamSources).mock.calls.length, details: vi.mocked(sourcesApi.getTeamSource).mock.calls.length }).toEqual({ lists: 1, details: 1 });
    expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id);
    expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(otherTeam.id, otherTeamSource.id);
    expect(query).toHaveBeenCalledTimes(1);
  });

  it("makes no metadata request on a same-source Back or Forward, one on a source pick and two on a team pick", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);
    const metadataRequests = async (navigate: () => Promise<unknown>) => {
      const [lists, details] = [vi.mocked(sourcesApi.listTeamSources).mock.calls.length, vi.mocked(sourcesApi.getTeamSource).mock.calls.length];
      await expectOneRun(query, navigate, store);
      return [vi.mocked(sourcesApi.listTeamSources).mock.calls.length - lists, vi.mocked(sourcesApi.getTeamSource).mock.calls.length - details];
    };

    expect(await metadataRequests(async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe(evidenceA.q));
    })).toEqual([0, 0]);
    expect(await metadataRequests(async () => {
      router.forward();
      await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe(evidenceB.q));
    })).toEqual([0, 0]);
    expect(await metadataRequests(() => nav.selectSource(otherSource.id))).toEqual([0, 1]);
    expect(await metadataRequests(() => nav.selectTeam(otherTeam.id))).toEqual([1, 1]);
  });

  it("fetches a team for another page after a navigation to that team was superseded", async () => {
    const { router, store } = await mountExplorer();
    await settle(store);
    const teamLoaded = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async (teamId: number) => {
      if (teamId === otherTeam.id) await teamLoaded.done;
      return ok(sourcesOfTeam(teamId)) as never;
    });
    void router.push({ path: "/logs/explore", query: { team: String(otherTeam.id) } });
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id));
    await router.push({ path: "/logs/explore", query: { team: "1", source: String(otherSource.id) } });
    teamLoaded.release();
    await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(otherTeam.id, otherTeamSource.id));
    await settle(store);
    expect(store.sourceId).toBe(otherSource.id);

    vi.mocked(sourcesApi.getTeamSource).mockImplementation(
      (_teamId: number, sourceId: number) => ok(sourceId === otherTeamSource.id ? { ...otherTeamSource, is_connected: false } : sourceById(sourceId)) as never,
    );
    const [lists, details] = [vi.mocked(sourcesApi.listTeamSources).mock.calls.length, vi.mocked(sourcesApi.getTeamSource).mock.calls.length];
    useContextStore().selectTeam(otherTeam.id);

    await vi.waitFor(() => expect(useSourcesStore().currentSourceDetails?.is_connected).toBe(false));
    expect(vi.mocked(sourcesApi.listTeamSources).mock.calls.length - lists).toBe(1);
    expect(vi.mocked(sourcesApi.getTeamSource).mock.calls.length - details).toBe(1);
  });

  // What Explorer's Share and Save buttons do once their API call returns.
  type Mounted = Awaited<ReturnType<typeof mountExplorer>>;
  const share = ({ store, nav }: Mounted) => nav.recordAfter(() => store.createQueryShare(), (created) => store.setActiveShareToken(created.token));
  const save = ({ store, nav, saved }: Mounted) => nav.recordAfter(() => saved.handleSaveQuery({ ...saveForm, query_content: JSON.stringify({ version: 1, sourceId: store.sourceId, limit: store.limit, content: store.logchefqlCode }) }), store.markSavedQuery);
  const stubShareAndSave = () => [
    vi.spyOn(exploreApi, "createQueryShare").mockReturnValue(ok(shareForm) as never),
    vi.spyOn(savedQueriesApi, "create").mockImplementation(data => ok({ ...savedFromForm, ...data }) as never),
    vi.spyOn(savedQueriesApi, "list").mockReturnValue(ok([]) as never),
  ];

  it.each([["Share", share], ["Save", save]])("ignores a %s while a navigation loads, and the navigation still opens", async (_action, act) => {
    const explorer = await mountExplorer();
    const { router, store, nav } = explorer;
    await settle(store);
    const [createShare, createSaved] = stubShareAndSave();
    const detailsLoaded = deferred();
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      if (sourceId === otherSource.id) await detailsLoaded.done;
      return ok(sourceById(sourceId)) as never;
    });

    await router.push({ path: "/logs/explore", query: { team: "1", source: String(otherSource.id) } });
    expect(nav.status.value).toBe("navigating");
    await act(explorer);
    detailsLoaded.release();

    await vi.waitFor(() => expect(store.sourceId).toBe(otherSource.id));
    await settle(store);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), mode: "logchefql" });
    expect(createShare).not.toHaveBeenCalled();
    expect(createSaved).not.toHaveBeenCalled();

    await act(explorer);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), ...(act === share ? { share: "tok2" } : { id: "77" }) });
  });

  it.each([["Share", share, "createQueryShare"], ["Save", save, "create"]] as const)(
    "drops a %s that returns after a navigation started",
    async (_action, act, method) => {
      const explorer = await mountExplorer();
      const { router, store, query } = explorer;
      await settle(store);
      stubShareAndSave();
      const response = deferred();
      const api = method === "createQueryShare" ? vi.mocked(exploreApi.createQueryShare) : vi.mocked(savedQueriesApi.create);
      api.mockImplementation(async () => {
        await response.done;
        return ok(method === "createQueryShare" ? shareForm : savedFromForm) as never;
      });

      const acting = act(explorer);
      await vi.waitFor(() => expect(api).toHaveBeenCalled());
      await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }), store);
      response.release();
      await acting;
      await settle(store);

      expect(router.currentRoute.value.query).toEqual({ ...evidenceB, limit: "100" });
      expect(store.activeShareToken).toBeNull();
      expect(store.selectedQueryId).toBeNull();
    },
  );

  it("records a Share that returns after a Run it overlapped", async () => {
    const explorer = await mountExplorer();
    const { router, store, nav, query } = explorer;
    await settle(store);
    stubShareAndSave();
    const ran = deferred();
    query.mockImplementation(async () => {
      await ran.done;
      return ok(noLogs) as never;
    });
    const shared = deferred();
    vi.mocked(exploreApi.createQueryShare).mockImplementation(async () => {
      await shared.done;
      return ok(shareForm) as never;
    });
    store.setLogchefqlCode('ran="yes"');

    const running = nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'ran="yes"' }));
    const sharing = share(explorer);
    await vi.waitFor(() => expect(exploreApi.createQueryShare).toHaveBeenCalled());
    ran.release();
    await running;
    await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe('ran="yes"'));
    await settle(store);
    shared.release();

    expect(await sharing).toEqual(shareForm);
    expect(router.currentRoute.value.query).toMatchObject({ share: shareForm.token });
  });

  it("records a Share that returns after Stop cancelled the running query", async () => {
    const explorer = await mountExplorer();
    const { router, store, nav, query } = explorer;
    await settle(store);
    stubShareAndSave();
    const ran = deferred();
    query.mockImplementationOnce(async () => {
      await ran.done;
      return ok(noLogs) as never;
    });
    const shared = deferred();
    vi.mocked(exploreApi.createQueryShare).mockImplementation(async () => {
      await shared.done;
      return ok(shareForm) as never;
    });
    store.setLogchefqlCode('stopped="yes"');

    const running = nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'stopped="yes"' }));
    const sharing = share(explorer);
    await vi.waitFor(() => expect(exploreApi.createQueryShare).toHaveBeenCalled());
    await nav.cancel();
    shared.release();

    expect(await sharing).toEqual(shareForm);
    expect(router.currentRoute.value.query).toMatchObject({ share: shareForm.token });
    ran.release();
    await running;
  });

  it("holds the editor while a Run validates, and runs only the text it validated", async () => {
    const { router, store, query, nav, editor } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    const validation = deferred();
    vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
      await validation.done;
      return ok({ valid: true }) as never;
    });
    store.setLogchefqlCode('validated="yes"');

    await expectNoRun(query, async () => {
      const running = nav.run();
      await vi.waitFor(() => expect(logchefqlApi.validate).toHaveBeenCalled());
      expect(editor.isExecutingQuery.value).toBe(true);
      // A field click edits the query without the editor.
      store.setLogchefqlCode('validated="yes" and');
      validation.release();
      await running;
    }, store);
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
  });

  it.each([
    ["New query", ({ nav }: Mounted) => nav.newQuery()],
    ["a window change", ({ nav }: Mounted) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["a link", ({ router }: Mounted) => router.push({ path: "/logs/explore", query: evidenceB })],
  ])("clears an invalid Run's error after %s", async (_next, next) => {
    const explorer = await mountExplorer();
    const { router, store, query, nav, banner } = explorer;
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);
    vi.mocked(logchefqlApi.validate).mockReturnValue(
      ok({ valid: false, error: { code: "PARSE_ERROR", message: "Missing boolean operator" } }) as never,
    );
    store.setLogchefqlCode('level="error" service="x"');
    await expectNoRun(query, () => nav.run(), store);
    expect(banner()).toBe("Missing boolean operator");

    await expectOneRun(query, async () => next(explorer), store);
    expect(banner()).toBeNull();
  });

  it.each([
    ["window", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["zoom", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "absolute", start: epochMs(1792346400000), end: epochMs(1792350000000) })],
    ["limit", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setLimit(500)],
  ])("filters sidebar values with executed text after a %s change", async (_change, change) => {
    const { store, query, nav, pinia } = await mountExplorer(evidenceA);
    await settle(store);
    const fieldValues = vi.spyOn(sourcesApi, "getFieldValues").mockImplementation(
      (_teamId, _sourceId, fieldName, fieldType) => ok({ field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [], total_distinct: 0 }),
    );
    const sidebar = createApp({
      render: () => h(FieldSideBar, { fields: [{ name: "level", type: "Int64" }], expanded: true, teamId: team.id, sourceId: source.id, source }),
    }).use(pinia).use(i18n);
    sidebar.mount(document.createElement("div"));
    try {
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalledTimes(1));
      fieldValues.mockClear();
      store.setLogchefqlCode('level="error" and');
      await expectOneRun(query, async () => change(nav), store);
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalledTimes(1));
      expect(fieldValues.mock.lastCall?.slice(8, 10)).toEqual(["logchefql", evidenceA.q]);
      expect(store.logchefqlCode).toBe('level="error" and');

      fieldValues.mockClear();
      store.setActiveMode("native");
      store.setNativeQuery("SELECT broken");
      await expectOneRun(query, async () => change(nav), store);
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalledTimes(1));
      expect(fieldValues.mock.lastCall?.slice(8, 10)).toEqual(["logchefql", evidenceA.q]);
    } finally {
      sidebar.unmount();
    }
  });

  it("loads each shown field's values once per navigation and once per Run", async () => {
    const { router, store, query, nav, pinia } = await mountExplorer();
    const fieldValues = vi.spyOn(sourcesApi, "getFieldValues").mockImplementation(
      async (_teamId, _sourceId, fieldName, fieldType) =>
        ({ status: "success", data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [], total_distinct: 0 } }) as never,
    );
    const sidebar = createApp({
      render: () => h(FieldSideBar, { fields: [{ name: "level", type: "Int64" }], expanded: true, teamId: team.id, sourceId: store.sourceId ?? undefined }),
    }).use(pinia).use(i18n);
    sidebar.mount(document.createElement("div"));
    try {
      await settle(store);
      const loads = async (navigate: () => Promise<unknown>) => {
        const before = fieldValues.mock.calls.length;
        await expectOneRun(query, navigate, store);
        await new Promise((resolve) => setTimeout(resolve, 10));
        return fieldValues.mock.calls.length - before;
      };

      expect(await loads(() => router.push({ path: "/logs/explore", query: evidenceA }))).toBe(1);
      store.setLogchefqlCode('level="warn"');
      expect(await loads(() => nav.run())).toBe(1);
    } finally {
      sidebar.unmount();
    }
  });
});
