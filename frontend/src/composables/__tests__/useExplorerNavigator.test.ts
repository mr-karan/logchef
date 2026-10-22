import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h, KeepAlive, type VNode } from "vue";
import { createPinia, disposePinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { teamsApi, type UserTeamMembership } from "@/api/teams";
import { authApi } from "@/api/auth";
import { metaApi } from "@/api/meta";
import { useAuthStore } from "@/stores/auth";
import { sourcesApi, type Source } from "@/api/sources";
import { logchefqlApi } from "@/api/logchefql";
import { exploreApi, type QueryParams } from "@/api/explore";
import { savedQueriesApi, type ResolvedSavedQuery } from "@/api/savedQueries";
import { streamWindowedQuery, type WindowedEvent, type WindowedRequest } from "@/api/windowed";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useExploreStore } from "@/stores/explore";
import { useExploreRuns } from "@/stores/exploreRuns";
import { useSourcesStore } from "@/stores/sources";
import { useExploreHistogramStore } from "@/stores/exploreHistogram";
import { useVariableStore } from "@/stores/variables";
import { epochMs, parseZoneId, sqlTimeCondition, type TimeWindow } from "@/utils/timeWindow";
import { exploreWindowQuery } from "@/utils/alertExplore";
import { contextRouterGuard } from "@/router/contextGuard";
import { controlledApi } from "@/test/controlledApi";
import { setForbiddenHandler } from "@/api/config";
import { i18n, setLocale } from "@/i18n";
import { toast } from "vue-sonner";
import FieldSideBar from "@/components/field-sidebar/FieldSideBar.vue";
import WindowedSearchPanel from "@/views/explore/components/WindowedSearchPanel.vue";

import { useExplorerNavigator } from "../useExplorerNavigator";
import { useQuery } from "../useQuery";
import { useSavedQueries } from "../useSavedQueries";

vi.mock("@/api/windowed", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/windowed")>();
  return { ...actual, streamWindowedQuery: vi.fn(actual.streamWindowedQuery) };
});

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
const windowedSource: Source = {
  ...source, id: 8, name: "edge-logs", source_type: "victorialogs", _meta_ts_field: "_time",
  connection: { base_url: "http://logs.test", optimizer: { enabled: true, histogram_enabled: false } },
};

const sourceById = (id: number) => [source, otherSource, sqlOnlySource, otherTeamSource, offlineSource, windowedSource].find((s) => s.id === id) ?? source;
const sourcesOfTeam = (teamId: number) => (teamId === otherTeam.id ? [otherTeamSource] : [source, otherSource, sqlOnlySource, windowedSource]);

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

// A request that stays open until its signal aborts, as a canceled request does.
const untilAborted = (signal?: AbortSignal) =>
  new Promise<never>((_resolve, reject) => signal?.addEventListener("abort", () => reject(new DOMException("Request canceled", "AbortError")), { once: true }));

// Streams each windowed search as `respond` returns it. A stream without an end event stays open until it is aborted.
const searchWindows = [
  { index: 0, start: "2026-09-01T00:01:00Z", end: "2026-09-01T00:02:00Z" },
  { index: 1, start: "2026-09-01T00:00:00Z", end: "2026-09-01T00:01:00Z" },
];
function streamWindows(respond: (request: WindowedRequest) => WindowedEvent[]) {
  vi.mocked(streamWindowedQuery).mockImplementation(async (_teamId, _sourceId, request, signal, onEvent) => {
    await Promise.resolve();
    const events = request.count ? [{ type: "end" as const, complete: true }] : respond(request);
    for (const event of events) onEvent(event);
    if (events.at(-1)?.type !== "end") await untilAborted(signal);
  });
}
const windowRows = (window: (typeof searchWindows)[number], message: string, cursor?: string): WindowedEvent => (
  { type: "rows", window, status: "complete", logs: [{ _time: window.start, _msg: message }], cursor, complete: false }
);

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
    i18n.global.locale.value = "en";
  });

  // `stub` replaces API stubs before Explorer reads its first location.
  async function mountExplorer(location: Record<string, string> | string = { team: "1", source: "3" }, stub?: () => void, role: "admin" | "member" = "member") {
    const toastError = vi.spyOn(toast, "error").mockReturnValue(0);
    vi.spyOn(teamsApi, "getTeam").mockImplementation(id => ok({ ...team, id, created_by: "1" }));
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([team, otherTeam]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockImplementation((teamId: number) => ok(sourcesOfTeam(teamId)) as never);
    vi.spyOn(sourcesApi, "getTeamSource").mockImplementation(
      (_teamId: number, sourceId: number) => ok(sourceById(sourceId)) as never,
    );
    const query = vi.spyOn(logchefqlApi, "query").mockReturnValue(ok(noLogs) as never);
    vi.spyOn(logchefqlApi, "validate").mockReturnValue(ok({ valid: true }) as never);
    vi.spyOn(exploreApi, "getHistogramData").mockReturnValue(ok({ data: [], granularity: "1m" }) as never);

    // A conflicting draft for the same team and source.
    if (localStorage.getItem("logchef.explore.draft.1.3") === null) {
      localStorage.setItem(
        "logchef.explore.draft.1.3",
        JSON.stringify({ version: 1, mode: "logchefql", nativeQuery: "", logchefqlCode: 'draft="yes"', limit: 100, relativeTime: "7d", absoluteStart: null, absoluteEnd: null, timezone: null, variables: [] }),
      );
    }

    stub?.();

    const pinia = createPinia();
    setActivePinia(pinia);
    vi.spyOn(metaApi, "getMeta").mockReturnValue(ok({ version: "test" }) as never);
    vi.spyOn(authApi, "getSession").mockReturnValue(ok({
      user: { id: "1", email: "owner@example.com", full_name: "Owner", role, status: "active", account_type: "human", created_at: "", updated_at: "" },
      session: null,
    }) as never);
    await useAuthStore().initialize();
    // App.vue creates the explore store before the first navigation.
    const store = useExploreStore();
    const runs = useExploreRuns();
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
    // Like the app's logs layout: other logs pages keep Explorer alive, pages outside it unmount it.
    const Logs = defineComponent({
      render: () => h(RouterView, null, { default: ({ Component }: { Component?: VNode }) => h(KeepAlive, null, { default: () => Component }) }),
    });
    const routes = [{ path: "/forbidden", name: "Forbidden", component: { render: () => null } }, { path: "/logs", component: Logs, children: [{ path: "explore", component: Explorer }, { path: "library", component: { render: () => null } }] }];
    const router = createRouter({ history: createMemoryHistory(), routes });
    const forbidden = vi.fn(() => { void router.push({ name: "Forbidden" }); });
    setForbiddenHandler(forbidden);
    router.beforeEach(contextRouterGuard);
    await router.push(typeof location === 'string' ? location : { path: "/logs/explore", query: location });
    const app = createApp({ render: () => h(RouterView) }).use(pinia).use(router);
    app.mount(document.createElement("div"));
    unmount = () => { app.unmount(); disposePinia(pinia); };
    const opened = nav;
    const shownEditor = editor;
    if (!opened || !saved || !shownEditor) throw new Error("explorer did not mount");
    await vi.waitFor(() => expect(opened.status.value !== "loading" || router.currentRoute.value.name === "Forbidden").toBe(true));
    // What Explorer's query error banner shows.
    const banner = () => runs.error ?? store.error?.message ?? null;
    // Whether the editor shows Cancel and Esc stops the query.
    const isRunning = () => runs.isRunning;
    // The rows the results table shows.
    const rows = () => runs.results?.rows ?? [];
    return { router, store, query, forbidden, toastError, nav: opened, saved, editor: shownEditor, banner, isRunning, rows, pinia };
  }

  // Lets late responses and validations land.
  const flush = () => new Promise((resolve) => setImmediate(resolve));

  async function settle() {
    await vi.waitFor(() => expect(useExploreRuns().isRunning).toBe(false));
    await flush();
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
  async function expectOneRun(query: ReturnType<typeof vi.spyOn>, navigate: () => Promise<unknown>) {
    const before = query.mock.calls.length;
    await navigate();
    await settle();
    const calls = query.mock.calls.slice(before);
    expect(calls).toHaveLength(1);
    expect((calls[0][3] as { signal: AbortSignal }).signal.aborted).toBe(false);
  }

  async function expectNoRun(query: ReturnType<typeof vi.spyOn>, navigate: () => Promise<unknown>) {
    const before = query.mock.calls.length;
    await navigate();
    await settle();
    expect(query.mock.calls.length).toBe(before);
  }

  // Runs a navigation race, then runs out the timers, so a late superseded
  // navigation would show up here.
  async function raceAndOutwait(
    query: ReturnType<typeof vi.spyOn>,
    race: () => Promise<unknown>,
    expectRuns = expectOneRun,
  ) {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      await expectRuns(query, race);
      const before = query.mock.calls.length;
      await vi.advanceTimersByTimeAsync(6000);
      await settle();
      expect(query.mock.calls.length).toBe(before);
    } finally {
      vi.useRealTimers();
    }
  }

  it("reopens each evidence link on Back and Forward instead of the saved draft", async () => {
    const { router, store, query } = await mountExplorer();
    await settle();

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    expectOpen(router, store, query, evidenceA);

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));
    expectOpen(router, store, query, evidenceB);

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceA.start));
    });
    expectOpen(router, store, query, evidenceA);

    await expectOneRun(query, async () => {
      router.forward();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceB.start));
    });
    expectOpen(router, store, query, evidenceB);
  });

  it("runs one query when Explorer opens", async () => {
    const { query } = await mountExplorer();
    await settle();
    expect(query).toHaveBeenCalledTimes(1);
  });

  it.each(["url", "share", "saved"])("opens an admin's %s link outside team memberships", async (entry) => {
    const requestedTeam = 14;
    const requestedSource = { ...source, id: 140 };
    const location: Record<string, string> = entry === "share" ? { share: "tok" } : entry === "saved" ? { id: "42" } : { team: "14", source: "140", q: 'level="error"' };
    const { router, query, nav } = await mountExplorer(location, () => {
      vi.mocked(teamsApi.listUserTeams).mockReturnValue(ok([team]));
      vi.mocked(sourcesApi.listTeamSources).mockImplementation(teamId => ok(teamId === requestedTeam ? [requestedSource] : sourcesOfTeam(teamId)));
      vi.mocked(sourcesApi.getTeamSource).mockImplementation((_team, sourceId) => ok(sourceId === requestedSource.id ? requestedSource : sourceById(sourceId)));
      vi.spyOn(exploreApi, "getQueryShare").mockReturnValue(ok({ ...shareOnOtherSource, team_id: requestedTeam, source_id: requestedSource.id }) as never);
      vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, resolved_team_id: requestedTeam, source_id: requestedSource.id, query_content: savedContent('level="error"') }) as never);
    }, "admin");
    await settle();

    expect(nav.status.value).toBe("ready");
    expect(useContextStore().teamId).toBe(requestedTeam);
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.slice(0, 2)).toEqual([requestedTeam, requestedSource.id]);
    expect(router.currentRoute.value.query).toMatchObject({ team: "14", source: "140" });
  });

  it.each(["url", "share", "saved"])("rejects a member's %s link outside team memberships without running elsewhere", async (entry) => {
    const location: Record<string, string> = entry === "share" ? { share: "tok" } : entry === "saved" ? { id: "42" } : { team: "14", source: "140", q: 'level="error"' };
    const network = controlledApi(config => config.url === "/teams/14/sources");
    const opening = mountExplorer(location, () => {
      vi.mocked(sourcesApi.listTeamSources).mockRestore();
      vi.spyOn(exploreApi, "getQueryShare").mockReturnValue(ok({ ...shareOnOtherSource, team_id: 14, source_id: 140 }) as never);
      vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, resolved_team_id: 14, source_id: 140, query_content: savedContent('level="error"') }) as never);
    });
    try {
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      network.requests[0]?.respond(403, { status: "error", message: "Membership required", error_type: "AuthorizationError" });
      const { router, query, nav, forbidden, toastError } = await opening;
      await settle();

      expect(forbidden).not.toHaveBeenCalled();
      expect(router.currentRoute.value.path).toBe("/logs/explore");
      expect(toastError).not.toHaveBeenCalled();
      expect(nav.status.value).toBe("error");
      expect(nav.error.value).toBe(i18n.global.t("explore.teamAccessDenied"));
      expect(query).not.toHaveBeenCalled();
      expect(router.currentRoute.value.query).toEqual(location);
    } finally {
      await opening;
      network.restore();
      setForbiddenHandler(() => {});
    }
  });

  it.each(["team", "source"])("keeps the shown view and shows one localized toast when a %s pick is denied", async resource => {
    const { nav, router, store, query, forbidden, toastError } = await mountExplorer(evidenceA);
    await settle();
    const shown = { ...router.currentRoute.value.query };
    const network = controlledApi(config => config.url === (resource === "team" ? "/teams/2/sources" : "/teams/1/sources/4"));
    if (resource === "team") vi.mocked(sourcesApi.listTeamSources).mockRestore();
    else vi.mocked(sourcesApi.getTeamSource).mockRestore();
    await setLocale("fr");
    try {
      if (resource === "team") await nav.selectTeam(2);
      else await nav.selectSource(4);
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      network.requests[0]?.respond(403, { status: "error", message: "Denied", error_type: "AuthorizationError" });
      await flush();
      await flush();

      expect(forbidden).not.toHaveBeenCalled();
      expect(router.currentRoute.value.path).toBe("/logs/explore");
      expect(router.currentRoute.value.query).toEqual(shown);
      expect(store.sourceId).toBe(source.id);
      expect(query).toHaveBeenCalledTimes(1);
      expect(toastError).toHaveBeenCalledExactlyOnceWith(i18n.global.t("explore.linkOpenFailed"), expect.objectContaining({
        description: i18n.global.t(resource === "team" ? "explore.teamAccessDenied" : "explore.sourceAccessDenied"),
      }));
    } finally { network.restore(); }
  });

  it.each(['share', 'saved'] as const)('handles a denied %s resolver with one local toast and no forbidden redirect', async entry => {
    const { router, query, forbidden, toastError, store } = await mountExplorer(evidenceA);
    await settle();
    const shown = { ...router.currentRoute.value.query };
    const network = controlledApi(config => entry === 'share'
      ? config.url === '/query-shares/denied'
      : !!config.url?.startsWith('/saved-queries/42/resolve'));
    await setLocale('fr');
    try {
      await router.push({ path: '/logs/explore', query: entry === 'share' ? { share: 'denied' } : { team: '1', id: '42' } });
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      network.requests[0]?.respond(403, { status: 'error', message: 'Denied', error_type: 'AuthorizationError' });
      await flush();
      await flush();

      expect(forbidden).not.toHaveBeenCalled();
      expect(router.currentRoute.value.path).toBe('/logs/explore');
      expect(router.currentRoute.value.query).toEqual(shown);
      expect(store.sourceId).toBe(source.id);
      expect(query).toHaveBeenCalledTimes(1);
      expect(toastError).toHaveBeenCalledExactlyOnceWith(i18n.global.t('explore.linkOpenFailed'), expect.objectContaining({
        description: i18n.global.t('explore.sourceAccessDenied'),
      }));
    } finally { network.restore(); }
  });

  it.each(['url', 'share', 'saved'] as const)('opens a fallback for an unavailable source in a %s link without running its query', async entry => {
    const requestedSourceId = 900;
    const location: Record<string, string> = entry === 'share' ? { share: 'tok' }
      : entry === 'saved' ? { team: '1', id: '42' }
      : { team: '1', source: String(requestedSourceId), mode: 'logchefql', q: 'private="yes"' };
    await setLocale('fr');
    const { router, nav, store, query, toastError } = await mountExplorer(location, () => {
      vi.spyOn(exploreApi, 'getQueryShare').mockReturnValue(ok({ ...shareOnOtherSource, source_id: requestedSourceId }) as never);
      vi.spyOn(savedQueriesApi, 'resolve').mockReturnValue(ok({ ...savedOnOtherSource, source_id: requestedSourceId, query_content: savedContent('private="yes"') }) as never);
    });
    await settle();

    expect(nav.status.value).toBe('ready');
    expect(store.sourceId).toBe(source.id);
    expect(router.currentRoute.value.query).toEqual({ team: String(team.id), source: String(source.id) });
    expect(query).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledExactlyOnceWith(i18n.global.t('explore.linkOpenFailed'), expect.objectContaining({
      description: i18n.global.t('explore.sourceUnavailableFallback', { sourceId: requestedSourceId, sourceName: source.name }),
    }));

    const recorded = router.currentRoute.value.fullPath;
    unmount?.();
    unmount = undefined;
    const reloaded = await mountExplorer(recorded);
    await settle();

    expect(reloaded.query.mock.calls.every(([, sourceId, request]) =>
      sourceId !== source.id || (request.query !== 'private="yes"' && request.query !== shareOnOtherSource.payload.query),
    )).toBe(true);
  });

  it("does not repeat an error toast already shown by the interceptor", async () => {
    const { nav, toastError } = await mountExplorer();
    await settle();
    vi.mocked(sourcesApi.getTeamSource).mockRejectedValue({ status: "error", message: "Already shown", error_type: "SecurityError", _toastShown: true });
    await nav.selectSource(4);
    await flush();
    await flush();
    expect(toastError).not.toHaveBeenCalled();
  });

  it.each([false, true])("rejects a nonexistent admin team while preserving the shown view, already open=%s", async alreadyOpen => {
    const network = controlledApi(config => config.url === "/teams/999");
    try {
      const opening = mountExplorer(alreadyOpen ? evidenceA : { team: "999" }, () => {
        vi.mocked(teamsApi.getTeam).mockRestore();
        vi.mocked(sourcesApi.listTeamSources).mockImplementation(id => ok(id === 999 ? [] : sourcesOfTeam(id)));
      }, "admin");
      if (alreadyOpen) {
        const { router } = await opening;
        await settle();
        await router.push({ path: "/logs/explore", query: { team: "999" } });
      }
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      network.requests[0]?.respond(404, { status: "error", message: "Unknown team", error_type: "NotFoundError" });
      const { nav, router, store, query, toastError } = await opening;
      await flush();
      await flush();
      expect(nav.status.value).toBe(alreadyOpen ? "ready" : "error");
      if (alreadyOpen) {
        expect(router.currentRoute.value.query).toMatchObject({ team: "1", source: "3", q: evidenceA.q });
        expect(store.sourceId).toBe(source.id);
        expect(toastError).toHaveBeenCalledExactlyOnceWith(i18n.global.t("explore.linkOpenFailed"), expect.objectContaining({ description: i18n.global.t("explore.teamNotFound") }));
      } else {
        expect(nav.error.value).toBe(i18n.global.t("explore.teamNotFound"));
        expect(query).not.toHaveBeenCalled();
      }
    } finally { network.restore(); }
  });

  it("shows the teams load failure for an explicit team instead of denying access", async () => {
    const { nav, query } = await mountExplorer({ team: "14", source: "140" }, () => {
      vi.mocked(teamsApi.listUserTeams).mockRejectedValue({ status: "error", message: "Unavailable", error_type: "ServerError" });
    });
    expect(nav.status.value).toBe("error");
    expect(nav.error.value).toBe(i18n.global.t("access.loadTeamsFailed"));
    expect(query).not.toHaveBeenCalled();
    expect(sourcesApi.listTeamSources).not.toHaveBeenCalled();
  });

  it("opens a newly granted team even while the memberships list is stale", async () => {
    const granted = { ...source, id: 140 };
    const { nav, query } = await mountExplorer({ team: "14", source: "140" }, () => {
      vi.mocked(sourcesApi.listTeamSources).mockImplementation(teamId => ok(teamId === 14 ? [granted] : sourcesOfTeam(teamId)));
      vi.mocked(sourcesApi.getTeamSource).mockReturnValue(ok(granted));
    });
    await settle();
    expect(nav.status.value).toBe("ready");
    expect(query.mock.lastCall?.slice(0, 2)).toEqual([14, 140]);
  });

  it("does not request abandoned source details after a pending team pick is superseded", async () => {
    const { nav, query } = await mountExplorer(evidenceA);
    await settle();
    const ready = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async teamId => {
      if (teamId === otherTeam.id) await ready.done;
      return ok(sourcesOfTeam(teamId));
    });
    const before = query.mock.calls.length;
    await nav.selectTeam(otherTeam.id);
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id, expect.anything()));
    await nav.selectTeam(team.id);
    await flush();
    ready.release();
    await flush();
    await flush();
    expect(sourcesApi.getTeamSource).not.toHaveBeenCalledWith(otherTeam.id, otherTeamSource.id, expect.anything());
    expect(nav.status.value).toBe("ready");
    expect(query).toHaveBeenCalledTimes(before);
  });

  it("retries invalidated metadata before requesting source details", async () => {
    const { nav, query } = await mountExplorer(evidenceA);
    await settle();
    const ready = deferred();
    vi.mocked(sourcesApi.listTeamSources)
      .mockImplementationOnce(async () => { await ready.done; return ok([otherTeamSource]); })
      .mockImplementation(teamId => ok(teamId === otherTeam.id ? [] : sourcesOfTeam(teamId)));
    await nav.selectTeam(otherTeam.id);
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id, expect.anything()));
    useSourcesStore().invalidateTeamSources();
    ready.release();
    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    expect(sourcesApi.getTeamSource).not.toHaveBeenCalledWith(otherTeam.id, otherTeamSource.id, expect.anything());
    expect(useContextStore().teamId).toBe(otherTeam.id);
    expect(useContextStore().sourceId).toBeNull();
    expect(query).toHaveBeenCalledTimes(1);
  });

  it("replaces a failed mid-session link with the shown URL without a run or history entry", async () => {
    const { router, store, nav, query } = await mountExplorer(evidenceA);
    await settle();
    await nav.selectTeam(otherTeam.id);
    await settle();
    const shown = { ...router.currentRoute.value.query };
    const before = query.mock.calls.length;
    const replace = vi.spyOn(router, "replace");
    vi.spyOn(savedQueriesApi, "resolve").mockRejectedValue({ status: "error", message: "Saved query not found", error_type: "NotFoundError" });
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", id: "999999" } });
    await flush();
    await flush();

    expect(nav.status.value).toBe("ready");
    expect(router.currentRoute.value.query).toEqual(shown);
    expect(replace).toHaveBeenCalledWith({ path: "/logs/explore", query: shown });
    expect(store.sourceId).toBe(otherTeamSource.id);
    expect(query).toHaveBeenCalledTimes(before);
    router.back();
    await vi.waitFor(() => expect(router.currentRoute.value.query).toEqual(shown));
    expect(query).toHaveBeenCalledTimes(before);
  });

  it("opens another team's link after the current context changes during resolution", async () => {
    const { router, nav, query } = await mountExplorer();
    await settle();
    const listReady = deferred();
    const signals: AbortSignal[] = [];
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async (teamId, options) => {
      if (teamId === otherTeam.id) {
        if (options?.signal) signals.push(options.signal);
        await Promise.race([listReady.done, untilAborted(options?.signal)]);
      }
      return ok(sourcesOfTeam(teamId));
    });
    const before = query.mock.calls.length;
    await router.push({ path: "/logs/explore", query: { team: "2", source: "7", q: 'level="error"' } });
    await vi.waitFor(() => expect(signals).toHaveLength(1));
    useContextStore().selectSource(otherSource.id);
    await flush();
    expect(signals[0]?.aborted).toBe(false);
    listReady.release();
    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    await settle();
    expect(useContextStore().teamId).toBe(otherTeam.id);
    expect(query).toHaveBeenCalledTimes(before + 1);
    expect(query.mock.lastCall?.slice(0, 2)).toEqual([otherTeam.id, otherTeamSource.id]);
  });

  it("retries a canceled source list without showing an error or running a fallback", async () => {
    const { nav, query } = await mountExplorer(evidenceA, () => {
      vi.mocked(sourcesApi.listTeamSources).mockRejectedValueOnce({ status: "error", error_type: "CanceledError", message: "Request canceled" });
    });
    await settle();
    expect(nav.status.value).toBe("ready");
    expect(nav.error.value).toBeNull();
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.slice(0, 2)).toEqual([team.id, source.id]);
  });

  it("bounds retries when every source list is canceled", async () => {
    const { nav, query } = await mountExplorer(evidenceA, () => {
      vi.mocked(sourcesApi.listTeamSources).mockRejectedValue({ status: "error", error_type: "CanceledError", message: "Request canceled" });
    });
    expect(nav.status.value).toBe("error");
    expect(nav.error.value).toBe(i18n.global.t("explore.sourcesChanged"));
    expect(sourcesApi.listTeamSources).toHaveBeenCalledTimes(3);
    expect(query).not.toHaveBeenCalled();
  });

  it.each(["teams", "share", "saved"])("localizes its %s error", async (entry) => {
    i18n.global.setLocaleMessage("navigator-test", {
      "explore.noTeams": "Localized teams error",
      "explore.shareNotFound": "Localized share error",
      "pages.savedQueryNotFound": "Localized saved error",
    });
    i18n.global.locale.value = "navigator-test";
    const location: Record<string, string> = entry === "share" ? { share: "tok" } : entry === "saved" ? { id: "42" } : {};
    const { nav, query } = await mountExplorer(location, () => {
      vi.mocked(teamsApi.listUserTeams).mockReturnValue(ok([]));
      vi.spyOn(exploreApi, "getQueryShare").mockReturnValue(ok(null) as never);
      vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok(null) as never);
    });
    expect(nav.status.value).toBe("error");
    expect(nav.error.value).toBe(`Localized ${entry} error`);
    expect(query).not.toHaveBeenCalled();
  });

  it.each(["team", "source"])("re-picking the shown %s cancels a pending pick", async (pick) => {
    const { router, nav, query, store } = await mountExplorer(evidenceA);
    await settle();
    const ready = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async teamId => {
      if (teamId === otherTeam.id) await ready.done;
      return ok(sourcesOfTeam(teamId));
    });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_team, id) => {
      if (id === otherSource.id) await ready.done;
      return ok(sourceById(id));
    });
    const before = query.mock.calls.length;
    if (pick === "team") await nav.selectTeam(otherTeam.id);
    else await nav.selectSource(otherSource.id);
    await vi.waitFor(() => expect(nav.status.value).toBe("navigating"));
    expect(nav.pending.value).toMatchObject(pick === "team" ? { teamId: otherTeam.id, sourceId: null } : { teamId: team.id, sourceId: otherSource.id });
    if (pick === "team") await nav.selectTeam(team.id);
    else await nav.selectSource(source.id);
    await flush();
    expect(nav.status.value).toBe("ready");
    expect(router.currentRoute.value.query).toMatchObject(evidenceA);
    ready.release();
    await flush();
    await settle();
    expect(nav.pending.value).toBeNull();
    expect(useContextStore().teamId).toBe(team.id);
    expect(store.sourceId).toBe(source.id);
    expect(query).toHaveBeenCalledTimes(before);
  });

  it("opens a link to another source with the link's query and window", async () => {
    const { router, store, query } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceOther }));
    expect(store.sourceId).toBe(otherSource.id);
    expectOpen(router, store, query, evidenceOther);
  });

  it("goes Back to the previous source's query after picking another source", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));
    store.setLogchefqlCode('unrun="edit"');

    await expectOneRun(query, () => nav.selectSource(otherSource.id));
    expect(router.currentRoute.value.query.source).toBe(String(otherSource.id));

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.source).toBe(String(source.id)));
    });
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceB);
  });

  it("keeps the URL in step with a window chosen after picking a source", async () => {
    const { router, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => nav.selectSource(otherSource.id));

    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "15m" }));
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), t: "15m" });
  });

  it("runs nothing when Back returns to the shown query before the next source loads", async () => {
    const { router, store, query } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    let releaseOther: () => void = () => {};
    const otherLoaded = new Promise<void>((resolve) => { releaseOther = resolve; });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      if (sourceId === otherSource.id) await otherLoaded;
      return ok(sourceById(sourceId)) as never;
    });

    await raceAndOutwait(query, async () => {
      await router.push({ path: "/logs/explore", query: evidenceOther });
      await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(team.id, otherSource.id, expect.anything()));
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.source).toBe(String(source.id)));
      releaseOther();
    }, expectNoRun);
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceA);
  });

  it("waits for the current team's sources when a left team's list returns first", async () => {
    const { router, query, store, nav } = await mountExplorer();
    await settle();
    const thirdTeam = { ...team, id: 9 };
    const currentSource = { ...source, id: 91 };
    useTeamsStore().userTeams.push(thirdTeam);
    const leftLoaded = deferred();
    const currentLoaded = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async (teamId) => {
      if (teamId === otherTeam.id) await leftLoaded.done;
      if (teamId === thirdTeam.id) await currentLoaded.done;
      return ok(teamId === thirdTeam.id ? [{ ...source, id: 90 }, currentSource] : sourcesOfTeam(teamId));
    });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId, sourceId) =>
      ok(sourceId === currentSource.id ? currentSource : sourceById(sourceId)));
    const before = query.mock.calls.length;
    await nav.selectTeam(otherTeam.id);
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id, expect.anything()));
    await router.push({ path: "/logs/explore", query: { team: String(thirdTeam.id), source: String(currentSource.id) } });
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(thirdTeam.id, expect.anything()));
    leftLoaded.release();
    await flush();
    expect(nav.status.value).toBe("navigating");
    expect(store.sourceId).toBe(source.id);
    expect(query).toHaveBeenCalledTimes(before);
    currentLoaded.release();
    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    await settle();
    expect(useContextStore().teamId).toBe(thirdTeam.id);
    expect(store.sourceId).toBe(currentSource.id);
    expect(useSourcesStore().teamSources.map(source => source.id)).toEqual([90, 91]);
    expect(query).toHaveBeenCalledTimes(before + 1);
  });

  it("keeps a remembered source when Explorer opens a stale source link", async () => {
    const { store } = await mountExplorer({ team: "1", source: "99999" }, () => {
      localStorage.setItem("logchef_context", JSON.stringify({ lastTeamId: 1, sourcePerTeam: { 1: otherSource.id } }));
    });
    await settle();
    expect(store.sourceId).toBe(otherSource.id);
    expect(useContextStore().getStoredDefaults().sourceId).toBe(otherSource.id);
  });

  it("opens the last of two quick links when the first source is still loading", async () => {
    const { router, store, query } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    let releaseOther: () => void = () => {};
    const otherLoaded = new Promise<void>((resolve) => { releaseOther = resolve; });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      if (sourceId === otherSource.id) await otherLoaded;
      return ok(sourceById(sourceId)) as never;
    });

    await raceAndOutwait(query, async () => {
      await router.push({ path: "/logs/explore", query: evidenceOther });
      await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(team.id, otherSource.id, expect.anything()));
      await router.push({ path: "/logs/explore", query: evidenceB });
      releaseOther();
      await vi.waitFor(() => expect(store.sourceId).toBe(source.id));
    });
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceB);
  });

  it("goes Back to the previous team's query after picking another team", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    await expectOneRun(query, () => nav.selectTeam(otherTeam.id));
    expect(router.currentRoute.value.query).toMatchObject({ team: String(otherTeam.id), source: String(otherTeamSource.id) });

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.team).toBe(String(team.id)));
    });
    expect(store.sourceId).toBe(source.id);
    expectOpen(router, store, query, evidenceA);
  });

  it("keeps Back and Forward working after re-picking the current source", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));

    await nav.selectSource(source.id);
    await settle();

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceA.start));
    });
    await expectOneRun(query, async () => {
      router.forward();
      await vi.waitFor(() => expect(router.currentRoute.value.query.start).toBe(evidenceB.start));
    });
    expectOpen(router, store, query, evidenceB);
  });

  it("pushes the mode the picked source supports", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
      await new Promise((resolve) => setTimeout(resolve, 80));
      return ok(sourceById(sourceId)) as never;
    });

    await nav.selectSource(sqlOnlySource.id);
    await vi.waitFor(() => expect(store.activeMode).toBe("native"));
    await settle();
    expect(query).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(sqlOnlySource.id), mode: "native" });
  });

  it("re-runs when the user chooses the same relative range again", async () => {
    const { router, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "15m" }));
    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "15m" }));
    expect(router.currentRoute.value.query).toMatchObject({ t: "15m", q: evidenceA.q });
  });
  it("runs one query on a fresh load whose source details arrive after the link is read", async () => {
    const { query } = await mountExplorer(evidenceA, () => {
      vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId: number, sourceId: number) => {
        await new Promise((resolve) => setTimeout(resolve, 80));
        return ok(sourceById(sourceId)) as never;
      });
    });
    await settle();
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
  });

  it("runs one query for a saved query link on another source", async () => {
    const { router, query } = await mountExplorer({ team: "1", source: "3", id: "42" }, () => {
      vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('saved="yes"') }) as never);
    });
    await settle();
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'saved="yes"', limit: 50 });
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), id: "42" });
  });

  it("opens a saved query picked on another source with one run", async () => {
    const { router, query, nav } = await mountExplorer();
    await settle();
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('picked="yes"') }) as never);

    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }));
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'picked="yes"' });
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), id: "42" });
  });

  it("runs one query for a share link on another source", async () => {
    const { router, query } = await mountExplorer({ team: "1", source: "3", share: "tok" }, () => {
      vi.spyOn(exploreApi, "getQueryShare").mockReturnValue(ok(shareOnOtherSource) as never);
    });
    await settle();
    expect(query).toHaveBeenCalledTimes(1);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'shared="yes"', limit: 50 });
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), share: "tok" });
  });

  it("shows the server's error for a share link it cannot open, and runs nothing", async () => {
    const { query, nav } = await mountExplorer({ share: "gone" }, () => {
      vi.spyOn(exploreApi, "getQueryShare").mockRejectedValue({ status: "error", message: "Share link expired", error_type: "NotFoundError" });
    });
    await settle();
    expect(nav.status.value).toBe("error");
    expect(nav.error.value).toBe("Share link expired");
    expect(query).not.toHaveBeenCalled();
  });

  it("opens the first connected source of a picked team", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(
      (teamId: number) => ok(teamId === otherTeam.id ? [offlineSource, otherTeamSource] : sourcesOfTeam(teamId)) as never,
    );

    const before = query.mock.calls.length;
    await nav.selectTeam(otherTeam.id);
    await settle();
    expect(store.sourceId).toBe(otherTeamSource.id);
    expect(query.mock.calls.length - before).toBe(1);
    expect(router.currentRoute.value.query).toMatchObject({ team: String(otherTeam.id), source: String(otherTeamSource.id) });
  });

  it("writes the restored draft of a bare team and source link to the URL", async () => {
    const { router, store } = await mountExplorer();
    await settle();
    expect(store.logchefqlCode).toBe('draft="yes"');
    expect(router.currentRoute.value.query).toMatchObject({ team: "1", source: "3", mode: "logchefql", q: 'draft="yes"', t: "7d", limit: "100" });
  });

  it("records the default SQL a SQL-only source runs, and reopening that URL runs it", async () => {
    const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
    const { router } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) }, () => localStorage.clear());
    await settle();
    expect(native).toHaveBeenCalledTimes(1);
    const sent = native.mock.lastCall?.[1].query_text;
    expect(sent).toContain("FROM logs.checkout");
    expect(router.currentRoute.value.query).toMatchObject({ mode: "native", sql: sent });

    const recorded = Object.fromEntries(Object.entries(router.currentRoute.value.query).map(([key, value]) => [key, String(value)]));
    unmount?.();
    native.mockClear();
    const reopened = await mountExplorer(recorded);
    await settle();
    expect(native).toHaveBeenCalledTimes(1);
    expect(native.mock.lastCall?.[1].query_text).toBe(sent);

    reopened.store.setNativeQuery("");
    await reopened.nav.run();
    await settle();
    expect(native).toHaveBeenCalledTimes(2);
    expect(reopened.router.currentRoute.value.query.sql).toBe(native.mock.lastCall?.[1].query_text);
    expect(reopened.store.nativeQuery).toBe(native.mock.lastCall?.[1].query_text);
  });

  it("runs nothing for a Run that a link supersedes while its query is validated", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
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
    });
    expectOpen(router, store, query, evidenceA);
  });

  it("releases validation immediately on Stop and never runs its late result", async () => {
    const { store, query, nav, isRunning } = await mountExplorer();
    await settle();
    const validation = deferred();
    vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
      await validation.done;
      return ok({ valid: true });
    });
    store.setLogchefqlCode('stopped="yes"');
    const calls = query.mock.calls.length;
    nav.run();
    await vi.waitFor(() => expect(isRunning()).toBe(true));
    nav.cancel();
    expect(isRunning()).toBe(false);
    validation.release();
    await flush();
    expect(query).toHaveBeenCalledTimes(calls);
  });

  it("opens a picked source when Stop cancels the query that ran before the pick", async () => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle();
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
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));

    await router.push({ path: "/logs/explore", query: { team: "1", source: String(otherSource.id) } });
    expect(nav.status.value).toBe("navigating");
    nav.cancel();
    details.release();
    response.release();

    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    await settle();
    expect(store.sourceId).toBe(otherSource.id);
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), mode: "logchefql" });
  });

  it("releases superseded validation without letting it clear a newer Run's loading state", async () => {
    const { router, store, nav, isRunning, banner } = await mountExplorer();
    await settle();
    const old = deferred();
    const newer = deferred();
    vi.mocked(logchefqlApi.validate)
      .mockImplementationOnce(async () => { await old.done; return ok({ valid: false, error: { message: "Old error", code: "PARSE_ERROR" } }); })
      .mockImplementationOnce(async () => { await newer.done; return ok({ valid: true }); });
    store.setLogchefqlCode('old="yes"');
    nav.run();
    await vi.waitFor(() => expect(isRunning()).toBe(true));
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", mode: "native", sql: "" } });
    await vi.waitFor(() => expect(store.activeMode).toBe("native"));
    expect(isRunning()).toBe(false);
    store.setActiveMode("logchefql");
    store.setLogchefqlCode('newer="yes"');
    nav.run();
    await vi.waitFor(() => expect(logchefqlApi.validate).toHaveBeenCalledTimes(2));
    old.release();
    await flush();
    expect(isRunning()).toBe(true);
    expect(banner()).toBeNull();
    newer.release();
    await settle();
  });

  it("pushes one history entry with the query a Run ran, not the edits typed while it ran", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
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
    });
    expect(router.currentRoute.value.query.q).toBe('ran="yes"');
    expect(store.logchefqlCode).toBe('typed="later"');

    await expectOneRun(query, async () => {
      router.back();
      await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe(evidenceA.q));
    });
  });

  it("runs nothing and records nothing for a Run whose LogchefQL is invalid", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    vi.mocked(logchefqlApi.validate).mockReturnValue(
      ok({ valid: false, error: { code: "PARSE_ERROR", message: "Expected a field value" } }) as never,
    );
    store.setLogchefqlCode("level=");

    await expectNoRun(query, async () => nav.run());
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
  });

  it("runs once and replaces the URL when the limit changes", async () => {
    const { router, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    await expectOneRun(query, async () => nav.setLimit(500));
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
    await settle();
    store.setLogchefqlCode('level="error" and');
    await expectOneRun(query, async () => change(nav));
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    expect(store.logchefqlCode).toBe('level="error" and');
    expect(JSON.parse(localStorage.getItem('logchef.explore.draft.1.3') ?? '{}').logchefqlCode).toBe('level="error" and');
    expect(logchefqlApi.validate).not.toHaveBeenCalled();
  });

  // Explorer opens evidence A, whose run shows one row.
  const openWithRows = () => mountExplorer(evidenceA, () => {
    vi.mocked(logchefqlApi.query).mockReturnValueOnce(ok({ ...noLogs, logs: [{ msg: "A" }] }) as never);
  });

  it.each(["stopped", "failed"])("keeps the last successful query after a %s Run", async (outcome) => {
    const { router, store, query, nav, rows } = await openWithRows();
    await settle();
    const response = deferred();
    query.mockImplementationOnce(async (_team, _source, _request, options) => {
      await response.done;
      if (options?.signal?.aborted) throw new DOMException("Request canceled", "AbortError");
      throw new Error("Query failed");
    });
    store.setLogchefqlCode('service="cart"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'service="cart"' }));
    if (outcome === "stopped") nav.cancel();
    response.release();
    await settle();

    expect(rows()).toEqual([{ msg: "A" }]);
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "1h" }));
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    expect(store.logchefqlCode).toBe('service="cart"');
  });

  it.each([
    ["window", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["limit", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setLimit(500)],
  ])("replays the in-flight query on a %s change without marking it successful early", async (_change, change) => {
    const { router, store, query, nav, rows } = await openWithRows();
    await settle();
    const response = deferred();
    query.mockImplementationOnce(async () => {
      await response.done;
      return ok(noLogs);
    });
    store.setLogchefqlCode('service="cart"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'service="cart"' }));
    expect(rows()).toEqual([{ msg: "A" }]);
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);
    store.setLogchefqlCode('service="cart" and');
    await expectOneRun(query, async () => change(nav));
    response.release();
    await flush();

    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'service="cart"' });
    expect(router.currentRoute.value.query.q).toBe('service="cart"');
    expect(store.logchefqlCode).toBe('service="cart" and');
  });

  it("replays the opened query when its first execution fails and the editor has unrun text", async () => {
    const { store, query, nav, banner } = await mountExplorer(evidenceA, () => {
      vi.mocked(logchefqlApi.query).mockRejectedValueOnce(new Error("Query failed"));
    });
    await settle();
    expect(banner()).toContain("Query failed");
    store.setLogchefqlCode('level="error" and');
    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "1h" }));
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceA.q });
    expect(store.logchefqlCode).toBe('level="error" and');
  });

  it("replays the last-run native query even after an unrun mode change", async () => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle();
    const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs) as never);
    store.setActiveMode("native");
    store.setNativeQuery("SELECT 1");
    await nav.run();
    await settle();
    store.setNativeQuery("SELECT broken");
    store.setActiveMode("logchefql");
    store.setLogchefqlCode('invalid="draft" and');
    const before = query.mock.calls.length;
    nav.setWindow({ kind: "relative", spec: "1h" });
    await settle();
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
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));

    await expectOneRun(query, async () => nav.newQuery());
    expect(store.logchefqlCode).toBe("");
    expect(router.currentRoute.value.query).toMatchObject({ q: "", t: "15m", limit: "100" });

    router.back();
    await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe('draft="yes"'));
  });

  it("opens the URL again on Retry after it failed to open", async () => {
    const { router, query, nav } = await mountExplorer({ share: "tok" }, () => {
      vi.spyOn(exploreApi, "getQueryShare")
        .mockRejectedValueOnce({ status: "error", message: "Share service is down", error_type: "ServerError" })
        .mockReturnValue(ok(shareOnOtherSource) as never);
    });
    expect(nav.status.value).toBe("error");

    await expectOneRun(query, () => nav.retry());
    expect(nav.status.value).toBe("ready");
    expect(nav.error.value).toBeNull();
    expect(router.currentRoute.value.query).toMatchObject({ source: String(otherSource.id), share: "tok" });
  });

  it("applies a window chosen while a team pick loads to that team", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    const teamLoaded = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async (teamId: number) => {
      if (teamId === otherTeam.id) await teamLoaded.done;
      return ok(sourcesOfTeam(teamId)) as never;
    });

    await expectOneRun(query, async () => {
      const picking = nav.selectTeam(otherTeam.id);
      await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id, expect.objectContaining({ signal: expect.any(AbortSignal) })));
      expect(nav.status.value).toBe("navigating");
      nav.setWindow({ kind: "relative", spec: "1h" });
      teamLoaded.release();
      await picking;
      await vi.waitFor(() => expect(store.sourceId).toBe(otherTeamSource.id));
    });
    expect(nav.status.value).toBe("ready");
    expect(router.currentRoute.value.query).toMatchObject({ team: String(otherTeam.id), source: String(otherTeamSource.id), t: "1h" });
  });

  it.each([
    ["the server rejects it", () => vi.spyOn(savedQueriesApi, "resolve").mockRejectedValue({ status: "error", message: "Saved query not found", error_type: "NotFoundError" })],
    ["its content is not JSON", () => vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: "{broken" }) as never)],
  ])("keeps the shown query when a saved query fails to open because %s", async (_reason, stub) => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    stub();

    await expectNoRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }));
    expect(nav.status.value).toBe("ready");
    expect(nav.error.value).toBeNull();
    expect(store.sourceId).toBe(source.id);
    expect(store.logchefqlCode).toBe(evidenceA.q);
    expect(router.currentRoute.value.query).toMatchObject(evidenceA);
    expect(router.currentRoute.value.query.id).toBeUndefined();
  });

  it("keeps the shown query and restores its URL when the linked team's sources fail to load", async () => {
    const { router, store, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    vi.mocked(sourcesApi.listTeamSources).mockImplementation((teamId: number) =>
      teamId === otherTeam.id ? Promise.reject(listFailure) : (ok(sourcesOfTeam(teamId)) as never),
    );
    const link = { team: String(otherTeam.id), source: String(otherTeamSource.id), q: 'a="b"', t: "1h" };

    await expectNoRun(query, () => router.push({ path: "/logs/explore", query: link }));
    expect(router.currentRoute.value.query).toMatchObject(evidenceA);
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
    const { router, query, banner } = await mountExplorer();
    await settle();
    query.mockRejectedValueOnce(new Error("Connection reset"));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    expect(banner()).toContain("Connection reset");

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));
    expect(banner()).toBeNull();
  });

  it("drops an older query's late results when a link on the same source runs nothing", async () => {
    const { router, store, query, isRunning, rows } = await mountExplorer();
    await settle();
    const response = deferred();
    query.mockImplementationOnce(async () => {
      await response.done;
      return ok({ ...noLogs, logs: [{ msg: "evidence A" }] }) as never;
    });
    await router.push({ path: "/logs/explore", query: evidenceA });
    await vi.waitFor(() => expect(isRunning()).toBe(true));

    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", mode: "native", sql: "" } });
    await vi.waitFor(() => expect(store.activeMode).toBe("native"));
    response.release();
    await settle();
    expect(rows()).toEqual([]);
  });

  it("opens a link to the source another page picked while Explorer stayed open", async () => {
    const { router, store, query } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    useContextStore().selectSource(otherSource.id);
    const link = { ...store.urlQueryParameters };
    expect(link.source).toBe(String(otherSource.id));

    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: link }));
    expect(query.mock.lastCall?.[1]).toBe(otherSource.id);
  });

  it("keeps an edited saved query selected so Update remains available", async () => {
    const { store, query, nav, saved } = await mountExplorer();
    await settle();
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('picked="yes"') }) as never);
    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }));
    expect(saved.isEditingExistingQuery.value).toBe(true);

    store.setLogchefqlCode('picked="edited"');
    expect(saved.isEditingExistingQuery.value).toBe(true);
    expect(store.selectedQueryId).toBe('42');
    expect(store.hasDivergedFromSavedQuery).toBe(true);
  });

  it("records an edited saved Run inline while keeping Update available until an inline URL opens", async () => {
    const { router, store, query, nav, saved } = await mountExplorer();
    await settle();
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok({ ...savedOnOtherSource, query_content: savedContent('picked="yes"') }) as never);
    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }));
    store.setLogchefqlCode('picked="edited"');
    await expectOneRun(query, async () => nav.run());
    expect(router.currentRoute.value.query).toMatchObject({ mode: "logchefql", q: 'picked="edited"', limit: "50", t: "1h" });
    expect(router.currentRoute.value.query.id).toBeUndefined();
    expect(store.selectedQueryId).toBe("42");
    expect(saved.isEditingExistingQuery.value).toBe(true);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));
    router.back();
    await vi.waitFor(() => expect(store.logchefqlCode).toBe('picked="edited"'));
    expect(store.selectedQueryId).toBeNull();
  });

  it("updates the selected query with the editor text and clears its modified state", async () => {
    const { store, query, nav, saved } = await mountExplorer();
    await settle();
    const original = { ...savedOnOtherSource, query_content: savedContent('picked="yes"') };
    vi.spyOn(savedQueriesApi, "resolve").mockReturnValue(ok(original) as never);
    vi.spyOn(savedQueriesApi, "list").mockReturnValue(ok([]) as never);
    const update = vi.spyOn(savedQueriesApi, "update").mockImplementation((_id, data) => ok({ ...original, ...data }) as never);
    await expectOneRun(query, () => nav.openSavedQuery({ id: 42, source_id: otherSource.id }));
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
    await settle();
    stubShareAndSave();
    store.setLogchefqlCode('saved="yes"');
    await save(explorer);
    expect(store.selectedQueryId).toBe("77");
    expect(store.hasDivergedFromSavedQuery).toBe(false);
    store.setLogchefqlCode('saved="edited"');
    expect(store.hasDivergedFromSavedQuery).toBe(true);
    expect(store.selectedQueryId).toBe("77");
    await expectOneRun(query, async () => nav.run());
    expect(router.currentRoute.value.query.q).toBe('saved="edited"');
    expect(router.currentRoute.value.query.id).toBeUndefined();
    expect(store.selectedQueryId).toBe("77");
  });

  it("keeps a removed team source out of a navigation that was resolving it", async () => {
    const { router, store, query, nav, toastError } = await mountExplorer();
    await settle();
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
    await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(1, 4, expect.anything()));
    vi.mocked(sourcesApi.listTeamSources).mockReturnValue(ok([source, sqlOnlySource]) as never);
    removal.release();
    await removing;
    await vi.waitFor(() => expect(sources.teamSources.some(s => s.id === 4)).toBe(false));
    const queriesBefore = query.mock.calls.length;
    oldDetails.release();
    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    await settle();
    expect(query).toHaveBeenCalledTimes(queriesBefore);
    expect(toastError).toHaveBeenCalledTimes(1);
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
    await settle();
    const details = vi.mocked(sourcesApi.getTeamSource);
    details.mockClear();
    const before = query.mock.calls.length;
    await nav.selectSource(offlineSource.id);
    await vi.waitFor(() => expect(store.sourceId).toBe(offlineSource.id));
    await new Promise(resolve => setImmediate(resolve));
    expect(details).toHaveBeenCalledTimes(1);
    expect(query).toHaveBeenCalledTimes(before);
    details.mockImplementation((_team, id) => ok({ ...sourceById(id), is_connected: true }));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: { team: "1", source: String(offlineSource.id), q: 'retry="yes"' } }));
    expect(details).toHaveBeenCalledTimes(2);
  });

  it("loads only the requested team's metadata with empty localStorage", async () => {
    const { query } = await mountExplorer({ team: String(otherTeam.id), source: String(otherTeamSource.id), q: 'first="yes"' }, () => localStorage.clear());
    await settle();
    expect({ lists: vi.mocked(sourcesApi.listTeamSources).mock.calls.length, details: vi.mocked(sourcesApi.getTeamSource).mock.calls.length }).toEqual({ lists: 1, details: 1 });
    expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(otherTeam.id, otherTeamSource.id, expect.anything());
    expect(query).toHaveBeenCalledTimes(1);
  });

  it("makes no metadata request on a same-source Back or Forward, one on a source pick and two on a team pick", async () => {
    const { router, query, nav } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));
    const metadataRequests = async (navigate: () => Promise<unknown>) => {
      const [lists, details] = [vi.mocked(sourcesApi.listTeamSources).mock.calls.length, vi.mocked(sourcesApi.getTeamSource).mock.calls.length];
      await expectOneRun(query, navigate);
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
    await settle();
    const teamLoaded = deferred();
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async (teamId: number) => {
      if (teamId === otherTeam.id) await teamLoaded.done;
      return ok(sourcesOfTeam(teamId)) as never;
    });
    void router.push({ path: "/logs/explore", query: { team: String(otherTeam.id) } });
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(otherTeam.id, expect.objectContaining({ signal: expect.any(AbortSignal) })));
    await router.push({ path: "/logs/explore", query: { team: "1", source: String(otherSource.id) } });
    teamLoaded.release();
    await flush();
    await settle();
    expect(sourcesApi.getTeamSource).not.toHaveBeenCalledWith(otherTeam.id, otherTeamSource.id, expect.anything());
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
  const share = ({ store, nav }: Mounted) => nav.recordAfter(() => store.createQueryShare(), store.markShare);
  const save = ({ store, nav, saved }: Mounted) => nav.recordAfter(() => saved.handleSaveQuery({ ...saveForm, query_content: JSON.stringify({ version: 1, sourceId: store.sourceId, limit: store.limit, content: store.logchefqlCode }) }), store.markSavedQuery);
  const stubShareAndSave = () => [
    vi.spyOn(exploreApi, "createQueryShare").mockReturnValue(ok(shareForm) as never),
    vi.spyOn(savedQueriesApi, "create").mockImplementation(data => ok({ ...savedFromForm, ...data }) as never),
    vi.spyOn(savedQueriesApi, "list").mockReturnValue(ok([]) as never),
  ];

  it.each([["Share", share], ["Save", save]])("ignores a %s while a navigation loads, and the navigation still opens", async (_action, act) => {
    const explorer = await mountExplorer();
    const { router, store, nav } = explorer;
    await settle();
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
    await settle();
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
      await settle();
      stubShareAndSave();
      const response = deferred();
      const api = method === "createQueryShare" ? vi.mocked(exploreApi.createQueryShare) : vi.mocked(savedQueriesApi.create);
      api.mockImplementation(async () => {
        await response.done;
        return ok(method === "createQueryShare" ? shareForm : savedFromForm) as never;
      });

      const acting = act(explorer);
      await vi.waitFor(() => expect(api).toHaveBeenCalled());
      await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceB }));
      response.release();
      await acting;
      await settle();

      expect(router.currentRoute.value.query).toEqual({ ...evidenceB, limit: "100" });
      expect(store.activeShareToken).toBeNull();
      expect(store.selectedQueryId).toBeNull();
    },
  );

  it("records a Share that returns after a Run it overlapped", async () => {
    const explorer = await mountExplorer();
    const { router, store, nav, query } = explorer;
    await settle();
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
    await settle();
    shared.release();

    expect(await sharing).toMatchObject(shareForm);
    expect(router.currentRoute.value.query).toMatchObject({ share: shareForm.token });
  });

  it("records a Share that returns after Stop cancelled the running query", async () => {
    const explorer = await mountExplorer();
    const { router, store, nav, query } = explorer;
    await settle();
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

    expect(await sharing).toMatchObject(shareForm);
    expect(router.currentRoute.value.query).toMatchObject({ share: shareForm.token });
    ran.release();
    await running;
  });

  it("holds the editor while a Run validates, and runs only the text it validated", async () => {
    const { router, store, query, nav, isRunning } = await mountExplorer();
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    const validation = deferred();
    vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
      await validation.done;
      return ok({ valid: true }) as never;
    });
    store.setLogchefqlCode('validated="yes"');

    await expectOneRun(query, async () => {
      nav.run();
      await vi.waitFor(() => expect(logchefqlApi.validate).toHaveBeenCalled());
      expect(isRunning()).toBe(true);
      // A field click edits the query without the editor.
      store.setLogchefqlCode('validated="yes" and');
      validation.release();
    });
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'validated="yes"' });
    expect(router.currentRoute.value.query.q).toBe('validated="yes"');
    expect(store.logchefqlCode).toBe('validated="yes" and');
  });

  it.each([
    ["New query", ({ nav }: Mounted) => nav.newQuery()],
    ["a window change", ({ nav }: Mounted) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["a link", ({ router }: Mounted) => router.push({ path: "/logs/explore", query: evidenceB })],
  ])("clears an invalid Run's error after %s", async (_next, next) => {
    const explorer = await mountExplorer();
    const { router, store, query, nav, banner } = explorer;
    await settle();
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }));
    vi.mocked(logchefqlApi.validate).mockReturnValue(
      ok({ valid: false, error: { code: "PARSE_ERROR", message: "Missing boolean operator" } }) as never,
    );
    store.setLogchefqlCode('level="error" service="x"');
    await expectNoRun(query, async () => nav.run());
    expect(banner()).toBe("Missing boolean operator");

    await expectOneRun(query, async () => next(explorer));
    expect(banner()).toBeNull();
  });

  it.each([
    ["window", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "relative", spec: "1h" })],
    ["zoom", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setWindow({ kind: "absolute", start: epochMs(1792346400000), end: epochMs(1792350000000) })],
    ["limit", (nav: ReturnType<typeof useExplorerNavigator>) => nav.setLimit(500)],
  ])("filters sidebar values with executed text after a %s change", async (_change, change) => {
    const { store, query, nav, pinia } = await mountExplorer(evidenceA);
    await settle();
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
      await expectOneRun(query, async () => change(nav));
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalledTimes(1));
      expect(fieldValues.mock.lastCall?.slice(8, 10)).toEqual(["logchefql", evidenceA.q]);
      expect(store.logchefqlCode).toBe('level="error" and');

      fieldValues.mockClear();
      store.setActiveMode("native");
      store.setNativeQuery("SELECT broken");
      await expectOneRun(query, async () => change(nav));
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
      await settle();
      const loads = async (navigate: () => Promise<unknown>) => {
        const before = fieldValues.mock.calls.length;
        await expectOneRun(query, navigate);
        await new Promise((resolve) => setTimeout(resolve, 10));
        return fieldValues.mock.calls.length - before;
      };

      expect(await loads(() => router.push({ path: "/logs/explore", query: evidenceA }))).toBe(1);
      store.setLogchefqlCode('level="warn"');
      expect(await loads(async () => nav.run())).toBe(1);
    } finally {
      sidebar.unmount();
    }
  });

  // The literal bounds of the SQL sent equal the window the request names, which spans `span` and ends at `end`.
  // vi.waitFor advances the fake clock, so a window resolved while Explorer mounts has no exact end.
  function expectSqlWindow(request: QueryParams | undefined, span: number, end?: number) {
    const zone = parseZoneId(request?.timezone ?? "");
    if (!request || !zone) throw new Error("no native request was sent");
    const window = { start: epochMs(Date.parse(request.start_time ?? "")), end: epochMs(Date.parse(request.end_time ?? "")), zone };
    expect(window.end - window.start).toBe(span);
    if (end !== undefined) expect(window.end).toBe(end);
    expect(request.query_text).toContain(sqlTimeCondition("timestamp", window));
  }

  const idleStart = Date.UTC(2026, 9, 21, 9);
  const idle = 30 * 60_000;

  it("generates default SQL from the window its Run sends after an idle tab", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const { nav } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id), mode: "native", sql: "", t: "15m" });
      await settle();
      expect(native).not.toHaveBeenCalled();

      vi.setSystemTime(idleStart + idle);
      await nav.run();
      await settle();
      expect(native).toHaveBeenCalledTimes(1);
      expectSqlWindow(native.mock.lastCall?.[1], 15 * 60_000, idleStart + idle);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each([
    ["a relative window", { kind: "relative", spec: "1h" } as TimeWindow, idleStart + idle, 3_600_000],
    ["a zoom", { kind: "absolute", start: epochMs(idleStart - 600_000), end: epochMs(idleStart - 300_000) } as TimeWindow, idleStart - 300_000, 300_000],
  ])("regenerates the default SQL that ran over %s chosen after an idle tab", async (_change, window, end, span) => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const { router, nav } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) }, () => localStorage.clear());
      await settle();
      expectSqlWindow(native.mock.lastCall?.[1], 15 * 60_000);

      vi.setSystemTime(idleStart + idle);
      nav.setWindow(window);
      await settle();
      expect(native).toHaveBeenCalledTimes(2);
      expectSqlWindow(native.mock.lastCall?.[1], span, end);
      expect(router.currentRoute.value.query.sql).toBe(native.mock.lastCall?.[1].query_text);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each(["failed", "stopped"])("replays a %s same-source link's own query on a window change", async (outcome) => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle();
    query.mockImplementationOnce((_team, _source, _request, options) =>
      outcome === "failed" ? Promise.reject(new Error("Query failed")) : untilAborted(options?.signal));
    await router.push({ path: "/logs/explore", query: evidenceB });
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceB.q }));
    if (outcome === "stopped") await nav.cancel();
    await settle();

    await expectOneRun(query, async () => nav.setWindow({ kind: "relative", spec: "1h" }));
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: evidenceB.q });
    expect(router.currentRoute.value.query).toMatchObject({ q: evidenceB.q, t: "1h" });
    expect(store.logchefqlCode).toBe(evidenceB.q);
  });

  const windowedLink = { team: "1", source: String(windowedSource.id), mode: "logchefql", q: 'level="info"', t: "15m" };
  const shownPage = [{ type: "plan", windows: [searchWindows[0]], complete: false }, windowRows(searchWindows[0], "info"), { type: "end", complete: true }] as WindowedEvent[];
  // A search that shows newer rows and offers Load older.
  const pagedSearch = () => streamWindows((request) => request.cursor === "second"
    ? [windowRows(searchWindows[1], "older"), { type: "end", complete: true }]
    : [{ type: "plan", windows: searchWindows, cursor: "first", complete: false }, windowRows(searchWindows[0], "newer", "second"), { type: "end", cursor: "second", complete: false }]);
  const searchRequests = () => vi.mocked(streamWindowedQuery).mock.calls.map(([, , request]) => request).filter((request) => !request.count);

  // The windowed search panel as Explorer wires it.
  function mountPanel({ pinia, nav }: Mounted) {
    const host = document.createElement("div");
    const panel = createApp({ render: () => h(WindowedSearchPanel, { onStop: () => nav.cancel(), onPage: nav.page }) }).use(pinia).use(i18n);
    panel.mount(host);
    const find = (label: string) => [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === label);
    return {
      buttons: () => [...host.querySelectorAll("button")].map((button) => button.textContent?.trim()),
      text: () => host.textContent ?? "",
      click: async (label: string) => (await vi.waitFor(() => {
        const button = find(label);
        if (!button) throw new Error(`no ${label} button`);
        return button;
      })).click(),
      unmount: () => panel.unmount(),
    };
  }

  it("records, filters the sidebar with and replays a windowed Run stopped after it showed rows", async () => {
    const { router, store, nav, rows, pinia } = await mountExplorer(windowedLink, () => streamWindows(() => shownPage));
    await settle();
    const fieldValues = vi.spyOn(sourcesApi, "getFieldValues").mockImplementation(
      (_teamId, _sourceId, fieldName, fieldType) => ok({ field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [], total_distinct: 0 }),
    );
    const sidebar = createApp({
      render: () => h(FieldSideBar, { fields: [{ name: "level", type: "Int64" }], expanded: true, teamId: team.id, sourceId: windowedSource.id, source: windowedSource }),
    }).use(pinia).use(i18n);
    sidebar.mount(document.createElement("div"));
    try {
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalled());
      fieldValues.mockClear();
      streamWindows(() => [{ type: "plan", windows: searchWindows, cursor: "first", complete: false }, windowRows(searchWindows[0], "error", "second")]);
      store.setLogchefqlCode('level="error"');
      const running = nav.run();
      await vi.waitFor(() => expect(rows().map((row) => row._msg)).toEqual(["error"]));
      await nav.cancel();
      await running;
      await settle();

      expect(router.currentRoute.value.query.q).toBe('level="error"');
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalled());
      expect(fieldValues.mock.lastCall?.slice(8, 10)).toEqual(["logchefql", 'level="error"']);

      streamWindows(() => shownPage);
      store.setLogchefqlCode('level="draft" and');
      nav.setWindow({ kind: "relative", spec: "1h" });
      await settle();
      expect(vi.mocked(streamWindowedQuery).mock.lastCall?.[2]).toMatchObject({ query_text: 'level="error"' });
      expect(router.currentRoute.value.query).toMatchObject({ q: 'level="error"', t: "1h" });
    } finally {
      sidebar.unmount();
    }
  });

  it.each(["stopped", "failed"])("keeps the shown histogram and coverage after a windowed Run %s before rows", async (outcome) => {
    const counted = { ...windowedSource, connection: { base_url: "http://logs.test", optimizer: { enabled: true, histogram_enabled: true } } };
    const explorer = await mountExplorer(windowedLink, () => {
      vi.mocked(sourcesApi.getTeamSource).mockImplementation((_teamId: number, sourceId: number) => ok(sourceId === counted.id ? counted : sourceById(sourceId)) as never);
      streamWindows((request) => request.query_text === windowedLink.q ? shownPage : [
        { type: "plan", windows: searchWindows, cursor: "first", complete: false },
        { type: "window", window: searchWindows[0], status: "running", cursor: "first", complete: false },
        ...(outcome === "failed" ? [{ type: "end" as const, message: "search failed", complete: false }] : []),
      ]);
    });
    const { store, nav, rows, banner } = explorer;
    await settle();
    const histogram = () => useExploreRuns().shown?.search?.histogram ?? [];
    await vi.waitFor(() => expect(histogram()).toHaveLength(1));
    const panel = mountPanel(explorer);
    try {
      store.setLogchefqlCode('level="error"');
      nav.run();
      if (outcome === "stopped") {
        await vi.waitFor(() => expect(useExploreRuns().current?.search?.coverage).toHaveLength(2));
        expect(histogram()).toHaveLength(1);
        expect(panel.text()).toContain("Searched 1 of 1");
        nav.cancel();
      }
      await settle();
      await flush();
      expect(rows().map((row) => row._msg)).toEqual(["info"]);
      expect(histogram()).toHaveLength(1);
      expect(panel.text()).toContain("Searched 1 of 1");
      expect(panel.buttons()).toEqual([]);
      expect(banner()).toBe(outcome === "failed" ? "search failed" : null);
    } finally {
      panel.unmount();
    }
  });

  it("replaces the windowed histogram when the new Run first shows rows", async () => {
    const counted = { ...windowedSource, connection: { base_url: "http://logs.test", optimizer: { enabled: true, histogram_enabled: true } } };
    const explorer = await mountExplorer(windowedLink, () => {
      vi.mocked(sourcesApi.getTeamSource).mockImplementation((_teamId, sourceId) => ok(sourceId === counted.id ? counted : sourceById(sourceId)));
      streamWindows(() => shownPage);
    });
    const { store, nav, rows } = explorer;
    await settle();
    const histogram = () => useExploreRuns().shown?.search?.histogram ?? [];
    await vi.waitFor(() => expect(histogram()).toHaveLength(1));
    const firstPage = deferred();
    const ended = deferred();
    vi.mocked(streamWindowedQuery).mockImplementation(async (_teamId, _sourceId, _request, _signal, onEvent) => {
      onEvent({ type: "plan", windows: searchWindows, cursor: "first", complete: false });
      await firstPage.done;
      onEvent({ ...windowRows(searchWindows[0], "error"), logs: [{ _time: searchWindows[0].start, _msg: "error" }, { _time: searchWindows[0].start, _msg: "error" }] });
      await ended.done;
      onEvent({ type: "end", complete: true });
    });
    store.setLogchefqlCode('level="error"');
    nav.run();
    await flush();
    expect(histogram()).toHaveLength(1);
    firstPage.release();
    await vi.waitFor(() => expect(rows()).toHaveLength(2));
    expect(histogram()).toEqual([]);
    ended.release();
    await settle();
    expect(histogram().map((bucket) => bucket.log_count)).toEqual([2]);
  });

  it("keeps Cancel on the query a pick starts after aborting the running one", async () => {
    const { store, query, nav, isRunning } = await mountExplorer(evidenceA);
    await settle();
    query.mockImplementationOnce((_team, _source, _request, options) => untilAborted(options?.signal));
    query.mockImplementationOnce((_team, _source, _request, options) => untilAborted(options?.signal));
    store.setLogchefqlCode('slow="yes"');
    void nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));

    await nav.selectSource(otherSource.id);
    await vi.waitFor(() => expect(query.mock.lastCall?.[1]).toBe(otherSource.id));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(isRunning()).toBe(true);

    const picked = query.mock.lastCall?.[3]?.signal;
    await nav.cancel();
    expect(picked?.aborted).toBe(true);
    await settle();
    expect(isRunning()).toBe(false);
  });
  it("opens a saved query link that a Run's first rows arrive during", async () => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle();
    const ran = deferred();
    query.mockImplementationOnce(async () => { await ran.done; return ok(noLogs) as never; });
    store.setLogchefqlCode('slow="yes"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));
    const resolved = deferred();
    vi.spyOn(savedQueriesApi, "resolve").mockImplementation(async () => {
      await resolved.done;
      return ok({ ...savedOnOtherSource, source_id: source.id, query_content: savedContent('saved="yes"') }) as never;
    });
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", id: "42" } });
    ran.release();
    await flush();
    resolved.release();
    await vi.waitFor(() => expect(store.logchefqlCode).toBe('saved="yes"'));
    await settle();
    expect(router.currentRoute.value.query).toEqual({ team: "1", source: "3", id: "42" });
  });

  it("opens a source picked while a Run was running", async () => {
    const { router, store, nav, query } = await mountExplorer(evidenceA);
    await settle();
    const ran = deferred();
    const details = deferred();
    query.mockImplementationOnce(async () => { await ran.done; return ok(noLogs) as never; });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation(async (_teamId, sourceId) => {
      if (sourceId === otherSource.id) await details.done;
      return ok(sourceById(sourceId)) as never;
    });
    store.setLogchefqlCode('slow="yes"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));
    await nav.selectSource(otherSource.id);
    ran.release();
    await flush();
    details.release();
    await vi.waitFor(() => expect(store.sourceId).toBe(otherSource.id));
    await settle();
    expect(router.currentRoute.value.query.source).toBe(String(otherSource.id));
  });

  it("stays on the page the user left while a Run was running", async () => {
    const { router, store, query, nav } = await mountExplorer(evidenceA);
    await settle();
    const ran = deferred();
    query.mockImplementationOnce(async () => { await ran.done; return ok(noLogs) as never; });
    store.setLogchefqlCode('slow="yes"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));
    await router.push({ path: "/dashboards" });
    ran.release();
    await flush();
    await flush();
    expect(router.currentRoute.value.path).toBe("/dashboards");
  });

  it.each(["while the user is away", "after the user returns"])("records a Run whose rows arrive %s on another logs page", async (arrival) => {
    const { router, store, query, nav, rows } = await mountExplorer(evidenceA);
    await settle();
    const ran = deferred();
    query.mockImplementationOnce(async () => { await ran.done; return ok({ ...noLogs, logs: [{ msg: "slow" }] }) as never; });
    store.setLogchefqlCode('slow="yes"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));
    await router.push({ path: "/logs/library" });
    if (arrival === "while the user is away") {
      ran.release();
      await flush();
    }
    router.back();
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe("/logs/explore"));
    ran.release();
    await settle();
    await flush();
    expect(rows().map((row) => row.msg)).toEqual(["slow"]);
    expect(router.currentRoute.value.query.q).toBe('slow="yes"');
    expect(query).toHaveBeenCalledTimes(2);
  });

  const windowedRunStarts = () => streamWindows((request) => request.window_index === 0 || request.skip_window === 0 ? []
    : [{ type: "plan", windows: searchWindows, cursor: "first", complete: false }, { type: "window", window: searchWindows[0], status: "running", cursor: "first", complete: false }]);

  it("keeps the shown rows and URL when the panel's Stop ends a windowed Run before rows", async () => {
    const explorer = await mountExplorer(windowedLink, () => streamWindows(() => shownPage));
    const { router, store, nav, rows } = explorer;
    await settle();
    const panel = mountPanel(explorer);
    try {
      windowedRunStarts();
      store.setLogchefqlCode('level="error"');
      nav.run();
      await panel.click("Stop");
      await settle();
      expect(rows().map((row) => row._msg)).toEqual(["info"]);
      expect(router.currentRoute.value.query.q).toBe(windowedLink.q);
    } finally {
      panel.unmount();
    }
  });

  it("sends nothing for a Skip that Stop cancels while the search before it ends", async () => {
    const explorer = await mountExplorer(windowedLink, () => streamWindows(() => shownPage));
    const { router, store, nav, rows } = explorer;
    await settle();
    const panel = mountPanel(explorer);
    try {
      streamWindows((request) => request.skip_window === 0
        ? [windowRows(searchWindows[1], "after-stop"), { type: "end", complete: true }]
        : [{ type: "plan", windows: searchWindows, cursor: "first", complete: false }, { type: "window", window: searchWindows[0], status: "running", cursor: "first", complete: false }]);
      store.setLogchefqlCode('level="error"');
      nav.run();
      await panel.click("Skip to next window");
      nav.cancel();
      await settle();
      await flush();
      expect(searchRequests().filter((request) => request.skip_window !== undefined)).toEqual([]);
      expect({ rows: rows().map((row) => row._msg), query: router.currentRoute.value.query.q }).toEqual({ rows: ["info"], query: windowedLink.q });
    } finally {
      panel.unmount();
    }
  });

  it("runs a Retry after Stop until it settles, so Stop cancels it", async () => {
    const explorer = await mountExplorer(windowedLink, () => streamWindows(() => shownPage));
    const { router, store, nav, rows, isRunning } = explorer;
    await settle();
    const panel = mountPanel(explorer);
    try {
      streamWindows((request) => request.window_index === 1 ? [] : [
        { type: "plan", windows: searchWindows, cursor: "first", complete: false },
        windowRows(searchWindows[0], "error", "second"),
        { type: "window", window: searchWindows[1], status: "running", cursor: "second", complete: false },
      ]);
      store.setLogchefqlCode('level="error"');
      nav.run();
      await vi.waitFor(() => expect(rows().map((row) => row._msg)).toEqual(["error"]));
      nav.cancel();
      await settle();

      await panel.click("Retry window");
      await vi.waitFor(() => expect(vi.mocked(streamWindowedQuery).mock.lastCall?.[2]).toMatchObject({ window_index: 1 }));
      expect(isRunning()).toBe(true);
      nav.cancel();
      expect(vi.mocked(streamWindowedQuery).mock.lastCall?.[3]?.aborted).toBe(true);
      await settle();

      streamWindows(() => [windowRows(searchWindows[1], "older"), { type: "end", complete: true }]);
      await panel.click("Retry window");
      await vi.waitFor(() => expect(rows().map((row) => row._msg)).toEqual(["error", "older"]));
      await settle();
      expect(router.currentRoute.value.query.q).toBe('level="error"');
    } finally {
      panel.unmount();
    }
  });

  it("shows nothing of a windowed Run until a Skip finds its rows", async () => {
    const explorer = await mountExplorer(windowedLink, () => streamWindows(() => shownPage));
    const { router, store, nav, rows, isRunning } = explorer;
    await settle();
    const panel = mountPanel(explorer);
    try {
      windowedRunStarts();
      store.setLogchefqlCode('level="error"');
      nav.run();
      await panel.click("Skip to next window");
      await vi.waitFor(() => expect(vi.mocked(streamWindowedQuery).mock.lastCall?.[2]).toMatchObject({ skip_window: 0 }));
      await flush();
      expect(rows().map((row) => row._msg)).toEqual(["info"]);
      expect(router.currentRoute.value.query.q).toBe(windowedLink.q);
      expect(isRunning()).toBe(true);
      nav.cancel();
      await settle();
    } finally {
      panel.unmount();
    }
  });

  it.each(["fails", "is stopped"])("pages the shown windowed results after a Run that %s validating", async (outcome) => {
    const explorer = await mountExplorer(windowedLink, pagedSearch);
    const { store, nav, rows } = explorer;
    await settle();
    const panel = mountPanel(explorer);
    try {
      const validated = deferred();
      vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
        await validated.done;
        return ok({ valid: false, error: { message: "bad" } }) as never;
      });
      store.setLogchefqlCode("level=");
      nav.run();
      if (outcome === "is stopped") nav.cancel();
      validated.release();
      await settle();

      await panel.click("Load older");
      await vi.waitFor(() => expect(rows().map((row) => row._msg)).toEqual(["newer", "older"]));
    } finally {
      panel.unmount();
    }
  });

  it.each([["a Run validates", 'level="error"'], ["a navigation loads", 'level="saved"']])("refuses Load older while %s, which then runs", async (pending, ran) => {
    const explorer = await mountExplorer(windowedLink, pagedSearch);
    const { router, store, nav } = explorer;
    await settle();
    const panel = mountPanel(explorer);
    try {
      const released = deferred();
      if (pending === "a Run validates") {
        vi.mocked(logchefqlApi.validate).mockImplementation(async () => {
          await released.done;
          return ok({ valid: true }) as never;
        });
        store.setLogchefqlCode(ran);
        nav.run();
      } else {
        vi.spyOn(savedQueriesApi, "resolve").mockImplementation(async () => {
          await released.done;
          return ok({ ...savedOnOtherSource, source_id: windowedSource.id, query_content: savedContent(ran) }) as never;
        });
        await router.push({ path: "/logs/explore", query: { team: "1", source: windowedLink.source, id: "42" } });
      }
      await panel.click("Load older");
      released.release();
      await vi.waitFor(() => expect(searchRequests().at(-1)?.query_text).toBe(ran));
      await settle();
      expect(searchRequests().filter((request) => request.cursor)).toEqual([]);
    } finally {
      panel.unmount();
    }
  });

  it("regenerates unchanged default SQL that a Run asks for again", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const { nav, store } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) }, () => localStorage.clear());
      await settle();
      expect(store.nativeQuery).toBe(native.mock.lastCall?.[1].query_text);

      vi.setSystemTime(idleStart + idle);
      nav.run();
      await settle();
      expectSqlWindow(native.mock.lastCall?.[1], 15 * 60_000, idleStart + idle);
      vi.setSystemTime(idleStart + 2 * idle);
      nav.setWindow({ kind: "relative", spec: "1h" });
      await settle();
      expectSqlWindow(native.mock.lastCall?.[1], 3_600_000, idleStart + 2 * idle);

      const edited = `${store.nativeQuery}\n-- mine`;
      store.setNativeQuery(edited);
      nav.run();
      await settle();
      expect(native.mock.lastCall?.[1].query_text).toBe(edited);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each(["echo", "edit and undo"])("runs generated SQL after an editor %s", async (change) => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const { store, nav } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) }, () => localStorage.clear());
      await settle();
      const generated = store.nativeQuery;
      if (change === "edit and undo") store.setNativeQuery(`${generated} `);
      store.setNativeQuery(generated);
      vi.setSystemTime(idleStart + idle);
      nav.run();
      await settle();
      if (change === "echo") expectSqlWindow(native.mock.lastCall?.[1], 15 * 60_000, idleStart + idle);
      else expect(native.mock.lastCall?.[1].query_text).toBe(generated);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each([["Save", "id", "77"], ["Share", "share", "tok2"]])("runs the default SQL a %s captured as literal SQL", async (action, parameter, value) => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const explorer = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) }, () => localStorage.clear());
      const { router, store, nav, saved } = explorer;
      await settle();
      const generated = store.nativeQuery;
      stubShareAndSave();
      if (action === "Share") await share(explorer);
      else await nav.recordAfter(() => saved.handleSaveQuery({
        ...saveForm, source_id: sqlOnlySource.id, query_language: "clickhouse-sql", editor_mode: "native",
        query_content: JSON.stringify({ version: 1, sourceId: sqlOnlySource.id, limit: store.limit, content: generated }),
      }), store.markSavedQuery);

      vi.setSystemTime(idleStart + idle);
      nav.run();
      await settle();
      expect(native.mock.lastCall?.[1].query_text).toBe(generated);
      expect(store.nativeQuery).toBe(generated);
      expect(router.currentRoute.value.query[parameter]).toBe(value);
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps a Share's token off a query edited while the Share was in flight", async () => {
    const explorer = await mountExplorer(evidenceA);
    const { router, store, nav, query } = explorer;
    await settle();
    stubShareAndSave();
    const shared = deferred();
    vi.mocked(exploreApi.createQueryShare).mockImplementation(async () => {
      await shared.done;
      return ok(shareForm) as never;
    });
    const sharing = share(explorer);
    await vi.waitFor(() => expect(exploreApi.createQueryShare).toHaveBeenCalled());
    store.setLogchefqlCode('edited="yes"');
    shared.release();
    await sharing;
    expect(router.currentRoute.value.query.q).toBe(evidenceA.q);

    nav.run();
    await settle();
    expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'edited="yes"' });
    expect(router.currentRoute.value.query).toMatchObject({ q: 'edited="yes"' });
    expect(router.currentRoute.value.query.share).toBeUndefined();
  });

  it("sends the shown run's variables with a group-by histogram", async () => {
    const { store, nav } = await mountExplorer(evidenceA);
    await settle();
    const variables = useVariableStore();
    const env = (value: string) => [{ name: "env", type: "text" as const, label: "env", inputType: "input" as const, value }];
    variables.setAllVariable(env("prod"));
    store.setLogchefqlCode('env="{{env}}"');
    nav.run();
    await settle();
    variables.setAllVariable(env("staging"));

    useExploreRuns().setGroupByField("level");
    await flush();
    expect(vi.mocked(exploreApi.getHistogramData).mock.lastCall?.[1]).toMatchObject({ group_by: "level", variables: [{ name: "env", value: "prod" }] });
  });

  it.each(["failed", "stopped"])("keeps the shown histogram after a %s Run", async (outcome) => {
    const { store, query, nav } = await mountExplorer(evidenceA, () => {
      vi.mocked(exploreApi.getHistogramData).mockReturnValue(ok({ data: [{ bucket: "2026-10-19T04:00:00Z", log_count: 3 }], granularity: "1m" }) as never);
    });
    await settle();
    const histogram = useExploreHistogramStore();
    await vi.waitFor(() => expect(histogram.histogramData).toHaveLength(1));
    query.mockImplementationOnce((_team, _source, _request, options) =>
      outcome === "failed" ? Promise.reject(new Error("Query failed")) : untilAborted(options?.signal));
    store.setLogchefqlCode('other="yes"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'other="yes"' }));
    if (outcome === "stopped") nav.cancel();
    await settle();
    expect(histogram.histogramData).toHaveLength(1);
  });

  it("clears sidebar values when a same-source link shows nothing", async () => {
    const { router, query, pinia } = await mountExplorer(evidenceA);
    await settle();
    vi.spyOn(sourcesApi, "getFieldValues").mockImplementation((_teamId, _sourceId, fieldName, fieldType) =>
      ok({ field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [{ value: "old-location-value", count: 1 }], total_distinct: 1 }));
    const host = document.createElement("div");
    const sidebar = createApp({
      render: () => h(FieldSideBar, { fields: [{ name: "level", type: "Int64" }], expanded: true, teamId: team.id, sourceId: source.id, source }),
    }).use(pinia).use(i18n);
    sidebar.mount(host);
    try {
      await vi.waitFor(() => expect(host.textContent).toContain("old-location-value"));
      query.mockRejectedValueOnce(new Error("link query failed"));
      await router.push({ path: "/logs/explore", query: evidenceB });
      await settle();
      expect(host.textContent).not.toContain("old-location-value");
    } finally {
      sidebar.unmount();
    }
  });

  it("loads each sidebar field's values once when a source opens", async () => {
    const { nav, pinia } = await mountExplorer(evidenceA);
    await settle();
    const fieldValues = vi.spyOn(sourcesApi, "getFieldValues").mockImplementation((_teamId, _sourceId, fieldName, fieldType) =>
      ok({ field_name: fieldName, field_type: fieldType, is_low_cardinality: false, values: [], total_distinct: 0 }));
    const context = useContextStore();
    const sidebar = createApp({
      render: () => h(FieldSideBar, { fields: [{ name: "level", type: "Int64" }], expanded: true, teamId: team.id, sourceId: context.sourceId ?? undefined, source: useSourcesStore().currentSourceDetails }),
    }).use(pinia).use(i18n);
    sidebar.mount(document.createElement("div"));
    try {
      await vi.waitFor(() => expect(fieldValues).toHaveBeenCalled());
      await flush();
      fieldValues.mockClear();
      await nav.selectSource(otherSource.id);
      await vi.waitFor(() => expect(context.sourceId).toBe(otherSource.id));
      await settle();
      await flush();
      expect(fieldValues).toHaveBeenCalledTimes(1);
      expect(fieldValues.mock.lastCall?.[1]).toBe(otherSource.id);
    } finally {
      sidebar.unmount();
    }
  });

  it("records an away-finished Run when Back cancels a pending link", async () => {
    const { router, store, query, nav, rows } = await mountExplorer(evidenceA);
    await settle();
    const ran = deferred();
    query.mockImplementationOnce(async () => { await ran.done; return ok({ ...noLogs, logs: [{ msg: "slow" }] }); });
    store.setLogchefqlCode('slow="yes"');
    nav.run();
    await vi.waitFor(() => expect(query.mock.lastCall?.[2]).toMatchObject({ query: 'slow="yes"' }));
    const resolved = deferred();
    vi.spyOn(savedQueriesApi, "resolve").mockImplementation(async () => {
      await resolved.done;
      return ok({ ...savedOnOtherSource, query_language: 'logchefql', editor_mode: 'builder', source_id: source.id, query_content: savedContent('saved="yes"') } satisfies ResolvedSavedQuery);
    });
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", id: "42" } });
    expect(nav.status.value).toBe("navigating");
    await router.push({ path: "/logs/library" });
    ran.release();
    await settle();
    expect(rows().map((row) => row.msg)).toEqual(["slow"]);
    router.back();
    await vi.waitFor(() => expect(nav.status.value).toBe("navigating"));
    router.back();
    await vi.waitFor(() => expect(nav.status.value).toBe("ready"));
    await flush();
    resolved.release();
    await flush();
    expect(rows().map((row) => row.msg)).toEqual(["slow"]);
    expect(query).toHaveBeenCalledTimes(2);
    expect(router.currentRoute.value.query.q).toBe('slow="yes"');
  });

  it("regenerates default SQL after a source switch away and back", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const { nav } = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) });
      await settle();
      await nav.selectSource(otherSource.id);
      await settle();
      vi.setSystemTime(idleStart + idle);
      await nav.selectSource(sqlOnlySource.id);
      await settle();
      expectSqlWindow(native.mock.lastCall?.[1], 15 * 60_000);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each(["stopped", "failed"])("preserves shown counts and coverage after a newer Run %s before rows", async (outcome) => {
    const counts = deferred();
    const counted = { ...windowedSource, connection: { base_url: "http://logs.test", optimizer: { enabled: true, histogram_enabled: true } } };
    const explorer = await mountExplorer(windowedLink, () => {
      vi.mocked(sourcesApi.getTeamSource).mockImplementation((_teamId, sourceId) => ok(sourceId === counted.id ? counted : sourceById(sourceId)));
      vi.mocked(streamWindowedQuery).mockImplementation(async (_team, _source, request, signal, onEvent) => {
        await Promise.resolve();
        if (request.count) {
          if (request.query_text !== windowedLink.q) return untilAborted(signal);
          await counts.done;
          for (const window of searchWindows) onEvent({ type: "count", window, status: "complete", buckets: [{ bucket: window.start, log_count: 7 }], complete: false });
          onEvent({ type: "end", complete: true });
          return;
        }
        onEvent({ type: "plan", windows: searchWindows, cursor: "first", complete: false });
        if (request.query_text === windowedLink.q) {
          onEvent(windowRows(searchWindows[0], "info", "second"));
          onEvent({ type: "end", cursor: "second", complete: false });
        } else {
          onEvent({ type: "window", window: searchWindows[0], status: "running", cursor: "first", complete: false });
          if (outcome === "failed") onEvent({ type: "end", message: "search failed", complete: false });
          else await untilAborted(signal);
        }
      });
    });
    const panel = mountPanel(explorer);
    try {
      await settle();
      await vi.waitFor(() => expect(vi.mocked(streamWindowedQuery).mock.calls.some(([, , request]) => request.count)).toBe(true));
      const countSignal = vi.mocked(streamWindowedQuery).mock.calls.find(([, , request]) => request.count)?.[3];
      explorer.store.setLogchefqlCode('level="error"');
      explorer.nav.run();
      await vi.waitFor(() => expect(searchRequests()).toHaveLength(2));
      if (outcome === "stopped") explorer.nav.cancel();
      await settle();
      expect(explorer.rows().map(row => row._msg)).toEqual(["info"]);
      expect(panel.text()).toContain("Searched 1 of 2");
      expect(countSignal?.aborted).toBe(false);
      counts.release();
      await vi.waitFor(() => expect(panel.text()).toContain("14"));
      expect(panel.text()).toContain("of 14 rows");
      const failedCounts = vi.mocked(streamWindowedQuery).mock.calls.filter(([, , request]) => request.count && request.query_text !== windowedLink.q);
      expect(failedCounts).toEqual([]);
    } finally {
      counts.release();
      panel.unmount();
    }
  });

  it.each([
    ["older", "validation"], ["retry", "validation"], ["skip", "validation"],
    ["older", "stopped"], ["retry", "stopped"], ["skip", "stopped"],
    ["older", "failed"], ["retry", "failed"], ["skip", "failed"],
  ])("pages the shown run with %s after a newer Run %s", async (action, outcome) => {
    const explorer = await mountExplorer(windowedLink, () => streamWindows(request => {
      if (request.cursor || request.window_index !== undefined) return [windowRows(searchWindows[1], "older"), { type: "end", complete: true }];
      const first = windowRows(searchWindows[0], "info", "second");

      return [
        { type: "plan", windows: searchWindows, cursor: "first", complete: false },
        { type: "window", window: searchWindows[0], status: "running", cursor: "first", complete: false }, first,
        ...(action === "retry" ? [{ type: "window" as const, window: searchWindows[0], status: "partial" as const, cursor: "first", complete: false }] : []),
        ...(action === "skip" ? [{ type: "window" as const, window: searchWindows[1], status: "failed" as const, cursor: "second", complete: false }] : []),
        { type: "end", cursor: "second", complete: false },
      ];
    }));
    await settle();
    if (outcome === "validation") vi.mocked(logchefqlApi.validate).mockReturnValueOnce(ok({ valid: false, error: { code: "syntax", message: "invalid", position: { line: 1, column: 1 } } }));
    else vi.mocked(streamWindowedQuery).mockImplementationOnce(async (_team, _source, _request, signal, onEvent) => {
      onEvent({ type: "plan", windows: searchWindows, cursor: "first", complete: false });
      onEvent({ type: "window", window: searchWindows[0], status: "running", cursor: "first", complete: false });
      if (outcome === "failed") onEvent({ type: "end", message: "search failed", complete: false });
      else await untilAborted(signal);
    });
    explorer.store.setLogchefqlCode('level="error"');
    explorer.nav.run();
    if (outcome === "stopped") {
      await vi.waitFor(() => expect(searchRequests()).toHaveLength(2));
      explorer.nav.cancel();
    }
    await settle();
    const panel = mountPanel(explorer);
    try {
      if (action === "retry") await panel.click("Retry window");
      else await panel.click(action === "older" ? "Load older" : "Skip to next window");
      await vi.waitFor(() => expect(explorer.rows().map(row => row._msg)).toEqual(["info", "older"]));
      await settle();
      expect(searchRequests().at(-1)?.query_text).toBe(windowedLink.q);
      expect(explorer.router.currentRoute.value.query.q).toBe(windowedLink.q);
    } finally { panel.unmount(); }
  });

  it.each(["Save", "Share"])("restores the literal default SQL that a %s captured", async (action) => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(idleStart);
      const native = vi.spyOn(exploreApi, "getLogs").mockReturnValue(ok(noLogs));
      const explorer = await mountExplorer({ team: "1", source: String(sqlOnlySource.id) });
      await settle();
      const generated = explorer.store.nativeQuery;
      stubShareAndSave();
      if (action === "Share") await share(explorer);
      else await explorer.nav.recordAfter(() => explorer.saved.handleSaveQuery({
        ...saveForm, source_id: sqlOnlySource.id, query_language: "clickhouse-sql", editor_mode: "native",
        query_content: JSON.stringify({ version: 1, sourceId: sqlOnlySource.id, limit: explorer.store.limit, content: generated }),
      }), explorer.store.markSavedQuery);
      await explorer.nav.selectSource(otherSource.id);
      await settle();
      vi.setSystemTime(idleStart + idle);
      await explorer.nav.selectSource(sqlOnlySource.id);
      await settle();
      expect(native.mock.lastCall?.[1].query_text).toBe(generated);
      expect(explorer.store.showsDefaultSql).toBe(false);
    } finally { vi.useRealTimers(); }
  });

  it("disposes both sessions when Explorer unmounts during a replacement Run", async () => {
    const counted = { ...windowedSource, connection: { base_url: "http://logs.test", optimizer: { enabled: true, histogram_enabled: true } } };
    const explorer = await mountExplorer(windowedLink, () => {
      vi.mocked(sourcesApi.getTeamSource).mockImplementation((_teamId, sourceId) => ok(sourceId === counted.id ? counted : sourceById(sourceId)));
      vi.mocked(streamWindowedQuery).mockImplementation(async (_team, _source, request, signal, onEvent) => {
        await Promise.resolve();
        if (request.count || request.query_text !== windowedLink.q) return untilAborted(signal);
        onEvent({ type: "plan", windows: searchWindows, cursor: "first", complete: false });
        onEvent(windowRows(searchWindows[0], "info", "second"));
        onEvent({ type: "end", cursor: "second", complete: false });
      });
    });
    await settle();
    const counts = vi.mocked(streamWindowedQuery).mock.calls.find(([, , request]) => request.count)?.[3];
    expect(counts?.aborted).toBe(false);
    explorer.store.setLogchefqlCode('level="error"');
    explorer.nav.run();
    await vi.waitFor(() => expect(searchRequests()).toHaveLength(2));
    await explorer.router.push('/dashboards');
    expect(counts?.aborted).toBe(true);
    expect(vi.mocked(streamWindowedQuery).mock.lastCall?.[3].aborted).toBe(true);
  });
});
