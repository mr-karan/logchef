import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

// Mock the dashboards API module so the store's CRUD + panel-execution paths
// resolve deterministically without touching the network. The store imports
// `dashboardsApi` and `dashboardPanelApi` from here; types are erased at compile.
const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  histogram: vi.fn(),
  logsQuery: vi.fn(),
  logchefqlQuery: vi.fn(),
  translate: vi.fn(),
}));

vi.mock("@/api/dashboards", () => ({
  dashboardsApi: {
    list: mocks.list,
    get: mocks.get,
    create: mocks.create,
    update: mocks.update,
    remove: mocks.remove,
  },
  dashboardPanelApi: {
    histogram: mocks.histogram,
    logsQuery: mocks.logsQuery,
    logchefqlQuery: mocks.logchefqlQuery,
    translate: mocks.translate,
  },
}));

import { useDashboardsStore } from "../dashboards";
import type { Dashboard, DashboardPanel, DashboardPanels } from "@/api/dashboards";

function emptyPanels(): DashboardPanels {
  return { version: 1, layout: [], panels: [] };
}

function makeDashboard(overrides: Partial<Dashboard> = {}): Dashboard {
  return {
    id: 3,
    name: "HTTP errors",
    description: "desc",
    panels: emptyPanels(),
    created_at: "2026-07-08T00:00:00Z",
    updated_at: "2026-07-08T00:00:00Z",
    can_edit: true,
    ...overrides,
  };
}

function tablePanel(id = "p1"): DashboardPanel {
  return {
    id,
    title: "Recent logs",
    type: "table",
    team_id: 1,
    source_id: 1,
    query: "",
    query_language: "logchefql",
    options: { limit: 50 },
  };
}

async function loadEditableDashboard(dashboard = makeDashboard()) {
  mocks.get.mockResolvedValue({ status: "success", data: dashboard });
  const store = useDashboardsStore();
  await store.loadDashboard(dashboard.id);
  return store;
}

describe("dashboards store — edit mode / dirty guard", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    // Panel execution (fired on add) resolves to an empty result set.
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [] } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
  });

  it("is not editing and not dirty after a fresh load", async () => {
    const store = await loadEditableDashboard();
    expect(store.canEdit).toBe(true);
    expect(store.isEditing).toBe(false);
    expect(store.isDirty).toBe(false);
  });

  it("enterEdit snapshots the loaded blob and starts clean", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    expect(store.isEditing).toBe(true);
    expect(store.isDirty).toBe(false);
    expect(store.editDraft).not.toBeNull();
  });

  it("refuses to enter edit mode when can_edit is false", async () => {
    const store = await loadEditableDashboard(makeDashboard({ can_edit: false }));
    store.enterEdit();
    expect(store.isEditing).toBe(false);
    expect(store.editDraft).toBeNull();
  });

  it("adding a panel marks the draft dirty and packs it into the layout", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);
    expect(store.editDraft?.panels).toHaveLength(1);
    // reflow gave the new panel a layout entry at default size in the first slot.
    expect(store.editDraft?.layout).toEqual([{ id: "p1", x: 0, y: 0, w: 6, h: 2 }]);
  });

  it("cancelEdit discards the draft and reverts to the last-loaded state", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);

    store.cancelEdit();
    expect(store.isEditing).toBe(false);
    expect(store.editDraft).toBeNull();
    expect(store.isDirty).toBe(false);
    // The live dashboard was never mutated.
    expect(store.current?.panels.panels).toHaveLength(0);
  });

  it("removing a panel returns the draft to a clean state matching the snapshot", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);
    store.removeDraftPanel("p1");
    // Back to an empty layout+panels == the snapshot, so no longer dirty.
    expect(store.isDirty).toBe(false);
  });

  it("saveEdit PUTs the draft, adopts the response, and exits edit mode", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel(tablePanel());

    const saved = makeDashboard({
      panels: { version: 1, layout: [{ id: "p1", x: 0, y: 0, w: 6, h: 2 }], panels: [tablePanel()] },
      updated_at: "2026-07-08T01:00:00Z",
    });
    mocks.update.mockResolvedValue({ status: "success", data: saved });

    const result = await store.saveEdit();

    expect(result.success).toBe(true);
    expect(mocks.update).toHaveBeenCalledTimes(1);
    const [id, body] = mocks.update.mock.calls[0];
    expect(id).toBe(3);
    expect(body.name).toBe("HTTP errors");
    expect(body.panels.panels).toHaveLength(1);
    expect(store.isEditing).toBe(false);
    expect(store.editDraft).toBeNull();
    expect(store.current?.panels.panels).toHaveLength(1);
    expect(store.current?.updated_at).toBe("2026-07-08T01:00:00Z");
  });

  it("saveEdit surfaces a friendly client-side validation error before the round-trip", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    // A panel missing its source mirrors a server rule; save should not PUT.
    store.upsertDraftPanel({ ...tablePanel(), source_id: 0 });

    const result = await store.saveEdit();

    expect(result.success).toBe(false);
    expect(result.error?.message).toMatch(/source/i);
    expect(mocks.update).not.toHaveBeenCalled();
    // Still editing — the user can fix it.
    expect(store.isEditing).toBe(true);
  });
});

describe("dashboards store — panel time params (issue #80: logchefql timezone shift)", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [] } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
  });

  const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
  const SQL_DATETIME = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/;

  it("sends a UTC SQL-datetime string paired with timezone: UTC to logchefql/query", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel(tablePanel());

    expect(mocks.logchefqlQuery).toHaveBeenCalledTimes(1);
    // dashboardPanelApi.logchefqlQuery(teamId, sourceId, body, signal)
    const [, , body] = mocks.logchefqlQuery.mock.calls[0];
    // The server's ClickHouse compiler only accepts "YYYY-MM-DD HH:MM:SS" here
    // (it rejects RFC3339 with a 400). Since that string carries no offset,
    // it must always be paired with timezone: "UTC" — sending the viewer's
    // real IANA zone here (as the histogram/logs-query paths correctly do)
    // would shift the query window by that zone's offset. This was the bug.
    expect(body.start_time).toMatch(SQL_DATETIME);
    expect(body.end_time).toMatch(SQL_DATETIME);
    expect(body.timezone).toBe("UTC");
  });

  it("sends a UTC SQL-datetime string paired with timezone: UTC to logchefql/translate", async () => {
    mocks.translate.mockResolvedValue({
      status: "success",
      data: { valid: true, sql: "", full_sql: "SELECT 1", conditions: [], fields_used: [] },
    });
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel({
      id: "p2",
      title: "Rate",
      type: "timeseries",
      team_id: 1,
      source_id: 1,
      query: "",
      query_language: "logchefql",
      options: {},
    });

    expect(mocks.translate).toHaveBeenCalledTimes(1);
    // dashboardPanelApi.translate(teamId, sourceId, body, signal)
    const [, , body] = mocks.translate.mock.calls[0];
    expect(body.start_time).toMatch(SQL_DATETIME);
    expect(body.end_time).toMatch(SQL_DATETIME);
    expect(body.timezone).toBe("UTC");
  });

  it("still sends RFC3339 to logs/histogram and logs/query (unaffected by this fix)", async () => {
    const store = await loadEditableDashboard();
    store.enterEdit();
    store.upsertDraftPanel({ ...tablePanel("p3"), query_language: "clickhouse-sql" });

    expect(mocks.logsQuery).toHaveBeenCalledTimes(1);
    const [, , sqlBody] = mocks.logsQuery.mock.calls[0];
    expect(sqlBody.start_time).toMatch(RFC3339);
    expect(sqlBody.end_time).toMatch(RFC3339);

    store.upsertDraftPanel({
      id: "p4",
      title: "Rate",
      type: "timeseries",
      team_id: 1,
      source_id: 1,
      query: "SELECT 1",
      query_language: "clickhouse-sql",
      options: {},
    });
    // Timeseries panels resolve their native query via an async helper before
    // calling the histogram endpoint, so the call lands a microtask later.
    await vi.waitFor(() => expect(mocks.histogram).toHaveBeenCalledTimes(1));
    const [, , histBody] = mocks.histogram.mock.calls[0];
    expect(histBody.start_time).toMatch(RFC3339);
    expect(histBody.end_time).toMatch(RFC3339);
  });
});
