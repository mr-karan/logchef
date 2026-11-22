import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h } from "vue";
import { createPinia, disposePinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { teamsApi, type UserTeamMembership } from "@/api/teams";
import { authApi } from "@/api/auth";
import { metaApi } from "@/api/meta";
import { sourcesApi, type Source } from "@/api/sources";
import { logchefqlApi } from "@/api/logchefql";
import { exploreApi } from "@/api/explore";
import { savedViewsApi } from "@/api/savedViews";
import { useAuthStore } from "@/stores/auth";
import { useExploreRuns } from "@/stores/exploreRuns";
import { contextRouterGuard } from "@/router/contextGuard";
import { i18n } from "@/i18n";
import LogExplorer from "../LogExplorer.vue";

// Monaco and ECharts do not run in jsdom.
vi.mock("@/components/query-editor/QueryEditor.vue", () => ({
  default: defineComponent({ setup: (_props, { expose }) => { expose({ focus: () => undefined }); return () => null; } }),
}));
vi.mock("../components/HistogramVisualization.vue", () => ({ default: defineComponent({ render: () => null }) }));

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
  columns: [{ name: "timestamp", type: "DateTime64(3)" }, { name: "host", type: "Int64" }],
};
const logs = {
  logs: [{ timestamp: "2026-11-22 11:09:23.182", host: 2 }],
  columns: [{ name: "timestamp", type: "DateTime64(3)" }, { name: "host", type: "Int64" }],
  stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 0 },
  generated_sql: "SELECT 1",
};

describe("LogExplorer filters", () => {
  let unmount: (() => void) | undefined;

  afterEach(() => {
    unmount?.();
    unmount = undefined;
    vi.restoreAllMocks();
    localStorage.clear();
    document.body.innerHTML = "";
  });

  async function mountExplorer(q: string) {
    vi.spyOn(teamsApi, "getTeam").mockImplementation(id => ok({ ...team, id, created_by: "1" }));
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([team]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([source]) as never);
    vi.spyOn(sourcesApi, "getTeamSource").mockReturnValue(ok(source) as never);
    vi.spyOn(sourcesApi, "getFieldValues").mockImplementation(async (_team, _source, fieldName, fieldType) => ({
      status: "success",
      data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: true, values: [{ value: "1", count: 4 }, { value: "2", count: 3 }], total_distinct: 2 },
    }));
    vi.spyOn(savedViewsApi, "list").mockReturnValue(ok({ views: [], default_view_id: null }) as never);
    const query = vi.spyOn(logchefqlApi, "query").mockImplementation(() => ok(logs) as never);
    vi.spyOn(logchefqlApi, "validate").mockReturnValue(ok({ valid: true }) as never);
    vi.spyOn(exploreApi, "getHistogramData").mockReturnValue(ok({ data: [], granularity: "1m" }) as never);
    vi.spyOn(metaApi, "getMeta").mockReturnValue(ok({ version: "test" }) as never);
    vi.spyOn(authApi, "getSession").mockReturnValue(ok({
      user: { id: "1", email: "owner@example.com", full_name: "Owner", role: "member", status: "active", account_type: "human", created_at: "", updated_at: "" },
      session: null,
    }) as never);

    const pinia = createPinia();
    setActivePinia(pinia);
    await useAuthStore().initialize();
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/logs/explore", name: "LogExplorer", component: LogExplorer }] });
    router.beforeEach(contextRouterGuard);
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", q, t: "7d" } });
    const host = document.createElement("div");
    document.body.appendChild(host);
    const app = createApp({ render: () => h(RouterView) }).use(pinia).use(router).use(i18n);
    app.mount(host);
    unmount = () => { app.unmount(); disposePinia(pinia); };
    await vi.waitFor(() => expect(query).toHaveBeenCalled());
    await vi.waitFor(() => expect(useExploreRuns().isRunning).toBe(false));
    await vi.waitFor(() => expect(host.querySelector("tbody tr")).not.toBeNull());
    return { host, query, router };
  }

  const lastQuery = (query: ReturnType<typeof vi.spyOn>) => (query.mock.lastCall?.[2] as { query: string } | undefined)?.query;

  async function expectRun(query: ReturnType<typeof vi.spyOn>, router: ReturnType<typeof createRouter>, text: string) {
    await vi.waitFor(() => expect(lastQuery(query)).toBe(text));
    await vi.waitFor(() => expect(router.currentRoute.value.query.q).toBe(text));
  }

  async function sidebarValue(host: HTMLElement, label: string) {
    await vi.waitFor(() => expect(host.querySelector(`button[title='host="2"']`)).not.toBeNull());
    const button = label === "include"
      ? host.querySelector<HTMLButtonElement>(`button[title='host="2"']`)
      : host.querySelector<HTMLButtonElement>(`button[aria-label='Exclude: host!="2"']`);
    if (!button) throw new Error(`Missing sidebar ${label} button`);
    return button;
  }

  it("runs the query when a sidebar value is included", async () => {
    const { host, query, router } = await mountExplorer("status=500");
    (await sidebarValue(host, "include")).click();
    await expectRun(query, router, "status=500 and host=2");
  });

  it("runs the query when a sidebar value is excluded", async () => {
    const { host, query, router } = await mountExplorer("status=500");
    (await sidebarValue(host, "exclude")).click();
    await expectRun(query, router, "status=500 and host!=2");
  });

  it("runs the query when a value in an expanded row is filtered", async () => {
    const { host, query, router } = await mountExplorer("status=500");
    host.querySelector<HTMLTableRowElement>("tbody tr")?.click();
    await vi.waitFor(() => expect(host.querySelector("[data-field-row]")).not.toBeNull());
    const row = [...host.querySelectorAll<HTMLElement>("[data-field-row]")].find(element => element.querySelector("span")?.textContent === "host");
    const filter = row?.querySelector<HTMLButtonElement>('button[aria-label^="Filter ="]');
    if (!filter) throw new Error("Missing expanded row filter button");
    filter.click();
    await expectRun(query, router, "status=500 and host=2");
  });
});
