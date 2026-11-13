import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { i18n } from "@/i18n";
import { dashboardPanelApi, dashboardsApi, type DashboardQueryPanel } from "@/api/dashboards";
import { sourcesApi, type Source } from "@/api/sources";
import { useDashboardsStore } from "@/stores/dashboards";
import { useTeamsStore } from "@/stores/teams";
import PanelBuilderDrawer from "../components/PanelBuilderDrawer.vue";

vi.mock("@/components/query-editor/SqlMonacoEditor.vue", () => ({ default: { template: "<div />" } }));
vi.mock("../components/DashboardPanel.vue", () => ({ default: { template: "<div />" } }));
const source: Source = { id: 1, name: "ClickHouse", source_type: "clickhouse", _meta_is_auto_created: false, _meta_ts_field: "timestamp", connection: { host: "localhost", port: 9000, database: "default", table_name: "http" }, ttl_days: 0, created_at: "", updated_at: "", is_connected: true };
let app: App | undefined;
let host: HTMLDivElement;
afterEach(() => { app?.unmount(); host?.remove(); useDashboardsStore().clearCurrent(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
async function mount(panel: DashboardQueryPanel, failure = false) {
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  const pinia = createPinia();
  setActivePinia(pinia);
  const teams = useTeamsStore();
  vi.spyOn(teams, "loadUserTeams").mockResolvedValue({ success: true });
  vi.spyOn(teams, "listTeamSources").mockResolvedValue({ success: true, data: [] });
  const detail = vi.spyOn(sourcesApi, "getTeamSource");
  if (failure) detail.mockRejectedValue(new Error("Source unavailable"));
  else detail.mockResolvedValue({ status: "success", data: source });
  vi.spyOn(dashboardsApi, "get").mockResolvedValue({ status: "success", data: { id: 1, name: "Test", description: "", corrupt: false, revision: 1, created_at: "", updated_at: "", can_edit: true, panels: { version: 2, features: [], tabs: [{ id: "main", title: "Main", layout: [] }], panels: [] } } });
  const store = useDashboardsStore();
  await store.loadDashboard(1);
  await store.enterEdit();
  vi.spyOn(store, "previewPanel").mockResolvedValue();
  store.upsertDraftPanel(panel);
  host = document.createElement("div");
  document.body.append(host);
  app = createApp(PanelBuilderDrawer, { open: true, panelId: panel.id }).use(pinia).use(i18n);
  app.mount(host);
  await vi.waitFor(() => expect(detail).toHaveBeenCalled());
  await nextTick();
  return store;
}
function button(text: string) {
  const match = [...document.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);
  if (!match) throw new Error(`Missing button ${text}`);
  return match;
}
const panel: DashboardQueryPanel = { id: "p", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "count", language: "logchefql", text: "", interval: "auto" }, viz: { type: "stat", reduce: "min", format: { decimals: 2 } } };
describe("panel builder mode transitions", () => {
  it.each(["chart", "pie", "toplist"] as const)("switches count %s to SQL result mode and restores the visualization", async (type) => {
    const original: DashboardQueryPanel = { ...panel, viz: { type, format: { decimals: 2 } } };
    const store = await mount(original);
    expect(button("ClickHouse SQL").disabled).toBe(false);
    button("ClickHouse SQL").click();
    await nextTick();
    expect(store.editDraft?.panels[0]).toMatchObject({ query: { mode: "result", language: "clickhouse-sql" } });
    expect(button("count").disabled).toBe(true);
    button("LogchefQL").click();
    await nextTick();
    expect(store.editDraft?.panels[0]).toEqual(original);
  });
  it("does not rewrite a LogsQL panel when source details fail", async () => {
    const logs: DashboardQueryPanel = { ...panel, query: { mode: "result", language: "logsql", text: "* | stats count() as n" }, viz: { type: "stat", value: "n" } };
    const store = await mount(logs, true);
    expect(store.editDraft?.panels[0]).toEqual(logs);
  });
  it("restores the count stat visualization after toggling SQL and back", async () => {
    const store = await mount(panel);
    button("ClickHouse SQL").click();
    await nextTick();
    expect(button("Time series").disabled).toBe(false);
    expect(button("Breakdown").disabled).toBe(false);
    button("LogchefQL").click();
    await nextTick();
    expect(store.editDraft?.panels[0]).toEqual(panel);
  });
  it("keeps the number format when switching result back to count", async () => {
    const store = await mount({ ...panel, query: { mode: "result", language: "logchefql", text: "" } });
    button("count").click();
    await nextTick();
    expect(store.editDraft?.panels[0].viz).toMatchObject({ type: "stat", format: { decimals: 2 } });
  });
});

it.each(["chart", "toplist", "pie"] as const)("falls back to LogchefQL for a count LogsQL %s on ClickHouse", async (type) => {
  const store = await mount({ ...panel, query: { mode: "count", language: "logsql", text: "*", interval: "auto" }, viz: { type } });
  expect(store.editDraft?.panels[0]).toMatchObject({ query: { mode: "count", language: "logchefql" }, viz: { type } });
});
it.each(["Table", "avg"])("keeps the SQL visualization change %s when returning to LogchefQL", async (change) => {
  const store = await mount(panel);
  button("ClickHouse SQL").click();
  await nextTick();
  button(change).click();
  await nextTick();
  const edited = store.editDraft?.panels[0].viz;
  button("LogchefQL").click();
  await nextTick();
  expect(store.editDraft?.panels[0]).toMatchObject({ query: { mode: "result", language: "logchefql" }, viz: edited });
});

it('keeps a result chart when toggling to count', async () => {
  const store = await mount({ ...panel, query: { mode: 'result', language: 'logchefql', text: '' }, viz: { type: 'chart', x: 'day', y: ['n'], mark: 'area' } });
  button('count').click();
  await nextTick();
  expect(store.editDraft?.panels[0].viz).toEqual({ type: 'chart', mark: 'area' });
});

async function deliverFrame(store: ReturnType<typeof useDashboardsStore>) {
  vi.mocked(store.previewPanel).mockRestore();
  vi.spyOn(dashboardPanelApi, "logsQuery").mockResolvedValue({ status: "success", data: {
    columns: [{ name: "day", type: "Date" }, { name: "n", type: "UInt32" }],
    stats: { execution_time_ms: 1, rows_read: 2, bytes_read: 0 },
    data: [{ day: "2026-11-10", n: 3 }, { day: "2026-11-11", n: 7 }],
  } });
  const current = store.editDraft?.panels[0];
  if (!current || current.locked) throw new Error("Expected editable panel");
  await store.previewPanel(current);
  await nextTick();
}
it("suggests a visualization from a delivered preview frame", async () => {
  const store = await mount(panel);
  button("ClickHouse SQL").click(); await nextTick();
  await deliverFrame(store);
  expect(store.editDraft?.panels[0].viz).toMatchObject({ type: "chart", x: "day", y: ["n"] });
});
it("assigns missing roles from a delivered frame and ignores a stale preview after query edits", async () => {
  const store = await mount({ ...panel, query: { mode: "result", language: "clickhouse-sql", text: "SELECT old" }, viz: { type: "chart" } });
  await deliverFrame(store);
  expect(store.editDraft?.panels[0].viz).toMatchObject({ type: "chart", x: "day", y: ["n"] });
  store.updateDraftPanel("p", { query: { text: "SELECT new" } }); await nextTick();
  button("Stat").click(); await nextTick();
  expect(store.editDraft?.panels[0].viz).not.toHaveProperty("value");
});
