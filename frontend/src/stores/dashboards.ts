import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "@/stores/base";
import {
  dashboardsApi,
  dashboardPanelApi,
  type Dashboard,
  type DashboardPanel,
  type DashboardPanels,
  type CreateDashboardRequest,
} from "@/api/dashboards";
import { isSuccessResponse, type APIResponse } from "@/api/types";
import { HistogramService, type HistogramData } from "@/services/HistogramService";
import { sumHistogramCounts, reflowPanels, validatePanelsBlob } from "@/utils/dashboardPanels";
import { parseRelativeTimeString, calendarDateTimeToTimestamp } from "@/utils/time";
import { isCanceledError } from "@/api/error-handler";
import { runWithConcurrency } from "@/utils/promisePool";

// Number of panel fetches allowed in flight at once. A 24-panel dashboard would
// otherwise fire 24 (or 48, counting the logchefql translate step) requests at
// once; this keeps the backend from being stampeded on load/refresh.
export const PANEL_FETCH_CONCURRENCY = 4;

const DEFAULT_RELATIVE_TIME = "15m";
const PANEL_QUERY_TIMEZONE = "UTC";

export type PanelStatus = "idle" | "loading" | "success" | "empty" | "error" | "locked";

export interface PanelColumn {
  name: string;
  type: string;
}

export interface PanelState {
  status: PanelStatus;
  error?: string;
  timeseries?: {
    buckets: HistogramData[];
    granularity: string | null;
    groupBy: string | null;
  };
  stat?: {
    value: number;
  };
  table?: {
    columns: PanelColumn[];
    rows: Record<string, any>[];
  };
}

export interface EffectiveRange {
  start: number; // epoch ms
  end: number; // epoch ms
}

interface DashboardsState {
  dashboards: Dashboard[];
  current: Dashboard | null;
  panelStates: Record<string, PanelState>;
  timeRelative: string | null;
  timeAbsolute: EffectiveRange | null;
  refreshIntervalMs: number;
  // Edit mode. `editDraft` is the working copy of the panel blob being mutated;
  // `editSnapshotJson` is the JSON of the blob at the moment Edit was entered,
  // used for a cheap dirty comparison. `previewState` backs the panel editor's
  // Preview button (executed through the same path as live panels).
  isEditing: boolean;
  editDraft: DashboardPanels | null;
  editSnapshotJson: string | null;
  previewState: PanelState | null;
}

/** Deep clone a JSON-serializable panel blob (breaks all reactive references). */
function cloneBlob(blob: DashboardPanels): DashboardPanels {
  return JSON.parse(JSON.stringify(blob));
}

// Stable, key-order-independent serialization used for the dirty comparison. The
// reflow step re-emits object keys in a different order than the loaded blob, so
// a plain JSON.stringify would report a semantically-identical draft as dirty.
function stableStringify(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }
  if (value && typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>).sort();
    return `{${keys
      .map((k) => `${JSON.stringify(k)}:${stableStringify((value as Record<string, unknown>)[k])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

// A UTC instant as RFC3339 (histogram endpoint contract).
function msToRfc3339(ms: number): string {
  return new Date(ms).toISOString();
}

// A UTC instant as "YYYY-MM-DD HH:MM:SS" (logchefql endpoint contract).
function msToSqlDateTime(ms: number): string {
  return new Date(ms).toISOString().slice(0, 19).replace("T", " ");
}

function isAbort(err: unknown): boolean {
  return (
    isCanceledError(err) ||
    (err instanceof Error && err.name === "AbortError") ||
    (typeof err === "object" && err !== null && (err as { error_type?: string }).error_type === "CanceledError")
  );
}

function classifyPanelError(err: unknown): { locked: boolean; message: string } {
  const asObj = (typeof err === "object" && err !== null ? err : {}) as {
    error_type?: string;
    message?: string;
  };
  const locked = asObj.error_type === "AuthorizationError";
  const message = asObj.message || (err instanceof Error ? err.message : "Failed to load panel");
  return { locked, message };
}

export const useDashboardsStore = defineStore("dashboards", () => {
  const state = useBaseStore<DashboardsState>({
    dashboards: [],
    current: null,
    panelStates: {},
    timeRelative: DEFAULT_RELATIVE_TIME,
    timeAbsolute: null,
    refreshIntervalMs: 0,
    isEditing: false,
    editDraft: null,
    editSnapshotJson: null,
    previewState: null,
  });

  // Non-reactive: the controller aborting the in-flight refresh batch.
  let refreshAbort: AbortController | null = null;
  // Non-reactive: the controller aborting an in-flight panel preview.
  let previewAbort: AbortController | null = null;

  const dashboards = computed(() => state.data.value.dashboards);
  const current = computed(() => state.data.value.current);
  const panelStates = computed(() => state.data.value.panelStates);
  const timeRelative = computed(() => state.data.value.timeRelative);
  const timeAbsolute = computed(() => state.data.value.timeAbsolute);
  const refreshIntervalMs = computed(() => state.data.value.refreshIntervalMs);

  const isEditing = computed(() => state.data.value.isEditing);
  const editDraft = computed(() => state.data.value.editDraft);
  const previewState = computed(() => state.data.value.previewState);
  const canEdit = computed(() => state.data.value.current?.can_edit === true);
  // Dirty when the working draft diverges from the snapshot taken on entering edit.
  const isDirty = computed(() => {
    const draft = state.data.value.editDraft;
    if (!state.data.value.isEditing || !draft) return false;
    return stableStringify(draft) !== state.data.value.editSnapshotJson;
  });

  const effectiveRange = computed<EffectiveRange>(() => {
    const rel = state.data.value.timeRelative;
    if (rel) {
      try {
        const { start, end } = parseRelativeTimeString(rel);
        return {
          start: calendarDateTimeToTimestamp(start),
          end: calendarDateTimeToTimestamp(end),
        };
      } catch {
        // fall through to absolute/default
      }
    }
    if (state.data.value.timeAbsolute) {
      return state.data.value.timeAbsolute;
    }
    const { start, end } = parseRelativeTimeString(DEFAULT_RELATIVE_TIME);
    return {
      start: calendarDateTimeToTimestamp(start),
      end: calendarDateTimeToTimestamp(end),
    };
  });

  function getPanelState(panelId: string): PanelState {
    return state.data.value.panelStates[panelId] ?? { status: "idle" };
  }

  function setPanelState(panelId: string, next: PanelState) {
    state.data.value.panelStates = {
      ...state.data.value.panelStates,
      [panelId]: next,
    };
  }

  // ---- CRUD ---------------------------------------------------------------

  async function fetchDashboards() {
    return await state.callApi<Dashboard[]>({
      apiCall: () => dashboardsApi.list(),
      operationKey: "fetchDashboards",
      showToast: false,
      defaultData: [],
      onSuccess: (data) => {
        state.data.value.dashboards = data ?? [];
      },
    });
  }

  async function loadDashboard(id: number) {
    const result = await state.callApi<Dashboard>({
      apiCall: () => dashboardsApi.get(id),
      operationKey: "loadDashboard",
      showToast: false,
      onSuccess: (data) => {
        state.data.value.current = data ?? null;
        state.data.value.panelStates = {};
      },
    });
    if (result.success && state.data.value.current) {
      await refreshAllPanels();
    }
    return result;
  }

  async function createDashboard(req: CreateDashboardRequest) {
    return await state.callApi<Dashboard>({
      apiCall: () => dashboardsApi.create(req),
      operationKey: "createDashboard",
      successMessage: "Dashboard created",
      onSuccess: (data) => {
        if (data) {
          state.data.value.dashboards = [data, ...state.data.value.dashboards];
        }
      },
    });
  }

  async function deleteDashboard(id: number) {
    return await state.callApi<{ id: number }>({
      apiCall: () => dashboardsApi.remove(id),
      operationKey: `deleteDashboard-${id}`,
      successMessage: "Dashboard deleted",
      onSuccess: () => {
        state.data.value.dashboards = state.data.value.dashboards.filter((d) => d.id !== id);
        if (state.data.value.current?.id === id) {
          state.data.value.current = null;
        }
      },
    });
  }

  // ---- Time range + refresh interval --------------------------------------

  function setRelativeTime(relative: string) {
    state.data.value.timeRelative = relative;
    state.data.value.timeAbsolute = null;
    void refreshAllPanels();
  }

  function setAbsoluteRange(start: number, end: number) {
    state.data.value.timeAbsolute = { start, end };
    state.data.value.timeRelative = null;
    void refreshAllPanels();
  }

  function setRefreshInterval(ms: number) {
    state.data.value.refreshIntervalMs = ms;
  }

  // ---- Panel execution ----------------------------------------------------

  function unwrap<T>(resp: APIResponse<T>): T {
    if (isSuccessResponse(resp)) {
      return resp.data as T;
    }
    // Non-2xx responses reject before this; a 200 with status:"error" lands here.
    throw resp;
  }

  // Resolve a panel's query to the native query text the histogram endpoint wants
  // (SQL for ClickHouse sources, LogsQL for VictoriaLogs). ClickHouse-SQL panels
  // already carry native SQL. Everything else goes through the shared LogchefQL
  // compile — exactly the multi-datasource path the explorer uses.
  async function resolveNativeQuery(
    panel: DashboardPanel,
    range: EffectiveRange,
    signal: AbortSignal
  ): Promise<string> {
    if (panel.query_language === "clickhouse-sql") {
      return panel.query;
    }
    const resp = await dashboardPanelApi.translate(
      panel.team_id,
      panel.source_id,
      {
        query: panel.query,
        start_time: msToSqlDateTime(range.start),
        end_time: msToSqlDateTime(range.end),
        timezone: PANEL_QUERY_TIMEZONE,
        limit: panel.options?.limit,
      },
      signal
    );
    const translated = unwrap(resp);
    if (translated.valid === false) {
      throw { error_type: "ValidationError", message: translated.error?.message || "Invalid query" };
    }
    return translated.full_sql || translated.generated_query || translated.sql || panel.query;
  }

  async function fetchPanelData(
    panel: DashboardPanel,
    range: EffectiveRange,
    signal: AbortSignal
  ): Promise<PanelState> {
    const startIso = msToRfc3339(range.start);
    const endIso = msToRfc3339(range.end);

    if (panel.type === "timeseries" || panel.type === "stat") {
      const queryText = await resolveNativeQuery(panel, range, signal);
      const window = HistogramService.calculateOptimalGranularity(startIso, endIso);
      const groupBy = panel.type === "timeseries" ? panel.options?.group_by || undefined : undefined;

      const resp = await dashboardPanelApi.histogram(
        panel.team_id,
        panel.source_id,
        {
          query_text: queryText,
          window,
          group_by: groupBy,
          start_time: startIso,
          end_time: endIso,
          timezone: PANEL_QUERY_TIMEZONE,
        },
        signal
      );
      const data = unwrap(resp);
      const buckets = data?.data ?? [];

      if (panel.type === "stat") {
        return { status: "success", stat: { value: sumHistogramCounts(buckets) } };
      }
      return {
        status: buckets.length ? "success" : "empty",
        timeseries: {
          buckets,
          granularity: data?.granularity ?? null,
          groupBy: groupBy ?? null,
        },
      };
    }

    // table
    const limit = panel.options?.limit && panel.options.limit > 0 ? panel.options.limit : 100;
    if (panel.query_language === "clickhouse-sql") {
      const resp = await dashboardPanelApi.logsQuery(
        panel.team_id,
        panel.source_id,
        {
          query_text: panel.query,
          limit,
          start_time: startIso,
          end_time: endIso,
          timezone: PANEL_QUERY_TIMEZONE,
        },
        signal
      );
      const data = unwrap(resp);
      const rows = data?.data ?? data?.logs ?? [];
      return {
        status: rows.length ? "success" : "empty",
        table: { columns: data?.columns ?? [], rows },
      };
    }

    const resp = await dashboardPanelApi.logchefqlQuery(
      panel.team_id,
      panel.source_id,
      {
        query: panel.query,
        start_time: msToSqlDateTime(range.start),
        end_time: msToSqlDateTime(range.end),
        timezone: PANEL_QUERY_TIMEZONE,
        limit,
      },
      signal
    );
    const data = unwrap(resp);
    const rows = data?.logs ?? [];
    return {
      status: rows.length ? "success" : "empty",
      table: { columns: data?.columns ?? [], rows },
    };
  }

  async function executePanel(panel: DashboardPanel, range: EffectiveRange, signal: AbortSignal) {
    setPanelState(panel.id, { status: "loading" });
    try {
      const result = await fetchPanelData(panel, range, signal);
      if (signal.aborted) return;
      setPanelState(panel.id, result);
    } catch (err) {
      if (signal.aborted || isAbort(err)) return;
      const { locked, message } = classifyPanelError(err);
      setPanelState(panel.id, { status: locked ? "locked" : "error", error: message });
    }
  }

  async function refreshAllPanels() {
    const dashboard = state.data.value.current;
    if (!dashboard) return;

    // Cancel any in-flight batch so a rapid time-range change doesn't leave stale
    // requests racing the new ones.
    refreshAbort?.abort();
    const controller = new AbortController();
    refreshAbort = controller;
    const signal = controller.signal;
    const range = effectiveRange.value;

    const panels = dashboard.panels?.panels ?? [];
    const tasks = panels.map((panel) => () => executePanel(panel, range, signal));
    await runWithConcurrency(tasks, PANEL_FETCH_CONCURRENCY);
  }

  // ---- Edit mode ----------------------------------------------------------

  // Execute a single panel into panelStates (used after add/edit so the panel
  // renders immediately without a full-dashboard refresh).
  function executeSinglePanel(panel: DashboardPanel) {
    const controller = new AbortController();
    void executePanel(panel, effectiveRange.value, controller.signal);
  }

  function enterEdit() {
    const dashboard = state.data.value.current;
    if (!dashboard || dashboard.can_edit !== true) return;
    const blob: DashboardPanels = dashboard.panels ?? { version: 1, layout: [], panels: [] };
    // Snapshot the loaded blob; the draft is an independent clone we mutate.
    state.data.value.editSnapshotJson = stableStringify(blob);
    state.data.value.editDraft = cloneBlob(blob);
    state.data.value.previewState = null;
    state.data.value.isEditing = true;
  }

  function cancelEdit() {
    // The live dashboard (current.panels) was never mutated, so simply dropping
    // the draft reverts to the last-loaded state.
    previewAbort?.abort();
    previewAbort = null;
    state.data.value.isEditing = false;
    state.data.value.editDraft = null;
    state.data.value.editSnapshotJson = null;
    state.data.value.previewState = null;
  }

  function setDraft(next: DashboardPanels) {
    state.data.value.editDraft = next;
  }

  // Add or replace a panel in the draft (by id), then reflow the layout so the
  // grid stays top-left-first with no overlaps. A newly added panel has no layout
  // entry, so reflowPanels places it in the next free slot at the default size.
  function upsertDraftPanel(panel: DashboardPanel) {
    const draft = state.data.value.editDraft;
    if (!draft) return;
    const panels = [...draft.panels];
    const idx = panels.findIndex((p) => p.id === panel.id);
    if (idx >= 0) {
      panels[idx] = panel;
    } else {
      panels.push(panel);
    }
    setDraft(reflowPanels({ ...draft, panels }));
    executeSinglePanel(panel);
  }

  function removeDraftPanel(id: string) {
    const draft = state.data.value.editDraft;
    if (!draft) return;
    const panels = draft.panels.filter((p) => p.id !== id);
    const layout = draft.layout.filter((l) => l.id !== id);
    setDraft(reflowPanels({ ...draft, panels, layout }));
  }

  function resizeDraftPanel(id: string, w: number, h: number) {
    const draft = state.data.value.editDraft;
    if (!draft) return;
    const layout = draft.layout.some((l) => l.id === id)
      ? draft.layout.map((l) => (l.id === id ? { ...l, w, h } : l))
      : [...draft.layout, { id, x: 0, y: 0, w, h }];
    setDraft(reflowPanels({ ...draft, layout }));
  }

  // Reorder the draft's panels to match `orderedIds` (drag-and-drop result), then
  // reflow. Ids not present are dropped; panels missing from the list are appended.
  function reorderDraftPanels(orderedIds: string[]) {
    const draft = state.data.value.editDraft;
    if (!draft) return;
    const byId = new Map(draft.panels.map((p) => [p.id, p]));
    const reordered: DashboardPanel[] = [];
    for (const id of orderedIds) {
      const p = byId.get(id);
      if (p) {
        reordered.push(p);
        byId.delete(id);
      }
    }
    for (const leftover of byId.values()) reordered.push(leftover);
    setDraft(reflowPanels({ ...draft, panels: reordered }));
  }

  async function saveEdit() {
    const dashboard = state.data.value.current;
    const draft = state.data.value.editDraft;
    if (!dashboard || !draft) {
      return { success: false, error: { message: "Nothing to save" } };
    }
    // Mirror the server-side validation with a friendly, inline message; the
    // server 400 remains the authoritative backstop.
    const validationError = validatePanelsBlob(draft);
    if (validationError) {
      return { success: false, error: { message: validationError } };
    }
    const result = await state.callApi<Dashboard>({
      apiCall: () =>
        dashboardsApi.update(dashboard.id, {
          name: dashboard.name,
          description: dashboard.description,
          panels: draft,
        }),
      operationKey: "saveDashboard",
      successMessage: "Dashboard saved",
      onSuccess: (data) => {
        // Adopt the saved blob as the live dashboard and re-render.
        if (data) {
          state.data.value.current = data;
          const idx = state.data.value.dashboards.findIndex((d) => d.id === data.id);
          if (idx >= 0) state.data.value.dashboards[idx] = data;
        } else {
          state.data.value.current = { ...dashboard, panels: cloneBlob(draft) };
        }
        state.data.value.isEditing = false;
        state.data.value.editDraft = null;
        state.data.value.editSnapshotJson = null;
        state.data.value.previewState = null;
      },
    });
    if (result.success) {
      state.data.value.panelStates = {};
      await refreshAllPanels();
    }
    return result;
  }

  // Run a panel's query through the SAME execution path live panels use, storing
  // the result in `previewState` for the editor's Preview pane.
  async function previewPanel(panel: DashboardPanel) {
    previewAbort?.abort();
    const controller = new AbortController();
    previewAbort = controller;
    const signal = controller.signal;
    state.data.value.previewState = { status: "loading" };
    try {
      const result = await fetchPanelData(panel, effectiveRange.value, signal);
      if (signal.aborted) return;
      state.data.value.previewState = result;
    } catch (err) {
      if (signal.aborted || isAbort(err)) return;
      const { locked, message } = classifyPanelError(err);
      state.data.value.previewState = { status: locked ? "locked" : "error", error: message };
    }
  }

  function clearPreview() {
    previewAbort?.abort();
    previewAbort = null;
    state.data.value.previewState = null;
  }

  function clearCurrent() {
    refreshAbort?.abort();
    refreshAbort = null;
    previewAbort?.abort();
    previewAbort = null;
    state.data.value.current = null;
    state.data.value.panelStates = {};
    state.data.value.isEditing = false;
    state.data.value.editDraft = null;
    state.data.value.editSnapshotJson = null;
    state.data.value.previewState = null;
  }

  return {
    // base passthrough
    isLoading: state.isLoading,
    isLoadingOperation: state.isLoadingOperation,
    error: state.error,

    // state
    dashboards,
    current,
    panelStates,
    timeRelative,
    timeAbsolute,
    refreshIntervalMs,
    effectiveRange,

    // edit state
    isEditing,
    editDraft,
    previewState,
    canEdit,
    isDirty,

    // getters
    getPanelState,

    // crud
    fetchDashboards,
    loadDashboard,
    createDashboard,
    deleteDashboard,

    // time + refresh
    setRelativeTime,
    setAbsoluteRange,
    setRefreshInterval,
    refreshAllPanels,
    clearCurrent,

    // edit mode
    enterEdit,
    cancelEdit,
    saveEdit,
    upsertDraftPanel,
    removeDraftPanel,
    resizeDraftPanel,
    reorderDraftPanels,
    previewPanel,
    clearPreview,
  };
});
