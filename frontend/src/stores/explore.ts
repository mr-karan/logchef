import { defineStore } from "pinia";
import { computed, watch } from "vue";
import { exploreApi, buildTailUrl, subscribeToTail } from "@/api/explore";
import { logchefqlApi } from "@/api/logchefql";
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
import type { SavedQueryContent } from "@/api/savedQueries";
import type { DateValue } from "@internationalized/date";
import { now, getLocalTimeZone, CalendarDateTime } from "@internationalized/date";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useBaseStore } from "./base";
import { useExploreHistogramStore } from "./exploreHistogram";
import { useExploreAIStore } from "./exploreAI";
import { usePreferencesStore } from "@/stores/preferences";
import { parseRelativeTimeString, timestampToCalendarDateTime, calendarDateTimeToTimestamp } from "@/utils/time";
import { SqlManager } from '@/services/SqlManager';
import { type TimeRange } from '@/types/query';
import { useVariables } from "@/composables/useVariables";
import { useVariableStore, type VariableState } from "@/stores/variables";
import { createTimeRangeCondition } from '@/utils/time-utils';
import { asClickHouseConnection } from '@/api/sources';
import {
  getExploreModeForQueryLanguage,
  getNativeQueryLanguageForSource,
  hasSourceCapability,
  normalizeExploreMode,
  resolveSavedQueryMetadata,
  supportsQueryLanguage,
} from "@/lib/queryMetadata";

interface SavedQuerySnapshot {
  queryContent: string;
  limit: number;
  relativeTime: string | null;
  absoluteStart: number | null;
  absoluteEnd: number | null;
}

interface ExploreDraft {
  version: number;
  mode: "logchefql" | "native";
  nativeQuery: string;
  logchefqlCode: string;
  limit: number;
  relativeTime: string | null;
  absoluteStart: number | null;
  absoluteEnd: number | null;
  timezone: string | null;
  variables: VariableState[];
}

export interface ExploreState {
  logs: Record<string, any>[];
  columns: ColumnInfo[];
  queryStats: QueryStats;
  queryWarnings: QueryWarning[];
  limit: number;
  timeRange: {
    start: DateValue;
    end: DateValue;
  } | null;
  selectedRelativeTime: string | null;
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
    timeRange: string;
    limit: number;
    mode: "logchefql" | "native";
    logchefqlQuery?: string;
    sqlQuery: string;
    sourceId: number;
  };
  lastExecutionTimestamp: number | null;
  hasExecutedQuery: boolean;
  selectedTimezoneIdentifier: string | null;
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
  const aiStore = useExploreAIStore();
  
  const state = useBaseStore<ExploreState>({
    logs: [],
    columns: [],
    queryStats: DEFAULT_QUERY_STATS,
    queryWarnings: [],
    limit: 100,
    timeRange: null,
    selectedRelativeTime: null,
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
    selectedTimezoneIdentifier: null,
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

  let suppressedSourceResetId: number | null = null;

  // Monotonic token stamped on each executeQuery run. A response is only
  // applied if it's still the latest run AND the source hasn't changed since it
  // started — otherwise a slow response from a superseded run (or a run against
  // a source the user has since switched away from) would clobber the current
  // logs/columns/stats/history. See #101.
  let executeQueryToken = 0;

  // Suppresses persistDraft() side-effects while initializeFromUrl restores a
  // saved draft. Without this, the setRelativeTimeRange/setLimit calls during
  // init persist an empty draft (query text not yet restored) and clobber the
  // real draft before restoreDraftForCurrentContext() reads it back. See #102.
  let suppressDraftPersistence = false;

  watch(
    () => contextStore.sourceId,
    (newSourceId, oldSourceId) => {
      if (newSourceId !== oldSourceId) {
        if (suppressedSourceResetId !== null && newSourceId === suppressedSourceResetId) {
          suppressedSourceResetId = null;
          return;
        }
        suppressedSourceResetId = null;
        onSourceChange(newSourceId || 0);
      }
    }
  );

  const sourceId = computed(() => contextStore.sourceId || 0);
  const hasValidSource = computed(() => !!sourceId.value);
  const hasValidTimeRange = computed(() => !!state.data.value.timeRange);
  const canExecuteQuery = computed(() => {
    if (!hasValidSource.value || !hasValidTimeRange.value) {
      return false;
    }
    return true;
  });
  const isExecutingQuery = computed(() => state.isLoadingOperation('executeQuery'));
  const canCancelQuery = computed(() => 
    (!!state.data.value.currentQueryAbortController || !!state.data.value.currentQueryId) && 
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
    return (
      state.data.value.activeMode === 'logchefql' ||
      (state.data.value.activeMode === 'native' && isNativeHistogramSource(source))
    );
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
    if (suppressDraftPersistence) {
      return;
    }
    const key = currentDraftKey();
    if (!key) {
      return;
    }

    const { activeMode, nativeQuery, logchefqlCode, limit, selectedRelativeTime, timeRange, selectedTimezoneIdentifier } = state.data.value;
    let absoluteStart: number | null = null;
    let absoluteEnd: number | null = null;

    if (!selectedRelativeTime && timeRange) {
      absoluteStart = calendarDateTimeToTimestamp(timeRange.start);
      absoluteEnd = calendarDateTimeToTimestamp(timeRange.end);
    }

    const draft: ExploreDraft = {
      version: 1,
      mode: activeMode,
      nativeQuery,
      logchefqlCode,
      limit,
      relativeTime: selectedRelativeTime,
      absoluteStart,
      absoluteEnd,
      timezone: selectedTimezoneIdentifier,
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

      const draft = JSON.parse(rawDraft) as ExploreDraft;
      if (draft.version !== 1) {
        return false;
      }

      state.data.value.activeMode = draft.mode ? normalizeExploreMode(draft.mode) : "logchefql";
      state.data.value.nativeQuery = draft.nativeQuery || "";
      state.data.value.logchefqlCode = draft.logchefqlCode || "";
      if (draft.limit > 0) {
        state.data.value.limit = draft.limit;
      }
      state.data.value.selectedTimezoneIdentifier = draft.timezone || null;

      if (draft.relativeTime) {
        const { start, end } = parseRelativeTimeString(draft.relativeTime);
        state.data.value.selectedRelativeTime = draft.relativeTime;
        state.data.value.timeRange = { start, end };
      } else if (draft.absoluteStart && draft.absoluteEnd) {
        state.data.value.timeRange = {
          start: timestampToCalendarDateTime(draft.absoluteStart),
          end: timestampToCalendarDateTime(draft.absoluteEnd),
        };
        state.data.value.selectedRelativeTime = null;
      }

      if (Array.isArray(draft.variables)) {
        variableStore.setAllVariable(draft.variables);
      } else {
        const { ensureVariablesFromSql } = useVariables();
        ensureVariablesFromSql(state.data.value.activeMode === "native" ? state.data.value.nativeQuery : state.data.value.logchefqlCode);
      }
      return true;
    } catch (error) {
      console.warn("Failed to restore query draft:", error);
      return false;
    }
  }

  function buildCurrentShareSnapshot(): string {
    const { activeMode, nativeQuery, logchefqlCode, limit, selectedRelativeTime, timeRange, selectedTimezoneIdentifier } = state.data.value;
    let absoluteStart: number | null = null;
    let absoluteEnd: number | null = null;

    if (!selectedRelativeTime && timeRange) {
      absoluteStart = calendarDateTimeToTimestamp(timeRange.start);
      absoluteEnd = calendarDateTimeToTimestamp(timeRange.end);
    }

    return JSON.stringify({
      sourceId: sourceId.value,
      mode: activeMode,
      query: activeMode === "native" ? nativeQuery : logchefqlCode,
      limit,
      relativeTime: selectedRelativeTime,
      absoluteStart,
      absoluteEnd,
      timezone: selectedTimezoneIdentifier,
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
    const { logchefqlCode, timeRange, limit, selectedTimezoneIdentifier } = state.data.value;

    if (!timeRange || !timeRange.start || !timeRange.end) {
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
    const timezone = selectedTimezoneIdentifier || getTimezoneIdentifier();
    const timeCondition = createTimeRangeCondition(tsField, timeRange as TimeRange, true, timezone);

    const formattedTsField = tsField.includes('`') ? tsField : `\`${tsField}\``;
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

    const timeRangeChanged = JSON.stringify(state.data.value.timeRange) !== lastExecutedState.timeRange;
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

  const hasDivergedFromSavedQuery = computed(() => {
    const { savedQuerySnapshot, selectedQueryId, activeMode, logchefqlCode, nativeQuery, limit, selectedRelativeTime, timeRange } = state.data.value;
    
    if (!selectedQueryId || !savedQuerySnapshot) {
      return false;
    }

    const currentContent = activeMode === 'logchefql' ? logchefqlCode : nativeQuery;
    if (currentContent !== savedQuerySnapshot.queryContent) {
      return true;
    }

    if (limit !== savedQuerySnapshot.limit) {
      return true;
    }

    if (selectedRelativeTime !== savedQuerySnapshot.relativeTime) {
      return true;
    }

    if (!selectedRelativeTime && timeRange) {
      const currentStart = calendarDateTimeToTimestamp(timeRange.start);
      const currentEnd = calendarDateTimeToTimestamp(timeRange.end);

      if (currentStart !== savedQuerySnapshot.absoluteStart || currentEnd !== savedQuerySnapshot.absoluteEnd) {
        return true;
      }
    }

    return false;
  });

  function clearSavedQueryIfDirty() {
    if (hasDivergedFromSavedQuery.value) {
      state.data.value.selectedQueryId = null;
      state.data.value.activeSavedQueryName = null;
      state.data.value.savedQuerySnapshot = null;
    }
  }

  const urlQueryParameters = computed(() => {
    const {
      timeRange,
      limit,
      activeMode,
      selectedRelativeTime,
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

    if (selectedQueryId && !hasDivergedFromSavedQuery.value) {
      params.id = selectedQueryId;
      return params;
    }

    if (activeShareToken) {
      params.share = activeShareToken;
      return params;
    }

    if (selectedRelativeTime) {
      params.t = selectedRelativeTime;
    } else if (timeRange) {
      const startTimestamp = new Date(
        timeRange.start.year,
        timeRange.start.month - 1,
        timeRange.start.day,
        'hour' in timeRange.start ? timeRange.start.hour : 0,
        'minute' in timeRange.start ? timeRange.start.minute : 0,
        'second' in timeRange.start ? timeRange.start.second : 0
      ).getTime();

      const endTimestamp = new Date(
        timeRange.end.year,
        timeRange.end.month - 1,
        timeRange.end.day,
        'hour' in timeRange.end ? timeRange.end.hour : 0,
        'minute' in timeRange.end ? timeRange.end.minute : 0,
        'second' in timeRange.end ? timeRange.end.second : 0
      ).getTime();

      params.start = startTimestamp.toString();
      params.end = endTimestamp.toString();
    }

    params.limit = limit.toString();

    if (activeMode === 'native') {
      params.mode = 'native';
    }

    return params;
  });

  function getTimezoneIdentifier(): string {
    return state.data.value.selectedTimezoneIdentifier ||
      (preferencesStore.preferences.timezone === 'utc' ? 'UTC' : getLocalTimeZone());
  }

  function setTimezoneIdentifier(timezone: string) {
    state.data.value.selectedTimezoneIdentifier = timezone;
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function onSourceChange(_newSourceId: number) {
    // Abort any query still in flight for the previous source so its response
    // can't land under the new source. The request-token guard in executeQuery
    // is the ultimate backstop (a response whose body already arrived before
    // this abort is dropped there), but aborting also frees the socket early.
    if (state.data.value.currentQueryAbortController) {
      state.data.value.currentQueryAbortController.abort();
      state.data.value.currentQueryAbortController = null;
    }
    state.data.value.currentQueryId = null;

    state.data.value.generatedDisplayQuery = null;
    state.data.value.logs = [];
    state.data.value.columns = [];
    state.data.value.queryStats = DEFAULT_QUERY_STATS;
    state.data.value.queryWarnings = [];
    clearActiveShareSelection();
    
    histogramStore.clearHistogramData();
    histogramStore.setGroupByField(null);
    
    state.data.value.lastExecutionTimestamp = null;
    state.data.value.lastExecutedState = undefined;
    state.data.value.hasExecutedQuery = false;
    state.data.value.nativeQuery = '';
    state.data.value.logchefqlCode = '';
  }

  function setSource(newSourceId: number) {
    if (newSourceId !== sourceId.value) {
      contextStore.selectSource(newSourceId);
    }
  }

  function suppressNextSourceReset(sourceId: number) {
    suppressedSourceResetId = sourceId;
  }

  function setTimeConfiguration(config: { absoluteRange?: { start: DateValue; end: DateValue }, relativeTime?: string }) {
    if (config.relativeTime) {
      setRelativeTimeRange(config.relativeTime);
    } else if (config.absoluteRange) {
      state.data.value.timeRange = config.absoluteRange;
      state.data.value.selectedRelativeTime = null;
      clearSavedQueryIfDirty();
      clearShareSelectionIfDirty();
      persistDraft();
    }
  }

  function setLimit(newLimit: number) {
    if (newLimit > 0) {
      state.data.value.limit = newLimit;
      clearSavedQueryIfDirty();
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
    clearSavedQueryIfDirty();
    clearShareSelectionIfDirty();
    persistDraft();
  }

  function setNativeQuery(sql: string) {
    state.data.value.nativeQuery = sql;
    clearSavedQueryIfDirty();
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

  function _updateLastExecutedState() {
    const executedSql = state.data.value.activeMode === 'logchefql'
      ? (state.data.value.generatedDisplayQuery || sqlForExecution.value)
      : sqlForExecution.value;

    state.data.value.lastExecutedState = {
      timeRange: JSON.stringify(state.data.value.timeRange),
      limit: state.data.value.limit,
      mode: state.data.value.activeMode,
      logchefqlQuery: state.data.value.logchefqlCode,
      sqlQuery: executedSql,
      sourceId: sourceId.value
    };
    state.data.value.lastExecutionTimestamp = Date.now();
  }

  function initializeFromUrl(
    params: Record<string, string | undefined>,
    options?: { updateLastExecutedState?: boolean }
  ): { needsResolve: boolean; queryId?: string; needsShareResolve?: boolean; shareToken?: string; shouldExecute: boolean } {
    // Suppress persistDraft during init so the setRelativeTimeRange/setLimit
    // side-effects below don't overwrite the saved draft with empty query text
    // before restoreDraftForCurrentContext() reads it back (#102). Restoring the
    // draft never needs to re-persist it, so keeping persistence off for the
    // whole init is safe.
    suppressDraftPersistence = true;
    try {
      return _initializeFromUrlImpl(params, options);
    } finally {
      suppressDraftPersistence = false;
    }
  }

  function _initializeFromUrlImpl(
    params: Record<string, string | undefined>,
    options?: { updateLastExecutedState?: boolean }
  ): { needsResolve: boolean; queryId?: string; needsShareResolve?: boolean; shareToken?: string; shouldExecute: boolean } {
    if (params.source) {
      const parsedSourceId = parseInt(params.source, 10);
      if (!isNaN(parsedSourceId)) {
        contextStore.selectSource(parsedSourceId);
      }
    }

    if (!params.share) {
      clearActiveShareSelection();
    }

    if (!params.id) {
      state.data.value.selectedQueryId = null;
      state.data.value.activeSavedQueryName = null;
      state.data.value.savedQuerySnapshot = null;
    }

    if (params.share) {
      state.data.value.activeShareToken = params.share;
      return { needsResolve: false, needsShareResolve: true, shareToken: params.share, shouldExecute: false };
    }

    const queryId = params.id;
    if (queryId) {
      state.data.value.selectedQueryId = queryId;
      return { needsResolve: true, queryId, shouldExecute: false };
    }

    const relativeTime = params.t || params.time;
    const startParam = params.start ?? params.start_time;
    const endParam = params.end ?? params.end_time;

    if (relativeTime) {
      setRelativeTimeRange(relativeTime);
    } else if (startParam && endParam) {
      try {
        const startTs = parseInt(startParam, 10);
        const endTs = parseInt(endParam, 10);

        if (!isNaN(startTs) && !isNaN(endTs)) {
          state.data.value.timeRange = {
            start: timestampToCalendarDateTime(startTs),
            end: timestampToCalendarDateTime(endTs)
          };
          state.data.value.selectedRelativeTime = null;
        }
      } catch (error) {
        console.error('Failed to parse time range from URL:', error);
      }
    } else if (!state.data.value.timeRange) {
      setRelativeTimeRange('15m');
    }

    if (params.limit) {
      const limit = parseInt(params.limit, 10);
      if (!isNaN(limit)) {
        setLimit(limit);
      }
    }

    if (params.mode) {
      const mode = normalizeModeForSource(normalizeExploreMode(params.mode));
      state.data.value.activeMode = mode;

      if (mode === 'logchefql' && params.q) {
        state.data.value.logchefqlCode = params.q;
      } else if (mode === 'native' && params.sql) {
        state.data.value.nativeQuery = params.sql;
      }
    } else {
      if (params.q) {
        state.data.value.activeMode = normalizeModeForSource('logchefql');
        state.data.value.logchefqlCode = params.q;
      } else if (params.sql) {
        state.data.value.activeMode = normalizeModeForSource('native');
        state.data.value.nativeQuery = params.sql;
      }
    }

    const hasLegacyUrlQuery = !!(params.q || params.sql);
    if (!hasLegacyUrlQuery && !queryId && !state.data.value.activeShareToken) {
      restoreDraftForCurrentContext();
    }

    // Ensure variables from SQL are initialized in the variable store.
    // This handles page reload where the variable store is empty but SQL has placeholders.
    const { ensureVariablesFromSql } = useVariables();
    const sqlToCheck = state.data.value.activeMode === 'native'
      ? state.data.value.nativeQuery
      : state.data.value.logchefqlCode;
    if (sqlToCheck) {
      ensureVariablesFromSql(sqlToCheck);
    }

    if (options?.updateLastExecutedState !== false) {
      _updateLastExecutedState();
    }

    const hasRequiredParams = !!(sourceId.value && state.data.value.timeRange);
    const hasQueryContent = state.data.value.activeMode === 'native' ? !!state.data.value.nativeQuery : true;

    return { needsResolve: false, shouldExecute: hasRequiredParams && hasQueryContent };
  }

  function hydrateFromResolvedQuery(data: {
    id: number;
    name: string;
    query_language?: string;
    editor_mode?: string;
    query_content: string;
  }): { shouldExecute: boolean } {
    try {
      const content = JSON.parse(data.query_content);
      const metadata = resolveSavedQueryMetadata({
        query_language: data.query_language,
        editor_mode: data.editor_mode,
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
      state.data.value.activeSavedQueryName = data.name;
      state.data.value.selectedQueryId = data.id.toString();

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

      let relativeTime: string | null = null;
      let absoluteStart: number | null = null;
      let absoluteEnd: number | null = null;

      if (content.timeRange?.relative) {
        relativeTime = content.timeRange.relative;
        const { start, end } = parseRelativeTimeString(content.timeRange.relative);
        state.data.value.selectedRelativeTime = relativeTime;
        state.data.value.timeRange = { start, end };
      } else if (content.timeRange?.absolute?.start && content.timeRange?.absolute?.end) {
        absoluteStart = content.timeRange.absolute.start;
        absoluteEnd = content.timeRange.absolute.end;

        state.data.value.timeRange = {
          start: timestampToCalendarDateTime(content.timeRange.absolute.start),
          end: timestampToCalendarDateTime(content.timeRange.absolute.end)
        };
        state.data.value.selectedRelativeTime = null;
      } else {
        // Saved query has no time range (timeRange: null) — use last 15 minutes
        // so the explorer doesn't get stuck on "Loading explorer...".
        const { start, end } = parseRelativeTimeString('15m');
        state.data.value.selectedRelativeTime = '15m';
        state.data.value.timeRange = { start, end };
      }

      state.data.value.savedQuerySnapshot = {
        queryContent,
        limit,
        relativeTime,
        absoluteStart,
        absoluteEnd,
      };

      // Ensure variables from the query content are initialized in the variable store
      if (Array.isArray(content.variables)) {
        const normalizedVariables = (content.variables as NonNullable<SavedQueryContent['variables']>).map((variable) => {
          const hasValue = variable.value !== '' && variable.value !== null && variable.value !== undefined;
          if (!hasValue && variable.defaultValue !== undefined && variable.defaultValue !== null && variable.defaultValue !== '') {
            return { ...variable, value: variable.defaultValue };
          }
          return variable;
        });
        variableStore.setAllVariable(normalizedVariables);
      } else {
        const { ensureVariablesFromSql } = useVariables();
        ensureVariablesFromSql(queryContent);
      }

      _updateLastExecutedState();

      return { shouldExecute: !!(sourceId.value && state.data.value.timeRange) };
    } catch (error) {
      console.error('Failed to hydrate from resolved query:', error);
      return { shouldExecute: false };
    }
  }

  function hydrateFromQueryShare(data: QueryShareResponse): { shouldExecute: boolean } {
    try {
      const payload = data.payload;

      state.data.value.activeShareToken = data.token;
      state.data.value.selectedQueryId = null;
      state.data.value.activeSavedQueryName = null;
      state.data.value.savedQuerySnapshot = null;
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
      state.data.value.selectedTimezoneIdentifier = payload.timezone || null;

      const relative = payload.time_range?.relative;
      const absolute = payload.time_range?.absolute;
      if (relative) {
        const { start, end } = parseRelativeTimeString(relative);
        state.data.value.selectedRelativeTime = relative;
        state.data.value.timeRange = { start, end };
      } else if (absolute?.start && absolute?.end) {
        state.data.value.timeRange = {
          start: timestampToCalendarDateTime(absolute.start),
          end: timestampToCalendarDateTime(absolute.end),
        };
        state.data.value.selectedRelativeTime = null;
      } else if (!state.data.value.timeRange) {
        const { start, end } = parseRelativeTimeString("15m");
        state.data.value.selectedRelativeTime = "15m";
        state.data.value.timeRange = { start, end };
      }

      suppressSharedVariableTracking = true;
      try {
        if (Array.isArray(payload.variables)) {
          variableStore.setAllVariable(payload.variables as VariableState[]);
        } else {
          const { ensureVariablesFromSql } = useVariables();
          ensureVariablesFromSql(payload.query || "");
        }
      } finally {
        suppressSharedVariableTracking = false;
      }

      _updateLastExecutedState();
      state.data.value.activeShareSnapshot = buildCurrentShareSnapshot();
      persistDraft();

      return { shouldExecute: !!(sourceId.value && state.data.value.timeRange) };
    } catch (error) {
      console.error("Failed to hydrate query share:", error);
      return { shouldExecute: false };
    }
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
    const nowDt = now(getLocalTimeZone());
    const timeRange = {
      start: new CalendarDateTime(
        nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
      ).subtract({ minutes: 15 }),
      end: new CalendarDateTime(
        nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
      )
    };

    state.data.value.timeRange = timeRange;
    state.data.value.selectedRelativeTime = '15m';
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
      const result = SqlManager.generateDefaultSql({
        tableName,
        tsField: timestampField,
        timeRange,
        limit: state.data.value.limit,
        timezone: state.data.value.selectedTimezoneIdentifier || undefined
      });

      state.data.value.nativeQuery = result.success ? result.sql : '';
    }

    histogramStore.clearHistogramData();

    _updateLastExecutedState();
  }

  function resetQueryContentForSourceChange() {
    _clearQueryContent();

    if (state.data.value.timeRange) {
      const sourceDetails = sourcesStore.currentSourceDetails;
      
      if (supportsClickHouseSQLForSource(sourceDetails)) {
        let tableName = 'logs.vector_logs';
        const chConn = asClickHouseConnection(sourceDetails?.connection);
        if (chConn?.database && chConn?.table_name) {
          tableName = `${chConn.database}.${chConn.table_name}`;
        }

        const timestampField = sourceDetails?._meta_ts_field || 'timestamp';

        const result = SqlManager.generateDefaultSql({
          tableName,
          tsField: timestampField,
          timeRange: state.data.value.timeRange,
          limit: state.data.value.limit,
          timezone: state.data.value.selectedTimezoneIdentifier || undefined
        });

        state.data.value.nativeQuery = result.success ? result.sql : '';
      }
    }

    histogramStore.clearHistogramData();
    _updateLastExecutedState();
  }

  async function executeQuery() {
    const relativeTime = state.data.value.selectedRelativeTime;

    // Refresh time range if using relative time to prevent stale queries
    if (relativeTime) {
      const { start, end } = parseRelativeTimeString(relativeTime);
      state.data.value.timeRange = { start, end };
    }

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

    state.data.value.lastExecutionTimestamp = null;
    
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

      if (state.data.value.activeMode === 'logchefql') {
        try {
          const { getVariablesForApi } = useVariables();
          const variables = getVariablesForApi();
          
          const timeRange = state.data.value.timeRange as TimeRange;
          const timezone = state.data.value.selectedTimezoneIdentifier || getTimezoneIdentifier();
          const queryTimeout = state.data.value.queryTimeout;
          
          const formatDateTime = (dt: any) => {
            if (!dt) return '';
            const year = dt.year;
            const month = String(dt.month).padStart(2, '0');
            const day = String(dt.day).padStart(2, '0');
            const hour = String(dt.hour || 0).padStart(2, '0');
            const minute = String(dt.minute || 0).padStart(2, '0');
            const second = String(dt.second || 0).padStart(2, '0');
            return `${year}-${month}-${day} ${hour}:${minute}:${second}`;
          };

          const queryResponse = await logchefqlApi.query(currentTeamId, sourceId.value, {
            query: state.data.value.logchefqlCode,
            start_time: formatDateTime(timeRange.start),
            end_time: formatDateTime(timeRange.end),
            timezone: timezone,
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
            state.data.value.logs = logs;
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

            _updateLastExecutedState();
            persistDraft();

            // Query history is recorded server-side on execution and surfaced
            // via GET /me/query-history (see QueryHistoryPanel) — no local write.

            if (relativeTime) {
              state.data.value.selectedRelativeTime = relativeTime;
            }

            state.data.value.lastExecutionTimestamp = Date.now();

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

      let sql = sqlForExecution.value;
      const activeTimeRange = state.data.value.timeRange as TimeRange;
      const activeTimezone = state.data.value.selectedTimezoneIdentifier || getTimezoneIdentifier();

      const toISOString = (dt: any) => {
        if (!dt) return '';
        return new Date(
          dt.year,
          dt.month - 1,
          dt.day,
          'hour' in dt ? dt.hour : 0,
          'minute' in dt ? dt.minute : 0,
          'second' in dt ? dt.second : 0
        ).toISOString();
      };

      const params: QueryParams = {
        query_text: '',
        query_timeout: state.data.value.queryTimeout,
        start_time: activeTimeRange?.start ? toISOString(activeTimeRange.start) : undefined,
        end_time: activeTimeRange?.end ? toISOString(activeTimeRange.end) : undefined,
        timezone: activeTimezone,
      };

      if (!sql || !sql.trim()) {
        if (supportsClickHouseSQLForSource(sourceDetails)) {
          const tsField = sourceDetails._meta_ts_field || 'timestamp';

          let tableName = 'default.logs';
          const chConn = asClickHouseConnection(sourceDetails.connection);
          if (chConn?.database && chConn?.table_name) {
            tableName = `${chConn.database}.${chConn.table_name}`;
          }

          const result = SqlManager.generateDefaultSql({
            tableName,
            tsField,
            timeRange: activeTimeRange,
            limit: state.data.value.limit,
            timezone: state.data.value.selectedTimezoneIdentifier || undefined
          });

          if (!result.success) {
            return state.handleError({
              status: "error",
              message: "Failed to generate default SQL",
              error_type: "ValidationError"
            }, operationKey);
          }

          sql = result.sql;

          if (state.data.value.activeMode === 'native') {
            state.data.value.nativeQuery = result.sql;
          }
        }
      }

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
              state.data.value.logs = logs;
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

            _updateLastExecutedState();
            persistDraft();

            // Query history is recorded server-side on execution and surfaced
            // via GET /me/query-history (see QueryHistoryPanel) — no local write.

            if (relativeTime) {
              state.data.value.selectedRelativeTime = relativeTime;
            }

            if (isHistogramEligible.value) {
              void fetchHistogramData();
            }
          },
          operationKey: operationKey,
        });

        if (!response.success && state.data.value.lastExecutionTimestamp === null) {
          state.data.value.lastExecutionTimestamp = Date.now();

          if (relativeTime) {
            state.data.value.selectedRelativeTime = relativeTime;
          }
        }
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

  function setRelativeTimeRange(relativeTimeString: string | null) {
    if (!relativeTimeString) {
      state.data.value.selectedRelativeTime = null;
      clearSavedQueryIfDirty();
      clearShareSelectionIfDirty();
      persistDraft();
      return;
    }

    try {
      const { start, end } = parseRelativeTimeString(relativeTimeString);
      state.data.value.selectedRelativeTime = relativeTimeString;
      state.data.value.timeRange = { start, end };
      clearSavedQueryIfDirty();
      clearShareSelectionIfDirty();
      persistDraft();
    } catch (error) {
      console.error('Failed to parse relative time string:', error);
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

  function setSelectedQueryId(queryId: string | null) {
    state.data.value.selectedQueryId = queryId;
  }

  function setActiveSavedQueryName(name: string | null) {
    state.data.value.activeSavedQueryName = name;
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
    const { activeMode, nativeQuery, logchefqlCode, limit, selectedRelativeTime, timeRange, selectedTimezoneIdentifier } = state.data.value;
    const payload: QuerySharePayload = {
      version: 1,
      mode: activeMode,
      query: activeMode === "native" ? nativeQuery : logchefqlCode,
      limit,
      timezone: selectedTimezoneIdentifier || getTimezoneIdentifier(),
      variables: variableStore.allVariables as unknown as QuerySharePayload["variables"],
    };

    if (selectedRelativeTime) {
      payload.time_range = { relative: selectedRelativeTime };
    } else if (timeRange) {
      payload.time_range = {
        absolute: {
          start: calendarDateTimeToTimestamp(timeRange.start),
          end: calendarDateTimeToTimestamp(timeRange.end),
        },
      };
    }

    return payload;
  }

  async function createQueryShare(options?: { persistActiveToken?: boolean }) {
    const currentTeamId = useTeamsStore().currentTeamId;
    if (!currentTeamId || !sourceId.value) {
      throw new Error("Team and source are required to share a query");
    }

    const payload = buildQuerySharePayload();
    if (payload.mode === "native" && !payload.query.trim()) {
      throw new Error("Query is required to create a share link");
    }

    const response = await exploreApi.createQueryShare(sourceId.value, payload, currentTeamId);
    if (response.data && options?.persistActiveToken !== false) {
      state.data.value.activeShareToken = response.data.token;
      state.data.value.activeShareSnapshot = buildCurrentShareSnapshot();
    }
    return response.data;
  }

  function setActiveShareToken(token: string | null) {
    state.data.value.activeShareToken = token;
    if (!token) {
      state.data.value.activeShareSnapshot = null;
    }
  }

  function clearError() {
    state.error.value = null;
  }

  // Histogram delegation
  async function setGroupByField(field: string | null) {
    histogramStore.setGroupByField(field);
    if (isHistogramEligible.value && state.data.value.hasExecutedQuery) {
      await fetchHistogramData();
    }
  }

  async function fetchHistogramData(granularity?: string) {
    if (!isHistogramEligible.value) {
      histogramStore.clearHistogramData();
      return { success: false, error: { message: "Histogram is not available for this query mode" } };
    }

    let queryText = "";
    if (state.data.value.activeMode === 'logchefql') {
      queryText = state.data.value.generatedDisplayQuery || "";
    } else if (isNativeHistogramSource()) {
      queryText = state.data.value.nativeQuery?.trim() || "*";
    }

    if (!queryText) {
      histogramStore.clearHistogramData();
      return { success: false, error: { message: "Run a query first to see the histogram" } };
    }

    return histogramStore.fetchHistogramData({
      queryText,
      timeRange: state.data.value.timeRange,
      timezone: state.data.value.selectedTimezoneIdentifier || undefined,
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

    if (result.success && result.data && state.data.value.activeMode === 'native') {
      state.data.value.nativeQuery = result.data.sql_query || '';
    }

    return result;
  }

  function clearAiSqlState() {
    aiStore.clearState();
  }

  let lastAutoExecKey: string | null = null;
  
  watch(
    () => contextStore.sourceId,
    () => {
      lastAutoExecKey = null;
    }
  );

  watch(
    () => sourcesStore.currentSourceDetails,
    (source) => {
      if (!source) {
        return;
      }

      if (!supportsLogchefQLForSource(source) && state.data.value.activeMode === 'logchefql') {
        if (!state.data.value.nativeQuery && state.data.value.logchefqlCode.trim()) {
          state.data.value.nativeQuery = state.data.value.logchefqlCode;
        }
        state.data.value.logchefqlCode = '';
        state.data.value.activeMode = 'native';
        return;
      }

      state.data.value.activeMode = normalizeModeForSource(state.data.value.activeMode, source);
    },
    { immediate: true }
  );

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
  
  watch(
    () => [sourcesStore.currentSourceDetails, state.data.value.timeRange] as const,
    ([newDetails, timeRange]) => {
      if (!newDetails?.id || !newDetails.is_connected) return;
      if (!timeRange) return;

      const execKey = `${newDetails.id}-${timeRange.start.toString()}-${timeRange.end.toString()}`;
      if (execKey === lastAutoExecKey) return;

      // Skip if a query was already executed (e.g., by URL state initialization)
      if (state.data.value.hasExecutedQuery && lastAutoExecKey === null) {
        lastAutoExecKey = execKey;
        return;
      }

      if (state.isLoadingOperation('executeQuery')) return;
      if (sourcesStore.isLoadingTeamSources) return;
      
      const sourceInTeam = sourcesStore.teamSources.some(s => s.id === newDetails.id);
      if (!sourceInTeam) {
        return;
      }
      
      lastAutoExecKey = execKey;
      
      executeQuery().catch(err => {
        console.error('ExploreStore: Auto-execute failed:', err);
      });
    }
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
    timeRange: computed(() => state.data.value.timeRange),
    selectedRelativeTime: computed(() => state.data.value.selectedRelativeTime),
    filterConditions: computed(() => state.data.value.filterConditions),
    nativeQuery: computed(() => state.data.value.nativeQuery),
    logchefqlCode: computed(() => state.data.value.logchefqlCode),
    activeMode: computed(() => state.data.value.activeMode),
    error: state.error,
    queryId: computed(() => state.data.value.queryId),
    lastExecutedState: computed(() => state.data.value.lastExecutedState),
    lastExecutionTimestamp: computed(() => state.data.value.lastExecutionTimestamp),
    hasExecutedQuery: computed(() => state.data.value.hasExecutedQuery),
    selectedQueryId: computed(() => state.data.value.selectedQueryId),
    activeShareToken: computed(() => state.data.value.activeShareToken),
    activeSavedQueryName: computed(() => state.data.value.activeSavedQueryName),
    selectedTimezoneIdentifier: computed(() => state.data.value.selectedTimezoneIdentifier),
    generatedDisplayQuery: computed(() => state.data.value.generatedDisplayQuery),

    // AI state (delegated)
    isGeneratingAISQL: computed(() => aiStore.isGeneratingAISQL),
    aiSqlError: computed(() => aiStore.aiSqlError),
    generatedAiSql: computed(() => aiStore.generatedAiSql),

    // Histogram state (delegated)
    histogramData: computed(() => histogramStore.histogramData),
    isLoadingHistogram: computed(() => histogramStore.isLoadingHistogram),
    histogramError: computed(() => histogramStore.histogramError),
    histogramGranularity: computed(() => histogramStore.histogramGranularity),
    groupByField: computed(() => histogramStore.groupByField),

    // Loading state
    isLoading: state.isLoading,

    logchefQlTranslationResult: _logchefQlTranslationResult,
    sqlForExecution,
    isQueryStateDirty,
    hasDivergedFromSavedQuery,
    urlQueryParameters,
    canExecuteQuery,

    // Actions
    setSource,
    suppressNextSourceReset,
    setTimeConfiguration,
    setLimit,
    setQueryTimeout,
    setFilterConditions,
    setNativeQuery,
    setActiveMode,
    setLogchefqlCode,
    clearActiveSavedQuerySelection,
    setSelectedQueryId,
    setActiveSavedQueryName,
    setRelativeTimeRange,
    resetQueryToDefaults,
    resetQueryContentForSourceChange,
    initializeFromUrl,
    hydrateFromResolvedQuery,
    hydrateFromQueryShare,
    executeQuery,
    cancelQuery,
    getLogContext,
    createQueryShare,
    setActiveShareToken,
    clearError,
    setGroupByField,
    setTimezoneIdentifier,
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
    isExecutingQuery,
    canCancelQuery,
    isCancellingQuery: computed(() => state.data.value.isCancellingQuery),
    
    // Histogram eligibility
    isHistogramEligible,

    // Timezone helper
    getTimezoneIdentifier,
  };
});
