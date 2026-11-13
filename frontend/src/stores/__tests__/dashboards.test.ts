import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

// Mock the dashboards API module so the store's CRUD + panel-execution paths
// resolve deterministically without touching the network. The store imports
// `dashboardsApi` and `dashboardPanelApi` from here; types are erased at compile.
// The browser zone is not the preference zone, so a browser-zone read shows up.
vi.hoisted(() => {
  process.env.TZ = "Asia/Kolkata";
});

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
import { epochMs } from "@/utils/timeWindow";
import { usePreferencesStore } from "@/stores/preferences";

// Tear each test's store down as the dashboard view does on unmount, so a
// pending refresh cannot fire into the next test's mocks.
afterEach(() => {
  useDashboardsStore().clearCurrent();
});

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
    revision: 1,
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
    await store.enterEdit();
    expect(store.isEditing).toBe(true);
    expect(store.isDirty).toBe(false);
    expect(store.editDraft).not.toBeNull();
  });

  it("refuses to enter edit mode when can_edit is false", async () => {
    const store = await loadEditableDashboard(makeDashboard({ can_edit: false }));
    await store.enterEdit();
    expect(store.isEditing).toBe(false);
    expect(store.editDraft).toBeNull();
  });

  it("adding a panel marks the draft dirty and packs it into the layout", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);
    expect(store.editDraft?.panels).toHaveLength(1);
    // reflow gave the new panel a layout entry at default size in the first slot.
    expect(store.editDraft?.layout).toEqual([{ id: "p1", x: 0, y: 0, w: 6, h: 2 }]);
  });

  it("cancelEdit discards the draft and reverts to the last-loaded state", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);

    await store.cancelEdit();
    expect(store.isEditing).toBe(false);
    expect(store.editDraft).toBeNull();
    expect(store.isDirty).toBe(false);
    // The live dashboard was never mutated.
    expect(store.current?.panels.panels).toHaveLength(0);
  });

  it("removing a panel returns the draft to a clean state matching the snapshot", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);
    store.removeDraftPanel("p1");
    // Back to an empty layout+panels == the snapshot, so no longer dirty.
    expect(store.isDirty).toBe(false);
  });

  it("duplicateDraftPanel inserts a same-size copy after the original", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel("p1"));
    store.upsertDraftPanel(tablePanel("p2"));
    store.resizeDraftPanel("p1", 4, 3);

    const copyId = store.duplicateDraftPanel("p1", "Recent logs (copy)");
    expect(copyId).not.toBeNull();
    expect(store.editDraft?.panels.map((p) => p.id)).toEqual(["p1", copyId, "p2"]);
    const copy = store.editDraft?.panels[1];
    expect(copy?.title).toBe("Recent logs (copy)");
    expect(copy?.options).toEqual({ limit: 50 });
    expect(copy?.options).not.toBe(store.editDraft?.panels[0].options);
    const copyLayout = store.editDraft?.layout.find((l) => l.id === copyId);
    expect(copyLayout).toMatchObject({ w: 4, h: 3 });
  });

  it("duplicateDraftPanel reuses a finished result and runs the copy otherwise", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel("p1"));

    // The original's query is still in flight: the copy must run its own.
    expect(store.getPanelState("p1").status).toBe("loading");
    const pending = store.duplicateDraftPanel("p1", "pending copy") ?? "";
    expect(store.getPanelState(pending).status).toBe("loading");
    await vi.waitFor(() => expect(store.getPanelState(pending).status).toBe("empty"));

    // Once finished, a new copy shows the same result without a new request.
    await vi.waitFor(() => expect(store.getPanelState("p1").status).toBe("empty"));
    const calls = mocks.logchefqlQuery.mock.calls.length;
    const reused = store.duplicateDraftPanel("p1", "reused copy") ?? "";
    expect(store.getPanelState(reused)).toBe(store.getPanelState("p1"));
    expect(mocks.logchefqlQuery.mock.calls.length).toBe(calls);
  });

  it("a refresh in edit mode runs the draft panels, including a duplicate", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel("p1"));
    const copyId = store.duplicateDraftPanel("p1", "copy") ?? "";
    await store.refreshAllPanels();
    expect(store.getPanelState(copyId).status).toBe("empty");
    expect(store.getPanelState("p1").status).toBe("empty");
  });

  it("opens a dashboard in its saved default range, and keeps the viewer's range otherwise", async () => {
    const store = useDashboardsStore();
    store.setWindow({ kind: "relative", spec: "30m" });
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard() });
    await store.loadDashboard(3);
    expect(store.window).toEqual({ kind: "relative", spec: "30m" });

    store.setWindow({ kind: "absolute", start: epochMs(1_000), end: epochMs(2_000) });
    const withDefault = makeDashboard({ id: 4, panels: { ...emptyPanels(), default_time_range: "6h" } });
    mocks.get.mockResolvedValue({ status: "success", data: withDefault });
    await store.loadDashboard(4);
    expect(store.window).toEqual({ kind: "relative", spec: "6h" });
  });

  it("setDraftDefaultTimeRange saves the default, applies it, and can clear it", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.setDraftDefaultTimeRange("7d");
    expect(store.editDraft?.default_time_range).toBe("7d");
    expect(store.window).toEqual({ kind: "relative", spec: "7d" });
    // A later layout change must not drop it.
    store.upsertDraftPanel(tablePanel("p1"));
    expect(store.editDraft?.default_time_range).toBe("7d");

    store.setDraftDefaultTimeRange(null);
    expect(store.editDraft).not.toHaveProperty("default_time_range");
    expect(store.isDirty).toBe(true);
  });

  it("duplicateDraftPanel refuses to go past the panel limit", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    for (let i = 0; i < 24; i++) store.upsertDraftPanel(tablePanel(`p${i}`));
    expect(store.duplicateDraftPanel("p0", "over")).toBeNull();
    expect(store.editDraft?.panels).toHaveLength(24);
  });

  it("saveEdit PUTs the draft, adopts the response, and exits edit mode", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
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
    await store.enterEdit();
    // A panel missing its source mirrors a server rule; save should not PUT.
    store.upsertDraftPanel({ ...tablePanel(), source_id: 0 });

    const result = await store.saveEdit();

    expect(result.success).toBe(false);
    expect(result.error?.message).toMatch(/source/i);
    expect(mocks.update).not.toHaveBeenCalled();
    // Still editing — the user can fix it.
    expect(store.isEditing).toBe(true);
  });

  it("renaming marks the draft dirty and saveEdit sends the trimmed name and description (#127)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    expect(store.editName).toBe("HTTP errors");
    expect(store.editDescription).toBe("desc");

    store.setDraftName("  API errors ");
    store.setDraftDescription(" 5xx by route ");
    expect(store.isDirty).toBe(true);

    mocks.update.mockResolvedValue({
      status: "success",
      data: makeDashboard({ name: "API errors", description: "5xx by route" }),
    });
    const result = await store.saveEdit();

    expect(result.success).toBe(true);
    const [, body] = mocks.update.mock.calls[0];
    expect(body.name).toBe("API errors");
    expect(body.description).toBe("5xx by route");
    expect(store.current?.name).toBe("API errors");
  });

  it("restoring the original name clears the dirty flag (#127)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.setDraftName("Other");
    expect(store.isDirty).toBe(true);
    store.setDraftName("HTTP errors ");
    expect(store.isDirty).toBe(false);
  });

  it("saveEdit rejects a blank name without a round-trip (#127)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.setDraftName("   ");

    const result = await store.saveEdit();

    expect(result.success).toBe(false);
    expect(result.error?.message).toMatch(/name/i);
    expect(mocks.update).not.toHaveBeenCalled();
    expect(store.isEditing).toBe(true);
  });
});

describe("dashboards store — panel time params", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [] } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    mocks.translate.mockResolvedValue({
      status: "success",
      data: { valid: true, sql: "", full_sql: "SELECT 1", conditions: [], fields_used: [] },
    });
  });

  // 250 ms and 999 ms catch a bound truncated to whole seconds.
  const window = {
    kind: "absolute",
    start: epochMs(Date.parse("2026-10-19T04:00:00.250Z")),
    end: epochMs(Date.parse("2026-10-19T05:00:00.999Z")),
  } as const;

  async function runEveryPanelType() {
    const store = await loadEditableDashboard();
    usePreferencesStore().preferences.timezone = "utc";
    store.setWindow(window);
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel("logchefql-table"));
    store.upsertDraftPanel({ ...tablePanel("sql-table"), query_language: "clickhouse-sql" });
    store.upsertDraftPanel({ ...tablePanel("logchefql-series"), type: "timeseries", options: {} });
    await vi.waitFor(() => expect(store.getPanelState("logchefql-series").status).toBe("empty"));
    return store;
  }

  it("sends every panel request the same exact bounds in the preference zone", async () => {
    await runEveryPanelType();

    // A debounced refresh after setWindow may run the draft panels a second time.
    const bodies = [mocks.logchefqlQuery, mocks.logsQuery, mocks.translate, mocks.histogram].flatMap((mock) => {
      expect(mock).toHaveBeenCalled();
      return mock.mock.calls.map((call) => call[2]);
    });
    for (const body of bodies) {
      expect(body).toMatchObject({ start_time: "2026-10-19T04:00:00.250Z", end_time: "2026-10-19T05:00:00.999Z", timezone: "UTC" });
    }
  });

  it("labels a timeseries panel in the zone its query ran in", async () => {
    const store = await runEveryPanelType();
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [{ bucket: "2026-10-19T04:00:00Z", log_count: 1 }], granularity: "1m" } });
    store.upsertDraftPanel({ ...tablePanel("filled-series"), type: "timeseries", options: {} });
    await vi.waitFor(() => expect(store.getPanelState("filled-series").status).toBe("success"));

    expect(store.getPanelState("filled-series").timeseries?.range).toEqual({ start: window.start, end: window.end, zone: "UTC" });
  });

  // #137 — the title link opens the window a panel's shown result ran over.
  it("records the window each panel result ran over", async () => {
    const store = await runEveryPanelType();

    expect(store.getPanelState("logchefql-table").range).toEqual({ start: window.start, end: window.end, zone: "UTC" });
    expect(store.getPanelState("logchefql-series").range).toEqual({ start: window.start, end: window.end, zone: "UTC" });
  });
});

describe("dashboards store — #119/#120 hardening", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [] } });
    mocks.translate.mockResolvedValue({
      status: "success",
      data: { valid: true, sql: "", full_sql: "SELECT 1", generated_query_language: "clickhouse-sql", conditions: [], fields_used: [] },
    });
  });

  // --- A2: stale-load race -------------------------------------------------
  it("discards a stale loadDashboard GET when a newer load has started (#119 A2)", async () => {
    const store = useDashboardsStore();
    let resolveFirst!: (v: any) => void;
    mocks.get.mockImplementationOnce(() => new Promise((r) => { resolveFirst = r; }));
    const first = store.loadDashboard(1);
    // A newer load for #2 resolves immediately and wins.
    mocks.get.mockResolvedValueOnce({ status: "success", data: makeDashboard({ id: 2, name: "Second" }) });
    await store.loadDashboard(2);
    expect(store.current?.id).toBe(2);
    // The slow #1 GET lands late — it must NOT clobber the current dashboard.
    resolveFirst({ status: "success", data: makeDashboard({ id: 1, name: "First" }) });
    await first;
    expect(store.current?.id).toBe(2);
    expect(store.current?.name).toBe("Second");
  });

  it("discards a loadDashboard GET that resolves after clearCurrent (#119 A2)", async () => {
    const store = useDashboardsStore();
    let resolveGet!: (v: any) => void;
    mocks.get.mockImplementationOnce(() => new Promise((r) => { resolveGet = r; }));
    const pending = store.loadDashboard(1);
    // View torn down / navigated away before the GET returns.
    store.clearCurrent();
    resolveGet({ status: "success", data: makeDashboard({ id: 1 }) });
    await pending;
    expect(store.current).toBeNull();
  });

  // --- A1: cross-dashboard save corruption --------------------------------
  it("saveEdit refuses to write a draft onto a different dashboard id (#119 A1)", async () => {
    const store = await loadEditableDashboard(makeDashboard({ id: 1 }));
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    expect(store.isDirty).toBe(true);
    // Simulate a param-nav that did NOT clear the draft: current switches to #2
    // while the draft still belongs to #1.
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ id: 2 }) });
    await store.loadDashboard(2);
    const result = await store.saveEdit();
    expect(result.success).toBe(false);
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it("sends the revision captured at edit start despite a later read, then adopts the saved revision", async () => {
    const store = await loadEditableDashboard(makeDashboard({ revision: 7 }));
    await store.enterEdit();
    store.setDraftName("My edit");
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 8 }) });
    await store.loadDashboard(3);
    mocks.update.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 9 }) });
    await store.saveEdit();
    expect(mocks.update.mock.calls[0][1].revision).toBe(7);
    expect(mocks.update.mock.calls[0][1]).not.toHaveProperty("updated_at");
    expect(store.current?.revision).toBe(9);
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 9 }) });
    await store.enterEdit();
    await store.saveEdit();
    expect(mocks.update.mock.calls[1][1].revision).toBe(9);
  });

  it("starts an edit from the latest saved version, not the one on screen", async () => {
    const store = await loadEditableDashboard(makeDashboard({ revision: 1, name: "Old name" }));
    mocks.get.mockResolvedValue({
      status: "success",
      data: makeDashboard({ revision: 2, name: "New name", panels: { version: 1, layout: [], panels: [tablePanel()] } }),
    });
    await store.enterEdit();
    expect(store.current?.revision).toBe(2);
    expect(store.editName).toBe("New name");
    expect(store.editDraft?.panels.map((p) => p.id)).toEqual(["p1"]);
    expect(store.isDirty).toBe(false);
    store.setDraftName("Mine");
    mocks.update.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 3, name: "Mine" }) });
    await store.saveEdit();
    expect(mocks.update.mock.calls[0][1].revision).toBe(2);
  });

  it("applies the newer revision's default time range before refreshing edit panels", async () => {
    const store = await loadEditableDashboard();
    store.setWindow({ kind: "relative", spec: "30m" });
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({
      revision: 2, panels: { ...emptyPanels(), default_time_range: "6h" },
    }) });
    await store.enterEdit();
    expect(store.window).toEqual({ kind: "relative", spec: "6h" });
    expect(Number(store.appliedRange?.end) - Number(store.appliedRange?.start)).toBe(6 * 60 * 60 * 1000);
  });

  it("stays out of edit mode when the fresh read fails", async () => {
    const store = await loadEditableDashboard();
    mocks.get.mockRejectedValue({ status: "error", error_type: "NotFoundError", message: "Dashboard not found" });
    await store.enterEdit();
    expect(store.isEditing).toBe(false);
    expect(store.editDraft).toBeNull();
  });

  it("after a refused save, overwrites with the latest revision or discards and reloads the latest", async () => {
    const store = await loadEditableDashboard(makeDashboard({ revision: 1 }));
    await store.enterEdit();
    store.setDraftName("Mine");
    mocks.update.mockRejectedValue({ status: "error", error_type: "ConflictError", message: "modified" });
    expect((await store.saveEdit()).error?.error_type).toBe("ConflictError");

    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 2, name: "Theirs" }) });
    const latest = await store.fetchLatest();
    expect(latest.data?.revision).toBe(2);
    expect(store.current?.revision).toBe(1);
    expect(store.editName).toBe("Mine");

    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 3, name: "Theirs again" }) });
    expect((await store.saveEdit({ revision: 2 })).success).toBe(false);
    expect(mocks.update.mock.calls[1][1]).toMatchObject({ revision: 2, name: "Mine" });
    expect(store.isEditing).toBe(true);

    await store.cancelEdit();
    expect(store.isEditing).toBe(false);
    expect(store.current).toMatchObject({ revision: 3, name: "Theirs again" });
  });

  it("does not reread the dashboard when discarding an edit that never conflicted", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.setDraftName("Mine");
    mocks.get.mockClear();
    await store.cancelEdit();
    expect(mocks.get).not.toHaveBeenCalled();
  });

  it("saves the draft as a new dashboard and leaves edit mode", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel(tablePanel());
    store.setDraftDescription("  my notes ");
    mocks.create.mockResolvedValue({ status: "success", data: makeDashboard({ id: 9, name: "HTTP errors (copy)" }) });
    const result = await store.saveDraftAsCopy("HTTP errors (copy)");
    expect(result.data?.id).toBe(9);
    const body = mocks.create.mock.calls[0][0];
    expect(body).toMatchObject({ name: "HTTP errors (copy)", description: "my notes" });
    expect(body.panels.panels.map((p: DashboardPanel) => p.id)).toEqual(["p1"]);
    expect(store.isEditing).toBe(false);
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it.each([
    ["ConflictError", "Dashboard was modified by someone else; reload and reapply your changes", /modified by someone else/],
    ["ValidationError", "This save does not say which revision of the dashboard it changes. Reload the page and apply your changes again.", /Reload the page/],
  ])("preserves edits and explains a refused save (%s)", async (error_type, message, expected) => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.setDraftName("Unsaved edit");
    mocks.update.mockRejectedValue({ status: "error", error_type, message });
    const result = await store.saveEdit();
    expect(result.success).toBe(false);
    expect(result.error?.message).toMatch(expected);
    expect(store.isEditing).toBe(true);
    expect(store.editName).toBe("Unsaved edit");
    expect(store.current?.revision).toBe(1);
    await store.saveEdit();
    expect(mocks.update.mock.calls[1][1].revision).toBe(1);
  });

  // --- A4: native LogsQL dispatch -----------------------------------------
  it("runs a native logsql table panel through logs/query, not logchefql (#119 A4)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel({ ...tablePanel("lp"), query: "level:error", query_language: "logsql" });
    await vi.waitFor(() => expect(mocks.logsQuery).toHaveBeenCalledTimes(1));
    expect(mocks.logchefqlQuery).not.toHaveBeenCalled();
    expect(mocks.translate).not.toHaveBeenCalled();
    const [, , body] = mocks.logsQuery.mock.calls[0];
    expect(body.query_text).toBe("level:error");
    expect(body.query_language).toBe("logsql");
  });

  it("runs a native logsql timeseries panel through histogram without translate (#119 A4)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel({
      id: "lt", title: "t", type: "timeseries", team_id: 1, source_id: 1,
      query: "*", query_language: "logsql", options: {},
    });
    await vi.waitFor(() => expect(mocks.histogram).toHaveBeenCalledTimes(1));
    expect(mocks.translate).not.toHaveBeenCalled();
    const [, , body] = mocks.histogram.mock.calls[0];
    expect(body.query_text).toBe("*");
    expect(body.query_language).toBe("logsql");
  });

  // --- A11: invalidate panelStates on draft type change --------------------
  it("invalidates panelStates[id] when a draft panel's type changes (#120 A11)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    // clickhouse-sql skips the translate step so the stat result is deterministic.
    store.upsertDraftPanel({
      id: "p1", title: "count", type: "stat", team_id: 1, source_id: 1,
      query: "SELECT 1", query_language: "clickhouse-sql", options: {},
    });
    await vi.waitFor(() => expect(store.getPanelState("p1").status).toBe("success"));
    expect(store.getPanelState("p1").stat).toBeDefined();
    // stat -> table: the cached stat value would render blank under a table, so
    // it must be dropped immediately (the panel re-executes into "loading").
    store.updateDraftPanel("p1", { type: "table" });
    expect(store.getPanelState("p1").stat).toBeUndefined();
    expect(store.getPanelState("p1").status).not.toBe("success");
  });

  // --- A13: table limit clamp ---------------------------------------------
  it("clamps an oversized table limit down to the max (#120 A13)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel({
      ...tablePanel("big"), query: "SELECT 1", query_language: "clickhouse-sql", options: { limit: 100000 },
    });
    await vi.waitFor(() => expect(mocks.logsQuery).toHaveBeenCalledTimes(1));
    const [, , body] = mocks.logsQuery.mock.calls[0];
    expect(body.limit).toBe(1000);
  });
});

describe("dashboards store — B1 per-panel redaction (locked panels)", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [] } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    mocks.translate.mockResolvedValue({ status: "success", data: { valid: true, full_sql: "SELECT 1" } });
  });

  it("renders a server-locked panel as locked and fires no data fetch, while an accessible panel loads", async () => {
    const accessible: DashboardPanel = { ...tablePanel("open"), query: "SELECT 1", query_language: "clickhouse-sql" };
    // A redacted panel: server blanked the query and set locked.
    const locked: DashboardPanel = {
      id: "shut",
      title: "Secret",
      type: "table",
      team_id: 2,
      source_id: 2,
      query: "",
      query_language: "logchefql",
      locked: true,
    };
    const dashboard = makeDashboard({
      can_edit: false,
      panels: {
        version: 1,
        layout: [
          { id: "open", x: 0, y: 0, w: 6, h: 2 },
          { id: "shut", x: 6, y: 0, w: 6, h: 2 },
        ],
        panels: [accessible, locked],
      },
    });
    mocks.get.mockResolvedValue({ status: "success", data: dashboard });

    const store = useDashboardsStore();
    await store.loadDashboard(dashboard.id);
    await vi.waitFor(() => expect(store.getPanelState("open").status).toBe("empty"));

    // The locked panel is marked locked and no request was made for it.
    expect(store.getPanelState("shut").status).toBe("locked");
    // Only the accessible (clickhouse-sql) panel hit the logs/query endpoint;
    // the locked panel triggered none of the panel data endpoints.
    expect(mocks.logsQuery).toHaveBeenCalledTimes(1);
    const [teamId, sourceId] = mocks.logsQuery.mock.calls[0];
    expect(teamId).toBe(1);
    expect(sourceId).toBe(1);
    expect(mocks.logchefqlQuery).not.toHaveBeenCalled();
    expect(mocks.translate).not.toHaveBeenCalled();
    expect(mocks.histogram).not.toHaveBeenCalled();
  });
});
