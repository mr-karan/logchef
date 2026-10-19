import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { teamsApi, type UserTeamMembership } from "@/api/teams";
import { sourcesApi, type Source } from "@/api/sources";
import { logchefqlApi } from "@/api/logchefql";
import { exploreApi } from "@/api/explore";
import { useExploreStore } from "@/stores/explore";
import { exploreWindowQuery } from "@/utils/alertExplore";

import { normalizeQueryParams, useUrlState } from "../useUrlState";

describe("normalizeQueryParams", () => {
  it("keeps an empty q or sql, which names an empty query", () => {
    expect(normalizeQueryParams({ source: "10", mode: "logchefql", q: "" })).toEqual({ source: "10", mode: "logchefql", q: "" });
    expect(normalizeQueryParams({ mode: "native", sql: "" })).toEqual({ mode: "native", sql: "" });
  });

  it("drops other empty params and leaves q and sql out when absent", () => {
    expect(normalizeQueryParams({ team: "", source: "10", limit: "" })).toEqual({ source: "10" });
  });
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

// Two evidence chips, as Ask Chef pushes them, with whole-second instants.
const evidenceA = exploreWindowQuery({ teamId: 1, sourceId: 3, start: Date.UTC(2026, 9, 19, 4), end: Date.UTC(2026, 9, 19, 5), query: 'level="error"' });
const evidenceB = exploreWindowQuery({ teamId: 1, sourceId: 3, start: Date.UTC(2026, 9, 18, 9), end: Date.UTC(2026, 9, 18, 10), query: 'service="cart"' });

describe("Explorer history navigation", () => {
  let unmount: (() => void) | undefined;

  afterEach(() => {
    unmount?.();
    unmount = undefined;
    vi.restoreAllMocks();
    localStorage.clear();
  });

  async function mountExplorer() {
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([team]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([source]) as never);
    vi.spyOn(sourcesApi, "getTeamSource").mockReturnValue(ok(source) as never);
    const query = vi.spyOn(logchefqlApi, "query").mockReturnValue(
      ok({ logs: [], columns: [], stats: { execution_time_ms: 1, rows_read: 0, bytes_read: 0 }, generated_sql: "SELECT 1" }) as never,
    );
    vi.spyOn(exploreApi, "getHistogramData").mockReturnValue(ok({ data: [], granularity: "1m" }) as never);

    // A conflicting draft for the same team and source.
    localStorage.setItem(
      "logchef.explore.draft.1.3",
      JSON.stringify({ version: 1, mode: "logchefql", nativeQuery: "", logchefqlCode: 'draft="yes"', limit: 100, relativeTime: "7d", absoluteStart: null, absoluteEnd: null, timezone: null, variables: [] }),
    );

    const pinia = createPinia();
    setActivePinia(pinia);
    let urlState: ReturnType<typeof useUrlState> | undefined;
    const Explorer = defineComponent({
      setup() {
        urlState = useUrlState();
        return () => null;
      },
    });
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/logs/explore", component: Explorer }] });
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3" } });
    const app = createApp({ render: () => h(RouterView) }).use(pinia).use(router);
    app.mount(document.createElement("div"));
    unmount = () => app.unmount();
    const store = useExploreStore();
    if (!urlState) throw new Error("explorer did not mount");
    await urlState.initialize();
    return { router, store, query };
  }

  async function settle(store: ReturnType<typeof useExploreStore>) {
    await vi.waitFor(() => expect(store.isExecutingQuery).toBe(false));
    await new Promise((resolve) => setTimeout(resolve, 0));
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

  it("re-runs when the user chooses the same relative range again", async () => {
    const { router, store, query } = await mountExplorer();
    await settle(store);
    await expectOneRun(query, () => router.push({ path: "/logs/explore", query: evidenceA }), store);

    await expectOneRun(query, async () => store.setWindow({ kind: "relative", spec: "15m" }), store);
    await expectOneRun(query, async () => store.setWindow({ kind: "relative", spec: "15m" }), store);
    expect(router.currentRoute.value.query).toMatchObject({ t: "15m", q: evidenceA.q });
  });
});
