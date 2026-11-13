import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { i18n } from "@/i18n";
import { dashboardsApi, type DashboardQueryPanel } from "@/api/dashboards";
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
  it("disables SQL for count charts and explains why", async () => {
    await mount({ ...panel, viz: { type: "chart" } });
    expect(button("ClickHouse SQL").disabled).toBe(true);
    expect(button("ClickHouse SQL").title).toContain("result");
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
    expect(button("Time series").disabled).toBe(true);
    expect(button("Breakdown").disabled).toBe(true);
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
