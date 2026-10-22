import { defineStore } from "pinia";
import { computed, markRaw, ref, watch } from "vue";
import { exploreApi, buildTailUrl, subscribeToTail } from "@/api/explore";
import { logchefqlApi, type FilterCondition as CompiledFilterCondition } from "@/api/logchefql";
import { isCanceledError } from "@/api/error-handler";
import type {
  ColumnInfo,
  QueryStats,
  FilterCondition,
  QueryParams,
  QuerySharePayload,
  QueryShareResponse,
  LogContextRequest,
  LogContextResponse,
  QuerySuccessResponse,
  QueryWarning,
} from "@/api/explore";
import type { ResolvedSavedQuery, SavedQuery, SavedQueryContent } from "@/api/savedQueries";
import type { ExploreTarget } from "@/lib/exploreLocation";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useBaseStore } from "./base";
import { useExploreHistogramStore } from "./exploreHistogram";
import { useExploreAIStore } from "./exploreAI";
import { usePreferencesStore } from "@/stores/preferences";
import { generateDefaultSql } from '@/services/SqlManager';
import { quoteClickHouseIdentifier } from '@/utils/clickhouse';
import { useVariables } from "@/composables/useVariables";
import { prepareLogchefQLTemplate } from "@/utils/logchefql/template";
import { useVariableStore, type VariableState } from "@/stores/variables";
import { epochMs, resolveWindow, sameWindow, sqlTimeCondition, toRfc3339, type EpochMs, type ResolvedWindow, type TimeWindow } from '@/utils/timeWindow';
import { decodeDraftWindow, decodeStoredRange, encodeStoredRange, encodeWindowForUrl, type DraftTime } from '@/utils/timeWindowCodec';
import { asClickHouseConnection, getVictoriaLogsOptimizer, type Source } from '@/api/sources';
import { useExploreWindowedStore } from './exploreWindowed';
import { WindowedHTTPError } from '@/api/windowed';
import {
  getExploreModeForQueryLanguage,
  getNativeQueryLanguageForSource,
  hasSourceCapability,
  normalizeExploreMode,
  resolveSavedQueryMetadata,
  supportsQueryLanguage,
} from "@/lib/queryMetadata";

export interface ExecutionQuery {
  mode: 'logchefql' | 'native';
  text: string;
}

interface SavedQuerySnapshot {
  mode: "logchefql" | "native";
  queryContent: string;
  limit: number;
  window: TimeWindow;
}

type ExploreDraft = DraftTime & {
  mode: "logchefql" | "native";
  nativeQuery: string;
  logchefqlCode: string;
  limit: number;
  variables: VariableState[];
};

// The non-time fields of a stored draft, any version. The time goes through decodeDraftWindow.
type StoredDraftFields = Partial<Omit<ExploreDraft, "version" | "window">> & { version?: unknown };

const DEFAULT_WINDOW: TimeWindow = { kind: "relative", spec: "15m" };

/** Everything needed to show an Explorer location: every id checked, every fetch done. */
export interface OpenedLocation {
  teamId: number;
  // Null when the team has no source.
  sourceId: number | null;
  content:
    | { kind: "draft" }
    | Extract<ExploreTarget, { kind: "query" }>
    | { kind: "saved"; saved: ResolvedSavedQuery; queryContent: SavedQueryContent }
    | { kind: "share"; share: QueryShareResponse };
}

export interface ExploreState {
  logs: Record<string, any>[];
  columns: ColumnInfo[];
  queryStats: QueryStats;
  queryWarnings: QueryWarning[];
  limit: number;
  // Null until the Explorer opens a location, a saved query or a share.
  window: TimeWindow | null;
  filterConditions: FilterCondition[];
  nativeQuery: string;
  pendingRawSql?: string;
  displaySql?: string;
  logchefQuery?: string;
  logchefqlCode: string;
  activeMode: "logchefql" | "native";
  isLoading?: boolean;
  error?: string | null;
  queryId?: string | null;
  selectedQueryId: string | null;
  activeShareToken: string | null;
  activeShareSnapshot: string | null;
  activeSavedQueryName: string | null;
  savedQuerySnapshot: SavedQuerySnapshot | null;
  stats?: any;
  lastExecutedState?: {
    window: string;
    limit: number;
    mode: "logchefql" | "native";
    logchefqlQuery?: string;
    sqlQuery: string;
    sourceId: number;
    compilation?: {
      conditions: CompiledFilterCondition[];
      fieldsUsed: string[];
    };
  };
  lastExecutionTimestamp: number | null;
  hasExecutedQuery: boolean;
  generatedDisplayQuery: string | null;
  queryTimeout: number;
  currentQueryAbortController: AbortController | null;
  currentQueryId: string | null;
  isCancellingQuery: boolean;

  // Live tail (SSE). Kept separate from the normal query-result state above so
  // that stopping live tail restores the last static results untouched.
  isLive: boolean;
  liveRows: Record<string, any>[];
  liveStatus: LiveTailStatus;
  liveError: string | null;
  liveEndReason: string | null;
  liveEndMessage: string | null;
  liveNotice: string | null;
  liveDroppedCount: number;
  liveTailAbortController: AbortController | null;
}

export type LiveTailStatus = "idle" | "connecting" | "streaming" | "ended" | "error";

// In-memory tail buffer cap (oldest rows dropped) until explore virtualization
// lands. Matches the ceiling documented in the live-tail spec (#69).
const MAX_LIVE_ROWS = 500;

const DEFAULT_QUERY_STATS: QueryStats = {
  execution_time_ms: 0,
  rows_read: 0,
  bytes_read: 0,
};

const DRAFT_STORAGE_PREFIX = "logchef.explore.draft";

function inferColumnType(value: unknown): string {
  if (value === null || value === undefined) {
    return "String";
  }

  if (typeof value === "number") {
    return Number.isInteger(value) ? "Int64" : "Float64";
  }

  if (typeof value === "boolean") {
    return "Bool";
  }

  if (typeof value === "string") {
    const trimmed = value.trim();
    if (trimmed !== "" && !Number.isNaN(Date.parse(trimmed))) {
      return "DateTime64";
    }
    return "String";
  }

  if (Array.isArray(value)) {
    return "Array";
  }

  return "JSON";
}

function normalizeQueryColumns(
  columns: ColumnInfo[] | null | undefined,
  rows: Record<string, any>[] | null | undefined
): ColumnInfo[] {
  if (Array.isArray(columns) && columns.length > 0) {
    return columns;
  }

  if (!Array.isArray(rows) || rows.length === 0) {
    return [];
  }

  const sampledRows = rows.slice(0, 25);
  const inferredTypes = new Map<string, string>();

  for (const row of sampledRows) {
    for (const [key, value] of Object.entries(row)) {
      if (!inferredTypes.has(key) && value !== null && value !== undefined) {
        inferredTypes.set(key, inferColumnType(value));
      }
    }
  }

  return Object.keys(rows[0]).map((name) => ({
    name,
    type: inferredTypes.get(name) || "String",
  }));
}

function cloneVariables(variables: VariableState[]): VariableState[] {
  return variables.map((variable) => ({
    ...variable,
    value: Array.isArray(variable.value) ? [...variable.value] : variable.value,
    defaultValue: Array.isArray(variable.defaultValue)
      ? [...variable.defaultValue]
      : variable.defaultValue,
    options: variable.options?.map((option) => ({ ...option })),
  }));
}

export const useExploreStore = defineStore("explore", () => {
  const contextStore = useContextStore();
  const sourcesStore = useSourcesStore();
  const preferencesStore = usePreferencesStore();
  const histogramStore = useExploreHistogramStore();
  const windowedStore = useExploreWindowedStore();
  watch(() => [windowedStore.histogram, windowedStore.granularity, windowedStore.isCounting] as const, ([data, granularity, isLoading]) => {
    if (windowedStore.active && windowedStore.histogramEnabled) histogramStore.setWindowedHistogram(data, granularity, isLoading);
  });
  const aiStore = useExploreAIStore();
  
  const state = useBaseStore<ExploreState>({
    logs: [],
    columns: [],
    queryStats: DEFAULT_QUERY_STATS,
    queryWarnings: [],
    limit: 100,
    window: null,
    filterConditions: [],
    nativeQuery: "",
    logchefqlCode: "",
    activeMode: "logchefql",
    lastExecutionTimestamp: null,
    hasExecutedQuery: false,
    selectedQueryId: null,
    activeShareToken: null,
    activeShareSnapshot: null,
    activeSavedQueryName: null,
    savedQuerySnapshot: null,
    generatedDisplayQuery: null,
    queryTimeout: 30,
    currentQueryAbortController: null,
    currentQueryId: null,
    isCancellingQuery: false,
    isLive: false,
    liveRows: [],
    liveStatus: "idle",
    liveError: null,
    liveEndReason: null,
    liveEndMessage: null,
    liveNotice: null,
    liveDroppedCount: 0,
    liveTailAbortController: null,
  });

  // Monotonic token stamped on each executeQuery run. A response is only
  // applied if it's still the latest run AND the source hasn't changed since it
  // started — otherwise a slow response from a superseded run (or a run against
  // a source the user has since switched away from) would clobber the current
  // logs/columns/stats/history. See #101.
  let executeQueryToken = 0;

  const sourceId = computed(() => contextStore.sourceId || 0);
  const hasValidSource = computed(() => !!sourceId.value);
  const hasWindow = computed(() => state.data.value.window !== null);

  // The instant "now" means for a relative window. A run re-anchors it, so the
  // log query, histogram and Ask Chef all read one resolved window.
  const anchor = ref<EpochMs>(epochMs(Date.now()));
  const timeWindow = computed<TimeWindow>(() => state.data.value.window ?? DEFAULT_WINDOW);
  const resolved = computed<ResolvedWindow>(() => resolveWindow(timeWindow.value, preferencesStore.zone, anchor.value));

  function assignWindow(window: TimeWindow) {
    state.data.value.window = window;
    anchor.value = epochMs(Date.now());
  }
  const canExecuteQuery = computed(() => {
    if (!hasValidSource.value || !hasWindow.value) {
      return false;
    }
    return true;
  });
  // A Run counts from its validation on, so the editor stays read-only until it ends.
  const isValidatingQuery = ref(false);
  const isExecutingQuery = computed(() => isValidatingQuery.value || state.isLoadingOperation('executeQuery'));
  const canCancelQuery = computed(() => 
    (isValidatingQuery.value || !!state.data.value.currentQueryAbortController || !!state.data.value.currentQueryId) &&
    !state.data.value.isCancellingQuery && 
    isExecutingQuery.value
  );

  const getCurrentSource = () => sourcesStore.currentSourceDetails ?? null;
  const supportsLogchefQLForSource = (source = getCurrentSource()) => supportsQueryLanguage(source, "logchefql");
  const supportsClickHouseSQLForSource = (source = getCurrentSource()) =>
    getNativeQueryLanguageForSource(source) === "clickhouse-sql";
  const getDefaultModeForSource = (source = getCurrentSource()): "logchefql" | "native" =>
    supportsLogchefQLForSource(source) ? "logchefql" : "native";
  const normalizeModeForSource = (
    mode: "logchefql" | "native",
    source = getCurrentSource(),
  ): "logchefql" | "native" => (mode === "logchefql" && !supportsLogchefQLForSource(source) ? "native" : mode);
  const templateDialect = computed(() => state.data.value.activeMode === "logchefql"
    ? "logchefql" : getCurrentSource() ? getNativeQueryLanguageForSource(getCurrentSource()) : null);

  const isNativeHistogramSource = (source = getCurrentSource()) => getNativeQueryLanguageForSource(source) === "logsql";

  // Live tail: the current source must advertise the capability, and the tail
  // endpoint only accepts LogchefQL (either backend) or native logsql
  // (VictoriaLogs). Native clickhouse-sql is rejected 400 server-side, so the
  // toggle stays disabled there.
  const supportsLiveTail = computed(() => hasSourceCapability(getCurrentSource(), "live_tail"));
  const canArmLiveTail = computed(() => {
    if (!supportsLiveTail.value || !hasValidSource.value) return false;
    if (state.data.value.activeMode === "logchefql") return true;
    return getNativeQueryLanguageForSource(getCurrentSource()) === "logsql";
  });

  const isHistogramEligible = computed(() => {
    const source = getCurrentSource();
    if (getVictoriaLogsOptimizer(source)?.histogram_enabled === false) return false;
    const mode = state.data.value.lastExecutedState?.mode ?? state.data.value.activeMode;
    return mode === 'logchefql' || (mode === 'native' && isNativeHistogramSource(source));
  });
  const variableStore = useVariableStore();
  let suppressSharedVariableTracking = false;

  function currentDraftKey(): string | null {
    const teamId = useTeamsStore().currentTeamId;
    if (!teamId || !sourceId.value) {
      return null;
    }
    return `${DRAFT_STORAGE_PREFIX}.${teamId}.${sourceId.value}`;
  }

  function persistDraft() {
    const key = currentDraftKey();
    if (!key) {
      return;
    }

    const { activeMode, nativeQuery, logchefqlCode, limit } = state.data.value;
    const draft: ExploreDraft = {
      version: 2,
      window: state.data.value.window,
      mode: activeMode,
      nativeQuery,
      logchefqlCode,
      limit,
      variables: cloneVariables(variableStore.allVariables),
    };

    try {
      localStorage.setItem(key, JSON.stringify(draft));
    } catch (error) {
      console.warn("Failed to persist query draft:", error);
    }
  }

  function restoreDraftForCurrentContext(): boolean {
    const key = currentDraftKey();
    if (!key) {
      return false;
    }

    try {
      const rawDraft = localStorage.getItem(key);
      if (!rawDraft) {
        return false;
      }

      const draft = JSON.parse(rawDraft) as StoredDraftFields;
      if (draft.version !== 1 && draft.version !== 2) {
        return false;
      }
      const window = decodeDraftWindow(draft, preferencesStore.zone);

      state.data.value.activeMode = draft.mode ? normalizeExploreMode(draft.mode) : "logchefql";
      state.data.value.nativeQuery = draft.nativeQuery || "";
      state.data.value.logchefqlCode = draft.logchefqlCode || "";
      if (draft.limit && draft.limit > 0) {
        state.data.value.limit = draft.limit;
      }
      if (window) {
        assignWindow(window);
      }

      if (Array.isArray(draft.variables)) {
        variableStore.setAllVariable(draft.variables);
      } else {
        const { ensureVariablesFromSql } = useVariables();
        if (templateDialect.value) ensureVariablesFromSql(state.data.value.activeMode === "native" ? state.data.value.nativeQuery : state.data.value.logchefqlCode, templateDialect.value);
      }
      return true;
    } catch (error) {
      console.warn("Failed to restore query draft:", error);
      return false;
    }
  }

  function buildCurrentShareSnapshot(): string {
    const { activeMode, nativeQuery, logchefqlCode, limit, window } = state.data.value;
    return JSON.stringify({
      sourceId: sourceId.value,
      mode: activeMode,
      query: activeMode === "native" ? nativeQuery : logchefqlCode,
      limit,
      window,
      variables: cloneVariables(variableStore.allVariables),
    });
  }

  function clearActiveShareSelection() {
    state.data.value.activeShareToken = null;
    state.data.value.activeShareSnapshot = null;
  }

  function clearActiveSavedQuerySelection() {
    state.data.value.selectedQueryId = null;
    state.data.value.activeSavedQueryName = null;
    state.data.value.savedQuerySnapshot = null;
  }

  function clearShareSelectionIfDirty() {
    if (!state.data.value.activeShareToken || !state.data.value.activeShareSnapshot) {
      return;
    }

    if (buildCurrentShareSnapshot() !== state.data.value.activeShareSnapshot) {
      clearActiveShareSelection();
    }
  }

  const _buildDisplaySql = () => {
    const { logchefqlCode, limit } = state.data.value;

    if (!hasWindow.value) {
      return null;
    }

    const sourceDetails = sourcesStore.currentSourceDetails;
    if (!sourceDetails) {
      return null;
    }

    let tableName = 'default.logs';
    const chConn = asClickHouseConnection(sourceDetails.connection);
    if (chConn?.database && chConn?.table_name) {
      tableName = `${chConn.database}.${chConn.table_name}`;
    } else {
      return null;
    }

    const tsField = sourceDetails._meta_ts_field || 'timestamp';
    const timeCondition = sqlTimeCondition(tsField, resolved.value);

    const formattedTsField = quoteClickHouseIdentifier(tsField);
    const whereClause = logchefqlCode?.trim() 
      ? `WHERE ${timeCondition}\n  -- LogchefQL: ${logchefqlCode}`
      : `WHERE ${timeCondition}`;

    return {
      sql: [
        'SELECT *',
        `FROM ${tableName}`,
        whereClause,
        `ORDER BY ${formattedTsField} DESC`,
        `LIMIT ${limit}`
      ].join('\n'),
      error: undefined,
      warnings: []
    };
  };

  const _logchefQlTranslationResult = computed(() => {
    return _buildDisplaySql();
  });

  const sqlForExecution = computed(() => {
    const { activeMode, nativeQuery } = state.data.value;
    if (activeMode === 'native') {
      return nativeQuery;
    }

    const translationResult = _logchefQlTranslationResult.value;
    if (!translationResult) {
      return '';
    }
    return translationResult.sql;
  });

  const isQueryStateDirty = computed(() => {
    const { lastExecutedState, limit, activeMode, logchefqlCode, nativeQuery } = state.data.value;

    if (!lastExecutedState) {
      return (activeMode === 'logchefql' && !!logchefqlCode?.trim()) ||
             (activeMode === 'native' && !!nativeQuery?.trim());
    }

    const timeRangeChanged = JSON.stringify(timeWindow.value) !== lastExecutedState.window;
    const limitChanged = limit !== lastExecutedState.limit;
    const modeChanged = activeMode !== lastExecutedState.mode;
    const sourceChanged = sourceId.value !== lastExecutedState.sourceId;

    let queryContentChanged = false;
    if (activeMode === 'logchefql') {
      queryContentChanged = logchefqlCode !== lastExecutedState.logchefqlQuery;
    } else {
      queryContentChanged = sqlForExecution.value !== lastExecutedState.sqlQuery;
    }

    return timeRangeChanged || limitChanged || modeChanged || sourceChanged || queryContentChanged;
  });

  const editorQuery = computed<ExecutionQuery>(() => ({
    mode: state.data.value.activeMode,
    text: state.data.value.activeMode === 'native' ? state.data.value.nativeQuery : state.data.value.logchefqlCode,
  }));

  const hasDivergedFromSavedQuery = computed(() => {
    const { savedQuerySnapshot, selectedQueryId, activeMode, logchefqlCode, nativeQuery, limit } = state.data.value;
    
    if (!selectedQueryId || !savedQuerySnapshot) {
      return false;
    }

    const currentContent = activeMode === 'logchefql' ? logchefqlCode : nativeQuery;
    if (activeMode !== savedQuerySnapshot.mode || currentContent !== savedQuerySnapshot.queryContent) {
      return true;
    }

    if (limit !== savedQuerySnapshot.limit) {
      return true;
    }

    return !sameWindow(timeWindow.value, savedQuerySnapshot.window);
  });

  function queryParameters(query = editorQuery.value) {
    const {
      window,
      limit,
      selectedQueryId,
      activeShareToken,
    } = state.data.value;
    const teamsStore = useTeamsStore();

    const params: Record<string, string> = {};

    if (teamsStore.currentTeamId) {
      params.team = teamsStore.currentTeamId.toString();
    }

    if (sourceId.value) {
      params.source = sourceId.value.toString();
    }

    const isEditorQuery = query.mode === editorQuery.value.mode && query.text === editorQuery.value.text;
    if (isEditorQuery && selectedQueryId && !hasDivergedFromSavedQuery.value) {
      params.id = selectedQueryId;
      return params;
    }

    if (isEditorQuery && activeShareToken) {
      params.share = activeShareToken;
      return params;
    }

    if (window) {
      Object.assign(params, encodeWindowForUrl(window));
    }

    params.limit = limit.toString();
    // The query is always written, even when empty, so Back and copied links reopen it.
    params.mode = query.mode;
    if (query.mode === 'native') {
      params.sql = query.text;
    } else {
      params.q = query.text;
    }

    return params;
  }
  const urlQueryParameters = computed(() => queryParameters());

  // Drops the query in flight, so its response can't fill another location's
  // results. The token drops a response whose body already arrived; the abort
  // frees the socket early.
  function abortQuery() {
    executeQueryToken++;
    state.data.value.currentQueryAbortController?.abort();
    state.data.value.currentQueryAbortController = null;
    state.data.value.currentQueryId = null;
  }

  function clearResults() {
    windowedStore.reset(true);
    state.data.value.generatedDisplayQuery = null;
    state.data.value.logs = [];
    state.data.value.columns = [];
    state.data.value.queryStats = DEFAULT_QUERY_STATS;
    state.data.value.queryWarnings = [];

    histogramStore.clearHistogramData();
    histogramStore.setGroupByField(null);

    state.data.value.lastExecutionTimestamp = null;
    state.data.value.lastExecutedState = undefined;
    state.data.value.hasExecutedQuery = false;
  }

  /** Does not run. The Explorer navigator's setWindow runs and records it. */
  function setWindow(window: TimeWindow) {
    assignWindow(window);
    clearShareSelectionIfDirty();
    persistDraft();
  }

  /** Does not run. The Explorer navigator's setLimit runs and records it. */
  function setLimit(newLimit: number) {
    if (newLimit > 0) {
      state.data.value.limit = newLimit;
      clearShareSelectionIfDirty();
      persistDraft();
    }
  }

  function setQueryTimeout(timeout: number) {
    if (timeout > 0 && timeout <= 3600) {
      state.data.value.queryTimeout = timeout;
    }
  }

  function setLogchefqlCode(code: string) {
    state.data.value.logchefqlCode = code;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function setNativeQuery(sql: string) {
    state.data.value.nativeQuery = sql;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function setActiveMode(mode: 'logchefql' | 'native') {
    const currentMode = state.data.value.activeMode;
    const normalizedMode = normalizeModeForSource(mode);
    if (normalizedMode === currentMode) return;
    state.data.value.activeMode = normalizedMode;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function captureExecutionState(query: ExecutionQuery): NonNullable<ExploreState['lastExecutedState']> {
    const executedSql = query.mode === 'logchefql'
      ? (state.data.value.generatedDisplayQuery || '')
      : query.text;

    return {
      window: JSON.stringify(timeWindow.value),
      limit: state.data.value.limit,
      mode: query.mode,
      logchefqlQuery: query.mode === 'logchefql' ? query.text : undefined,
      sqlQuery: executedSql,
      sourceId: sourceId.value
    };
  }

  function _updateLastExecutedState(snapshot: NonNullable<ExploreState['lastExecutedState']>) {
    state.data.value.lastExecutedState = snapshot;
    state.data.value.lastExecutionTimestamp = Date.now();
  }

  // Mode and content the source can run. Raw LogchefQL is no native query,
  // so a source without LogchefQL drops it and runs its default SQL instead.
  function fitModeToSource(source: Source | null) {
    if (!source) return;
    if (!supportsLogchefQLForSource(source) && state.data.value.activeMode === 'logchefql') {
      state.data.value.logchefqlCode = '';
      state.data.value.nativeQuery = '';
      state.data.value.activeMode = 'native';
      return;
    }
    state.data.value.activeMode = normalizeModeForSource(state.data.value.activeMode, source);
  }

  function openDraft() {
    if (!state.data.value.window) assignWindow(DEFAULT_WINDOW);
    state.data.value.logchefqlCode = '';
    state.data.value.nativeQuery = '';
    state.data.value.activeMode = getDefaultModeForSource();
    restoreDraftForCurrentContext();
  }

  function openQuery({ mode, text, window, limit }: Extract<ExploreTarget, { kind: 'query' }>) {
    assignWindow(window ?? DEFAULT_WINDOW);
    if (limit && limit > 0) state.data.value.limit = limit;
    if (mode) state.data.value.activeMode = mode;
    state.data.value.logchefqlCode = state.data.value.activeMode === 'logchefql' ? text : '';
    state.data.value.nativeQuery = state.data.value.activeMode === 'native' ? text : '';
    if (text && templateDialect.value) useVariables().ensureVariablesFromSql(text, templateDialect.value);
  }

  function openSavedQuery(saved: ResolvedSavedQuery, content: SavedQueryContent) {
    const metadata = resolveSavedQueryMetadata({
      query_language: saved.query_language,
      editor_mode: saved.editor_mode,
      source_type: sourcesStore.currentSourceDetails?.source_type ?? "clickhouse",
      query_languages: sourcesStore.currentSourceDetails?.query_languages ?? [],
      saved_query_editor_modes: sourcesStore.currentSourceDetails?.saved_query_editor_modes ?? [],
    });
    // Route content by the RESOLVED active mode, not the query's own
    // language. When the source can't honour the saved language,
    // normalizeModeForSource coerces the mode; routing by the raw language
    // would drop the content into the hidden editor and run default SQL
    // instead. Clear the opposite slot so no stale content lingers behind the
    // hidden editor. See #105.
    const resolvedMode = normalizeModeForSource(getExploreModeForQueryLanguage(metadata.queryLanguage));

    state.data.value.activeMode = resolvedMode;
    state.data.value.activeSavedQueryName = saved.name;
    state.data.value.selectedQueryId = saved.id.toString();

    const queryContent = content.content || '';
    if (resolvedMode === 'logchefql') {
      state.data.value.logchefqlCode = queryContent;
      state.data.value.nativeQuery = '';
    } else {
      state.data.value.nativeQuery = queryContent;
      state.data.value.logchefqlCode = '';
    }

    const limit = content.limit || 100;
    state.data.value.limit = limit;

    // A saved query without a time range keeps the current window.
    const window = decodeStoredRange(content.timeRange, {
      format: "saved-query",
      version: typeof content.version === "number" ? content.version : 1,
      prefZone: preferencesStore.zone,
    }) ?? timeWindow.value;
    assignWindow(window);

    state.data.value.savedQuerySnapshot = { mode: resolvedMode, queryContent, limit, window };

    // Ensure variables from the query content are initialized in the variable store
    if (Array.isArray(content.variables)) {
      const normalizedVariables = content.variables.map((variable) => {
        const hasValue = variable.value !== '' && variable.value !== null && variable.value !== undefined;
        if (!hasValue && variable.defaultValue !== undefined && variable.defaultValue !== null && variable.defaultValue !== '') {
          return { ...variable, value: variable.defaultValue };
        }
        return variable;
      });
      variableStore.setAllVariable(normalizedVariables);
    } else {
      const { ensureVariablesFromSql } = useVariables();
      if (templateDialect.value) ensureVariablesFromSql(queryContent, templateDialect.value);
    }
  }

  function openShare(share: QueryShareResponse) {
    const payload = share.payload;

    state.data.value.activeShareToken = share.token;
    state.data.value.activeMode = payload.mode ? normalizeExploreMode(payload.mode) : "logchefql";

    if (state.data.value.activeMode === "native") {
      state.data.value.nativeQuery = payload.query || "";
      state.data.value.logchefqlCode = "";
    } else {
      state.data.value.logchefqlCode = payload.query || "";
      state.data.value.nativeQuery = "";
    }

    if (payload.limit > 0) {
      state.data.value.limit = payload.limit;
    }
    assignWindow(
      decodeStoredRange(payload.time_range, { format: "share", version: payload.version, timezone: payload.timezone }) ?? timeWindow.value,
    );

    suppressSharedVariableTracking = true;
    try {
      if (Array.isArray(payload.variables)) {
        variableStore.setAllVariable(payload.variables as VariableState[]);
      } else {
        const { ensureVariablesFromSql } = useVariables();
        if (templateDialect.value) ensureVariablesFromSql(payload.query || "", templateDialect.value);
      }
    } finally {
      suppressSharedVariableTracking = false;
    }
    state.data.value.activeShareSnapshot = buildCurrentShareSnapshot();
    persistDraft();
  }

  /**
   * Shows one location: selects its team and source, drops another source's
   * results and loads its content. Returns whether it has something to run.
   */
  function open(location: OpenedLocation): boolean {
    const sourceChanged = location.sourceId !== contextStore.sourceId;
    abortQuery();
    contextStore.selectTeam(location.teamId);
    if (location.sourceId) contextStore.selectSource(location.sourceId);
    else contextStore.clearSource();
    if (location.sourceId) contextStore.rememberCurrentSource();
    clearActiveShareSelection();
    clearActiveSavedQuerySelection();

    const { content } = location;
    if (content.kind === 'draft') openDraft();
    else if (content.kind === 'query') openQuery(content);
    else if (content.kind === 'saved') openSavedQuery(content.saved, content.queryContent);
    else openShare(content.share);
    fitModeToSource(getCurrentSource());

    // A link that names an empty SQL query opens it without running default SQL.
    const { activeMode, nativeQuery } = state.data.value;
    const runs = location.sourceId !== null && !(content.kind === 'query' && activeMode === 'native' && !nativeQuery);
    // Another source's results, or results nothing will replace, no longer match.
    if (sourceChanged || !runs) clearResults();
    clearError();
    return runs;
  }

  function _clearQueryContent() {
    state.data.value.logchefqlCode = '';
    state.data.value.nativeQuery = '';
    state.data.value.activeMode = getDefaultModeForSource();
    state.data.value.selectedQueryId = null;
    clearActiveShareSelection();
    state.data.value.activeSavedQueryName = null;
    state.data.value.savedQuerySnapshot = null;
    state.data.value.hasExecutedQuery = false;
  }

  function resetQueryToDefaults() {
    assignWindow(DEFAULT_WINDOW);
    state.data.value.limit = 100;

    _clearQueryContent();

    const sourceDetails = sourcesStore.currentSourceDetails;
    
    if (supportsClickHouseSQLForSource(sourceDetails)) {
      let tableName = 'logs.vector_logs';
      const chConn = asClickHouseConnection(sourceDetails?.connection);
      if (chConn?.database && chConn?.table_name) {
        tableName = `${chConn.database}.${chConn.table_name}`;
      }

      const timestampField = sourceDetails?._meta_ts_field || 'timestamp';
      const sql = generateDefaultSql({
        tableName,
        tsField: timestampField,
        window: resolved.value,
        limit: state.data.value.limit,
      });

      state.data.value.nativeQuery = sql;
    }

    histogramStore.clearHistogramData();
  }

  function resolveExecutionQuery(query = editorQuery.value): ExecutionQuery {
    const source = getCurrentSource();
    if (query.mode !== 'native' || query.text.trim() || !source || !supportsClickHouseSQLForSource(source)) return query;
    const connection = asClickHouseConnection(source.connection);
    const sql = generateDefaultSql({
      tableName: connection?.database && connection.table_name ? `${connection.database}.${connection.table_name}` : 'default.logs',
      tsField: source._meta_ts_field || 'timestamp',
      window: resolved.value,
      limit: state.data.value.limit,
    });
    if (state.data.value.activeMode === 'native' && !state.data.value.nativeQuery.trim()) {
      state.data.value.nativeQuery = sql;
    }
    return { mode: 'native', text: sql };
  }

  async function executeQuery(query = editorQuery.value) {
    windowedStore.reset();
    clearError();

    // A relative window means "up to now" at every run.
    if (state.data.value.window?.kind === 'relative') {
      anchor.value = epochMs(Date.now());
    }
    const run = resolved.value;
    query = resolveExecutionQuery(query);

    if (state.data.value.currentQueryAbortController) {
      state.data.value.currentQueryAbortController.abort();
    }

    // Stamp this run so late responses can be dropped if a newer run started or
    // the source changed while this one was in flight (#101).
    const requestToken = ++executeQueryToken;
    const requestSourceId = sourceId.value;
    const isStaleResponse = () =>
      requestToken !== executeQueryToken || sourceId.value !== requestSourceId;

    const abortController = new AbortController();
    state.data.value.currentQueryAbortController = abortController;
    state.data.value.isCancellingQuery = false;

    histogramStore.clearHistogramData();
    
    const operationKey = 'executeQuery';

    return await state.withLoading(operationKey, async () => {
      const currentTeamId = useTeamsStore().currentTeamId;
      if (!currentTeamId) {
        return state.handleError({ status: "error", message: "No team selected", error_type: "ValidationError" }, operationKey);
      }

      const sourceDetails = sourcesStore.currentSourceDetails;

      if (!sourceDetails || sourceDetails.id !== sourceId.value) {
        return { 
          success: false, 
          data: null, 
          error: { 
            message: "Source coordination in progress",
            error_type: "CoordinationError"
          }
        };
      }

      const teamSources = sourcesStore.teamSources || [];
      if (!teamSources.some(s => s.id === sourceId.value)) {
        return state.handleError({
          status: "error", 
          message: "Source does not belong to current team. Please refresh the page.",
          error_type: "ValidationError"
        }, operationKey);
      }

      // Mark that a query execution attempt has started (used for initial loading UX)
      state.data.value.hasExecutedQuery = true;

      const executionSnapshot = captureExecutionState(query);
      const optimizer = getVictoriaLogsOptimizer(sourceDetails);
      if (optimizer && hasWindow.value) {
        const { getVariablesForApi } = useVariables();
        state.data.value.logs = [];
        state.data.value.columns = [];
        try {
          await windowedStore.start({
            teamId: currentTeamId,
            sourceId: requestSourceId,
            histogram: optimizer.histogram_enabled !== false,
            request: {
              query_text: query.text,
              query_language: executionSnapshot.mode === 'logchefql' ? 'logchefql' : 'logsql',
              start_time: toRfc3339(run.start),
              end_time: toRfc3339(run.end),
              timezone: run.zone,
              limit: state.data.value.limit,
              query_timeout: state.data.value.queryTimeout,
              variables: getVariablesForApi(),
              extra_stream_filters: windowedStore.streamFilters,
            },
            onRows: (logs, stats) => {
              if (isStaleResponse()) return;
              state.data.value.logs = markRaw(logs);
              state.data.value.columns = normalizeQueryColumns([], logs);
              state.data.value.queryStats = stats ?? DEFAULT_QUERY_STATS;
              state.data.value.queryWarnings = [];
            },
          }, abortController.signal);
          const success = !isStaleResponse() && !abortController.signal.aborted && !windowedStore.error;
          if (success) { _updateLastExecutedState(executionSnapshot); persistDraft(); }
          return { success, data: null, error: windowedStore.error ? { message: windowedStore.error, error_type: 'DatabaseError' } : null };
        } catch (err) {
          if (!(err instanceof WindowedHTTPError && err.status === 422)) throw err;
        } finally {
          if (windowedStore.active && state.data.value.currentQueryAbortController === abortController) state.data.value.currentQueryAbortController = null;
        }
      }
      if (executionSnapshot.mode === 'logchefql') {
        try {
          const { getVariablesForApi } = useVariables();
          const variables = getVariablesForApi();

          const queryTimeout = state.data.value.queryTimeout;

          const queryResponse = await logchefqlApi.query(currentTeamId, sourceId.value, {
            extra_stream_filters: optimizer ? windowedStore.streamFilters : undefined,
            query: query.text,
            start_time: toRfc3339(run.start),
            end_time: toRfc3339(run.end),
            timezone: run.zone,
            limit: state.data.value.limit,
            query_timeout: queryTimeout,
            variables: variables.length > 0 ? variables : undefined
          }, { signal: abortController.signal, timeout: queryTimeout });

          // A newer run started, or the source changed, while this request was
          // in flight — drop the result instead of applying it under the wrong
          // source/run (#101).
          if (isStaleResponse()) {
            return { success: false, data: null, error: { message: 'Superseded by a newer query', error_type: 'StaleResponse' } };
          }

          if (queryResponse.data) {
            const logs = queryResponse.data.logs || [];
            state.data.value.logs = markRaw(logs);
            state.data.value.columns = normalizeQueryColumns(queryResponse.data.columns, logs);
            state.data.value.queryStats = queryResponse.data.stats || DEFAULT_QUERY_STATS;
            state.data.value.queryWarnings = queryResponse.data.warnings || [];

            if (queryResponse.data.query_id) {
              state.data.value.currentQueryId = queryResponse.data.query_id;
            }

            if (queryResponse.data.generated_query || queryResponse.data.generated_sql) {
              state.data.value.generatedDisplayQuery =
                queryResponse.data.generated_query || queryResponse.data.generated_sql || null;
            }

            _updateLastExecutedState({
              ...executionSnapshot,
              sqlQuery: queryResponse.data.generated_query || queryResponse.data.generated_sql || '',
              compilation: {
                conditions: queryResponse.data.conditions ?? [],
                fieldsUsed: queryResponse.data.fields_used ?? [],
              },
            });
            persistDraft();

            // Query history is recorded server-side on execution and surfaced
            // via GET /me/query-history (see QueryHistoryPanel) — no local write.

            void fetchHistogramData();

            return { success: true, data: queryResponse.data, error: null };
          } else {
            // Set error without toast - QueryError component displays it inline
            const apiError = { status: "error" as const, message: "Query execution failed", error_type: "DatabaseError" };
            state.error.value = apiError;
            return { success: false, data: null, error: apiError };
          }
        } catch (error: any) {
          if (isCanceledError(error) || error?.name === 'AbortError' || error?.name === 'CanceledError') {
            return { success: false, data: null, error: { message: 'Request canceled', error_type: 'CanceledError' } };
          }
          // Set error without toast - QueryError component displays it inline
          const apiError = { status: "error" as const, message: `LogchefQL query error: ${error.message}`, error_type: "DatabaseError" };
          state.error.value = apiError;
          return { success: false, data: null, error: apiError };
        } finally {
          // Only tear down the shared controller/query-id if they still belong
          // to this run. A superseded run that reaches finally after a newer
          // run installed its own controller must not clobber it (#101).
          if (state.data.value.currentQueryAbortController === abortController) {
            state.data.value.currentQueryAbortController = null;
            state.data.value.currentQueryId = null;
            state.data.value.isCancellingQuery = false;
          }
        }
      }

      const sql = query.text;

      const params: QueryParams = {
        extra_stream_filters: optimizer ? windowedStore.streamFilters : undefined,
        limit: optimizer ? state.data.value.limit : undefined,
        query_text: '',
        query_timeout: state.data.value.queryTimeout,
        start_time: toRfc3339(run.start),
        end_time: toRfc3339(run.end),
        timezone: run.zone,
      };

      // Pass variables to backend for server-side substitution (safer, handles escaping)
      const { getVariablesForApi } = useVariables();
      const variables = getVariablesForApi();

      params.query_text = sql;
      if (variables.length > 0) {
        params.variables = variables;
      }

      let response;
      
      try {
        response = await state.callApi({
          apiCall: async () => exploreApi.getLogs(sourceId.value, params, currentTeamId, abortController.signal),
          showToast: false, // Errors shown inline via QueryError component
          onSuccess: (data: QuerySuccessResponse | null) => {
            // Drop a superseded/stale response so it can't populate logs,
            // columns, stats or history under a source the user switched to
            // mid-flight (#101).
            if (isStaleResponse()) {
              return;
            }
            if (data && (data.data || data.logs)) {
              const logs = data.data || data.logs || [];
              state.data.value.logs = markRaw(logs);
              state.data.value.columns = normalizeQueryColumns(data.columns, logs);
              state.data.value.queryStats = data.stats || DEFAULT_QUERY_STATS;
              state.data.value.queryWarnings = data.warnings || [];
              if (data.params && typeof data.params === 'object' && "query_id" in data.params) {
                state.data.value.queryId = data.params.query_id as string;
              } else {
                state.data.value.queryId = null;
              }
            } else {
              state.data.value.logs = [];
              state.data.value.columns = [];
              state.data.value.queryStats = DEFAULT_QUERY_STATS;
              state.data.value.queryWarnings = [];
              state.data.value.queryId = null;
            }
            
            if (data && data.query_id) {
              state.data.value.currentQueryId = data.query_id;
            }

            _updateLastExecutedState({ ...executionSnapshot, sqlQuery: sql });
            persistDraft();

            // Query history is recorded server-side on execution and surfaced
            // via GET /me/query-history (see QueryHistoryPanel) — no local write.

            if (isHistogramEligible.value) {
              void fetchHistogramData();
            }
          },
          operationKey: operationKey,
        });
      } finally {
        // Only tear down if the shared controller still belongs to this run —
        // a superseded run must not clobber a newer run's controller (#101).
        if (state.data.value.currentQueryAbortController === abortController) {
          state.data.value.currentQueryAbortController = null;
          // Only clear queryId if not mid-cancellation — cancelQuery needs it for backend KILL QUERY
          if (!state.data.value.isCancellingQuery) {
            state.data.value.currentQueryId = null;
          }
        }
      }

      return response;
    });
  }

  async function cancelQuery() {
    if (windowedStore.active) windowedStore.stop();
    if (state.data.value.isCancellingQuery) {
      return;
    }
    
    if (!state.data.value.currentQueryAbortController && !state.data.value.currentQueryId) {
      return;
    }

    state.data.value.isCancellingQuery = true;
    
    try {
      if (state.data.value.currentQueryAbortController) {
        state.data.value.currentQueryAbortController.abort();
      }

      if (state.data.value.currentQueryId) {
        const currentTeamId = useTeamsStore().currentTeamId;
        if (currentTeamId && sourceId.value) {
          try {
            await exploreApi.cancelQuery(
              sourceId.value,
              state.data.value.currentQueryId,
              currentTeamId
            );
          } catch (error) {
            console.warn("Backend query cancellation failed, but HTTP request was aborted:", error);
          }
        }
      }
    } catch (error) {
      console.error("An error occurred during the cancellation process:", error);
    } finally {
      state.data.value.currentQueryId = null;
      state.data.value.isCancellingQuery = false;
    }
  }

  // --- Live tail ----------------------------------------------------------

  function appendLiveRows(rows: Record<string, any>[]) {
    if (!rows?.length) return;
    // Backend batches are oldest-first; the tail view shows newest-at-top, so
    // reverse each batch before prepending. Cap the buffer, dropping the oldest.
    const reversed = [...rows].reverse();
    const merged = [...reversed, ...state.data.value.liveRows];
    state.data.value.liveRows =
      merged.length > MAX_LIVE_ROWS ? merged.slice(0, MAX_LIVE_ROWS) : merged;
  }

  /**
   * Abort any in-flight tail stream and leave live mode. Idempotent. Does not
   * touch the static query results (logs/columns), so the last static view is
   * restored when the tail view is hidden.
   */
  function stopLiveTail() {
    if (state.data.value.liveTailAbortController) {
      state.data.value.liveTailAbortController.abort();
      state.data.value.liveTailAbortController = null;
    }
    state.data.value.isLive = false;
    state.data.value.liveStatus = "idle";
  }

  function startLiveTail() {
    const source = getCurrentSource();
    if (!canArmLiveTail.value || !source) return;

    const currentTeamId = useTeamsStore().currentTeamId;
    const sid = sourceId.value;
    if (!currentTeamId || !sid) return;

    let queryLanguage: "logchefql" | "logsql";
    let queryText: string;
    if (state.data.value.activeMode === "logchefql") {
      queryLanguage = "logchefql";
      queryText = state.data.value.logchefqlCode || "";
    } else {
      // canArmLiveTail already guaranteed native === logsql here.
      queryLanguage = "logsql";
      queryText = state.data.value.nativeQuery || "";
    }

    // Abort any prior stream and reset the buffer for a fresh session.
    stopLiveTail();
    state.data.value.liveRows = [];
    state.data.value.liveNotice = null;
    state.data.value.liveDroppedCount = 0;
    state.data.value.liveEndReason = null;
    state.data.value.liveEndMessage = null;
    state.data.value.liveError = null;
    state.data.value.isLive = true;
    state.data.value.liveStatus = "connecting";

    const controller = new AbortController();
    state.data.value.liveTailAbortController = controller;
    const url = buildTailUrl(currentTeamId, sid, queryText, queryLanguage);

    const isActive = () => state.data.value.liveTailAbortController === controller;

    subscribeToTail(url, controller.signal, {
      onOpen: () => {
        if (isActive()) state.data.value.liveStatus = "streaming";
      },
      onRows: (rows) => {
        if (isActive()) appendLiveRows(rows);
      },
      onNotice: (notice) => {
        if (!isActive()) return;
        state.data.value.liveNotice = notice.message || "Live tail rate-limited";
        // Prefer the server's cumulative count when present (newer backends).
        // Older backends only send the human message, so fall back to
        // extracting the per-notice count and accumulating client-side.
        if (typeof notice.dropped_total === "number" && Number.isFinite(notice.dropped_total)) {
          state.data.value.liveDroppedCount = notice.dropped_total;
        } else {
          const match = /(\d+)/.exec(notice.message || "");
          if (match) state.data.value.liveDroppedCount += parseInt(match[1], 10);
        }
      },
      onEnd: (end) => {
        if (!isActive()) return;
        // TTL / completion / abnormal close: keep isLive true so the view can
        // offer "resume", but the stream itself is finished — release the
        // controller. `reason` may be a value this build doesn't recognize
        // (e.g. a new VL connection-lost reason) — it's rendered as opaque
        // text, so unknown values degrade gracefully rather than crashing.
        state.data.value.liveStatus = "ended";
        state.data.value.liveEndReason = end.reason || "ended";
        state.data.value.liveEndMessage = end.message || null;
        state.data.value.liveTailAbortController = null;
      },
    })
      .then(() => {
        if (isActive()) state.data.value.liveTailAbortController = null;
      })
      .catch((err) => {
        // Aborts (toggle off, source/query change, unmount) resolve silently;
        // guard against stale controllers finishing after a restart.
        if (controller.signal.aborted || !isActive()) return;
        state.data.value.liveStatus = "error";
        state.data.value.liveError = err instanceof Error ? err.message : String(err);
        state.data.value.liveTailAbortController = null;
      });
  }

  function toggleLiveTail() {
    if (state.data.value.isLive) {
      stopLiveTail();
    } else {
      startLiveTail();
    }
  }

  function setFilterConditions(filters: FilterCondition[]) {
    const isForceClearing =
      filters.length === 1 && "_force_clear" in filters[0];

    if (isForceClearing) {
      state.data.value.filterConditions = [];
    } else {
      state.data.value.filterConditions = filters;
    }
  }

  /** Marks the shown query as the saved query it was just saved as. */
  function markSavedQuery(saved: Pick<SavedQuery, 'id' | 'name' | 'query_content' | 'query_language'>) {
    state.data.value.selectedQueryId = String(saved.id);
    state.data.value.activeSavedQueryName = saved.name;
    const content: SavedQueryContent = JSON.parse(saved.query_content);
    state.data.value.savedQuerySnapshot = {
      mode: normalizeModeForSource(getExploreModeForQueryLanguage(saved.query_language)),
      queryContent: content.content || '',
      limit: content.limit || 100,
      window: decodeStoredRange(content.timeRange, {
        format: 'saved-query', version: content.version, prefZone: preferencesStore.zone,
      }) ?? timeWindow.value,
    };
  }

  async function getLogContext(sourceId: number, params: LogContextRequest) {
    const operationKey = `getLogContext-${sourceId}`;
    return await state.withLoading(operationKey, async () => {
      if (!sourceId) {
        return state.handleError(
          { status: "error", message: "Source ID is required", error_type: "ValidationError" },
          operationKey
        );
      }
      const teamsStore = useTeamsStore();
      const currentTeamId = teamsStore.currentTeamId;
      if (!currentTeamId) {
        return state.handleError(
          { status: "error", message: "No team selected.", error_type: "ValidationError" },
          operationKey
        );
      }
      return await state.callApi<LogContextResponse>({
        apiCall: () => exploreApi.getLogContext(sourceId, params, currentTeamId),
        operationKey: operationKey,
        showToast: false,
      });
    });
  }

  function buildQuerySharePayload(): QuerySharePayload {
    const { activeMode, nativeQuery, logchefqlCode, limit, window } = state.data.value;
    const stored = window ? encodeStoredRange(window) : null;
    return {
      version: 2,
      mode: activeMode === "native" ? "sql" : "logchefql",
      query: activeMode === "native" ? nativeQuery : logchefqlCode,
      limit,
      timezone: resolved.value.zone,
      variables: variableStore.allVariables as unknown as QuerySharePayload["variables"],
      ...(stored ? { time_range: stored.timeRange } : {}),
    };
  }

  /** Creates a share link for the shown query. setActiveShareToken marks the query with it. */
  async function createQueryShare() {
    const currentTeamId = useTeamsStore().currentTeamId;
    if (!currentTeamId || !sourceId.value) {
      throw new Error("Team and source are required to share a query");
    }

    const payload = buildQuerySharePayload();
    if (payload.mode === "sql" && !payload.query.trim()) {
      throw new Error("Query is required to create a share link");
    }

    return (await exploreApi.createQueryShare(sourceId.value, payload, currentTeamId)).data ?? null;
  }

  function setActiveShareToken(token: string | null) {
    state.data.value.activeShareToken = token;
    state.data.value.activeShareSnapshot = token ? buildCurrentShareSnapshot() : null;
  }

  function clearError() {
    state.error.value = null;
  }

  function setQueryError(message: string) {
    state.error.value = { status: "error", message, error_type: "ValidationError" };
  }

  /**
   * Checks the editor's LogchefQL before a Run and shows why it is invalid.
   * False also when a run, an open or an edit came first, so a Run never
   * executes text other than the text it validated.
   */
  async function validateQuery(isCurrent = () => true): Promise<boolean> {
    clearError();
    const query = state.data.value.logchefqlCode;
    const teamId = useTeamsStore().currentTeamId;
    const source = sourceId.value;
    if (state.data.value.activeMode !== 'logchefql' || !query.trim() || !teamId || !source) return true;
    let invalid: string | null = null;
    try {
      const { data } = await logchefqlApi.validate(teamId, source, prepareLogchefQLTemplate(query));
      invalid = data && !data.valid ? data.error?.message || 'Invalid LogchefQL syntax' : null;
    } catch (error) {
      // The query request reports validation failures when this check is unavailable.
      console.warn("LogchefQL validation API error:", error);
    }
    if (!isCurrent() || query !== state.data.value.logchefqlCode) return false;
    if (invalid) setQueryError(invalid);
    return !invalid;
  }

  // Histogram delegation
  async function setGroupByField(field: string | null) {
    histogramStore.setGroupByField(field);
    if (isHistogramEligible.value && state.data.value.hasExecutedQuery) {
      await fetchHistogramData();
    }
  }

  async function fetchHistogramData(granularity?: string) {
    if (windowedStore.active) return { success: true, data: null };
    if (!isHistogramEligible.value) {
      histogramStore.clearHistogramData();
      return { success: false, error: { message: "Histogram is not available for this query mode" } };
    }

    let queryText = "";
    if (state.data.value.lastExecutedState?.mode === 'logchefql') {
      queryText = state.data.value.generatedDisplayQuery || "";
    } else if (isNativeHistogramSource()) {
      queryText = state.data.value.lastExecutedState?.sqlQuery.trim() || "*";
    }

    if (!queryText) {
      histogramStore.clearHistogramData();
      return { success: false, error: { message: "Run a query first to see the histogram" } };
    }

    return histogramStore.fetchHistogramData({
      extraStreamFilters: getVictoriaLogsOptimizer(getCurrentSource()) ? windowedStore.streamFilters : undefined,
      queryText,
      window: resolved.value,
      queryTimeout: state.data.value.queryTimeout,
      granularity,
    });
  }

  // AI delegation
  async function generateAiSql(naturalLanguageQuery: string, currentQuery?: string) {
    // The concrete target language is derived server-side from the source
    // backend + this editor mode; forward the explorer's current mode.
    const mode = state.data.value.activeMode === 'logchefql' ? 'logchefql' : 'native';
    const result = await aiStore.generateAiSql(naturalLanguageQuery, currentQuery, mode);

    if (result.success && aiStore.generatedAiQuery?.mode === 'native') {
      state.data.value.nativeQuery = aiStore.generatedAiQuery.query;
    }

    return result;
  }

  function clearAiSqlState() {
    aiStore.clearState();
  }

  watch(
    () => variableStore.allVariables,
    () => {
      if (suppressSharedVariableTracking || !state.data.value.activeShareToken) {
        return;
      }

      clearShareSelectionIfDirty();
      persistDraft();
    },
    { deep: true }
  );
  
  // Exit paths that MUST abort an in-flight tail. Toggle-off, route change and
  // unmount are handled by the caller (they invoke stopLiveTail directly); these
  // watchers cover source change and any query/mode edit while live.
  watch(
    () => sourceId.value,
    () => {
      if (state.data.value.isLive) stopLiveTail();
    }
  );
  watch(
    () =>
      [
        state.data.value.activeMode,
        state.data.value.logchefqlCode,
        state.data.value.nativeQuery,
      ] as const,
    () => {
      if (state.data.value.isLive) stopLiveTail();
    }
  );

  return {
    // State
    logs: computed(() => state.data.value.logs),
    columns: computed(() => state.data.value.columns),
    queryStats: computed(() => state.data.value.queryStats),
    queryWarnings: computed(() => state.data.value.queryWarnings),
    sourceId,
    limit: computed(() => state.data.value.limit),
    queryTimeout: computed(() => state.data.value.queryTimeout),
    window: timeWindow,
    resolved,
    hasWindow,
    filterConditions: computed(() => state.data.value.filterConditions),
    nativeQuery: computed(() => state.data.value.nativeQuery),
    logchefqlCode: computed(() => state.data.value.logchefqlCode),
    activeMode: computed(() => state.data.value.activeMode),
    templateDialect,
    error: state.error,
    queryId: computed(() => state.data.value.queryId),
    lastExecutedState: computed(() => state.data.value.lastExecutedState),
    lastExecutionTimestamp: computed(() => state.data.value.lastExecutionTimestamp),
    hasExecutedQuery: computed(() => state.data.value.hasExecutedQuery),
    selectedQueryId: computed(() => state.data.value.selectedQueryId),
    activeShareToken: computed(() => state.data.value.activeShareToken),
    activeSavedQueryName: computed(() => state.data.value.activeSavedQueryName),
    generatedDisplayQuery: computed(() => state.data.value.generatedDisplayQuery),

    // AI state (delegated)
    isGeneratingAISQL: computed(() => aiStore.isGeneratingAISQL),
    aiSqlError: computed(() => aiStore.aiSqlError),
    generatedAiQuery: computed(() => aiStore.generatedAiQuery),

    // Histogram state (delegated)
    histogramData: computed(() => histogramStore.histogramData),
    isLoadingHistogram: computed(() => histogramStore.isLoadingHistogram),
    histogramError: computed(() => histogramStore.histogramError),
    histogramNotice: computed(() => histogramStore.histogramNotice),
    histogramGranularity: computed(() => histogramStore.histogramGranularity),
    groupByField: computed(() => histogramStore.groupByField),

    // Loading state
    isLoading: state.isLoading,

    logchefQlTranslationResult: _logchefQlTranslationResult,
    sqlForExecution,
    isQueryStateDirty,
    hasDivergedFromSavedQuery,
    editorQuery,
    resolveExecutionQuery,
    queryParameters,
    urlQueryParameters,
    canExecuteQuery,

    // Actions
    open,
    setWindow,
    setLimit,
    setQueryTimeout,
    setFilterConditions,
    setNativeQuery,
    setActiveMode,
    setLogchefqlCode,
    clearActiveSavedQuerySelection,
    markSavedQuery,
    resetQueryToDefaults,
    executeQuery,
    cancelQuery,
    getLogContext,
    createQueryShare,
    setActiveShareToken,
    clearError,
    setQueryError,
    validateQuery,
    setGroupByField,
    generateAiSql,
    clearAiSqlState,
    fetchHistogramData,

    // Live tail state
    isLive: computed(() => state.data.value.isLive),
    liveRows: computed(() => state.data.value.liveRows),
    liveStatus: computed(() => state.data.value.liveStatus),
    liveError: computed(() => state.data.value.liveError),
    liveEndReason: computed(() => state.data.value.liveEndReason),
    liveEndMessage: computed(() => state.data.value.liveEndMessage),
    liveNotice: computed(() => state.data.value.liveNotice),
    liveDroppedCount: computed(() => state.data.value.liveDroppedCount),
    supportsLiveTail,
    canArmLiveTail,

    // Live tail actions
    startLiveTail,
    stopLiveTail,
    toggleLiveTail,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
    isValidatingQuery,
    isExecutingQuery,
    canCancelQuery,
    isCancellingQuery: computed(() => state.data.value.isCancellingQuery),
    
    // Histogram eligibility
    isHistogramEligible,
  };
});
