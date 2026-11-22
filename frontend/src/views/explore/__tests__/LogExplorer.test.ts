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
import { savedViewsApi, type SavedView, type SavedViewList } from "@/api/savedViews";
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
  logs: [{ timestamp: "2026-11-22 11:09:23.182", host: 2 }, { timestamp: "2026-11-22 11:08:41.182", host: 1 }],
  columns: [{ name: "timestamp", type: "DateTime64(3)" }, { name: "host", type: "Int64" }],
  stats: { execution_time_ms: 1, rows_read: 1, bytes_read: 0 },
  generated_sql: "SELECT 1",
};

const noViews: SavedViewList = { views: [], default_view_id: null };

describe("LogExplorer", () => {
  let unmount: (() => void) | undefined;

  afterEach(() => {
    unmount?.();
    unmount = undefined;
    vi.restoreAllMocks();
    localStorage.clear();
    document.body.innerHTML = "";
  });

  async function mountExplorer(location: Record<string, string>, views = noViews, result = logs) {
    vi.spyOn(teamsApi, "getTeam").mockImplementation(id => ok({ ...team, id, created_by: "1" }));
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([team]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([source]) as never);
    vi.spyOn(sourcesApi, "getTeamSource").mockReturnValue(ok(source) as never);
    vi.spyOn(sourcesApi, "getFieldValues").mockImplementation(async (_team, _source, fieldName, fieldType) => ({
      status: "success",
      data: { field_name: fieldName, field_type: fieldType, is_low_cardinality: true, values: [{ value: "1", count: 4 }, { value: "2", count: 3 }], total_distinct: 2 },
    }));
    vi.spyOn(savedViewsApi, "list").mockReturnValue(ok(views) as never);
    const query = vi.spyOn(logchefqlApi, "query").mockImplementation(() => ok(result) as never);
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
    await router.push({ path: "/logs/explore", query: { team: "1", source: "3", ...location } });
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
    const { host, query, router } = await mountExplorer({ q: "status=500", t: "7d" });
    (await sidebarValue(host, "include")).click();
    await expectRun(query, router, "status=500 and host=2");
  });

  it("runs the query when a sidebar value is excluded", async () => {
    const { host, query, router } = await mountExplorer({ q: "status=500", t: "7d" });
    (await sidebarValue(host, "exclude")).click();
    await expectRun(query, router, "status=500 and host!=2");
  });

  it("runs the query when a value in an expanded row is filtered", async () => {
    const { host, query, router } = await mountExplorer({ q: "status=500", t: "7d" });
    host.querySelector<HTMLTableRowElement>("tbody tr")?.click();
    await vi.waitFor(() => expect(host.querySelector("[data-field-row]")).not.toBeNull());
    const row = [...host.querySelectorAll<HTMLElement>("[data-field-row]")].find(element => element.querySelector("span")?.textContent === "host");
    const filter = row?.querySelector<HTMLButtonElement>('button[aria-label^="Filter ="]');
    if (!filter) throw new Error("Missing expanded row filter button");
    filter.click();
    await expectRun(query, router, "status=500 and host=2");
  });

  it("runs the query when a nested field in an expanded row is added as a column", async () => {
    const nested = {
      ...logs,
      logs: logs.logs.map(row => ({ ...row, attrs: { user: "ann" } })),
      columns: [...logs.columns, { name: "attrs", type: "Map(String, String)" }],
    };
    const { host, query, router } = await mountExplorer({ q: "status=500", t: "7d" }, noViews, nested);
    host.querySelector<HTMLTableRowElement>("tbody tr")?.click();
    await vi.waitFor(() => expect(host.querySelector('button[aria-label="Add as column: attrs.user"]')).not.toBeNull());
    host.querySelector<HTMLButtonElement>('button[aria-label="Add as column: attrs.user"]')?.click();
    await expectRun(query, router, "status=500 | timestamp host attrs attrs.user");
  });

  // A view made from the layout with no view: that layout hides host too.
  const hostlessView: SavedView = {
    id: 12, source_id: 3, scope: "personal", name: "errors", description: "",
    definition: { version: 1, columns: [{ name: "timestamp", width: 300 }], sort: [{ column: "host", desc: false }], filter: "status=500", time_range: "3d" },
    is_default: false, managed: false, revision: 1, created_at: "", updated_at: "", can_edit: true, can_set_default: true,
  };

  function storeHostlessLayout() {
    localStorage.setItem("logchef-tableState-1-3", JSON.stringify({
      columnOrder: ["timestamp", "host"], orderSource: "explicit", columnClasses: {}, columnSizing: { timestamp: 300 },
      columnVisibility: { timestamp: true, host: false }, visibilityVersion: 2, sizingVersion: 1,
    }));
  }

  async function chooseNoView(host: HTMLElement) {
    const trigger = host.querySelector<HTMLElement>("[data-testid=view-picker]");
    if (!trigger) throw new Error("Missing view picker");
    trigger.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, pointerType: "mouse" }));
    trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await vi.waitFor(() => expect(document.body.querySelector("[role=menuitem]")).not.toBeNull());
    const noView = [...document.body.querySelectorAll<HTMLElement>("[role=menuitem]")].find(item => item.textContent?.trim() === "No view");
    if (!noView) throw new Error("Missing No view item");
    noView.click();
  }

  const headers = (host: HTMLElement) => [...host.querySelectorAll("thead th")].map(th => th.textContent?.trim()).filter(Boolean);
  const width = (host: HTMLElement, column: string) => [...host.querySelectorAll<HTMLElement>("thead th")].find(th => th.textContent?.trim() === column)?.style.width;
  const hostColumn = (host: HTMLElement) => [...host.querySelectorAll("tbody tr")].map(tr => tr.textContent).join("|");

  it("returns to the default layout and drops the view's filter and range on No view", async () => {
    storeHostlessLayout();
    const { host, query, router } = await mountExplorer({ view: "12" }, { views: [hostlessView], default_view_id: null });
    expect(lastQuery(query)).toBe("status=500");
    expect(router.currentRoute.value.query).toMatchObject({ view: "12", q: "status=500", t: "3d" });
    expect(headers(host)).toEqual(["timestamp"]);
    expect(width(host, "timestamp")).toBe("300px");

    await chooseNoView(host);

    await expectRun(query, router, "");
    expect(router.currentRoute.value.query.view).toBeUndefined();
    expect(router.currentRoute.value.query.t).toBe("15m");
    await vi.waitFor(() => expect(headers(host)).toEqual(["timestamp", "host"]));
    expect(width(host, "timestamp")).not.toBe("300px");
    // Newest first, not the view's host order.
    expect(hostColumn(host).indexOf("11:09:23")).toBeLessThan(hostColumn(host).indexOf("11:08:41"));
  });

  it("keeps an edited query on No view", async () => {
    const { host, query, router } = await mountExplorer({ view: "12" }, { views: [hostlessView], default_view_id: null });
    (await sidebarValue(host, "include")).click();
    await expectRun(query, router, "status=500 and host=2");
    const runs = query.mock.calls.length;

    await chooseNoView(host);

    await vi.waitFor(() => expect(router.currentRoute.value.query.view).toBeUndefined());
    expect(router.currentRoute.value.query).toMatchObject({ q: "status=500 and host=2", t: "3d" });
    expect(query.mock.calls.length).toBe(runs);
  });
});
