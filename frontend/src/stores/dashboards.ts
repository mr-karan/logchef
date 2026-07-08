import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "@/stores/base";
import {
  dashboardsApi,
  dashboardPanelApi,
  type Dashboard,
  type DashboardPanel,
  type CreateDashboardRequest,
} from "@/api/dashboards";
import { isSuccessResponse, type APIResponse } from "@/api/types";
import { HistogramService, type HistogramData } from "@/services/HistogramService";
import { sumHistogramCounts } from "@/utils/dashboardPanels";
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
  });

  // Non-reactive: the controller aborting the in-flight refresh batch.
  let refreshAbort: AbortController | null = null;

  const dashboards = computed(() => state.data.value.dashboards);
  const current = computed(() => state.data.value.current);
  const panelStates = computed(() => state.data.value.panelStates);
  const timeRelative = computed(() => state.data.value.timeRelative);
  const timeAbsolute = computed(() => state.data.value.timeAbsolute);
  const refreshIntervalMs = computed(() => state.data.value.refreshIntervalMs);

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

  function clearCurrent() {
    refreshAbort?.abort();
    refreshAbort = null;
    state.data.value.current = null;
    state.data.value.panelStates = {};
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
  };
});
