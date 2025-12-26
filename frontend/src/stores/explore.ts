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
  AIGenerateSQLRequest,
  AIGenerateSQLResponse,
} from "@/api/explore";
import type { DateValue } from "@internationalized/date";
import { now, getLocalTimeZone, CalendarDateTime } from "@internationalized/date";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { useBaseStore } from "./base";
import { parseRelativeTimeString, timestampToCalendarDateTime, calendarDateTimeToTimestamp } from "@/utils/time";
import { SqlManager } from '@/services/SqlManager';
import { type TimeRange } from '@/types/query';
import { HistogramService, type HistogramData } from '@/services/HistogramService';
import { useVariables } from "@/composables/useVariables";
import { queryHistoryService } from "@/services/QueryHistoryService";
import { createTimeRangeCondition } from '@/utils/time-utils';

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
  sourceId: number;
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
  // Query stats
  stats?: any;
  // Last executed query state - crucial for dirty checking and consistency
  lastExecutedState?: {
    timeRange: string;
    limit: number;
    mode: "logchefql" | "sql";
    logchefqlQuery?: string;
    sqlQuery: string;
    sourceId: number;
  };
  // Add field for last successful execution timestamp
  lastExecutionTimestamp: number | null;
  // Group by field for histogram
  groupByField: string | null;
  // User's selected timezone identifier (e.g., 'America/New_York', 'UTC')
  selectedTimezoneIdentifier: string | null;
  // Add a new state property to hold the reactively generated SQL for display/internal use
  generatedDisplaySql: string | null;
  // AI SQL generation loading state
  isGeneratingAISQL: boolean;
  // AI SQL generation error message
  aiSqlError: string | null;
  // Generated AI SQL query
  generatedAiSql: string | null;
  // Histogram data state
  histogramData: HistogramData[];
  // Histogram loading state
  isLoadingHistogram: boolean;
  // Histogram error state
  histogramError: string | null;
  // Histogram granularity
  histogramGranularity: string | null;
  // Query timeout in seconds
  queryTimeout: number;
  // Query cancellation state
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
  // Store references for use in computed properties
  const contextStore = useContextStore();
  
  // Initialize base store with default state
  const state = useBaseStore<ExploreState>({
    logs: [],
    columns: [],
    queryStats: DEFAULT_QUERY_STATS,
    sourceId: 0,
    limit: 100,
    timeRange: null,
    selectedRelativeTime: null, // Initialize the relative time selection to null
    filterConditions: [],
    rawSql: "",
    logchefqlCode: "",
    activeMode: "logchefql",
    lastExecutionTimestamp: null,
    selectedQueryId: null,
    activeSavedQueryName: null,
    savedQuerySnapshot: null,
    groupByField: null, // Initialize the groupByField
    selectedTimezoneIdentifier: null, // Initialize the timezone identifier
    generatedDisplaySql: null, // Initialize the new state property
    isGeneratingAISQL: false, // Initialize AI SQL generation loading state
    aiSqlError: null, // Initialize AI SQL generation error message
    generatedAiSql: null, // Initialize generated AI SQL query
    histogramData: [],
    isLoadingHistogram: false,
    histogramError: null,
    histogramGranularity: null,
    queryTimeout: 30, // Default to 30 seconds
    currentQueryAbortController: null,
    currentQueryId: null,
    isCancellingQuery: false,
  });

  // Watch context store for team/source changes
  watch(
    () => contextStore.teamId,
    (newTeamId) => {
      if (newTeamId) {
        // Update the old teams store to maintain compatibility
        const teamsStore = useTeamsStore();
        teamsStore.setCurrentTeam(newTeamId);
      }
    }
  );

  watch(
    () => contextStore.sourceId,
    (newSourceId) => {
      if (newSourceId !== state.data.value.sourceId) {
        setSource(newSourceId || 0);
      }
    }
  );

  // Getters
  const hasValidSource = computed(() => !!state.data.value.sourceId);
  const hasValidTimeRange = computed(() => !!state.data.value.timeRange);
  const canExecuteQuery = computed(() => {
    // Basic requirements
    if (!hasValidSource.value || !hasValidTimeRange.value) {
      return false;
    }
    
    // Allow execution even if source details don't match temporarily
    // The executeQuery function will handle coordination issues gracefully
    return true;
  });
  const isExecutingQuery = computed(() => state.isLoadingOperation('executeQuery'));
  const canCancelQuery = computed(() => 
    (!!state.data.value.currentQueryAbortController || !!state.data.value.currentQueryId) && 
    !state.data.value.isCancellingQuery && 
    isExecutingQuery.value
  );

  // Computed property to check if histogram should be generated
  const isHistogramEligible = computed(() => {
    // Simple rule: Only LogchefQL queries in LogchefQL mode are eligible for histogram
    return state.data.value.activeMode === 'logchefql';
  });

  // Key computed properties from refactoring plan

  // Helper function to build a display SQL (without actual LogchefQL translation - that happens at execution)
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

    let tableName = 'default.logs';
    if (sourceDetails.connection?.database && sourceDetails.connection?.table_name) {
      tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
    } else {
      return null;
    }

    const tsField = sourceDetails._meta_ts_field || 'timestamp';
    const timezone = selectedTimezoneIdentifier || getTimezoneIdentifier();
    const timeCondition = createTimeRangeCondition(tsField, timeRange as TimeRange, true, timezone);

    // Build a basic SQL for display (actual LogchefQL translation happens at execution time via backend)
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

  // 1. Internal computed property for display SQL (actual translation happens at execution)
  const _logchefQlTranslationResult = computed(() => {
    return _buildDisplaySql();
  });

  // 2. Definitive SQL string for execution
  const sqlForExecution = computed(() => {
    const { activeMode, rawSql } = state.data.value;
    if (activeMode === 'sql') {
      // In SQL mode, return the raw SQL exactly as the user wrote it
      // NO modifications - user has full control
      return rawSql;
    }

    // For LogchefQL mode, use the translation result
    const translationResult = _logchefQlTranslationResult.value;
    if (!translationResult) {
      return '';
    }
    console.log("logchefqlCode : "+translationResult.sql);
    return translationResult.sql;
  });

  // 3. Is query state dirty (compared to last executed state)
  const isQueryStateDirty = computed(() => {
    const { lastExecutedState, sourceId, limit, activeMode, logchefqlCode, rawSql } = state.data.value;

    if (!lastExecutedState) {
      return (activeMode === 'logchefql' && !!logchefqlCode?.trim()) ||
             (activeMode === 'sql' && !!rawSql?.trim());
    }

    const timeRangeChanged = JSON.stringify(state.data.value.timeRange) !== lastExecutedState.timeRange;
    const limitChanged = limit !== lastExecutedState.limit;
    const modeChanged = activeMode !== lastExecutedState.mode;
    const sourceChanged = sourceId !== lastExecutedState.sourceId;

    let queryContentChanged = false;
    if (activeMode === 'logchefql') {
      queryContentChanged = logchefqlCode !== lastExecutedState.logchefqlQuery;
    } else {
      queryContentChanged = sqlForExecution.value !== lastExecutedState.sqlQuery;
    }

    return timeRangeChanged || limitChanged || modeChanged || sourceChanged || queryContentChanged;
  });

  // Check if current state diverges from loaded saved query
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
    const { sourceId, timeRange, limit, activeMode, logchefqlCode, rawSql, selectedRelativeTime, selectedQueryId } = state.data.value;
    const teamsStore = useTeamsStore();

    const params: Record<string, string> = {};

    if (teamsStore.currentTeamId) {
      params.team = teamsStore.currentTeamId.toString();
    }

    if (sourceId) {
      params.source = sourceId.toString();
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

  // Get the current timezone identifier
  function getTimezoneIdentifier(): string {
    // Use the stored timezone or default to the browser's local timezone
    return state.data.value.selectedTimezoneIdentifier ||
      (localStorage.getItem('logchef_timezone') === 'utc' ? 'UTC' : getLocalTimeZone());
  }

  // Set the timezone identifier
  function setTimezoneIdentifier(timezone: string) {
    state.data.value.selectedTimezoneIdentifier = timezone;
    console.log(`Explore store: Set timezone identifier to ${timezone}`);
  }

  // Actions - simplified for clean approach
  function setSource(sourceId: number) {
    console.log(`Explore store: Setting source to ${sourceId}`);

    // Clear query results to prevent showing old data
    state.data.value.generatedDisplaySql = null;
    state.data.value.logs = [];
    state.data.value.columns = [];
    state.data.value.queryStats = DEFAULT_QUERY_STATS;
    
    // Clear histogram data and reset execution state
    _clearHistogramData();
    state.data.value.lastExecutionTimestamp = null;
    state.data.value.lastExecutedState = undefined;

    // Set the new source ID
    state.data.value.sourceId = sourceId;

    // Clear queries when source changes to prevent stale table references
    if (state.data.value.activeMode === 'sql') {
      state.data.value.rawSql = '';
      console.log('Explore store: Cleared SQL on source change');
    } else {
      state.data.value.logchefqlCode = '';
      console.log('Explore store: Cleared LogchefQL on source change');
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

  // Set query timeout
  function setQueryTimeout(timeout: number) {
    console.log('Explore store: setQueryTimeout called with:', timeout);
    if (timeout > 0 && timeout <= 3600) { // Max 1 hour timeout
      console.log('Explore store: Setting queryTimeout from', state.data.value.queryTimeout, 'to', timeout);
      state.data.value.queryTimeout = timeout;
    } else {
      console.log('Explore store: Invalid timeout value:', timeout, 'keeping current:', state.data.value.queryTimeout);
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

  // Set active mode with simplified switching logic
  // NOTE: SQL population for mode switching is handled in useQuery.ts changeMode()
  // This function just updates the mode - the rawSql should already be set by the caller
  function setActiveMode(mode: 'logchefql' | 'sql') {
    const currentMode = state.data.value.activeMode;
    if (mode === currentMode) return;

    // Update the mode
    state.data.value.activeMode = mode;
  }

  // Internal action to update last executed state
  function _updateLastExecutedState() {
    state.data.value.lastExecutedState = {
      timeRange: JSON.stringify(state.data.value.timeRange),
      limit: state.data.value.limit,
      mode: state.data.value.activeMode,
      logchefqlQuery: state.data.value.logchefqlCode,
      sqlQuery: sqlForExecution.value,
      sourceId: state.data.value.sourceId
    };
    // Also update the execution timestamp
    state.data.value.lastExecutionTimestamp = Date.now();
  }

  function initializeFromUrl(params: Record<string, string | undefined>): { needsResolve: boolean; queryId?: string; shouldExecute: boolean } {
    console.log('Explore store: Initializing from URL with params:', params);

    if (params.source) {
      const sourceId = parseInt(params.source, 10);
      if (!isNaN(sourceId)) {
        state.data.value.sourceId = sourceId;
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

    const hasRequiredParams = !!(state.data.value.sourceId && state.data.value.timeRange);
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

      return { shouldExecute: !!(state.data.value.sourceId && state.data.value.timeRange) };
    } catch (error) {
      console.error('Failed to hydrate from resolved query:', error);
      return { shouldExecute: false };
    }
  }

  // Helper to clear histogram data
  function _clearHistogramData() {
    state.data.value.histogramData = [];
    state.data.value.histogramError = null;
    state.data.value.histogramGranularity = null;
    state.data.value.isLoadingHistogram = false;
  }

  function _clearQueryContent() {
    state.data.value.logchefqlCode = '';
    state.data.value.rawSql = '';
    state.data.value.activeMode = 'logchefql';
    state.data.value.selectedQueryId = null;
    state.data.value.activeSavedQueryName = null;
    state.data.value.savedQuerySnapshot = null;
  }

  // Reset query to defaults
  function resetQueryToDefaults() {
    // Create a default time range (last 15 minutes)
    const nowDt = now(getLocalTimeZone());
    const timeRange = {
      start: new CalendarDateTime(
        nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
      ).subtract({ minutes: 15 }),
      end: new CalendarDateTime(
        nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
      )
    };

    // Reset time range and relative time
    state.data.value.timeRange = timeRange;
    state.data.value.selectedRelativeTime = '15m';
    state.data.value.limit = 100;

    // Clear all query content
    _clearQueryContent();

    // Generate default SQL
    const sourcesStore = useSourcesStore();
    const sourceDetails = sourcesStore.currentSourceDetails;
    
    // Get table name directly from source details
    let tableName = 'logs.vector_logs'; // Default fallback
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

    // Clear histogram data
    _clearHistogramData();

    // Update last executed state
    _updateLastExecutedState();
  }

  // Reset query content but preserve time range and limit for source changes
  function resetQueryContentForSourceChange() {
    // Clear query content
    _clearQueryContent();

    // Generate SQL for new source if time range exists
    if (state.data.value.timeRange) {
      const sourcesStore = useSourcesStore();
      const sourceDetails = sourcesStore.currentSourceDetails;
      
      // Get table name directly from source details
      let tableName = 'logs.vector_logs'; // Default fallback
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

    // Clear histogram data and mark as dirty
    _clearHistogramData();
    _updateLastExecutedState();
  }

  // Execute query action
  async function executeQuery() {

    // Store the relative time so we can restore it after execution
    const relativeTime = state.data.value.selectedRelativeTime;

    // Cancel any existing query
    if (state.data.value.currentQueryAbortController) {
      state.data.value.currentQueryAbortController.abort();
    }

    // Create new AbortController for this query
    const abortController = new AbortController();
    state.data.value.currentQueryAbortController = abortController;
    state.data.value.isCancellingQuery = false;

    // Reset timestamp at the start of execution attempt
    state.data.value.lastExecutionTimestamp = null;
    
    // Clear histogram data at the start of query execution to prevent stale data
    state.data.value.histogramData = [];
    state.data.value.histogramError = null;
    state.data.value.histogramGranularity = null;
    
    const operationKey = 'executeQuery';

    return await state.withLoading(operationKey, async () => {
      // Get current team ID
      const currentTeamId = useTeamsStore().currentTeamId;
      if (!currentTeamId) {
        return state.handleError({ status: "error", message: "No team selected", error_type: "ValidationError" }, operationKey);
      }

      // Get source details and stores to validate full loading state
      const sourcesStore = useSourcesStore();
      const sourceDetails = sourcesStore.currentSourceDetails;

      // Validate that we have the current source details fully loaded and matching
      if (!sourceDetails || sourceDetails.id !== state.data.value.sourceId) {
        console.warn(`Source details not loaded or mismatch: have ID ${sourceDetails?.id}, need ID ${state.data.value.sourceId}`);
        
        // This is likely a coordination issue during team/source switching
        // Don't show user-facing errors - just silently fail and let the UI retry
        console.log("Explore store: Silently skipping query due to source coordination issue");
        return { 
          success: false, 
          data: null, 
          error: { 
            message: "Source coordination in progress",
            error_type: "CoordinationError"
          }
        };
      }

      // 4. Validate that the source belongs to the current team
      const teamSources = sourcesStore.teamSources || [];
      if (!teamSources.some(s => s.id === state.data.value.sourceId)) {
        console.warn(`Source ${state.data.value.sourceId} does not belong to team ${currentTeamId}`);
        return state.handleError({
          status: "error", 
          message: "Source does not belong to current team. Please refresh the page.",
          error_type: "ValidationError"
        }, operationKey);
      }

      // ========== LOGCHEFQL MODE ==========
      // In LogchefQL mode, the UI controls the query parameters:
      // - Time range: Controlled by the date picker → sent to backend
      // - Limit: Controlled by the limit dropdown → sent to backend
      // - Timezone: Controlled by the timezone setting → sent to backend
      // 
      // Backend builds the full SQL query and executes it
      // Response includes `generated_sql` for "View as SQL" feature
      // Empty LogchefQL query → backend generates default SELECT * query
      // =====================================
      
      if (state.data.value.activeMode === 'logchefql') {
        console.log("Explore store: LogchefQL mode - sending to /logchefql/query endpoint");
        try {
          // Replace dynamic variables with actual values
          const { convertVariables } = useVariables();
          const queryWithVariables = convertVariables(state.data.value.logchefqlCode);
          
          // Format time range for API
          const timeRange = state.data.value.timeRange as TimeRange;
          const timezone = state.data.value.selectedTimezoneIdentifier || getTimezoneIdentifier();
          
          // Format dates as ISO8601 strings
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

          const queryResponse = await logchefqlApi.query(currentTeamId, state.data.value.sourceId, {
            query: queryWithVariables,
            start_time: formatDateTime(timeRange.start),
            end_time: formatDateTime(timeRange.end),
            timezone: timezone,
            limit: state.data.value.limit,
            query_timeout: state.data.value.queryTimeout
          });

          if (queryResponse.data) {
            // Success - update store with results
            state.data.value.logs = queryResponse.data.logs || [];
            state.data.value.columns = queryResponse.data.columns || [];
            state.data.value.queryStats = queryResponse.data.stats || DEFAULT_QUERY_STATS;
            
            // Store query ID for cancellation
            if (queryResponse.data.query_id) {
              state.data.value.currentQueryId = queryResponse.data.query_id;
            }

            // Store the generated SQL for "Show SQL" feature
            if (queryResponse.data.generated_sql) {
              state.data.value.generatedDisplaySql = queryResponse.data.generated_sql;
              console.log("Explore store: Stored generated SQL from backend:", queryResponse.data.generated_sql.substring(0, 100) + "...");
            } else {
              console.warn("Explore store: Backend did not return generated_sql");
            }

            // Update lastExecutedState
            _updateLastExecutedState();

            // Add to query history
            try {
              if (currentTeamId && state.data.value.sourceId) {
                queryHistoryService.addQueryEntry({
                  teamId: currentTeamId,
                  sourceId: state.data.value.sourceId,
                  query: state.data.value.logchefqlCode,
                  mode: 'logchefql'
                });
              }
            } catch (historyError) {
              console.warn("Failed to add query to history:", historyError);
            }

            // Restore relative time if it was set
            if (relativeTime) {
              state.data.value.selectedRelativeTime = relativeTime;
            }

            // Set execution timestamp
            state.data.value.lastExecutionTimestamp = Date.now();

            // Fetch histogram data
            fetchHistogramData();

            return { success: true, data: queryResponse.data, error: null };
          } else {
            // Handle error response - no data means query failed
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

      // ========== SQL MODE ==========
      // In SQL mode, user has FULL CONTROL over their query:
      // - Time range: Specified in the SQL WHERE clause by user
      // - Limit: Specified in the SQL LIMIT clause by user
      // - The frontend does NOT modify the SQL in any way
      // 
      // The raw SQL is sent to /logs/query endpoint exactly as written
      // ================================
      
      let sql = sqlForExecution.value;
      let usedDefaultSql = false;

      console.log("SQL mode execution:", sql.substring(0, 100) + "...");

      // Prepare parameters for the API call
      const params: QueryParams = {
        raw_sql: '', // Will be set below
        limit: state.data.value.limit,
        query_timeout: state.data.value.queryTimeout
      };

      // Handle empty SQL for both modes
      if (!sql || !sql.trim()) {
        // Generate default SQL for both LogchefQL and SQL modes when SQL is empty
        console.log(`Explore store: Generating default SQL for empty ${state.data.value.activeMode} query`);

        const tsField = sourceDetails._meta_ts_field || 'timestamp';
        
        // Use table name directly from the current source details to avoid stale cached values
        let tableName = 'default.logs'; // Default fallback
        if (sourceDetails.connection?.database && sourceDetails.connection?.table_name) {
          tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
          console.log(`Explore store: Using table name from source details: ${tableName}`);
        } else {
          console.log(`Explore store: Using default table name: ${tableName}`);
        }

        // Generate default SQL using SqlManager
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
            message: "Failed to generate default SQL",
            error_type: "ValidationError"
          }, operationKey);
        }

        sql = result.sql;
        // Use the generated SQL
        usedDefaultSql = true;

        // If in SQL mode, update the UI to show the generated SQL
        if (state.data.value.activeMode === 'sql') {
          state.data.value.rawSql = result.sql;
        }
      }

      // dynamic variable to value
      const { convertVariables } = useVariables();
      sql = convertVariables(sql);

      console.log("Replaced dynamic variables in query for validation: " + sql);


      // Set the SQL in the params
      params.raw_sql = sql;

      console.log("Explore store: Executing query with SQL:", {
        sqlLength: sql.length,
        usedDefaultSql
      });

      let response;
      
      try {
        // Use the centralized API calling mechanism from base store
        response = await state.callApi({
          apiCall: async () => exploreApi.getLogs(state.data.value.sourceId, params, currentTeamId, abortController.signal),
          // Update results ONLY on successful API call with data
          onSuccess: (data: QuerySuccessResponse | null) => {
            if (data && (data.data || data.logs)) {
              // We have new data, update the store
              // Handle both new 'data' property and legacy 'logs' property
              state.data.value.logs = data.data || data.logs || [];
              state.data.value.columns = data.columns || [];
              state.data.value.queryStats = data.stats || DEFAULT_QUERY_STATS;
              // Check if query_id exists in params before accessing it
              if (data.params && typeof data.params === 'object' && "query_id" in data.params) {
                state.data.value.queryId = data.params.query_id as string;
              } else {
                state.data.value.queryId = null; // Reset if not present
              }
            } else {
              // Query was successful but returned no logs or null data
              console.warn("Query successful but received no logs or null data.");
              // Clear the logs, columns, stats now that the API call is complete
              state.data.value.logs = [];
              state.data.value.columns = [];
              state.data.value.queryStats = DEFAULT_QUERY_STATS;
              state.data.value.queryId = null;
            }
            
            // Extract query ID from response for cancellation tracking
            if (data && data.query_id) {
              state.data.value.currentQueryId = data.query_id;
              console.log("Stored query ID for cancellation:", state.data.value.currentQueryId);
            }

            // Update lastExecutedState after successful execution
            _updateLastExecutedState();

            // Add query to history
            try {
              const teamsStore = useTeamsStore();
              const currentTeamId = teamsStore.currentTeamId;
              if (currentTeamId && state.data.value.sourceId) {
                const queryContent = state.data.value.activeMode === 'logchefql'
                  ? state.data.value.logchefqlCode
                  : sql;

                queryHistoryService.addQueryEntry({
                  teamId: currentTeamId,
                  sourceId: state.data.value.sourceId,
                  mode: state.data.value.activeMode,
                  query: queryContent,
                  title: state.data.value.activeSavedQueryName || undefined
                });
              }
            } catch (error) {
              console.warn('Failed to save query to history:', error);
            }

            // Restore the relative time if it was set before
            if (relativeTime) {
              state.data.value.selectedRelativeTime = relativeTime;
            }
          },
          operationKey: operationKey,
        });

        // Ensure lastExecutionTimestamp is set even if there was an error
        if (!response.success && state.data.value.lastExecutionTimestamp === null) {
          state.data.value.lastExecutionTimestamp = Date.now();

          // Restore the relative time if it was set before execution, even on error
          if (relativeTime) {
            state.data.value.selectedRelativeTime = relativeTime;
          }
        }

        // SQL mode does not support histogram - user controls their own query
        // Histogram is only available in LogchefQL mode where we have the backend-generated SQL
        if (response.success) {
          console.log("Explore store: SQL mode query successful - histogram not available in this mode");
        }
      } finally {
        // Clean up AbortController and query ID after query completion - this ALWAYS runs
        console.log("Cleaning up query state - AbortController and query ID");
        state.data.value.currentQueryAbortController = null;
        state.data.value.currentQueryId = null;
        state.data.value.isCancellingQuery = false;
      }

      // Return the response
      return response;
    });
  }

  // Cancel current query
  async function cancelQuery() {
    if (state.data.value.isCancellingQuery) {
      return; // Already cancelling
    }
    
    // Prevent cancelling if there's nothing to cancel
    if (!state.data.value.currentQueryAbortController && !state.data.value.currentQueryId) {
      console.warn("Attempted to cancel a query that was already complete.");
      return;
    }

    state.data.value.isCancellingQuery = true;
    
    try {
      // First, abort the HTTP request for immediate user feedback
      if (state.data.value.currentQueryAbortController) {
        state.data.value.currentQueryAbortController.abort();
        console.log("HTTP request aborted");
      }

      // Then try to cancel via backend API if we have a query ID
      if (state.data.value.currentQueryId) {
        const currentTeamId = useTeamsStore().currentTeamId;
        if (currentTeamId && state.data.value.sourceId) {
          try {
            await exploreApi.cancelQuery(
              state.data.value.sourceId,
              state.data.value.currentQueryId,
              currentTeamId
            );
            console.log("Query cancelled via backend API");
          } catch (error) {
            console.warn("Backend query cancellation failed, but HTTP request was aborted:", error);
            // Don't show error to user since HTTP cancellation worked
          }
        }
      }
      
      console.log("Query cancellation requested");
    } catch (error) {
      console.error("An error occurred during the cancellation process:", error);
    }
    // Note: isCancellingQuery will be reset by executeQuery's finally block to avoid race conditions
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

  // Add setFilterConditions function
  function setFilterConditions(filters: FilterCondition[]) {
    // Check for the special _force_clear marker
    const isForceClearing =
      filters.length === 1 && "_force_clear" in filters[0];

    if (isForceClearing) {
      console.log("Explore store: force-clearing filter conditions");
      // This is our special signal to clear conditions
      state.data.value.filterConditions = [];
    } else {
      console.log("Explore store: setting filter conditions:", filters.length);
      state.data.value.filterConditions = filters;
    }
  }

  // Add setSelectedQueryId function
  function setSelectedQueryId(queryId: string | null) {
    state.data.value.selectedQueryId = queryId;
  }

  // Add setActiveSavedQueryName function
  function setActiveSavedQueryName(name: string | null) {
    state.data.value.activeSavedQueryName = name;
  }

  // Add getLogContext function
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
        showToast: false, // Typically don't toast for context fetches
      });
    });
  }

  // Add clearError function
  function clearError() {
    state.error.value = null;
  }

  // Add setGroupByField function
  function setGroupByField(field: string | null) {
    state.data.value.groupByField = field;
  }

  // Add generateAiSql function
  async function generateAiSql(naturalLanguageQuery: string, currentQuery?: string) {
    const operationKey = 'generateAiSql';

    // Set loading state
    state.data.value.isGeneratingAISQL = true;
    state.data.value.aiSqlError = null;
    state.data.value.generatedAiSql = null;

    try {
      const teamsStore = useTeamsStore();
      const currentTeamId = teamsStore.currentTeamId;
      if (!currentTeamId) {
        throw new Error("No team selected");
      }

      const sourcesStore = useSourcesStore();
      const sourceDetails = sourcesStore.currentSourceDetails;
      if (!sourceDetails) {
        throw new Error("Source details not available");
      }

      const request: AIGenerateSQLRequest = {
        natural_language_query: naturalLanguageQuery,
        current_query: currentQuery // Include current query if provided
      };

      const response = await state.callApi<AIGenerateSQLResponse>({
        // The API expects sourceId as first parameter, then the request, then teamId
        apiCall: () => exploreApi.generateAISQL(
          state.data.value.sourceId,
          request,
          currentTeamId
        ),
        operationKey: operationKey,
      });

      if (response.success && response.data) {
        // Use the correct property name from AIGenerateSQLResponse
        state.data.value.generatedAiSql = response.data.sql_query || '';

        // Automatically set the SQL if in SQL mode
        if (state.data.value.activeMode === 'sql') {
          state.data.value.rawSql = response.data.sql_query || '';
        }

        return response;
      } else {
        state.data.value.aiSqlError = response.error?.message || 'Failed to generate SQL';
        return response;
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error occurred';
      state.data.value.aiSqlError = errorMessage;
      return {
        success: false,
        error: { message: errorMessage, status: 'error', error_type: 'AIGenerationError' }
      };
    } finally {
      state.data.value.isGeneratingAISQL = false;
    }
  }

  // Add clearAiSqlState function
  function clearAiSqlState() {
    state.data.value.isGeneratingAISQL = false;
    state.data.value.aiSqlError = null;
    state.data.value.generatedAiSql = null;
  }

  // ========== HISTOGRAM ==========
  // Histogram is only available in LogchefQL mode.
  // It uses the `generatedDisplaySql` from the backend (the actual executed query).
  // This ensures consistency - histogram shows data for the exact query that was run.
  // ================================
  
  async function fetchHistogramData(granularity?: string) {
    const operationKey = 'fetchHistogramData';

    // Check if histogram is eligible (LogchefQL mode only)
    if (!isHistogramEligible.value) {
      console.log("Explore store: Histogram only available in LogchefQL mode");
      _clearHistogramData();
      state.data.value.histogramError = "Histogram is only available for LogchefQL queries";
      return { success: false, error: { message: "Histogram is only available for LogchefQL queries" } };
    }

    // Must have generated SQL from a previous LogchefQL execution
    const sql = state.data.value.generatedDisplaySql;
    if (!sql) {
      console.log("Explore store: No generated SQL available for histogram");
      _clearHistogramData();
      state.data.value.histogramError = "Run a LogchefQL query first to see the histogram";
      return { success: false, error: { message: "Run a LogchefQL query first" } };
    }

    // Set loading state
    state.data.value.isLoadingHistogram = true;
    state.data.value.histogramError = null;

    try {
      const currentTeamId = useTeamsStore().currentTeamId;
      if (!currentTeamId) {
        state.data.value.histogramError = "No team selected";
        state.data.value.isLoadingHistogram = false;
        return { success: false, error: { message: "No team selected" } };
      }

      console.log("Explore store: Fetching histogram with backend-generated SQL", {
        sqlLength: sql.length,
        sql: sql.substring(0, 100) + "..."
      });

      const timeRange = state.data.value.timeRange;
      let windowGranularity = granularity;
      if (!windowGranularity && timeRange) {
        const startISO = new Date(
          timeRange.start.year, timeRange.start.month - 1, timeRange.start.day,
          'hour' in timeRange.start ? timeRange.start.hour : 0,
          'minute' in timeRange.start ? timeRange.start.minute : 0,
          'second' in timeRange.start ? timeRange.start.second : 0
        ).toISOString();
        const endISO = new Date(
          timeRange.end.year, timeRange.end.month - 1, timeRange.end.day,
          'hour' in timeRange.end ? timeRange.end.hour : 0,
          'minute' in timeRange.end ? timeRange.end.minute : 0,
          'second' in timeRange.end ? timeRange.end.second : 0
        ).toISOString();
        windowGranularity = HistogramService.calculateOptimalGranularity(startISO, endISO);
      }

      const params = {
        raw_sql: sql,
        limit: 100,
        window: windowGranularity || '1m',
        timezone: state.data.value.selectedTimezoneIdentifier || undefined,
        group_by: state.data.value.groupByField === "__none__" || state.data.value.groupByField === null 
          ? undefined 
          : state.data.value.groupByField,
        query_timeout: state.data.value.queryTimeout,
      };

      const response = await state.callApi<{ data: Array<HistogramData>, granularity: string }>({
        apiCall: async () => exploreApi.getHistogramData(
          state.data.value.sourceId,
          params,
          currentTeamId
        ),
        operationKey,
        showToast: false,
      });

      if (response.success && response.data) {
        state.data.value.histogramData = response.data.data || [];
        state.data.value.histogramGranularity = response.data.granularity || null;
        state.data.value.histogramError = null;
      } else {
        state.data.value.histogramData = [];
        state.data.value.histogramGranularity = null;
        state.data.value.histogramError = response.error?.message || "Failed to fetch histogram data";
      }

      return response;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      console.error("Error in fetchHistogramData:", errorMessage);
      _clearHistogramData();
      state.data.value.histogramError = errorMessage;
      return { success: false, error: { message: errorMessage } };
    } finally {
      state.data.value.isLoadingHistogram = false;
    }
  }

  // Return the store
  return {
    // State - exposed as computed properties
    logs: computed(() => state.data.value.logs),
    columns: computed(() => state.data.value.columns),
    queryStats: computed(() => state.data.value.queryStats),
    sourceId: computed(() => state.data.value.sourceId),
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
    groupByField: computed(() => state.data.value.groupByField),
    selectedTimezoneIdentifier: computed(() => state.data.value.selectedTimezoneIdentifier),

    // AI SQL generation state
    isGeneratingAISQL: computed(() => state.data.value.isGeneratingAISQL),
    aiSqlError: computed(() => state.data.value.aiSqlError),
    generatedAiSql: computed(() => state.data.value.generatedAiSql),

    // Generated SQL from last LogchefQL query execution (for "View as SQL" feature)
    generatedDisplaySql: computed(() => state.data.value.generatedDisplaySql),

    // Histogram state
    histogramData: computed(() => state.data.value.histogramData),
    isLoadingHistogram: computed(() => state.data.value.isLoadingHistogram),
    histogramError: computed(() => state.data.value.histogramError),
    histogramGranularity: computed(() => state.data.value.histogramGranularity),

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
