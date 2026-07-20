import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "@/stores/base";
import { useMetaStore } from "@/stores/meta";
import type { DashboardCachePolicy } from "@/api/meta";
import {
  dashboardsApi,
  dashboardPanelApi,
  type Dashboard,
  type DashboardPanel,
  type DashboardPanels,
  type CreateDashboardRequest,
  type UpdateDashboardRequest,
  type HistogramRequestBody,
  type SqlQueryRequestBody,
  type PanelQueryLanguage,
  type CacheDirective,
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

// Table panel row cap. The builder's limit option maxes at 1000; a hand-edited
// or legacy blob could carry anything (a saved limit:100000 would try to render
// 100k rows), so the runtime clamps into [1, TABLE_LIMIT_MAX] regardless.
const DEFAULT_TABLE_LIMIT = 100;
const TABLE_LIMIT_MAX = 1000;

// A burst of time-picker emissions (partial date entry, quick-range flips) each
// mutate the range; coalesce them into a single panel refresh instead of firing
// a full refreshAllPanels per emission.
const REFRESH_COALESCE_MS = 80;

const DEFAULT_RELATIVE_TIME = "15m";

// UI fallback for the "Default" cache-TTL label shown in the edit toolbar when
// the server advertises no policy (old server). The authoritative default now
// comes from the server's dashboard_cache.default_ttl_seconds (see meta store);
// this constant is only the last-resort display fallback.
export const DEFAULT_DASHBOARD_CACHE_TTL_SECONDS = 600;
// Panel queries run in the viewer's browser timezone (falling back to UTC if
// it can't be resolved). This used to be hardcoded to UTC because the
// VictoriaLogs histogram path mis-formatted a non-UTC zone as an
// `offset=+05:30` clock string where VictoriaLogs expects a duration,
// causing a 400; that's fixed in internal/victorialogs/querying.go.
const PANEL_QUERY_TIMEZONE = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
})();

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
    range: { start: number; end: number };
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
  // The range actually EXECUTED by the most recent refresh (snapped/moving),
  // for toolbar display. Distinct from `effectiveRange` (the user's SELECTED
  // range). null until the first refresh runs.
  appliedRange: EffectiveRange | null;
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

// A UTC instant as RFC3339 (histogram + logs/query endpoint contract).
function msToRfc3339(ms: number): string {
  return new Date(ms).toISOString();
}

// A UTC instant as "YYYY-MM-DD HH:MM:SS" (logchefql translate/query endpoint
// contract). Unlike the histogram/logs-query endpoints, these two do NOT
// accept RFC3339 for ClickHouse-backed sources: the compiler bakes this
// string straight into `toDateTime(str, tz)` and its validator rejects
// anything but this exact layout (400 "invalid time format: expected
// 'YYYY-MM-DD HH:MM:SS'"); `/translate` is worse — it silently omits
// `full_sql` instead of erroring, which would run stat/timeseries panels
// against the wrong (empty) query. So this format stays.
function msToSqlDateTime(ms: number): string {
  return new Date(ms).toISOString().slice(0, 19).replace("T", " ");
}

// Because msToSqlDateTime always emits a UTC wall-clock string, the timezone
// used to interpret it must always be UTC too — sending the viewer's real
// IANA zone here (as PANEL_QUERY_TIMEZONE does for the RFC3339 endpoints)
// would tell the server to parse a UTC string as if it were local time in
// that zone, shifting the query window by the zone's offset. This is the
// bug this fix addresses: non-UTC viewers previously got a shifted window on
// the two logchefql endpoints.
const LOGCHEFQL_TIME_TIMEZONE = "UTC";

function isAbort(err: unknown): boolean {
  return (
    isCanceledError(err) ||
    (err instanceof Error && err.name === "AbortError") ||
    (typeof err === "object" && err !== null && (err as { error_type?: string }).error_type === "CanceledError")
  );
}

// Resolve a dashboard's EFFECTIVE result-cache TTL (seconds), combining the blob
// value with the server's advertised policy. Client and server apply the SAME
// clamp with the SAME max, so the snap bucket, the directive TTL, and the
// server-side cache TTL all coincide.
//
// FAILS CLOSED to 0 (no caching) whenever the policy is missing/disabled/
// malformed — this is deliberate. Guessing enabled/600 on a stale or old server
// would change the query-window semantics (snapping) and risk serving stale
// data against a cache the server may not even keep.
export function resolveEffectiveCacheTtl(
  blobTtl: number | null | undefined,
  policy: DashboardCachePolicy | null | undefined
): number {
  // Old/malformed server, or cache disabled → fail closed (no caching).
  if (!policy || policy.enabled !== true) return 0;
  if (!Number.isSafeInteger(policy.default_ttl_seconds) || policy.default_ttl_seconds < 1) return 0;
  if (!Number.isSafeInteger(policy.max_ttl_seconds) || policy.max_ttl_seconds < 1) return 0;

  if (blobTtl === 0) return 0; // explicit per-dashboard "off"
  const requested =
    blobTtl === undefined || blobTtl === null
      ? policy.default_ttl_seconds // dashboard didn't set one → server default
      : blobTtl;
  if (!Number.isSafeInteger(requested) || requested < 1) return 0; // fail closed on garbage
  return Math.min(requested, policy.max_ttl_seconds); // clamp to server max (same clamp the server applies)
}

// Compute the range actually executed for a refresh, given an injected `nowMs`.
// Pure + fake-clock testable. `baseStart`/`baseEnd` are the unsnapped range for
// this now (from the existing time parsing); `durationMs` is only meaningful for
// "rolling".
//
// - absolute → passthrough (never snapped; its key is naturally stable).
// - calendar (today/yesterday) → passthrough, PRESERVING calendar boundaries —
//   never subtract a rolling duration (which would move the day off its edge).
// - rolling + effTtl>0 → snap the end to the current TTL bucket so successive
//   refreshes within a bucket produce a byte-identical (cacheable) query.
// - rolling + effTtl===0 → no snap; a fresh moving window anchored on nowMs.
export function resolveAppliedRange(input: {
  kind: "rolling" | "calendar" | "absolute";
  baseStart: number;
  baseEnd: number;
  durationMs: number;
  effTtlSeconds: number;
  nowMs: number;
}): { start: number; end: number } {
  const { kind, baseStart, baseEnd, durationMs, effTtlSeconds, nowMs } = input;
  if (kind === "absolute" || kind === "calendar") {
    return { start: baseStart, end: baseEnd };
  }
  // rolling
  if (effTtlSeconds > 0) {
    const ttlMs = effTtlSeconds * 1000;
    const bucketEnd = Math.floor(nowMs / ttlMs) * ttlMs;
    return { start: bucketEnd - durationMs, end: bucketEnd };
  }
  return { start: nowMs - durationMs, end: nowMs };
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
    appliedRange: null,
    refreshIntervalMs: 0,
    isEditing: false,
    editDraft: null,
    editSnapshotJson: null,
    previewState: null,
  });

  // Server dashboard-cache policy source. Read fresh per refresh so a late
  // /meta load (or a policy change) is picked up without reloading the store.
  const metaStore = useMetaStore();

  // Non-reactive: the controller aborting the in-flight refresh batch.
  let refreshAbort: AbortController | null = null;
  // Non-reactive: the controller aborting an in-flight panel preview.
  let previewAbort: AbortController | null = null;
  // Non-reactive: controllers for single-panel executions (add/edit re-renders),
  // keyed by panel id so a re-run supersedes its own prior in-flight request and
  // teardown can cancel them all (fixes the previously-untracked controller).
  const singlePanelAborts = new Map<string, AbortController>();
  // Non-reactive: coalescing timer for time-range-driven refreshes.
  let refreshCoalesceTimer: ReturnType<typeof setTimeout> | null = null;
  // Non-reactive: monotonic load token. A loadDashboard response is applied only
  // if its token is still current; clearCurrent/unmount and newer loads bump it
  // so a slow/late GET can't repopulate the store for a dashboard we left.
  let loadToken = 0;
  // Non-reactive: identity + optimistic-concurrency baseline captured on
  // entering edit. `editDashboardId` guards saveEdit from ever PUTting one
  // dashboard's draft onto another id; `editBaseUpdatedAt` is the precondition
  // sent to the server for last-writer-wins conflict detection.
  let editDashboardId: number | null = null;
  let editBaseUpdatedAt: string | null = null;

  const dashboards = computed(() => state.data.value.dashboards);
  const current = computed(() => state.data.value.current);
  const panelStates = computed(() => state.data.value.panelStates);
  const timeRelative = computed(() => state.data.value.timeRelative);
  const timeAbsolute = computed(() => state.data.value.timeAbsolute);
  const appliedRange = computed(() => state.data.value.appliedRange);
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

  // Resolve the range to EXECUTE for a refresh, anchored on a caller-supplied
  // `nowMs` (captured once per refresh — do NOT read the memoized effectiveRange
  // here, that's what freezes `now`). Classifies the current selection, derives
  // the unsnapped base range from the existing time parsing, then delegates the
  // snap/moving-window decision to the pure resolveAppliedRange.
  function computeAppliedRange(nowMs: number, effTtlSeconds: number): EffectiveRange {
    const abs = state.data.value.timeAbsolute;
    if (abs) {
      return resolveAppliedRange({
        kind: "absolute",
        baseStart: abs.start,
        baseEnd: abs.end,
        durationMs: 0,
        effTtlSeconds,
        nowMs,
      });
    }
    const rel = state.data.value.timeRelative ?? DEFAULT_RELATIVE_TIME;
    const parse = (s: string) => {
      const { start, end } = parseRelativeTimeString(s);
      return { start: calendarDateTimeToTimestamp(start), end: calendarDateTimeToTimestamp(end) };
    };
    // today/yesterday are calendar-boundary presets — never snapped.
    const kind: "calendar" | "rolling" = rel === "today" || rel === "yesterday" ? "calendar" : "rolling";
    try {
      const { start, end } = parse(rel);
      return resolveAppliedRange({
        kind,
        baseStart: start,
        baseEnd: end,
        durationMs: end - start,
        effTtlSeconds,
        nowMs,
      });
    } catch {
      // Unparseable relative → fall back to the default rolling window.
      const { start, end } = parse(DEFAULT_RELATIVE_TIME);
      return resolveAppliedRange({
        kind: "rolling",
        baseStart: start,
        baseEnd: end,
        durationMs: end - start,
        effTtlSeconds,
        nowMs,
      });
    }
  }

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
    // Claim a load generation. The GET itself isn't abortable (shared API
    // surface), so the token is the guard: apply the response only if it's
    // still the newest load and the view hasn't been torn down.
    const token = ++loadToken;
    const result = await state.callApi<Dashboard>({
      apiCall: () => dashboardsApi.get(id),
      operationKey: "loadDashboard",
      showToast: false,
    });
    // A newer load started, or clearCurrent() ran (unmount / navigation).
    // Discard this stale GET so it can't repopulate the store or fire a panel
    // refresh for a dashboard we're no longer viewing.
    if (token !== loadToken) return result;
    if (result.success) {
      state.data.value.current = result.data ?? null;
      state.data.value.panelStates = {};
      if (state.data.value.current) {
        await refreshAllPanels();
      }
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

  // Collapse a burst of range changes into one refresh. refreshAllPanels still
  // aborts any in-flight batch, so the final scheduled call wins cleanly.
  function scheduleRefresh() {
    if (refreshCoalesceTimer !== null) clearTimeout(refreshCoalesceTimer);
    refreshCoalesceTimer = setTimeout(() => {
      refreshCoalesceTimer = null;
      void refreshAllPanels();
    }, REFRESH_COALESCE_MS);
  }

  function setRelativeTime(relative: string) {
    state.data.value.timeRelative = relative;
    state.data.value.timeAbsolute = null;
    scheduleRefresh();
  }

  function setAbsoluteRange(start: number, end: number) {
    state.data.value.timeAbsolute = { start, end };
    state.data.value.timeRelative = null;
    scheduleRefresh();
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
  // (SQL for ClickHouse sources, LogsQL for VictoriaLogs), plus the language of
  // that native text. ClickHouse-SQL and native LogsQL panels already carry the
  // source's native query — run them verbatim. Only LogchefQL goes through the
  // shared compile, exactly the multi-datasource path the explorer uses.
  // (A logsql panel used to fall through here and get compiled as LogchefQL,
  // silently breaking VictoriaLogs panels — see #119 A4.)
  async function resolveNativeQuery(
    panel: DashboardPanel,
    range: EffectiveRange,
    signal: AbortSignal
  ): Promise<{ query: string; language: PanelQueryLanguage }> {
    if (panel.query_language === "clickhouse-sql" || panel.query_language === "logsql") {
      return { query: panel.query, language: panel.query_language };
    }
    const resp = await dashboardPanelApi.translate(
      panel.team_id,
      panel.source_id,
      {
        query: panel.query,
        start_time: msToSqlDateTime(range.start),
        end_time: msToSqlDateTime(range.end),
        timezone: LOGCHEFQL_TIME_TIMEZONE,
        limit: panel.options?.limit,
      },
      signal
    );
    const translated = unwrap(resp);
    if (translated.valid === false) {
      throw { error_type: "ValidationError", message: translated.error?.message || "Invalid query" };
    }
    const query = translated.full_sql || translated.generated_query || translated.sql || panel.query;
    // The compiler reports the language it produced (SQL for ClickHouse, LogsQL
    // for VictoriaLogs); default to clickhouse-sql for older server builds.
    const language = (translated.generated_query_language as PanelQueryLanguage) || "clickhouse-sql";
    return { query, language };
  }

  // Clamp a table panel's row cap into [1, TABLE_LIMIT_MAX]; absent/invalid
  // falls back to the default. Guards against a legacy/hand-edited blob asking
  // to render tens of thousands of DOM rows.
  function clampTableLimit(value: number | undefined): number {
    if (!value || !Number.isFinite(value) || value <= 0) return DEFAULT_TABLE_LIMIT;
    return Math.min(Math.max(Math.floor(value), 1), TABLE_LIMIT_MAX);
  }

  async function fetchPanelData(
    panel: DashboardPanel,
    range: EffectiveRange,
    signal: AbortSignal,
    // Result-cache directive attached to the (cacheable) panel request bodies.
    // Undefined => caching off for this refresh; the `cache` field is omitted.
    cache?: CacheDirective
  ): Promise<PanelState> {
    const startIso = msToRfc3339(range.start);
    const endIso = msToRfc3339(range.end);

    if (panel.type === "timeseries" || panel.type === "stat") {
      const { query: queryText, language: nativeLanguage } = await resolveNativeQuery(panel, range, signal);
      const window = HistogramService.calculateOptimalGranularity(startIso, endIso);
      const groupBy = panel.type === "timeseries" ? panel.options?.group_by || undefined : undefined;

      // query_language is advisory today (the native endpoints interpret
      // query_text by source type) but is sent so a VictoriaLogs source runs
      // native LogsQL rather than being re-parsed as ClickHouse SQL.
      const histBody: HistogramRequestBody = {
        query_text: queryText,
        window,
        group_by: groupBy,
        start_time: startIso,
        end_time: endIso,
        timezone: PANEL_QUERY_TIMEZONE,
        query_language: nativeLanguage,
        ...(cache ? { cache } : {}),
      };
      const resp = await dashboardPanelApi.histogram(panel.team_id, panel.source_id, histBody, signal);
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
          range: { start: range.start, end: range.end },
        },
      };
    }

    // table
    const limit = clampTableLimit(panel.options?.limit);
    // Native queries (ClickHouse SQL, VictoriaLogs LogsQL) run verbatim through
    // the logs/query endpoint. Only LogchefQL takes the compile path below;
    // routing a logsql panel there used to break VictoriaLogs tables (#119 A4).
    if (panel.query_language === "clickhouse-sql" || panel.query_language === "logsql") {
      const sqlBody: SqlQueryRequestBody = {
        query_text: panel.query,
        limit,
        start_time: startIso,
        end_time: endIso,
        timezone: PANEL_QUERY_TIMEZONE,
        query_language: panel.query_language,
        ...(cache ? { cache } : {}),
      };
      const resp = await dashboardPanelApi.logsQuery(panel.team_id, panel.source_id, sqlBody, signal);
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
        timezone: LOGCHEFQL_TIME_TIMEZONE,
        limit,
        ...(cache ? { cache } : {}),
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

  async function executePanel(
    panel: DashboardPanel,
    range: EffectiveRange,
    signal: AbortSignal,
    cache?: CacheDirective
  ) {
    // Server-side redaction (B1 per-panel): a panel the viewer can't reach comes
    // back flagged `locked` with its query blanked. Render the locked state
    // directly and never fire a request — there is no query to run.
    if (panel.locked) {
      setPanelState(panel.id, { status: "locked" });
      return;
    }
    // Bail before writing "loading": a task pulled off the concurrency queue
    // after its batch was superseded must not stomp a completed panel back to
    // loading (and then never resolve because the request never fires).
    if (signal.aborted) return;
    setPanelState(panel.id, { status: "loading" });
    try {
      const result = await fetchPanelData(panel, range, signal, cache);
      if (signal.aborted) return;
      setPanelState(panel.id, result);
    } catch (err) {
      if (signal.aborted || isAbort(err)) return;
      const { locked, message } = classifyPanelError(err);
      setPanelState(panel.id, { status: locked ? "locked" : "error", error: message });
    }
  }

  function abortSinglePanels() {
    for (const controller of singlePanelAborts.values()) controller.abort();
    singlePanelAborts.clear();
  }

  async function refreshAllPanels() {
    const dashboard = state.data.value.current;
    if (!dashboard) return;

    // Cancel any in-flight batch so a rapid time-range change doesn't leave stale
    // requests racing the new ones. A full refresh also supersedes any tracked
    // single-panel executions (add/edit re-renders).
    abortSinglePanels();
    refreshAbort?.abort();
    const controller = new AbortController();
    refreshAbort = controller;
    const signal = controller.signal;

    // Capture the clock ONCE for this refresh. Everything downstream (snap
    // bucket, moving window) anchors on this — NOT on the memoized
    // effectiveRange computed, which keys on the selection string and so
    // freezes `now` between selection changes (the frozen-now auto-refresh bug).
    const nowMs = Date.now();

    // Resolve the dashboard's effective cache TTL once for the whole refresh,
    // combining the blob value with the server's advertised policy (fail-closed
    // to 0 when the server advertises no/disabled/malformed policy).
    const effTtl = resolveEffectiveCacheTtl(dashboard.panels?.cache_ttl_seconds, metaStore.dashboardCachePolicy);
    // Send the directive ONLY when caching is actually on.
    const cache: CacheDirective | undefined =
      effTtl > 0 ? { scope: "dashboard", ttl_seconds: effTtl } : undefined;

    // Compute the executed range ONCE so every panel in this refresh shares an
    // identical window — a prerequisite for cross-panel/-viewer cache collapse.
    // For a rolling relative with caching on, this snaps the window to the
    // current TTL bucket so successive refreshes within a bucket produce a
    // byte-identical query (and thus the same cache key). This MUST happen here,
    // upstream of resolveNativeQuery/translate, because ClickHouse bakes the
    // start/end timestamps into the compiled SQL — a snap applied only at
    // request time (after translation) would never reach the executed query.
    // today/yesterday (calendar) and absolute ranges pass through unsnapped;
    // when caching is off, rolling windows move with `nowMs`.
    const range = computeAppliedRange(nowMs, effTtl);
    // Publish what was actually queried for the toolbar to display.
    state.data.value.appliedRange = range;

    const panels = dashboard.panels?.panels ?? [];
    const tasks = panels.map((panel) => () => executePanel(panel, range, signal, cache));
    await runWithConcurrency(tasks, PANEL_FETCH_CONCURRENCY);
  }

  // ---- Edit mode ----------------------------------------------------------

  // Execute a single panel into panelStates (used after add/edit so the panel
  // renders immediately without a full-dashboard refresh).
  function executeSinglePanel(panel: DashboardPanel) {
    // Supersede any prior in-flight execution of the SAME panel (rapid edits)
    // and track the controller so teardown/refresh can cancel it.
    singlePanelAborts.get(panel.id)?.abort();
    const controller = new AbortController();
    singlePanelAborts.set(panel.id, controller);
    void executePanel(panel, effectiveRange.value, controller.signal).finally(() => {
      if (singlePanelAborts.get(panel.id) === controller) {
        singlePanelAborts.delete(panel.id);
      }
    });
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
    // Pin the dashboard this draft belongs to, and the version we branched from,
    // for the save-time identity guard (#119 A1) and concurrency check (A3).
    editDashboardId = dashboard.id;
    editBaseUpdatedAt = dashboard.updated_at ?? null;
  }

  function cancelEdit() {
    // The live dashboard (current.panels) was never mutated, so simply dropping
    // the draft reverts to the last-loaded state.
    previewAbort?.abort();
    previewAbort = null;
    abortSinglePanels();
    state.data.value.isEditing = false;
    state.data.value.editDraft = null;
    state.data.value.editSnapshotJson = null;
    state.data.value.previewState = null;
    editDashboardId = null;
    editBaseUpdatedAt = null;
  }

  function setDraft(next: DashboardPanels) {
    state.data.value.editDraft = next;
  }

  // Set the draft's per-dashboard cache TTL (seconds). 0 = off. Persisted in the
  // panels blob on save; no reflow needed since layout is unaffected.
  function setDraftCacheTtl(seconds: number) {
    const draft = state.data.value.editDraft;
    if (!draft) return;
    setDraft({ ...draft, cache_ttl_seconds: seconds });
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

  // Patch-merge a partial update into one draft panel (by id), then reflow.
  // `options` merges shallowly into the panel's existing options rather than
  // replacing the whole object, so callers only need to pass the fields that
  // changed. This is the ONLY way the panel builder drawer mutates a panel —
  // there is no detached local copy of the panel config.
  function updateDraftPanel(
    id: string,
    patch: Partial<Omit<DashboardPanel, "options">> & { options?: Partial<DashboardPanel["options"]> }
  ) {
    const draft = state.data.value.editDraft;
    if (!draft) return;
    const idx = draft.panels.findIndex((p) => p.id === id);
    if (idx < 0) return;
    const { options: optionsPatch, ...rest } = patch;
    const current = draft.panels[idx];
    const merged: DashboardPanel = {
      ...current,
      ...rest,
      options: optionsPatch ? { ...(current.options ?? {}), ...optionsPatch } : current.options,
    };
    const panels = [...draft.panels];
    panels[idx] = merged;
    setDraft(reflowPanels({ ...draft, panels }));

    // If anything that affects the executed query/result changed (e.g. a
    // stat→table type switch, a new query/source/limit), the cached panelState
    // is now stale — a stat value would render blank under a table view. Drop
    // it, then re-run the panel so the edit canvas reflects the new config.
    if (execSignature(current) !== execSignature(merged)) {
      const { [id]: _stale, ...rest2 } = state.data.value.panelStates;
      state.data.value.panelStates = rest2;
      if (merged.team_id > 0 && merged.source_id > 0) {
        executeSinglePanel(merged);
      }
    }
  }

  // Signature of the fields that determine a panel's fetched result. Two panels
  // with the same signature render identically, so panelState is reusable.
  function execSignature(p: DashboardPanel): string {
    return JSON.stringify([
      p.type,
      p.query,
      p.query_language,
      p.team_id,
      p.source_id,
      p.options?.limit ?? null,
      p.options?.group_by ?? null,
    ]);
  }

  // Create-on-canvas: push a bare panel shell into the draft immediately (so the
  // canvas reflects it at the next free slot via reflowPanels' default sizing)
  // and return its id. The caller opens the builder drawer on that id; the panel
  // query never executes until team_id + source_id are set (the drawer gates
  // preview on that, and saveEdit validates it before the round-trip).
  function createDraftShell(): string | null {
    const draft = state.data.value.editDraft;
    if (!draft) return null;
    const id =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `p-${Math.random().toString(36).slice(2, 10)}`;
    const panel: DashboardPanel = {
      id,
      title: "New panel",
      type: "timeseries",
      team_id: 0,
      source_id: 0,
      query: "",
      query_language: "logchefql",
      options: {},
    };
    const panels = [...draft.panels, panel];
    setDraft(reflowPanels({ ...draft, panels }));
    return id;
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
    // Identity guard (#119 A1): the draft belongs to whichever dashboard was
    // open when edit started. If `current` has since switched (e.g. a param
    // navigation that didn't clear the draft), refuse rather than PUT one
    // dashboard's panels onto another's id.
    if (editDashboardId !== null && dashboard.id !== editDashboardId) {
      return {
        success: false,
        error: {
          message:
            "The dashboard changed while you were editing. Your unsaved changes were not saved — re-open the original dashboard to edit it.",
        },
      };
    }
    // Mirror the server-side validation with a friendly, inline message; the
    // server 400 remains the authoritative backstop.
    const validationError = validatePanelsBlob(draft);
    if (validationError) {
      return { success: false, error: { message: validationError } };
    }
    // Optimistic-concurrency precondition (#119 A3): send the updated_at we
    // branched from so the server can reject a last-writer-wins clobber with a
    // 409. Sent as an extension field so it works whether or not the shared
    // request type has adopted it yet; older servers ignore the unknown field.
    const body: UpdateDashboardRequest & { updated_at?: string | null } = {
      name: dashboard.name,
      description: dashboard.description,
      panels: draft,
    };
    if (editBaseUpdatedAt) body.updated_at = editBaseUpdatedAt;
    const result = await state.callApi<Dashboard>({
      apiCall: () => dashboardsApi.update(dashboard.id, body),
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
        editDashboardId = null;
        editBaseUpdatedAt = null;
      },
    });
    if (result.success) {
      state.data.value.panelStates = {};
      await refreshAllPanels();
      return result;
    }
    // Surface a concurrent-edit conflict with actionable guidance instead of the
    // raw backend message. The draft is kept so the user can copy their work.
    if (result.error?.error_type === "ConflictError") {
      return {
        success: false,
        error: {
          message:
            "This dashboard was changed by someone else since you started editing. Reload the page to get the latest version, then re-apply your changes.",
        },
      };
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

  // Abort an in-flight preview WITHOUT clearing the shown result, so a debounced
  // re-preview can't have a stale request resolve into previewState during the
  // debounce window; the last result stays visible until the new one lands.
  function abortPreview() {
    previewAbort?.abort();
    previewAbort = null;
  }

  function clearCurrent() {
    // Bump the load token so any in-flight/late loadDashboard GET is discarded
    // rather than repopulating the store after we've navigated away.
    loadToken++;
    if (refreshCoalesceTimer !== null) {
      clearTimeout(refreshCoalesceTimer);
      refreshCoalesceTimer = null;
    }
    abortSinglePanels();
    refreshAbort?.abort();
    refreshAbort = null;
    previewAbort?.abort();
    previewAbort = null;
    state.data.value.current = null;
    state.data.value.panelStates = {};
    state.data.value.appliedRange = null;
    state.data.value.isEditing = false;
    state.data.value.editDraft = null;
    state.data.value.editSnapshotJson = null;
    state.data.value.previewState = null;
    editDashboardId = null;
    editBaseUpdatedAt = null;
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
    appliedRange,
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
    updateDraftPanel,
    setDraftCacheTtl,
    createDraftShell,
    removeDraftPanel,
    resizeDraftPanel,
    reorderDraftPanels,
    previewPanel,
    clearPreview,
    abortPreview,
  };
});
