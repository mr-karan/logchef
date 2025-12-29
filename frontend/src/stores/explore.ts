import { defineStore } from "pinia";
import { computed, watch } from "vue";
import { exploreApi } from "@/api/explore";
import { logchefqlApi } from "@/api/logchefql";
import type {
  ColumnInfo,
  QueryStats,
  FilterCondition,
  QueryParams,
  LogContextRequest,
  LogContextResponse,
  QuerySuccessResponse,
} from "@/api/explore";
import type { DateValue } from "@internationalized/date";
import { now, getLocalTimeZone, CalendarDateTime } from "@internationalized/date";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useBaseStore } from "./base";
import { useExploreHistogramStore } from "./exploreHistogram";
import { useExploreAIStore } from "./exploreAI";
import { parseRelativeTimeString, timestampToCalendarDateTime, calendarDateTimeToTimestamp } from "@/utils/time";
import { SqlManager } from '@/services/SqlManager';
import { type TimeRange } from '@/types/query';
import { useVariables } from "@/composables/useVariables";
import { queryHistoryService } from "@/services/QueryHistoryService";
import { createTimeRangeCondition, formatDateForSQL } from '@/utils/time-utils';
import { isVictoriaLogsSource } from '@/api/sources';

interface SavedQuerySnapshot {
  queryContent: string;
  limit: number;
  relativeTime: string | null;
  absoluteStart: number | null;
  absoluteEnd: number | null;
}

export interface ExploreState {
  logs: Record<string, any>[];
  columns: ColumnInfo[];
  queryStats: QueryStats;
  limit: number;
  timeRange: {
    start: DateValue;
    end: DateValue;
  } | null;
  selectedRelativeTime: string | null;
  filterConditions: FilterCondition[];
  rawSql: string;
  pendingRawSql?: string;
  displaySql?: string;
  logchefQuery?: string;
  logchefqlCode: string;
  activeMode: "logchefql" | "sql";
  isLoading?: boolean;
  error?: string | null;
  queryId?: string | null;
  selectedQueryId: string | null;
  activeSavedQueryName: string | null;
  savedQuerySnapshot: SavedQuerySnapshot | null;
  stats?: any;
  lastExecutedState?: {
    timeRange: string;
    limit: number;
    mode: "logchefql" | "sql";
    logchefqlQuery?: string;
    sqlQuery: string;
    sourceId: number;
  };
  lastExecutionTimestamp: number | null;
  selectedTimezoneIdentifier: string | null;
  generatedDisplaySql: string | null;
  queryTimeout: number;
  currentQueryAbortController: AbortController | null;
  currentQueryId: string | null;
  isCancellingQuery: boolean;
}

const DEFAULT_QUERY_STATS: QueryStats = {
  execution_time_ms: 0,
  rows_read: 0,
  bytes_read: 0,
};

export const useExploreStore = defineStore("explore", () => {
  const contextStore = useContextStore();
  const histogramStore = useExploreHistogramStore();
  const aiStore = useExploreAIStore();
  
  const state = useBaseStore<ExploreState>({
    logs: [],
    columns: [],
    queryStats: DEFAULT_QUERY_STATS,
    limit: 100,
    timeRange: null,
    selectedRelativeTime: null,
    filterConditions: [],
    rawSql: "",
    logchefqlCode: "",
    activeMode: "logchefql",
    lastExecutionTimestamp: null,
    selectedQueryId: null,
    activeSavedQueryName: null,
    savedQuerySnapshot: null,
    selectedTimezoneIdentifier: null,
    generatedDisplaySql: null,
    queryTimeout: 30,
    currentQueryAbortController: null,
    currentQueryId: null,
    isCancellingQuery: false,
  });

  watch(
    () => contextStore.sourceId,
    (newSourceId, oldSourceId) => {
      if (newSourceId !== oldSourceId) {
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

  const isHistogramEligible = computed(() => {
    return state.data.value.activeMode === 'logchefql';
  });

  const _buildDisplaySql = () => {
    const { logchefqlCode, timeRange, limit, selectedTimezoneIdentifier } = state.data.value;

    if (!timeRange || !timeRange.start || !timeRange.end) {
      return null;
    }

    const sourcesStore = useSourcesStore();
    const sourceDetails = sourcesStore.currentSourceDetails;
    if (!sourceDetails) {
      return null;
    }

    const isVL = isVictoriaLogsSource(sourceDetails);

    if (isVL) {
      const startTime = formatDateForSQL(timeRange.start, false);
      const endTime = formatDateForSQL(timeRange.end, false);
      
      let logsql = '';
      if (logchefqlCode?.trim()) {
        logsql += `-- LogchefQL: ${logchefqlCode}\n`;
      }
      logsql += `_time:[${startTime}, ${endTime}]`;
      logsql += ` | sort by (_time desc)`;
      logsql += ` | limit ${limit}`;

      return {
        sql: logsql,
        error: undefined,
        warnings: []
      };
    }

    let tableName = 'default.logs';
    if (sourceDetails.connection?.database && sourceDetails.connection?.table_name) {
      tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
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
    const { activeMode, rawSql } = state.data.value;
    if (activeMode === 'sql') {
      return rawSql;
    }

    const translationResult = _logchefQlTranslationResult.value;
    if (!translationResult) {
      return '';
    }
    return translationResult.sql;
  });

  const isQueryStateDirty = computed(() => {
    const { lastExecutedState, limit, activeMode, logchefqlCode, rawSql } = state.data.value;

    if (!lastExecutedState) {
      return (activeMode === 'logchefql' && !!logchefqlCode?.trim()) ||
             (activeMode === 'sql' && !!rawSql?.trim());
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
    const { savedQuerySnapshot, selectedQueryId, activeMode, logchefqlCode, rawSql, limit, selectedRelativeTime, timeRange } = state.data.value;
    
    if (!selectedQueryId || !savedQuerySnapshot) {
      return false;
    }

    const currentContent = activeMode === 'logchefql' ? logchefqlCode : rawSql;
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
    const { timeRange, limit, activeMode, logchefqlCode, rawSql, selectedRelativeTime, selectedQueryId } = state.data.value;
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

    if (activeMode === 'logchefql' && logchefqlCode) {
      params.q = logchefqlCode;
    } else if (activeMode === 'sql') {
      params.mode = 'sql';
      if (rawSql) {
        params.sql = rawSql;
      }
    }

    return params;
  });

  function getTimezoneIdentifier(): string {
    return state.data.value.selectedTimezoneIdentifier ||
      (localStorage.getItem('logchef_timezone') === 'utc' ? 'UTC' : getLocalTimeZone());
  }

  function setTimezoneIdentifier(timezone: string) {
    state.data.value.selectedTimezoneIdentifier = timezone;
  }

  function onSourceChange(_newSourceId: number) {
    state.data.value.generatedDisplaySql = null;
    state.data.value.logs = [];
    state.data.value.columns = [];
    state.data.value.queryStats = DEFAULT_QUERY_STATS;
    
    histogramStore.clearHistogramData();
    histogramStore.setGroupByField(null);
    
    state.data.value.lastExecutionTimestamp = null;
    state.data.value.lastExecutedState = undefined;

    if (state.data.value.activeMode === 'sql') {
      state.data.value.rawSql = '';
    } else {
      state.data.value.logchefqlCode = '';
    }
  }

  function setSource(newSourceId: number) {
    if (newSourceId !== sourceId.value) {
      contextStore.selectSource(newSourceId);
    }
  }

  function setTimeConfiguration(config: { absoluteRange?: { start: DateValue; end: DateValue }, relativeTime?: string }) {
    if (config.relativeTime) {
      setRelativeTimeRange(config.relativeTime);
    } else if (config.absoluteRange) {
      state.data.value.timeRange = config.absoluteRange;
      state.data.value.selectedRelativeTime = null;
      clearSavedQueryIfDirty();
    }
  }

  function setLimit(newLimit: number) {
    if (newLimit > 0 && newLimit <= 10000) {
      state.data.value.limit = newLimit;
      clearSavedQueryIfDirty();
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
  }

  function setRawSql(sql: string) {
    state.data.value.rawSql = sql;
    clearSavedQueryIfDirty();
  }

  function setActiveMode(mode: 'logchefql' | 'sql') {
    const currentMode = state.data.value.activeMode;
    if (mode === currentMode) return;
    state.data.value.activeMode = mode;
  }

  function _updateLastExecutedState() {
    state.data.value.lastExecutedState = {
      timeRange: JSON.stringify(state.data.value.timeRange),
      limit: state.data.value.limit,
      mode: state.data.value.activeMode,
      logchefqlQuery: state.data.value.logchefqlCode,
      sqlQuery: sqlForExecution.value,
      sourceId: sourceId.value
    };
    state.data.value.lastExecutionTimestamp = Date.now();
  }

  function initializeFromUrl(params: Record<string, string | undefined>): { needsResolve: boolean; queryId?: string; shouldExecute: boolean } {
    if (params.source) {
      const parsedSourceId = parseInt(params.source, 10);
      if (!isNaN(parsedSourceId)) {
        contextStore.selectSource(parsedSourceId);
      }
    }

    const queryId = params.id || params.query_id;
    if (queryId) {
      state.data.value.selectedQueryId = queryId;
      return { needsResolve: true, queryId, shouldExecute: false };
    }

    const relativeTime = params.t || params.time;
    if (relativeTime) {
      setRelativeTimeRange(relativeTime);
    } else if (params.start && params.end) {
      try {
        const startTs = parseInt(params.start, 10);
        const endTs = parseInt(params.end, 10);

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
      const mode = params.mode === 'sql' ? 'sql' : 'logchefql';
      state.data.value.activeMode = mode;

      if (mode === 'logchefql' && params.q) {
        state.data.value.logchefqlCode = params.q;
      } else if (mode === 'sql' && params.sql) {
        state.data.value.rawSql = params.sql;
      }
    } else {
      if (params.q) {
        state.data.value.activeMode = 'logchefql';
        state.data.value.logchefqlCode = params.q;
      } else if (params.sql) {
        state.data.value.activeMode = 'sql';
        state.data.value.rawSql = params.sql;
      }
    }

    _updateLastExecutedState();

    const hasRequiredParams = !!(sourceId.value && state.data.value.timeRange);
    const hasQueryContent = state.data.value.activeMode === 'sql' ? !!state.data.value.rawSql : true;

    return { needsResolve: false, shouldExecute: hasRequiredParams && hasQueryContent };
  }

  function hydrateFromResolvedQuery(data: {
    id: number;
    name: string;
    query_type: string;
    query_content: string;
  }): { shouldExecute: boolean } {
    try {
      const content = JSON.parse(data.query_content);
      const isLogchefQL = data.query_type === 'logchefql';

      state.data.value.activeMode = isLogchefQL ? 'logchefql' : 'sql';
      state.data.value.activeSavedQueryName = data.name;
      state.data.value.selectedQueryId = data.id.toString();

      const queryContent = content.content || '';
      if (isLogchefQL) {
        state.data.value.logchefqlCode = queryContent;
      } else {
        state.data.value.rawSql = queryContent;
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
      }

      state.data.value.savedQuerySnapshot = {
        queryContent,
        limit,
        relativeTime,
        absoluteStart,
        absoluteEnd,
      };

      _updateLastExecutedState();

      return { shouldExecute: !!(sourceId.value && state.data.value.timeRange) };
    } catch (error) {
      console.error('Failed to hydrate from resolved query:', error);
      return { shouldExecute: false };
    }
  }

  function _clearQueryContent() {
    state.data.value.logchefqlCode = '';
    state.data.value.rawSql = '';
    state.data.value.activeMode = 'logchefql';
    state.data.value.selectedQueryId = null;
    state.data.value.activeSavedQueryName = null;
    state.data.value.savedQuerySnapshot = null;
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

    const sourcesStore = useSourcesStore();
    const sourceDetails = sourcesStore.currentSourceDetails;
    
    if (sourceDetails && isVictoriaLogsSource(sourceDetails)) {
      const startTime = formatDateForSQL(timeRange.start, false);
      const endTime = formatDateForSQL(timeRange.end, false);
      state.data.value.rawSql = `_time:[${startTime}, ${endTime}] | sort by (_time desc) | limit ${state.data.value.limit}`;
    } else {
      let tableName = 'logs.vector_logs';
      if (sourceDetails?.connection?.database && sourceDetails?.connection?.table_name) {
        tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
      }
      
      const timestampField = sourceDetails?._meta_ts_field || 'timestamp';

      const result = SqlManager.generateDefaultSql({
        tableName,
        tsField: timestampField,
        timeRange,
        limit: state.data.value.limit,
        timezone: state.data.value.selectedTimezoneIdentifier || undefined
      });

      state.data.value.rawSql = result.success ? result.sql : '';
    }

    histogramStore.clearHistogramData();

    _updateLastExecutedState();
  }

  function resetQueryContentForSourceChange() {
    _clearQueryContent();

    if (state.data.value.timeRange) {
      const sourcesStore = useSourcesStore();
      const sourceDetails = sourcesStore.currentSourceDetails;
      
      if (sourceDetails && isVictoriaLogsSource(sourceDetails)) {
        const startTime = formatDateForSQL(state.data.value.timeRange.start, false);
        const endTime = formatDateForSQL(state.data.value.timeRange.end, false);
        state.data.value.rawSql = `_time:[${startTime}, ${endTime}] | sort by (_time desc) | limit ${state.data.value.limit}`;
        histogramStore.clearHistogramData();
        _updateLastExecutedState();
        return;
      }
      
      let tableName = 'logs.vector_logs';
      if (sourceDetails?.connection?.database && sourceDetails?.connection?.table_name) {
        tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
      }
      
      const timestampField = sourceDetails?._meta_ts_field || 'timestamp';

      const result = SqlManager.generateDefaultSql({
        tableName,
        tsField: timestampField,
        timeRange: state.data.value.timeRange,
        limit: state.data.value.limit,
        timezone: state.data.value.selectedTimezoneIdentifier || undefined
      });

      state.data.value.rawSql = result.success ? result.sql : '';
    }

    histogramStore.clearHistogramData();
    _updateLastExecutedState();
  }

  async function executeQuery() {
    const relativeTime = state.data.value.selectedRelativeTime;

    if (state.data.value.currentQueryAbortController) {
      state.data.value.currentQueryAbortController.abort();
    }

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

      const sourcesStore = useSourcesStore();
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

      if (state.data.value.activeMode === 'logchefql') {
        try {
          const { convertVariables } = useVariables();
          const queryWithVariables = convertVariables(state.data.value.logchefqlCode);
          
          const timeRange = state.data.value.timeRange as TimeRange;
          const timezone = state.data.value.selectedTimezoneIdentifier || getTimezoneIdentifier();
          
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
            query: queryWithVariables,
            start_time: formatDateTime(timeRange.start),
            end_time: formatDateTime(timeRange.end),
            timezone: timezone,
            limit: state.data.value.limit,
            query_timeout: state.data.value.queryTimeout
          });

          if (queryResponse.data) {
            state.data.value.logs = queryResponse.data.logs || [];
            state.data.value.columns = queryResponse.data.columns || [];
            state.data.value.queryStats = queryResponse.data.stats || DEFAULT_QUERY_STATS;
            
            if (queryResponse.data.query_id) {
              state.data.value.currentQueryId = queryResponse.data.query_id;
            }

            if (queryResponse.data.generated_sql) {
              state.data.value.generatedDisplaySql = queryResponse.data.generated_sql;
            }

            _updateLastExecutedState();

            try {
              if (currentTeamId && sourceId.value) {
                queryHistoryService.addQueryEntry({
                  teamId: currentTeamId,
                  sourceId: sourceId.value,
                  query: state.data.value.logchefqlCode,
                  mode: 'logchefql'
                });
              }
            } catch (historyError) {
              console.warn("Failed to add query to history:", historyError);
            }

            if (relativeTime) {
              state.data.value.selectedRelativeTime = relativeTime;
            }

            state.data.value.lastExecutionTimestamp = Date.now();

            fetchHistogramData();

            return { success: true, data: queryResponse.data, error: null };
          } else {
            return state.handleError({
              status: "error",
              message: "Query execution failed",
              error_type: "DatabaseError"
            }, operationKey);
          }
        } catch (error: any) {
          return state.handleError({
            status: "error",
            message: `LogchefQL query error: ${error.message}`,
            error_type: "DatabaseError"
          }, operationKey);
        }
      }

      let sql = sqlForExecution.value;
      const isVL = isVictoriaLogsSource(sourceDetails);

      const params: QueryParams = {
        raw_sql: '',
        limit: state.data.value.limit,
        query_timeout: state.data.value.queryTimeout
      };

      if (!sql || !sql.trim()) {
        if (isVL) {
          const timeRange = state.data.value.timeRange as TimeRange;
          const startTime = formatDateForSQL(timeRange.start, false);
          const endTime = formatDateForSQL(timeRange.end, false);
          sql = `_time:[${startTime}, ${endTime}] | sort by (_time desc) | limit ${state.data.value.limit}`;
        } else {
          const tsField = sourceDetails._meta_ts_field || 'timestamp';
          
          let tableName = 'default.logs';
          if (sourceDetails.connection?.database && sourceDetails.connection?.table_name) {
            tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
          }

          const result = SqlManager.generateDefaultSql({
            tableName,
            tsField,
            timeRange: state.data.value.timeRange as TimeRange,
            limit: state.data.value.limit,
            timezone: state.data.value.selectedTimezoneIdentifier || undefined
          });

          if (!result.success) {
            return state.handleError({
              status: "error",
              message: "Failed to generate default query",
              error_type: "ValidationError"
            }, operationKey);
          }

          sql = result.sql;
        }

        if (state.data.value.activeMode === 'sql') {
          state.data.value.rawSql = sql;
        }
      }

      const { convertVariables } = useVariables();
      sql = convertVariables(sql);

      params.raw_sql = sql;

      let response;
      
      try {
        response = await state.callApi({
          apiCall: async () => exploreApi.getLogs(sourceId.value, params, currentTeamId, abortController.signal),
          onSuccess: (data: QuerySuccessResponse | null) => {
            if (data && (data.data || data.logs)) {
              state.data.value.logs = data.data || data.logs || [];
              state.data.value.columns = data.columns || [];
              state.data.value.queryStats = data.stats || DEFAULT_QUERY_STATS;
              if (data.params && typeof data.params === 'object' && "query_id" in data.params) {
                state.data.value.queryId = data.params.query_id as string;
              } else {
                state.data.value.queryId = null;
              }
            } else {
              state.data.value.logs = [];
              state.data.value.columns = [];
              state.data.value.queryStats = DEFAULT_QUERY_STATS;
              state.data.value.queryId = null;
            }
            
            if (data && data.query_id) {
              state.data.value.currentQueryId = data.query_id;
            }

            _updateLastExecutedState();

            try {
              const teamsStore = useTeamsStore();
              const currentTeamId = teamsStore.currentTeamId;
              if (currentTeamId && sourceId.value) {
                const queryContent = state.data.value.activeMode === 'logchefql'
                  ? state.data.value.logchefqlCode
                  : sql;

                queryHistoryService.addQueryEntry({
                  teamId: currentTeamId,
                  sourceId: sourceId.value,
                  mode: state.data.value.activeMode,
                  query: queryContent,
                  title: state.data.value.activeSavedQueryName || undefined
                });
              }
            } catch (error) {
              console.warn('Failed to save query to history:', error);
            }

            if (relativeTime) {
              state.data.value.selectedRelativeTime = relativeTime;
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
        state.data.value.currentQueryAbortController = null;
        state.data.value.currentQueryId = null;
        state.data.value.isCancellingQuery = false;
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
    }
  }

  function setRelativeTimeRange(relativeTimeString: string | null) {
    if (!relativeTimeString) {
      state.data.value.selectedRelativeTime = null;
      clearSavedQueryIfDirty();
      return;
    }

    try {
      const { start, end } = parseRelativeTimeString(relativeTimeString);
      state.data.value.selectedRelativeTime = relativeTimeString;
      state.data.value.timeRange = { start, end };
      clearSavedQueryIfDirty();
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

  function clearError() {
    state.error.value = null;
  }

  // Histogram delegation
  function setGroupByField(field: string | null) {
    histogramStore.setGroupByField(field);
  }

  async function fetchHistogramData(granularity?: string) {
    if (!isHistogramEligible.value) {
      histogramStore.clearHistogramData();
      return { success: false, error: { message: "Histogram is only available for LogchefQL queries" } };
    }

    const sql = state.data.value.generatedDisplaySql;
    if (!sql) {
      histogramStore.clearHistogramData();
      return { success: false, error: { message: "Run a LogchefQL query first" } };
    }

    return histogramStore.fetchHistogramData({
      sql,
      timeRange: state.data.value.timeRange,
      timezone: state.data.value.selectedTimezoneIdentifier || undefined,
      queryTimeout: state.data.value.queryTimeout,
      granularity,
    });
  }

  // AI delegation
  async function generateAiSql(naturalLanguageQuery: string, currentQuery?: string) {
    const result = await aiStore.generateAiSql(naturalLanguageQuery, currentQuery);
    
    if (result.success && result.data && state.data.value.activeMode === 'sql') {
      state.data.value.rawSql = result.data.sql_query || '';
    }
    
    return result;
  }

  function clearAiSqlState() {
    aiStore.clearState();
  }

  return {
    // State
    logs: computed(() => state.data.value.logs),
    columns: computed(() => state.data.value.columns),
    queryStats: computed(() => state.data.value.queryStats),
    sourceId,
    limit: computed(() => state.data.value.limit),
    queryTimeout: computed(() => state.data.value.queryTimeout),
    timeRange: computed(() => state.data.value.timeRange),
    selectedRelativeTime: computed(() => state.data.value.selectedRelativeTime),
    filterConditions: computed(() => state.data.value.filterConditions),
    rawSql: computed(() => state.data.value.rawSql),
    logchefqlCode: computed(() => state.data.value.logchefqlCode),
    activeMode: computed(() => state.data.value.activeMode),
    error: state.error,
    queryId: computed(() => state.data.value.queryId),
    lastExecutedState: computed(() => state.data.value.lastExecutedState),
    lastExecutionTimestamp: computed(() => state.data.value.lastExecutionTimestamp),
    selectedQueryId: computed(() => state.data.value.selectedQueryId),
    activeSavedQueryName: computed(() => state.data.value.activeSavedQueryName),
    selectedTimezoneIdentifier: computed(() => state.data.value.selectedTimezoneIdentifier),
    generatedDisplaySql: computed(() => state.data.value.generatedDisplaySql),

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
    setTimeConfiguration,
    setLimit,
    setQueryTimeout,
    setFilterConditions,
    setRawSql,
    setActiveMode,
    setLogchefqlCode,
    setSelectedQueryId,
    setActiveSavedQueryName,
    setRelativeTimeRange,
    resetQueryToDefaults,
    resetQueryContentForSourceChange,
    initializeFromUrl,
    hydrateFromResolvedQuery,
    executeQuery,
    cancelQuery,
    getLogContext,
    clearError,
    setGroupByField,
    setTimezoneIdentifier,
    generateAiSql,
    clearAiSqlState,
    fetchHistogramData,

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
