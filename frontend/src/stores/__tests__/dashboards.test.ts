import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { isReactive } from "vue";
import { setActivePinia, createPinia } from "pinia";
import { resetLocalTimeZone, setLocalTimeZone } from "@internationalized/date";

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

vi.mock("@/api/dashboards", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/dashboards")>()),
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
import { DASHBOARD_CLIENT_FEATURES } from "@/api/dashboards";
import type { Dashboard, DashboardLockedPanel, DashboardPanel, DashboardPanels, DashboardQueryPanel } from "@/api/dashboards";
import { epochMs } from "@/utils/timeWindow";
import { frameCellText, reduceStat } from "@/utils/panelFrame";
import { usePreferencesStore } from "@/stores/preferences";
import { emptyDashboardPanels, tabLayout } from "@/utils/dashboardPanels";

type ValidDashboard = Extract<Dashboard, { corrupt: false }>;

// Tear each test's store down as the dashboard view does on unmount, so a
// pending refresh cannot fire into the next test's mocks.
afterEach(() => {
  useDashboardsStore().clearCurrent();
});

function emptyPanels(): DashboardPanels {
  return emptyDashboardPanels();
}

/** A blob whose single tab holds `layout`. */
function panelsWith(panels: DashboardPanel[], layout: { id: string; x: number; y: number; w: number; h: number }[] = []): DashboardPanels {
  return { ...emptyPanels(), tabs: [{ id: "main", title: "Main", layout }], panels };
}

function makeDashboard(overrides: Partial<ValidDashboard> = {}): ValidDashboard {
  return {
    id: 3,
    name: "HTTP errors",
    description: "desc",
    corrupt: false,
    panels: emptyPanels(),
    created_at: "2026-07-08T00:00:00Z",
    updated_at: "2026-07-08T00:00:00Z",
    revision: 1,
    can_edit: true,
    ...overrides,
  };
}

function currentPanels(store: ReturnType<typeof useDashboardsStore>): DashboardPanel[] {
  const current = store.current;
  return current && !current.corrupt ? current.panels.panels : [];
}

function tablePanel(id = "p1"): DashboardQueryPanel {
  return {
    id,
    kind: "query",
    title: "Recent logs",
    team_id: 1,
    source_id: 1,
    query: { mode: "result", language: "logchefql", text: "", limit: 50 },
    viz: { type: "table" },
  };
}

function chartPanel(id: string, query: Partial<DashboardQueryPanel["query"]> = {}): DashboardQueryPanel {
  return {
    id,
    kind: "query",
    title: "Series",
    team_id: 1,
    source_id: 1,
    query: { mode: "count", language: "logchefql", text: "", interval: "auto", ...query },
    viz: { type: "chart", mark: "line", stack: "none" },
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

  it("reuses a result frame across visualization and role changes", async () => {
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [{ service: "api", n: 20 }], columns: [{ name: "service", type: "String" }, { name: "n", type: "UInt32" }] } });
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel({ ...tablePanel(), query: { mode: "result", language: "clickhouse-sql", text: "SELECT service, n" } });
    await vi.waitFor(() => expect(store.getPanelState("p1").status).toBe("success"));
    const frame = store.getPanelState("p1").frame;
    expect(frame).toBeDefined();
    expect(isReactive(frame)).toBe(false);
    expect(isReactive(frame?.rows)).toBe(false);
    for (const kind of ["chart", "breakdown", "stat", "table"] as const) store.switchDraftPanelKind("p1", kind);
    store.updateDraftPanel("p1", { viz: { type: "chart", x: "service", y: ["n"], mark: "bar", show_values: true, x_scale: "category" } });
    expect(store.getPanelState("p1").frame).toBe(frame);
    expect(mocks.logsQuery).toHaveBeenCalledTimes(1);
    expect(mocks.histogram).not.toHaveBeenCalled();
    expect(store.editDraft?.panels[0]).toMatchObject({ query: { mode: "result" }, viz: { x: "service", y: ["n"], show_values: true } });
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
    expect(tabLayout(store.editDraft!, "main")).toEqual([{ id: "p1", x: 0, y: 0, w: 6, h: 2 }]);
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
    expect(currentPanels(store)).toHaveLength(0);
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
    expect(copy?.locked).toBeFalsy();
    const copyQuery = copy && !copy.locked ? copy.query : undefined;
    expect(copyQuery).toEqual({ mode: "result", language: "logchefql", text: "", limit: 50 });
    const original = store.editDraft?.panels[0];
    expect(copyQuery).not.toBe(original && !original.locked ? original.query : undefined);
    const copyLayout = tabLayout(store.editDraft!, "main").find((l) => l.id === copyId);
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
      panels: panelsWith([tablePanel()], [{ id: "p1", x: 0, y: 0, w: 6, h: 2 }]),
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
    expect(currentPanels(store)).toHaveLength(1);
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
    store.upsertDraftPanel({ ...tablePanel("sql-table"), query: { ...tablePanel().query, language: "clickhouse-sql" } });
    store.upsertDraftPanel(chartPanel("logchefql-series"));
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
    store.upsertDraftPanel(chartPanel("filled-series"));
    await vi.waitFor(() => expect(store.getPanelState("filled-series").status).toBe("success"));

    expect(store.getPanelState("filled-series").range).toEqual({ start: window.start, end: window.end, zone: "UTC" });
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
      data: makeDashboard({ revision: 2, name: "New name", panels: panelsWith([tablePanel()]) }),
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

  it("keeps draft panel runs alive when a conflicted discard fails to reload", async () => {
    const store = await loadEditableDashboard(makeDashboard({ revision: 1 }));
    await store.enterEdit();
    mocks.update.mockRejectedValue({ status: "error", error_type: "ConflictError", message: "modified" });
    await store.saveEdit();

    let release!: (value: unknown) => void;
    const pending = new Promise((resolve) => { release = resolve; });
    mocks.logsQuery.mockReturnValue(pending);
    mocks.logchefqlQuery.mockReturnValue(pending);
    const preview = store.previewPanel(tablePanel());
    expect(store.previewState?.status).toBe("loading");

    mocks.get.mockRejectedValue({ status: "error", error_type: "NetworkError", message: "offline" });
    expect((await store.cancelEdit()).success).toBe(false);
    expect(store.isEditing).toBe(true);

    release({ status: "success", data: { data: [{ a: 1 }], columns: [{ name: "a", type: "UInt8" }], logs: [{ a: 1 }] } });
    await preview;
    expect(store.previewState?.status).not.toBe("loading");
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
    store.upsertDraftPanel({ ...tablePanel("lp"), query: { mode: "result", language: "logsql", text: "level:error", limit: 50 } });
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
    store.upsertDraftPanel(chartPanel("lt", { language: "logsql", text: "*" }));
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
      id: "p1", kind: "query", title: "count", team_id: 1, source_id: 1,
      query: { mode: "count", language: "clickhouse-sql", text: "SELECT 1", interval: "auto" },
      viz: { type: "stat", reduce: "sum" },
    });
    await vi.waitFor(() => expect(store.getPanelState("p1").status).toBe("empty"));
    expect(store.getPanelState("p1").frame).toBeDefined();
    // stat -> table: the cached stat value would render blank under a table, so
    // it must be dropped immediately (the panel re-executes into "loading").
    store.switchDraftPanelKind("p1", "table");
    expect(store.getPanelState("p1").frame).toBeUndefined();
    expect(store.getPanelState("p1").status).not.toBe("success");
  });

  // --- A13: table limit clamp ---------------------------------------------
  it("clamps an oversized table limit down to the max (#120 A13)", async () => {
    const store = await loadEditableDashboard();
    await store.enterEdit();
    store.upsertDraftPanel({
      ...tablePanel("big"), query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", limit: 10000 },
    });
    await vi.waitFor(() => expect(mocks.logsQuery).toHaveBeenCalledTimes(1));
    const [, , body] = mocks.logsQuery.mock.calls[0];
    expect(body.limit).toBe(10000);
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
    const accessible: DashboardQueryPanel = { ...tablePanel("open"), query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", limit: 50 } };
    // A redacted panel: the server dropped the query and kept the viz type.
    const locked: DashboardLockedPanel = {
      id: "shut",
      kind: "query",
      title: "Secret",
      team_id: 2,
      source_id: 2,
      viz: { type: "table" },
      locked: true,
    };
    const dashboard = makeDashboard({
      can_edit: false,
      panels: panelsWith([accessible, locked], [
        { id: "open", x: 0, y: 0, w: 6, h: 2 },
        { id: "shut", x: 6, y: 0, w: 6, h: 2 },
      ]),
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

describe("dashboards store — schema 2 request parity", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [] } });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
    mocks.translate.mockResolvedValue({
      status: "success",
      data: { valid: true, sql: "", full_sql: "SELECT 1", generated_query_language: "clickhouse-sql", conditions: [], fields_used: [] },
    });
  });

  async function runPanels(...panels: DashboardQueryPanel[]) {
    const layout = panels.map((p, i) => ({ id: p.id, x: 0, y: i, w: 12, h: 1 }));
    const store = await loadEditableDashboard(makeDashboard({ panels: panelsWith(panels, layout) }));
    for (const p of panels) {
      await vi.waitFor(() => expect(["empty", "success"]).toContain(store.getPanelState(p.id).status));
    }
    return store;
  }

  it("ignores VictoriaLogs type guesses for a LogchefQL result before source metadata is loaded", async () => {
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [{ n: "00123" }], columns: [{ name: "n", type: "UInt64" }], generated_query_language: "logsql" } });
    const store = await runPanels(tablePanel("vl-result"));
    const frame = store.getPanelState("vl-result").frame;
    expect(frame?.columns[0]).toMatchObject({ backendType: null, kind: "category" });
    expect(frame?.rows[0].n).toBe("00123");
  });

  it("runs result SQL as written and reuses the frame for role, reducer, kind, format and visualization edits", async () => {
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [{ n: "9007199254740993" }], columns: [{ name: "n", type: "UInt64" }] } });
    const panel: DashboardQueryPanel = { ...tablePanel("computed"), query: { mode: "result", language: "clickhouse-sql", text: "SELECT sum(amount) AS n", limit: 1000 }, viz: { type: "stat", value: "n" } };
    const store = await runPanels(panel);
    expect(mocks.histogram).not.toHaveBeenCalled();
    expect(mocks.logsQuery.mock.calls[0][2].query_text).toBe("SELECT sum(amount) AS n");
    const frame = store.getPanelState("computed").frame;
    expect(frame?.rows[0].n).toEqual({ kind: "exact", coefficient: 9007199254740993n, scale: 0 });
    await store.enterEdit();
    store.updateDraftPanel("computed", { viz: { type: "stat", value: "n", reduce: "sum", columns: { n: { kind: "number", confidence: "confirmed" } }, format: { scale: 1e-7, decimals: 2, suffix: " Cr" } } });
    store.updateDraftPanel("computed", { viz: { type: "table", visible_columns: ["n"] } });
    expect(mocks.logsQuery).toHaveBeenCalledTimes(1);
    expect(store.getPanelState("computed").frame).toEqual(frame);
  });

  it("sends an upgraded stat's translate limit to translate only, on both backends", async () => {
    const stat: DashboardQueryPanel = {
      ...chartPanel("stat", { translate_limit: 50000 }),
      viz: { type: "stat", reduce: "sum" },
    };
    await runPanels(stat);
    expect(mocks.translate).toHaveBeenCalledTimes(1);
    expect(mocks.translate.mock.calls[0][2].limit).toBe(50000);
    expect(mocks.histogram.mock.calls[0][2]).not.toHaveProperty("limit");

    vi.clearAllMocks();
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "1m" } });
    mocks.translate.mockResolvedValue({
      status: "success",
      data: { valid: true, generated_query: "level:error", generated_query_language: "logsql", conditions: [], fields_used: [] },
    });
    await runPanels(stat);
    expect(mocks.translate.mock.calls[0][2].limit).toBe(50000);
    expect(mocks.histogram.mock.calls[0][2]).toMatchObject({ query_text: "level:error", query_language: "logsql" });
  });

  it("sends limit 100 for a table whose blob sets none, through both table paths", async () => {
    const logchefql: DashboardQueryPanel = { ...tablePanel("lq"), query: { mode: "result", language: "logchefql", text: "" } };
    const sql: DashboardQueryPanel = { ...tablePanel("sql"), query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1" } };
    await runPanels(logchefql, sql);
    expect(mocks.logchefqlQuery.mock.calls[0][2].limit).toBe(1000);
    expect(mocks.logsQuery.mock.calls[0][2].limit).toBe(1000);
  });

  it("keeps a table result's limit source and truncation state, through both table paths", async () => {
    const stats = { execution_time_ms: 1, rows_read: 2, rows_returned: 1, limit_applied: 1, limit_source: "request", truncation: "rows" };
    mocks.logchefqlQuery.mockResolvedValue({ status: "success", data: { logs: [{ n: 1 }], columns: [], stats } });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [{ n: 1 }], columns: [], stats } });
    const logchefql: DashboardQueryPanel = { ...tablePanel("lq"), query: { mode: "result", language: "logchefql", text: "" } };
    const sql: DashboardQueryPanel = { ...tablePanel("sql"), query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1" } };
    const store = await runPanels(logchefql, sql);
    expect(store.getPanelState("lq").stats).toMatchObject({ limit_source: "request", truncation: "rows" });
    expect(store.getPanelState("sql").stats).toMatchObject({ limit_source: "request", truncation: "rows" });
  });

  it("keeps the truncation state of a table result that a byte cut left empty", async () => {
    const stats = { execution_time_ms: 1, rows_read: 1, rows_returned: 0, limit_applied: 100, limit_source: "request", truncation: "bytes" };
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { logs: [], columns: [], stats } });
    const logsql: DashboardQueryPanel = { ...tablePanel("vl"), query: { mode: "result", language: "logsql", text: "*" } };
    const store = await runPanels(logsql);
    expect(store.getPanelState("vl")).toMatchObject({ status: "empty", stats: { truncation: "bytes" } });
  });

  it("trims U+0085 around a group when building the request and keeps U+FEFF", async () => {
    await runPanels(chartPanel("nel", { group_by: "\u0085service\u0085" }), chartPanel("bom", { group_by: "\ufeff" }));
    const groups = mocks.histogram.mock.calls.map((call) => call[2].group_by);
    expect(groups).toContain("service");
    expect(groups).toContain("\ufeff");
  });

  it("a stat retains query grouping independently of its visualization", async () => {
    await runPanels({ ...chartPanel("s", { group_by: "host" }), viz: { type: "stat", reduce: "sum" } });
    expect(mocks.histogram.mock.calls[0][2].group_by).toBe("host");
  });

  it("saving an unchanged dashboard sends the loaded blob back as it was", async () => {
    // A fixture that uses every feature this build declares and
    // every base field, so a dropped field would show up here.
    const fixture: DashboardPanels = {
      version: 2,
      features: [...DASHBOARD_CLIENT_FEATURES],
      cache_ttl_seconds: 300,
      default_time_range: "today-6d",
      // Two tabs, a free width (5, 7) and a free height (9): tabs and free_grid.
      tabs: [
        { id: "today", title: "Current day", layout: [
          { id: "c", x: 0, y: 0, w: 5, h: 2 }, { id: "s", x: 5, y: 0, w: 7, h: 2 }, { id: "p", x: 0, y: 2, w: 3, h: 9 },
        ] },
        { id: "week", title: "Last 7 days", layout: [{ id: "t", x: 0, y: 0, w: 12, h: 3 }] },
      ],
      panels: [
        { id: "c", kind: "query", title: "chart", team_id: 1, source_id: 1, query: { mode: "count", language: "logchefql", text: "level=error", group_by: " service ", interval: "24h", translate_limit: 50, range: "today-6d" }, viz: { type: "chart", mark: "bar", stack: "none", legend: false } },
        { id: "s", kind: "query", title: "stat", team_id: 1, source_id: 1, query: { mode: "result", language: "logsql", text: "* | stats count() as n" }, viz: { type: "stat", value: "n", order_by: "at", columns: { n: { kind: "number", confidence: "confirmed" }, at: { kind: "time", confidence: "confirmed" } }, reduce: "last", compare: { mode: "previous_row", good: "up" }, sparkline: true, thresholds: [{ value: null, color: "green" }, { value: "100", color: "red" }], color_mode: "background", format: { decimals: 2 } } },
        { id: "p", kind: "query", title: "pie", team_id: 1, source_id: 1, query: { mode: "count", language: "logchefql", text: "", group_by: "host", interval: "auto" }, viz: { type: "pie", donut: true } },
        { id: "t", kind: "query", title: "table", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1 FROM logs WHERE {{__time_filter}}", limit: 100, interval: "1h", range: "7d" }, viz: { type: "table", visible_columns: ["message", "host"], rules: [{ columns: ["message"], op: "contains", value: "retry", scope: "row", color: "red" }], column_formats: { host: { kind: "number", decimals: 2 } }, columns: { host: { kind: "category", confidence: "confirmed" } } } },
      ],
    };
    const store = await loadEditableDashboard(makeDashboard({ panels: JSON.parse(JSON.stringify(fixture)) }));
    await store.enterEdit();
    expect(store.isDirty).toBe(false);
    mocks.update.mockResolvedValue({ status: "success", data: makeDashboard({ revision: 2, panels: fixture }) });
    const result = await store.saveEdit();
    expect(result.success).toBe(true);
    expect(mocks.update.mock.calls[0][1].panels).toEqual(fixture);
    const saved: DashboardPanels = mocks.update.mock.calls[0][1].panels;
    expect(saved.panels.filter(panel => panel.kind === "query").find(panel => panel.id === "s")?.viz).toMatchObject({ thresholds: [{ value: null, color: "green" }, { value: "100", color: "red" }], color_mode: "background" });
    expect(saved.panels.filter(panel => panel.kind === "query").find(panel => panel.id === "t")?.viz).toMatchObject({ rules: [{ columns: ["message"], op: "contains", value: "retry", scope: "row", color: "red" }], column_formats: { host: { kind: "number", decimals: 2 } } });
  });

  it("refuses to enter edit mode on a corrupt dashboard and renders nothing for it", async () => {
    const corrupt: Dashboard = { ...makeDashboard(), corrupt: true, raw_panels_json: "{nope" } as unknown as Dashboard;
    mocks.get.mockResolvedValue({ status: "success", data: corrupt });
    const store = useDashboardsStore();
    await store.loadDashboard(3);
    expect(store.canEdit).toBe(false);
    await store.enterEdit();
    expect(store.isEditing).toBe(false);
    expect(mocks.histogram).not.toHaveBeenCalled();
    expect(mocks.logchefqlQuery).not.toHaveBeenCalled();
  });
});

describe("dashboard read compatibility", () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks(); });
  it("blocks a newer blob discovered by the edit-time read", async () => {
    mocks.get.mockResolvedValueOnce({ status: "success", data: makeDashboard({ panels: panelsWith([tablePanel()]) }) });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [] } });
    const store = useDashboardsStore();
    await store.loadDashboard(3);
    mocks.get.mockResolvedValueOnce({ status: "success", data: makeDashboard({ revision: 2, panels: { ...panelsWith([tablePanel()]), features: ["future_feature"] } }) });
    await store.enterEdit();
    expect(store.canEdit).toBe(false);
    expect(store.isEditing).toBe(false);
    expect(store.getPanelState("p1").status).toBe("unsupported");
  });
  it.each([false, true])("blocks edit and execution for a newer response, stub=%s", async (stub) => {
    const dashboard = makeDashboard({ panels: { ...panelsWith([tablePanel()]), features: ["future_feature"] }, ...(stub ? { panels_corrupt: true, min_schema: 2 } : {}) });
    if (stub) dashboard.panels.version = 1;
    mocks.get.mockResolvedValue({ status: "success", data: dashboard });
    const store = useDashboardsStore();
    await store.loadDashboard(3);
    expect(store.canEdit).toBe(false);
    expect(store.needsReload).toBe(true);
    expect(store.getPanelState("p1").status).toBe("unsupported");
    await store.enterEdit();
    expect(store.isEditing).toBe(false);
    expect(mocks.get).toHaveBeenCalledTimes(1);
    expect(mocks.histogram).not.toHaveBeenCalled();
    expect(mocks.logsQuery).not.toHaveBeenCalled();
  });
});


describe("count preview", () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks(); });
  it("carries the executed range into a chart preview", async () => {
    mocks.translate.mockResolvedValue({ status: "success", data: { sql: "SELECT 1" } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [{ bucket: "2026-11-12T00:01:00Z", log_count: 4 }], granularity: "1m" } });
    const store = useDashboardsStore();
    await store.previewPanel({ ...tablePanel(), query: { mode: "count", language: "logsql", text: "*", interval: "auto" }, viz: { type: "chart" } });
    expect(store.previewState?.status).toBe("success");
    expect(store.previewState?.range).toBeDefined();
    expect(new Date(store.previewState!.range!.start).toISOString()).toBe(mocks.histogram.mock.calls[0][2].start_time);
  });
});


describe("VictoriaLogs fixed-offset count grids", () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks(); });
  afterEach(() => { resetLocalTimeZone(); });
  it.each([
    { zone: "Asia/Kolkata", interval: "1h", start: "2026-11-12T00:00:00Z", end: "2026-11-12T03:00:00Z", times: ["2026-11-11T23:30:00Z", "2026-11-12T00:30:00Z", "2026-11-12T01:30:00Z", "2026-11-12T02:30:00Z"] },
    { zone: "Asia/Kolkata", interval: "2h", start: "2026-11-12T00:00:00Z", end: "2026-11-12T06:00:00Z", times: ["2026-11-11T22:30:00Z", "2026-11-12T00:30:00Z", "2026-11-12T02:30:00Z", "2026-11-12T04:30:00Z"] },
    { zone: "Europe/Berlin", interval: "2h", start: "2026-11-12T00:00:00Z", end: "2026-11-12T06:00:00Z", times: ["2026-11-11T23:00:00Z", "2026-11-12T01:00:00Z", "2026-11-12T03:00:00Z", "2026-11-12T05:00:00Z"] },
    { zone: "Europe/Berlin", interval: "24h", start: "2026-11-12T00:00:00Z", end: "2026-11-15T00:00:00Z", times: ["2026-11-11T23:00:00Z", "2026-11-12T23:00:00Z", "2026-11-13T23:00:00Z", "2026-11-14T23:00:00Z"] },
    { zone: "America/New_York", interval: "2h", start: "2026-03-08T05:30:00Z", end: "2026-03-08T10:30:00Z", times: ["2026-03-08T05:00:00Z", "2026-03-08T07:00:00Z", "2026-03-08T09:00:00Z"] },
    { zone: "America/New_York", interval: "24h", start: "2026-03-07T17:00:00Z", end: "2026-03-10T16:00:00Z", times: ["2026-03-07T05:00:00Z", "2026-03-08T05:00:00Z", "2026-03-09T05:00:00Z", "2026-03-10T05:00:00Z"] },
  ])("does not double $interval buckets in $zone from $start", async ({ zone, interval, start, end, times }) => {
    setLocalTimeZone(zone);
    usePreferencesStore().preferences.timezone = "local";
    const store = useDashboardsStore();
    store.setWindow({ kind: "absolute", start: epochMs(Date.parse(start)), end: epochMs(Date.parse(end)) });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: times.map((bucket, i) => ({ bucket, log_count: 2 * (i + 1) })), granularity: interval } });
    mocks.translate.mockResolvedValue({ status: "success", data: { valid: true, generated_query: "*", generated_query_language: "logsql" } });
    for (const language of ["logsql", "logchefql"] as const) {
      await store.previewPanel({ ...chartPanel("vl-count", { language, text: "*" }), viz: { type: "stat", reduce: "avg" } });
      expect(mocks.histogram.mock.lastCall?.[2].timezone).toBe(zone);
      const frame = store.previewState?.frame;
      expect(isReactive(frame)).toBe(false);
      expect(frame?.rawRows.map((row) => row.time)).toEqual(times);
      if (!frame) throw new Error("Missing count frame");
      expect(frameCellText(reduceStat(frame, { value: "count", reduce: "avg", count: true }).value)).toBe(String(times.length + 1));
      expect(frameCellText(reduceStat(frame, { value: "count", reduce: "min", count: true }).value)).toBe("2");
      expect(frameCellText(reduceStat(frame, { value: "count", reduce: "last", count: true }).value)).toBe(String(2 * times.length));
    }
  });
});

describe("dashboards store — tabs", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
  });

  // Each panel sends its id as the query text, so a request names its panel.
  function sqlPanel(id: string): DashboardQueryPanel {
    return { ...tablePanel(id), query: { mode: "result", language: "clickhouse-sql", text: id, limit: 50 } };
  }
  function requested(): string[] {
    return mocks.logsQuery.mock.calls.map((call) => call[2].query_text);
  }
  /** "today" holds `today` panels a1, a2, ... and "week" holds `week` panels b1, b2, ... */
  function tabbed(today = 2, week = 1): ValidDashboard {
    const ids = (prefix: string, n: number) => Array.from({ length: n }, (_, i) => `${prefix}${i + 1}`);
    const layout = (list: string[]) => list.map((id, i) => ({ id, x: 0, y: i, w: 12, h: 1 }));
    const a = ids("a", today);
    const b = ids("b", week);
    return makeDashboard({
      panels: {
        ...emptyPanels(),
        tabs: [
          { id: "today", title: "Current day", layout: layout(a) },
          { id: "week", title: "Last 7 days", layout: layout(b) },
        ],
        panels: [...a, ...b].map(sqlPanel),
      },
    });
  }

  it("a hidden tab sends no request until it is shown, and a shown tab runs once", async () => {
    const store = await loadEditableDashboard(tabbed());
    expect(store.activeTabId).toBe("today");
    expect(requested().sort()).toEqual(["a1", "a2"]);
    expect(store.getPanelState("b1").status).toBe("idle");

    store.setActiveTab("week");
    await vi.waitFor(() => expect(store.getPanelState("b1").status).toBe("empty"));
    expect(requested().sort()).toEqual(["a1", "a2", "b1"]);

    // Both tabs have run in this refresh: going back and forth sends nothing.
    store.setActiveTab("today");
    store.setActiveTab("week");
    await Promise.resolve();
    expect(requested()).toHaveLength(3);
    expect(store.getPanelState("a1").status).toBe("empty");
  });

  it("opens on the tab the URL names, and on the first tab for an id the dashboard lacks", async () => {
    const store = useDashboardsStore();
    store.setActiveTab("week");
    mocks.get.mockResolvedValue({ status: "success", data: tabbed() });
    await store.loadDashboard(3);
    expect(store.activeTabId).toBe("week");
    expect(requested()).toEqual(["b1"]);

    // The view tears the store down before it opens another URL.
    store.clearCurrent();
    vi.clearAllMocks();
    mocks.get.mockResolvedValue({ status: "success", data: tabbed() });
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    store.setActiveTab("deleted");
    await store.loadDashboard(3);
    expect(store.activeTabId).toBe("today");
    expect(requested().sort()).toEqual(["a1", "a2"]);
  });

  it("a refresh runs only the tab on screen, and a hidden tab runs again when it is shown", async () => {
    const store = await loadEditableDashboard(tabbed());
    store.setActiveTab("week");
    await vi.waitFor(() => expect(store.getPanelState("b1").status).toBe("empty"));
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });

    await store.refreshAllPanels();
    expect(requested()).toEqual(["b1"]);
    // The hidden tab's results came from the earlier refresh and are released.
    expect(store.getPanelState("a1").status).toBe("idle");

    store.setActiveTab("today");
    await vi.waitFor(() => expect(store.getPanelState("a2").status).toBe("empty"));
    expect(requested().sort()).toEqual(["a1", "a2", "b1"]);
    // The tab ran with the range of the refresh that was on screen.
    expect(store.getPanelState("a1").range).toEqual(store.appliedRange);
  });

  it("drops a left tab's queued panels, lets its requests in flight finish, and stays within the pool", async () => {
    const waiting: Array<{ id: string; finish: () => void }> = [];
    let inFlight = 0;
    let peak = 0;
    mocks.logsQuery.mockImplementation((_team: number, _source: number, body: { query_text: string }) =>
      new Promise((resolve) => {
        inFlight++;
        peak = Math.max(peak, inFlight);
        waiting.push({ id: body.query_text, finish: () => { inFlight--; resolve({ status: "success", data: { data: [], columns: [] } }); } });
      }));
    const finish = async (id: string) => {
      const index = waiting.findIndex((w) => w.id === id);
      expect(index, `${id} is in flight`).toBeGreaterThanOrEqual(0);
      waiting.splice(index, 1)[0].finish();
      await new Promise((resolve) => setTimeout(resolve, 0));
    };

    mocks.get.mockResolvedValue({ status: "success", data: tabbed(6, 2) });
    const store = useDashboardsStore();
    const loading = store.loadDashboard(3);
    await vi.waitFor(() => expect(requested()).toEqual(["a1", "a2", "a3", "a4"]));

    // a5 and a6 wait for a slot when the viewer leaves their tab.
    store.setActiveTab("week");
    await finish("a1");
    expect(requested()).toEqual(["a1", "a2", "a3", "a4", "b1"]);
    await finish("a2");
    await finish("a3");
    await finish("a4");
    await finish("b1");
    await finish("b2");
    await loading;
    expect(requested()).toEqual(["a1", "a2", "a3", "a4", "b1", "b2"]);
    expect(peak).toBe(4);
    // The four that were in flight kept their results.
    for (const id of ["a1", "a2", "a3", "a4", "b1", "b2"]) expect(store.getPanelState(id).status).toBe("empty");
    expect(store.getPanelState("a5").status).toBe("idle");

    // Back on the first tab, only the two dropped panels run.
    store.setActiveTab("today");
    await vi.waitFor(() => expect(waiting.map((w) => w.id)).toEqual(["a5", "a6"]));
    await finish("a5");
    await finish("a6");
    expect(requested()).toEqual(["a1", "a2", "a3", "a4", "b1", "b2", "a5", "a6"]);
  });

  it("reruns dropped panels with old results after returning during a later refresh", async () => {
    const store = await loadEditableDashboard(tabbed(6, 1));
    vi.clearAllMocks();
    const finishers: Array<() => void> = [];
    mocks.logsQuery.mockImplementation(() => new Promise((resolve) => {
      finishers.push(() => resolve({ status: "success", data: { data: [], columns: [] } }));
    }));
    const refreshing = store.refreshAllPanels();
    await vi.waitFor(() => expect(requested()).toEqual(["a1", "a2", "a3", "a4"]));
    store.setActiveTab("week");
    while (finishers.length) {
      finishers.shift()?.();
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    await refreshing;
    expect(requested()).toEqual(["a1", "a2", "a3", "a4", "b1"]);
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    store.setActiveTab("today");
    await vi.waitFor(() => expect(requested()).toEqual(["a1", "a2", "a3", "a4", "b1", "a5", "a6"]));
    expect(store.getPanelState("a6").status).toBe("empty");
  });

  it("restores a moved panel's result on discard after a refresh hid it", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    store.moveDraftPanelToTab("a1", "week");
    await store.refreshAllPanels();
    expect(store.getPanelState("a1").status).toBe("idle");
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [], columns: [] } });
    await store.cancelEdit();
    await vi.waitFor(() => expect(store.getPanelState("a1").status).toBe("empty"));
    expect(store.activeTabId).toBe("today");
    expect(requested()).toEqual(["a1"]);
  });

  const ok = () => ({ status: "success", data: { data: [], columns: [] } });
  const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

  it("a duplicated panel's copied result does not rerun on a tab round trip", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    expect(store.duplicateDraftPanel("a1", "copy")).not.toBeNull();
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue(ok());
    store.setActiveTab("week");
    await vi.waitFor(() => expect(store.getPanelState("b1").status).toBe("empty"));
    store.setActiveTab("today");
    await tick();
    expect(requested()).toEqual(["b1"]);
  });

  it("an in-flight single-panel run is not sent twice by a tab round trip", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    vi.clearAllMocks();
    const finishers: Array<() => void> = [];
    mocks.logsQuery.mockImplementation(() => new Promise((resolve) => { finishers.push(() => resolve(ok())); }));
    store.updateDraftPanel("a1", { query: { text: "a1-edited" } });
    await vi.waitFor(() => expect(requested()).toEqual(["a1-edited"]));
    store.setActiveTab("week");
    store.setActiveTab("today");
    await tick();
    while (finishers.length) { finishers.shift()?.(); await tick(); }
    expect(requested().filter((q) => q === "a1-edited")).toHaveLength(1);
  });

  it.each([
    { change: "query", patch: { query: { text: "a1-edited" } } },
    { change: "source", patch: { source_id: 2 } },
  ])("discard after editing a $change reruns the saved definition", async ({ patch }) => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    store.updateDraftPanel("a1", patch);
    await vi.waitFor(() => expect(store.getPanelState("a1").status).toBe("empty"));
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue(ok());
    await store.cancelEdit();
    await tick();
    expect(requested()).toEqual(["a1"]);
    expect(mocks.logsQuery.mock.lastCall?.slice(0, 2)).toEqual([1, 1]);
  });

  it("a single-panel run that completed in this refresh is not rerun", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    store.updateDraftPanel("a1", { query: { text: "a1-edited" } });
    await vi.waitFor(() => expect(store.getPanelState("a1").status).toBe("empty"));
    store.setActiveTab("week");
    await vi.waitFor(() => expect(store.getPanelState("b1").status).toBe("empty"));
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue(ok());
    store.setActiveTab("today");
    await tick();
    expect(requested()).toEqual([]);
  });

  it("saving a copy sends no restored-panel request for the original", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    store.moveDraftPanelToTab("a1", "week");
    await store.refreshAllPanels();
    vi.clearAllMocks();
    mocks.create.mockResolvedValue({ status: "success", data: tabbed() });
    await store.saveDraftAsCopy("Copy");
    await tick();
    expect(requested()).toEqual([]);
  });

  it("queues a panel once when its tab is shown again before its turn", async () => {
    const finishers: Array<() => void> = [];
    mocks.logsQuery.mockImplementation(() => new Promise((resolve) => {
      finishers.push(() => resolve({ status: "success", data: { data: [], columns: [] } }));
    }));
    mocks.get.mockResolvedValue({ status: "success", data: tabbed(6, 2) });
    const store = useDashboardsStore();
    const loading = store.loadDashboard(3);
    await vi.waitFor(() => expect(requested()).toHaveLength(4));

    store.setActiveTab("week");
    store.setActiveTab("today");
    while (finishers.length) {
      finishers.shift()?.();
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    await loading;
    // The second tab was left before any of its panels got a slot.
    expect(requested().sort()).toEqual(["a1", "a2", "a3", "a4", "a5", "a6"]);
  });

  it("adds a tab, shows it, and puts a new panel on it without touching the other tab", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    const before = tabLayout(store.editDraft!, "today");

    const id = store.addDraftTab("  Errors ");
    expect(id).not.toBeNull();
    expect(store.activeTabId).toBe(id);
    expect(store.editDraft?.tabs.map((tab) => tab.title)).toEqual(["Current day", "Last 7 days", "Errors"]);
    expect(store.isDirty).toBe(true);

    store.upsertDraftPanel(sqlPanel("n1"));
    expect(tabLayout(store.editDraft!, id!)).toEqual([{ id: "n1", x: 0, y: 0, w: 6, h: 2 }]);
    expect(tabLayout(store.editDraft!, "today")).toBe(before);
  });

  it("refuses an eleventh tab and a title that is blank or too long", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    expect(store.addDraftTab("   ")).toBeNull();
    expect(store.addDraftTab("x".repeat(61))).toBeNull();
    for (let i = 3; i <= 10; i++) expect(store.addDraftTab(`Tab ${i}`)).not.toBeNull();
    expect(store.editDraft?.tabs).toHaveLength(10);
    expect(store.addDraftTab("Tab 11")).toBeNull();
    expect(store.renameDraftTab("today", "")).toBe(false);
    expect(store.editDraft?.tabs[0].title).toBe("Current day");
  });

  it("renames and reorders tabs", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    expect(store.renameDraftTab("week", " Past week ")).toBe(true);
    store.moveDraftTab("week", 0);
    expect(store.editDraft?.tabs.map((tab) => [tab.id, tab.title])).toEqual([["week", "Past week"], ["today", "Current day"]]);
    // The tab on screen follows its id, not its position.
    expect(store.activeTabId).toBe("today");
  });

  it("resizes to a free width and height, and reorders, inside one tab only", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    const week = tabLayout(store.editDraft!, "week");

    store.resizeDraftPanel("a1", 5, 9);
    store.resizeDraftPanel("a2", 7, 2);
    expect(tabLayout(store.editDraft!, "today")).toEqual([
      { id: "a1", x: 0, y: 0, w: 5, h: 9 },
      { id: "a2", x: 5, y: 0, w: 7, h: 2 },
    ]);

    store.reorderDraftPanels(["a2", "a1"]);
    expect(tabLayout(store.editDraft!, "today")).toEqual([
      { id: "a2", x: 0, y: 0, w: 7, h: 2 },
      { id: "a1", x: 7, y: 0, w: 5, h: 9 },
    ]);
    expect(tabLayout(store.editDraft!, "week")).toBe(week);
    expect(store.editDraft?.panels.map((p) => p.id)).toEqual(["a2", "a1", "b1"]);
  });

  it("moves a panel to another tab with its result and without a request", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    const result = store.getPanelState("a1");
    expect(result.status).toBe("empty");
    const sent = requested().length;

    store.moveDraftPanelToTab("a1", "week");
    expect(tabLayout(store.editDraft!, "today").map((l) => l.id)).toEqual(["a2"]);
    expect(tabLayout(store.editDraft!, "week")).toEqual([
      { id: "b1", x: 0, y: 0, w: 12, h: 1 },
      { id: "a1", x: 0, y: 1, w: 12, h: 1 },
    ]);
    expect(store.getPanelState("a1")).toBe(result);

    // The target tab never ran: showing it runs its own panel, not the moved one.
    store.setActiveTab("week");
    await vi.waitFor(() => expect(store.getPanelState("b1").status).toBe("empty"));
    expect(requested().slice(sent)).toEqual(["b1"]);
  });

  it("deletes a tab with its panels, and shows the tab that took its place", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();

    store.removeDraftTab("today");
    expect(store.editDraft?.tabs.map((tab) => tab.id)).toEqual(["week"]);
    expect(store.editDraft?.panels.map((p) => p.id)).toEqual(["b1"]);
    expect(store.activeTabId).toBe("week");
    await vi.waitFor(() => expect(store.getPanelState("b1").status).toBe("empty"));

    // A dashboard keeps its last tab.
    store.removeDraftTab("week");
    expect(store.editDraft?.tabs.map((tab) => tab.id)).toEqual(["week"]);
  });

  it("saves tab edits with the revision the edit started from, and shows the same tab afterwards", async () => {
    const dashboard = { ...tabbed(), revision: 7 };
    const store = await loadEditableDashboard(dashboard);
    store.setActiveTab("week");
    await store.enterEdit();
    expect(store.isDirty).toBe(false);
    store.renameDraftTab("week", "Past week");
    store.moveDraftPanelToTab("a2", "week");

    const draft: DashboardPanels = JSON.parse(JSON.stringify(store.editDraft));
    mocks.update.mockResolvedValue({ status: "success", data: { ...dashboard, revision: 8, panels: { ...draft, features: ["tabs"] } } });
    const result = await store.saveEdit();
    expect(result.success).toBe(true);
    const [, body] = mocks.update.mock.calls[0];
    expect(body.revision).toBe(7);
    expect(body.panels.tabs).toEqual([
      { id: "today", title: "Current day", layout: [{ id: "a1", x: 0, y: 0, w: 12, h: 1 }] },
      { id: "week", title: "Past week", layout: [{ id: "b1", x: 0, y: 0, w: 12, h: 1 }, { id: "a2", x: 0, y: 1, w: 12, h: 1 }] },
    ]);
    expect(store.activeTabId).toBe("week");
  });

  it("leaves a tab that only the discarded draft had", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    store.addDraftTab("Scratch");
    await store.cancelEdit();
    expect(store.activeTabId).toBe("today");
  });


  it("discarding after a query edit was reverted mid-flight does not leave the panel loading", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    vi.clearAllMocks();
    mocks.logsQuery.mockImplementation(() => new Promise(() => {}));
    store.updateDraftPanel("a1", { query: { text: "a1-edited" } });
    await vi.waitFor(() => expect(requested()).toEqual(["a1-edited"]));
    store.updateDraftPanel("a1", { query: { text: "a1" } });
    await vi.waitFor(() => expect(requested()).toEqual(["a1-edited", "a1"]));
    expect(store.getPanelState("a1").status).toBe("loading");
    mocks.logsQuery.mockResolvedValue(ok());
    await store.cancelEdit();
    await vi.waitFor(() => expect(store.getPanelState("a1").status).toBe("empty"));
  });

  it("discarding a visualization-only edit sends no request", async () => {
    const store = await loadEditableDashboard(tabbed());
    await store.enterEdit();
    store.updateDraftPanel("a1", { viz: { type: "table", visible_columns: ["x"] } });
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue(ok());
    await store.cancelEdit();
    await tick();
    expect(requested()).toEqual([]);
  });
});

// Phase 14: a panel follows the dashboard range unless it pins its own, and
// its bucket size is the interval it chose.
describe("panel time", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.logsQuery.mockResolvedValue({ status: "success", data: { data: [{ n: 1 }], columns: [{ name: "n", type: "UInt64" }] } });
    mocks.histogram.mockResolvedValue({ status: "success", data: { data: [], granularity: "24h" } });
    mocks.translate.mockResolvedValue({ status: "success", data: { valid: true, generated_query: "*", generated_query_language: "logsql" } });
  });
  afterEach(() => { vi.useRealTimers(); resetLocalTimeZone(); });

  const sql = (id: string, query: Partial<DashboardQueryPanel["query"]> = {}): DashboardQueryPanel => ({
    id, kind: "query", title: id, team_id: 1, source_id: 1,
    query: { mode: "result", language: "clickhouse-sql", text: "SELECT count() AS n FROM logs WHERE {{__time_filter}}", ...query },
    viz: { type: "stat", value: "n", reduce: "last" },
  });
  const absolute = (start: string, end: string) => ({ kind: "absolute", start: epochMs(Date.parse(start)), end: epochMs(Date.parse(end)) }) as const;

  async function load(...panels: DashboardQueryPanel[]) {
    const layout = panels.map((p, i) => ({ id: p.id, x: 0, y: i, w: 12, h: 1 }));
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ panels: panelsWith(panels, layout) }) });
    const store = useDashboardsStore();
    usePreferencesStore().preferences.timezone = "utc";
    await store.loadDashboard(3);
    return store;
  }
  const requestFor = (mock: typeof mocks.logsQuery, text: string) => mock.mock.calls.map((call) => call[2]).filter((body) => body.query_text === text).at(-1);

  it("moves a bound panel with the time picker and leaves a pinned panel on its own range", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-12T10:15:00Z"));
    const pinnedText = "SELECT count() AS n FROM logs WHERE {{__time_filter}} AND 1";
    const store = await load(sql("bound"), sql("pinned", { text: pinnedText, range: "today-6d" }));

    const boundText = sql("bound").query.text;
    expect(requestFor(mocks.logsQuery, boundText)).toMatchObject({ start_time: "2026-11-12T10:00:00.000Z", end_time: "2026-11-12T10:15:00.000Z" });
    // today-6d: from midnight six days before today until now.
    expect(requestFor(mocks.logsQuery, pinnedText)).toMatchObject({ start_time: "2026-11-06T00:00:00.000Z", end_time: "2026-11-12T10:15:00.000Z", timezone: "UTC" });
    expect(store.getPanelState("pinned").range).toEqual({ start: Date.parse("2026-11-06T00:00:00Z"), end: Date.parse("2026-11-12T10:15:00Z"), zone: "UTC" });

    store.setWindow(absolute("2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z"));
    await store.refreshAllPanels();
    expect(requestFor(mocks.logsQuery, boundText)).toMatchObject({ start_time: "2026-10-01T00:00:00.000Z", end_time: "2026-10-02T00:00:00.000Z" });
    expect(requestFor(mocks.logsQuery, pinnedText)).toMatchObject({ start_time: "2026-11-06T00:00:00.000Z", end_time: "2026-11-12T10:15:00.000Z" });
    expect(store.getPanelState("bound").range).toMatchObject({ start: Date.parse("2026-10-01T00:00:00Z") });
  });

  it("reads a dashboard that stores the panel_time feature, so it runs and stays editable", async () => {
    const pinned = sql("pinned", { range: "today-6d" });
    mocks.get.mockResolvedValue({ status: "success", data: makeDashboard({ panels: { ...panelsWith([pinned], [{ id: "pinned", x: 0, y: 0, w: 12, h: 1 }]), features: ["panel_time"] } }) });
    const store = useDashboardsStore();
    await store.loadDashboard(3);
    expect(store.needsReload).toBe(false);
    expect(store.canEdit).toBe(true);
    await vi.waitFor(() => expect(store.getPanelState("pinned").status).toBe("success"));
  });

  it("sends a count panel's fixed interval as the histogram window: 24h on a 7-day range", async () => {
    const store = await load(chartPanel("days", { language: "logsql", text: "*", interval: "24h" }));
    store.setWindow(absolute("2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z"));
    await store.refreshAllPanels();
    expect(mocks.histogram.mock.lastCall?.[2]).toMatchObject({ window: "24h", start_time: "2026-11-05T18:30:00.000Z", end_time: "2026-11-12T18:30:00.000Z" });
    expect(store.getPanelState("days")).toMatchObject({ interval: "24h", intervalRaised: false });
  });

  it("raises a fixed interval that would give more than 1,000 buckets and says which one it used", async () => {
    const store = await load(chartPanel("fine", { language: "logsql", text: "*", interval: "10m" }));
    store.setWindow(absolute("2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z"));
    await store.refreshAllPanels();
    expect(mocks.histogram.mock.lastCall?.[2].window).toBe("15m");
    expect(store.getPanelState("fine")).toMatchObject({ interval: "15m", intervalRaised: true });
  });

  it("sends a result panel's interval for {{__interval}}, resolved from auto when it sets none", async () => {
    const store = await load(sql("auto"), sql("fixed", { text: "SELECT 2", interval: "24h" }));
    store.setWindow(absolute("2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z"));
    await store.refreshAllPanels();
    // 7 days / 100 buckets = 6048 s, and the next window up is 2h.
    expect(requestFor(mocks.logsQuery, sql("auto").query.text).interval).toBe("2h");
    expect(requestFor(mocks.logsQuery, "SELECT 2").interval).toBe("24h");
  });

  describe("the daylight-saving notice on VictoriaLogs count panels", () => {
    // Berlin left summer time at 01:00 UTC on 2026-10-25.
    const crossing = absolute("2026-10-22T22:00:00Z", "2026-10-27T23:00:00Z");
    async function notices(panel: DashboardQueryPanel, window: ReturnType<typeof absolute>, zone = "Europe/Berlin") {
      setLocalTimeZone(zone);
      usePreferencesStore().preferences.timezone = "local";
      const store = useDashboardsStore();
      store.setWindow(window);
      mocks.histogram.mockResolvedValue({ status: "success", data: { data: [{ bucket: "2026-10-25T22:00:00Z", log_count: 3 }], granularity: panel.query.interval } });
      await store.previewPanel(panel);
      return store.previewState?.frame?.notices ?? [];
    }

    it("names the change when the range crosses it and the bucket is longer than one hour", async () => {
      const shown = await notices(chartPanel("vl", { language: "logsql", text: "*", interval: "24h" }), crossing);
      expect(shown).toHaveLength(1);
      expect(shown[0]).toContain("Europe/Berlin changed from UTC+02:00 to UTC+01:00");
      expect(shown[0]).toContain("Oct 25, 2026");
      expect(shown[0]).toContain("do not start at local midnight");
    });

    it("shows on a LogchefQL count panel that compiles to LogsQL", async () => {
      expect(await notices(chartPanel("vl", { language: "logchefql", text: "level=error", interval: "6h" }), crossing)).toHaveLength(1);
    });

    it.each([
      ["a bucket of one hour", chartPanel("vl", { language: "logsql", text: "*", interval: "1h" }), crossing, "Europe/Berlin"],
      ["a range after the change", chartPanel("vl", { language: "logsql", text: "*", interval: "24h" }), absolute("2026-11-01T00:00:00Z", "2026-11-08T00:00:00Z"), "Europe/Berlin"],
      ["a zone without daylight saving", chartPanel("vl", { language: "logsql", text: "*", interval: "24h" }), crossing, "Asia/Kolkata"],
    ] as const)("stays quiet for %s", async (_name, panel, window, zone) => {
      expect(await notices(panel, window, zone)).toEqual([]);
    });

    it("stays quiet on ClickHouse, whose buckets follow the change", async () => {
      mocks.translate.mockResolvedValue({ status: "success", data: { valid: true, full_sql: "SELECT 1", generated_query_language: "clickhouse-sql" } });
      expect(await notices(chartPanel("ch", { language: "logchefql", text: "level=error", interval: "24h" }), crossing)).toEqual([]);
    });
  });
});
