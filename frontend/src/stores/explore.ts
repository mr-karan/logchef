import { defineStore } from "pinia";
import { computed, watch } from "vue";
import { exploreApi } from "@/api/explore";
import type {
  ColumnInfo,
  QueryStats,
  FilterCondition,
  QueryResponse,
  QueryParams,
  LogContextRequest,
  LogContextResponse,
  QueryErrorResponse,
  QuerySuccessResponse,
  AIGenerateSQLRequest,
  AIGenerateSQLResponse,
} from "@/api/explore";
import type { QueryResult } from "@/types/query";
import type { DateValue } from "@internationalized/date";
import { now, getLocalTimeZone, CalendarDateTime } from "@internationalized/date";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useBaseStore } from "./base";
import { QueryService } from '@/services/QueryService'
import { parseRelativeTimeString } from "@/utils/time";
import type { APIErrorResponse, APISuccessResponse, APIResponse } from "@/api/types";
import { SqlManager } from '@/services/SqlManager';
import { type TimeRange } from '@/types/query';
import { getErrorMessage } from '@/api/types';
import { HistogramService, type HistogramData } from '@/services/HistogramService';
import { useVariables } from "@/composables/useVariables";
import { useToast } from "@/composables/useToast";

// Helper function to get formatted table name
export function getFormattedTableName(source: any): string {
  if (!source) {
    console.error("getFormattedTableName called with null/undefined source");
    return "logs.vector_logs"; // Default fallback
  }

  if (source?.connection?.database && source?.connection?.table_name) {
    return `${source.connection.database}.${source.connection.table_name}`;
  }

  console.warn("Source missing connection details:", source);

  // Try to extract information from source if possible
  let database = source?.connection?.database || "logs";
  let tableName = source?.connection?.table_name || "vector_logs";

  return `${database}.${tableName}`; // Default fallback
}

// Helper to get the default time range (15 minutes)
export function getDefaultTimeRange() {
  const now = new Date();
  const fifteenMinutesAgo = new Date(now);
  fifteenMinutesAgo.setMinutes(now.getMinutes() - 15); // Fifteen minutes ago

  // Return timestamps in milliseconds
  return {
    start: fifteenMinutesAgo.getTime(),
    end: now.getTime(),
  };
}

export interface ExploreState {
  logs: Record<string, any>[];
  columns: ColumnInfo[];
  queryStats: QueryStats;
  // Core query state
  sourceId: number;
  limit: number;
  timeRange: {
    start: DateValue;
    end: DateValue;
  } | null;
  // Selected relative time window (e.g., "15m", "1h", "7d", "today", etc.)
  selectedRelativeTime: string | null;
  // UI state for filter builder
  filterConditions: FilterCondition[];
  // Raw SQL state
  rawSql: string;
  // Pending raw SQL to execute when source details are available
  pendingRawSql?: string;
  // SQL for display (with human-readable timestamps)
  displaySql?: string;
  // LogchefQL query
  logchefQuery?: string;
  // LogchefQL code state
  logchefqlCode: string;
  // Active mode (logchefql or sql)
  activeMode: "logchefql" | "sql";
  // Loading and error states
  isLoading?: boolean;
  error?: string | null;
  // Query ID for tracking
  queryId?: string | null;
  // ID of the currently loaded saved query (if any)
  selectedQueryId: string | null;
  // Name of the currently active saved query (if any)
  activeSavedQueryName: string | null;
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
    selectedQueryId: null, // Initialize new state
    activeSavedQueryName: null,
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

  // Getters
  const hasValidSource = computed(() => !!state.data.value.sourceId);
  const hasValidTimeRange = computed(() => !!state.data.value.timeRange);
  const canExecuteQuery = computed(
    () => hasValidSource.value && hasValidTimeRange.value
  );
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

  // 1. Internal computed property to translate LogchefQL to SQL
  const _logchefQlTranslationResult = computed(() => {
    const { logchefqlCode, timeRange, limit, selectedTimezoneIdentifier } = state.data.value;

    console.log("logchefqlCode : "+logchefqlCode);
    if (!logchefqlCode || !timeRange || !timeRange.start || !timeRange.end) {
      return null;
    }

    const sourcesStore = useSourcesStore();
    const sourceDetails = sourcesStore.currentSourceDetails;
    if (!sourceDetails) {
      return null;
    }

    const tableName = sourcesStore.getCurrentSourceTableName;
    if (!tableName) {
      return null;
    }

    const tsField = sourceDetails._meta_ts_field || 'timestamp';
    const timezone = selectedTimezoneIdentifier || getTimezoneIdentifier();

    try {
      const result = QueryService.translateLogchefQLToSQL({
        tableName,
        tsField,
        timeRange,
        limit,
        logchefqlQuery: logchefqlCode,
        timezone
      });

      return {
        sql: result.success ? result.sql : '',
        error: result.success ? undefined : result.error,
        warnings: result.warnings
      };
    } catch (error) {
      return {
        sql: '',
        error: error instanceof Error ? error.message : 'Translation error',
        warnings: []
      };
    }
  });

  // 2. Definitive SQL string for execution
  const sqlForExecution = computed(() => {
    const { activeMode, rawSql, limit } = state.data.value;
    if (activeMode === 'sql') {
      // Use SqlManager to ensure correct limit
      return SqlManager.ensureCorrectLimit(rawSql, limit);
    }

    // For LogchefQL mode, use the translation result
    const translationResult = _logchefQlTranslationResult.value;
    if (!translationResult) {
      return '';
    }
    console.log("logchefqlCode : "+translationResult.sql);
    return translationResult.sql;
  });

  // 3. Is query state dirty
  const isQueryStateDirty = computed(() => {
    const { lastExecutedState, sourceId, limit, activeMode, logchefqlCode, rawSql } = state.data.value;

    if (!lastExecutedState) {
      // If there's no last executed state, check if we have any query content
      return (activeMode === 'logchefql' && !!logchefqlCode?.trim()) ||
             (activeMode === 'sql' && !!rawSql?.trim());
    }

    // Compare current state with last executed state
    const timeRangeChanged = JSON.stringify(state.data.value.timeRange) !== lastExecutedState.timeRange;
    const limitChanged = limit !== lastExecutedState.limit;
    const modeChanged = activeMode !== lastExecutedState.mode;
    const sourceChanged = sourceId !== lastExecutedState.sourceId;

    // Compare query content based on mode
    let queryContentChanged = false;
    if (activeMode === 'logchefql') {
      queryContentChanged = logchefqlCode !== lastExecutedState.logchefqlQuery;
    } else {
      queryContentChanged = sqlForExecution.value !== lastExecutedState.sqlQuery;
    }

    return timeRangeChanged || limitChanged || modeChanged || sourceChanged || queryContentChanged;
  });

  // 4. URL query parameters based on current state
  const urlQueryParameters = computed(() => {
    const { sourceId, timeRange, limit, activeMode, logchefqlCode, rawSql, selectedRelativeTime, selectedQueryId } = state.data.value;
    const teamsStore = useTeamsStore();

    const params: Record<string, string> = {};

    // Team and source
    if (teamsStore.currentTeamId) {
      params.team = teamsStore.currentTeamId.toString();
    }

    if (sourceId) {
      params.source = sourceId.toString();
    }

    // Time configuration
    if (selectedRelativeTime) {
      params.time = selectedRelativeTime;
    } else if (timeRange) {
      // Format absolute time range for URL
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

    // Limit
    params.limit = limit.toString();

    // Mode and query content
    params.mode = activeMode;

    // Send raw query content without any encoding - the router will handle the encoding
    if (activeMode === 'logchefql' && logchefqlCode) {
      params.q = logchefqlCode;
    } else if (activeMode === 'sql' && rawSql) {
      params.sql = rawSql;
    }

    // Saved query ID if applicable
    if (selectedQueryId) {
      params.query_id = selectedQueryId;
    }

    return params;
  });

  // Helper to get timestamps in milliseconds
  function getTimestamps() {
    const { timeRange } = state.data.value;
    if (!timeRange) {
      // If no time range is set, default to "now to 15 minutes ago"
      console.warn("No time range set, using default (now to 15 minutes ago)");
      return getDefaultTimeRange();
    }

    const startDate = new Date(
      timeRange.start.year,
      timeRange.start.month - 1,
      timeRange.start.day,
      "hour" in timeRange.start ? timeRange.start.hour : 0,
      "minute" in timeRange.start ? timeRange.start.minute : 0,
      "second" in timeRange.start ? timeRange.start.second : 0
    );

    const endDate = new Date(
      timeRange.end.year,
      timeRange.end.month - 1,
      timeRange.end.day,
      "hour" in timeRange.end ? timeRange.end.hour : 0,
      "minute" in timeRange.end ? timeRange.end.minute : 0,
      "second" in timeRange.end ? timeRange.end.second : 0
    );

    const start = startDate.getTime();
    const end = endDate.getTime();

    console.log("Time range:", {
      startDate: startDate.toISOString(),
      endDate: endDate.toISOString(),
      startMs: start,
      endMs: end,
    });

    return {
      start,
      end,
    };
  }

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

  // Actions
  function setSource(sourceId: number) {
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

    // Generate appropriate SQL for new source
    const sourcesStore = useSourcesStore();
    if (sourceId && sourcesStore.getCurrentSourceTableName && state.data.value.timeRange) {
      const tableName = sourcesStore.getCurrentSourceTableName;
      const tsField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';

      if (state.data.value.activeMode === 'sql') {
        const result = QueryService.generateDefaultSQL({
          tableName,
          tsField,
          timeRange: state.data.value.timeRange,
          limit: state.data.value.limit
        });
        state.data.value.rawSql = result.success ? result.sql : '';
      } else {
        // In LogchefQL mode, just clear the query
        state.data.value.logchefqlCode = '';
      }
    }
  }

  // Set time configuration (absolute or relative)
  function setTimeConfiguration(config: { absoluteRange?: { start: DateValue; end: DateValue }, relativeTime?: string }) {
    if (config.relativeTime) {
      setRelativeTimeRange(config.relativeTime);
    } else if (config.absoluteRange) {
      state.data.value.timeRange = config.absoluteRange;
      state.data.value.selectedRelativeTime = null; // Clear relative time when setting absolute
    }
  }

  // Set limit
  function setLimit(newLimit: number) {
    if (newLimit > 0 && newLimit <= 10000) {
      state.data.value.limit = newLimit;
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

  // Set LogchefQL code
  function setLogchefqlCode(code: string) {
    state.data.value.logchefqlCode = code;
  }

  // Set raw SQL
  function setRawSql(sql: string) {
    state.data.value.rawSql = sql;
  }

  // Set active mode with simplified switching logic
  function setActiveMode(mode: 'logchefql' | 'sql') {
    const currentMode = state.data.value.activeMode;
    if (mode === currentMode) return;

    // Simple mode switching: translate LogchefQL to SQL when switching to SQL mode
    if (mode === 'sql' && currentMode === 'logchefql') {
      const { logchefqlCode } = state.data.value;
      
      if (logchefqlCode && _logchefQlTranslationResult.value?.sql) {
        // If LogchefQL exists and translates, set rawSql
        state.data.value.rawSql = _logchefQlTranslationResult.value.sql;
      } else {
        // If no LogchefQL code, generate default SQL
        const sourcesStore = useSourcesStore();
        if (sourcesStore.getCurrentSourceTableName && state.data.value.timeRange) {
          const result = SqlManager.generateDefaultSql({
            tableName: sourcesStore.getCurrentSourceTableName,
            tsField: sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp',
            timeRange: state.data.value.timeRange,
            limit: state.data.value.limit,
            timezone: state.data.value.selectedTimezoneIdentifier || undefined
          });

          if (result.success) {
            state.data.value.rawSql = result.sql;
          }
        }
      }
    }

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

  // Initialize from URL parameters
  function initializeFromUrl(params: Record<string, string | undefined>) {
    console.log('Explore store: Initializing from URL with params:', params);

    // Parse source ID
    if (params.source) {
      const sourceId = parseInt(params.source, 10);
      if (!isNaN(sourceId)) {
        state.data.value.sourceId = sourceId;
      }
    }

    // Parse time configuration
    if (params.time) {
      // Handle relative time
      setRelativeTimeRange(params.time);
    } else if (params.start && params.end) {
      // Handle absolute time range
      try {
        const startTs = parseInt(params.start, 10);
        const endTs = parseInt(params.end, 10);

        if (!isNaN(startTs) && !isNaN(endTs)) {
          const startDate = new Date(startTs);
          const endDate = new Date(endTs);

          const startCalendar = new CalendarDateTime(
            startDate.getFullYear(),
            startDate.getMonth() + 1,
            startDate.getDate(),
            startDate.getHours(),
            startDate.getMinutes(),
            startDate.getSeconds()
          );

          const endCalendar = new CalendarDateTime(
            endDate.getFullYear(),
            endDate.getMonth() + 1,
            endDate.getDate(),
            endDate.getHours(),
            endDate.getMinutes(),
            endDate.getSeconds()
          );

          state.data.value.timeRange = {
            start: startCalendar,
            end: endCalendar
          };
          state.data.value.selectedRelativeTime = null;
        }
      } catch (error) {
        console.error('Failed to parse time range from URL:', error);
      }
    }

    // Parse limit
    if (params.limit) {
      const limit = parseInt(params.limit, 10);
      if (!isNaN(limit)) {
        setLimit(limit);
      }
    }

    // Parse mode and query content
    if (params.mode) {
      const mode = params.mode === 'sql' ? 'sql' : 'logchefql';
      state.data.value.activeMode = mode;

      if (mode === 'logchefql' && params.q) {
        // No need to decode - the router already decoded the URL parameter
        state.data.value.logchefqlCode = params.q;
      } else if (mode === 'sql' && params.sql) {
        state.data.value.rawSql = params.sql;
      }
    } else {
      // Handle backward compatibility where mode isn't specified but query is
      if (params.q) {
        state.data.value.activeMode = 'logchefql';
        // No need to decode - the router already decoded the URL parameter
        state.data.value.logchefqlCode = params.q;
      } else if (params.sql) {
        state.data.value.activeMode = 'sql';
        state.data.value.rawSql = params.sql;
      }
    }

    // Handle saved query ID
    if (params.query_id) {
      state.data.value.selectedQueryId = params.query_id;
    }

    // After initializing all values, update lastExecutedState to mark the initial state as "clean"
    _updateLastExecutedState();

    // Execute query automatically after initialization if we have enough parameters
    // Wait for the next tick to ensure all reactive properties are updated
    setTimeout(() => {
      const hasRequiredParams = state.data.value.sourceId && state.data.value.timeRange;
      const hasQueryContent = state.data.value.activeMode === 'sql' ? !!state.data.value.rawSql : true;

      if (hasRequiredParams && hasQueryContent) {
        console.log('Explore store: Executing query automatically after URL initialization');
        executeQuery().catch(error => {
          console.error('Error executing initial query:', error);
        });
      } else {
        console.log('Explore store: Skipping automatic query execution, missing required parameters', {
          hasSource: !!state.data.value.sourceId,
          hasTimeRange: !!state.data.value.timeRange,
          hasQueryContent
        });
      }
    }, 100);
  }

  // Helper to clear histogram data
  function _clearHistogramData() {
    state.data.value.histogramData = [];
    state.data.value.histogramError = null;
    state.data.value.histogramGranularity = null;
    state.data.value.isLoadingHistogram = false;
  }

  // Helper to clear query content
  function _clearQueryContent() {
    state.data.value.logchefqlCode = '';
    state.data.value.rawSql = '';
    state.data.value.activeMode = 'logchefql';
    state.data.value.selectedQueryId = null;
    state.data.value.activeSavedQueryName = null;
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
    const tableName = sourcesStore.getCurrentSourceTableName || 'logs.vector_logs';
    const timestampField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';

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
      const tableName = sourcesStore.getCurrentSourceTableName || 'logs.vector_logs';
      const timestampField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';

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

      // Get source details to check they're fully loaded
      const sourcesStore = useSourcesStore();
      const sourceDetails = sourcesStore.currentSourceDetails;

      // Validate that we have the current source details loaded
      if (!sourceDetails || sourceDetails.id !== state.data.value.sourceId) {
        console.warn(`Source details not loaded or mismatch: have ID ${sourceDetails?.id}, need ID ${state.data.value.sourceId}`);
        return state.handleError({
          status: "error",
          message: "Source details not fully loaded. Please try again.",
          error_type: "ValidationError"
        }, operationKey);
      }

      // Get the SQL to execute
      let sql = sqlForExecution.value;

      console.log("logchef sqlForExecution.value; "+sql);

      let usedDefaultSql = false;

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
        const tableName = sourcesStore.getCurrentSourceTableName || 'default.logs';

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

        // If the query was successful, also fetch histogram data
        if (response.success) {
          console.log("Explore store: Query successful, fetching histogram data with same SQL");
          // Use a setTimeout to avoid blocking the UI
          setTimeout(() => {
            fetchHistogramData(sql).catch(error => {
              console.error("Error fetching histogram data:", error);
            });
          }, 50);
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
        
        // Show toast notification after successful abort
        const { toast } = useToast();
        toast({
          title: "Query Cancelled",
          description: "The running query has been cancelled.",
          variant: "default"
        });
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

  // Add function to set relative time range
  function setRelativeTimeRange(relativeTimeString: string | null) {
    if (!relativeTimeString) {
      state.data.value.selectedRelativeTime = null;
      return;
    }

    try {
      // Parse the relative time string into absolute start/end times
      const { start, end } = parseRelativeTimeString(relativeTimeString);

      // Store the relative time string for URL sharing FIRST
      state.data.value.selectedRelativeTime = relativeTimeString;

      // Then set the absolute time range WITHOUT clearing the selectedRelativeTime
      // We need to update the internal timeRange property directly to avoid
      // the automatic clearing of selectedRelativeTime that happens in setTimeRange
      state.data.value.timeRange = { start, end };

      console.log('Explore store: Set relative time range:', {
        relativeTime: relativeTimeString,
        absoluteRange: {
          start: start.toString(),
          end: end.toString()
        }
      });
    } catch (error) {
      console.error('Failed to parse relative time string:', error);
      // Don't update the state if parsing fails
    }
  }

  // Function to get the currently selected relative time
  function getSelectedRelativeTime() {
    return state.data.value.selectedRelativeTime;
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

  // Add setLastExecutedState function
  function setLastExecutedState(executedState: {
    timeRange: string;
    limit: number;
    query?: string;
    mode?: "logchefql" | "sql";
    logchefqlQuery?: string;
    sqlQuery?: string;
  }) {
    console.log('Explore store: Setting last executed state:', executedState);
    const updatedState = {
      ...executedState,
      mode: executedState.mode || state.data.value.activeMode,
      sourceId: state.data.value.sourceId
    };
    state.data.value.lastExecutedState = updatedState as any;
  }

  // Add updateExecutionTimestamp function
  function updateExecutionTimestamp() {
    console.log('Explore store: Updating execution timestamp');
    state.data.value.lastExecutionTimestamp = Date.now();
  }

  // Add resetState function
  function resetState() {
    // Preserve certain values during reset
    const preserved = {
      sourceId: state.data.value.sourceId,
      limit: state.data.value.limit,
      timeRange: state.data.value.timeRange,
      selectedRelativeTime: state.data.value.selectedRelativeTime,
      activeMode: state.data.value.activeMode,
      selectedTimezoneIdentifier: state.data.value.selectedTimezoneIdentifier,
      queryTimeout: state.data.value.queryTimeout,
    };

    state.data.value = {
      logs: [],
      columns: [],
      queryStats: DEFAULT_QUERY_STATS,
      ...preserved,
      filterConditions: [],
      rawSql: "",
      logchefqlCode: "",
      lastExecutedState: undefined,
      lastExecutionTimestamp: null,
      selectedQueryId: null,
      activeSavedQueryName: null,
      groupByField: null,
      generatedDisplaySql: null,
      isGeneratingAISQL: false,
      aiSqlError: null,
      generatedAiSql: null,
      histogramData: [],
      isLoadingHistogram: false,
      histogramError: null,
      histogramGranularity: null,
    };
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

  // Add a function to fetch histogram data with the exact same SQL as the query
  async function fetchHistogramData(sql?: string, granularity?: string) {
    const operationKey = 'fetchHistogramData';

    // Check if histogram is eligible for current query state
    if (!isHistogramEligible.value) {
      console.log("Explore store: Skipping histogram - only available for LogchefQL mode", {
        activeMode: state.data.value.activeMode
      });
      
      // Clear histogram data and set appropriate state
      state.data.value.histogramData = [];
      state.data.value.histogramGranularity = null;
      state.data.value.histogramError = "Histogram is only available for LogchefQL queries";
      state.data.value.isLoadingHistogram = false;
      
      return {
        success: false,
        error: {
          status: "skipped",
          message: "Histogram is only available for LogchefQL queries",
          error_type: "HistogramSkipped"
        }
      };
    }

    // Set loading state
    state.data.value.isLoadingHistogram = true;
    state.data.value.histogramError = null;

    try {
      // Get current team ID
      const currentTeamId = useTeamsStore().currentTeamId;
      if (!currentTeamId) {
        state.data.value.histogramError = "No team selected";
        state.data.value.isLoadingHistogram = false;
        return {
          success: false,
          error: {
            status: "error",
            message: "No team selected",
            error_type: "ValidationError"
          }
        };
      }

      // Get source details
      const sourcesStore = useSourcesStore();

      // Validate source details are loaded
      if (!sourcesStore.currentSourceDetails || sourcesStore.currentSourceDetails.id !== state.data.value.sourceId) {
        console.warn(`Histogram: Source details not loaded or mismatch. Have ID ${sourcesStore.currentSourceDetails?.id}, need ID ${state.data.value.sourceId}`);
        state.data.value.histogramError = "Source details not fully loaded for histogram.";
        state.data.value.isLoadingHistogram = false;
        return {
          success: false,
          error: {
            status: "error",
            message: "Source details not fully loaded for histogram.",
            error_type: "ValidationError"
          }
        };
      }
      
      // Validate source is connected and valid
      if (!sourcesStore.hasValidCurrentSource) {
        console.warn(`Histogram: Source ${state.data.value.sourceId} is not connected or valid`);
        state.data.value.histogramError = "Source is not connected or valid.";
        state.data.value.isLoadingHistogram = false;
        return {
          success: false,
          error: {
            status: "error", 
            message: "Source is not connected or valid.",
            error_type: "ValidationError"
          }
        };
      }

      // Validate SQL input - if empty or not provided, use sqlForExecution
      let finalSql = sql || sqlForExecution.value;
      if (!finalSql || !finalSql.trim()) {
        console.log("Histogram: No SQL provided, generating default query");

        const tsField = sourcesStore.currentSourceDetails._meta_ts_field || 'timestamp';
        const tableName = sourcesStore.getCurrentSourceTableName || 'default.logs';

        // Generate default SQL using SqlManager
        const result = SqlManager.generateDefaultSql({
          tableName,
          tsField,
          timeRange: state.data.value.timeRange as TimeRange,
          limit: state.data.value.limit,
          timezone: state.data.value.selectedTimezoneIdentifier || undefined
        });

        if (!result.success) {
          state.data.value.histogramError = "Failed to generate default SQL for histogram";
          state.data.value.isLoadingHistogram = false;
          return {
            success: false,
            error: {
              status: "error",
              message: "Failed to generate default SQL for histogram",
              error_type: "ValidationError"
            }
          };
        }

        finalSql = result.sql;
      }

      console.log("Explore store: Fetching histogram data with SQL", {
        sourceId: state.data.value.sourceId,
        sqlLength: finalSql.length,
        sql: finalSql.substring(0, 100) + (finalSql.length > 100 ? '...' : '')
      });

      // The final histogram parameters including only the SQL with time range
      const params = {
        raw_sql: finalSql, // Always use a valid SQL query with time range included
        limit: 100,
        window: granularity || calculateOptimalGranularity(),
        timezone: state.data.value.selectedTimezoneIdentifier || undefined,
        group_by: state.data.value.groupByField === "__none__" || state.data.value.groupByField === null ? undefined : state.data.value.groupByField,
        query_timeout: state.data.value.queryTimeout,
      };

      console.log("Explore store: Histogram request params:", {
        raw_sql_length: params.raw_sql.length,
        window: params.window,
        group_by: params.group_by,
        timezone: params.timezone
      });

      // Call the API directly
      const response = await state.callApi<{ data: Array<HistogramData>, granularity: string }>({
        apiCall: async () => exploreApi.getHistogramData(
          state.data.value.sourceId,
          params,
          currentTeamId
        ),
        operationKey: operationKey,
        showToast: false, // Don't show toast for histogram data
      });

      // Update histogram state based on response
      if (response.success && response.data) {
        console.log("Explore store: Histogram data fetch successful", {
          dataPoints: response.data.data?.length || 0,
          granularity: response.data.granularity
        });

        state.data.value.histogramData = response.data.data || [];
        state.data.value.histogramGranularity = response.data.granularity || null;
        state.data.value.histogramError = null;
      } else {
        console.warn("Explore store: Histogram data fetch failed", response.error);
        state.data.value.histogramData = [];
        state.data.value.histogramGranularity = null;
        state.data.value.histogramError = response.error?.message || "Failed to fetch histogram data";
      }

      return response;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      console.error("Error in fetchHistogramData:", errorMessage);

      state.data.value.histogramData = [];
      state.data.value.histogramGranularity = null;
      state.data.value.histogramError = errorMessage;

      return {
        success: false,
        error: {
          message: errorMessage,
          status: "error",
          error_type: "UnknownError"
        }
      };
    } finally {
      state.data.value.isLoadingHistogram = false;
    }
  }

  // Helper function to calculate optimal granularity based on time range
  function calculateOptimalGranularity(): string {
    // Get the time range values
    const { timeRange } = state.data.value;
    if (!timeRange) return "1m"; // Default to 1 minute

    // Calculate time difference in milliseconds
    const startDate = new Date(
      timeRange.start.year,
      timeRange.start.month - 1,
      timeRange.start.day,
      "hour" in timeRange.start ? timeRange.start.hour : 0,
      "minute" in timeRange.start ? timeRange.start.minute : 0,
      "second" in timeRange.start ? timeRange.start.second : 0
    );

    const endDate = new Date(
      timeRange.end.year,
      timeRange.end.month - 1,
      timeRange.end.day,
      "hour" in timeRange.end ? timeRange.end.hour : 0,
      "minute" in timeRange.end ? timeRange.end.minute : 0,
      "second" in timeRange.end ? timeRange.end.second : 0
    );

    const diffMs = endDate.getTime() - startDate.getTime();
    const diffSeconds = diffMs / 1000;
    const diffMinutes = diffSeconds / 60;
    const diffHours = diffMinutes / 60;
    const diffDays = diffHours / 24;

    // Use the same granularity rules as in HistogramService
    if (diffMinutes <= 5) return "1s";
    if (diffMinutes <= 30) return "5s";
    if (diffMinutes <= 60) return "10s";
    if (diffHours <= 3) return "1m";
    if (diffHours <= 12) return "5m";
    if (diffHours <= 24) return "10m";
    if (diffDays <= 7) return "1h";
    if (diffDays <= 30) return "6h";
    if (diffDays <= 90) return "1d";
    return "1d"; // Default for very long time ranges
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

    // Histogram state
    histogramData: computed(() => state.data.value.histogramData),
    isLoadingHistogram: computed(() => state.data.value.isLoadingHistogram),
    histogramError: computed(() => state.data.value.histogramError),
    histogramGranularity: computed(() => state.data.value.histogramGranularity),

    // Loading state
    isLoading: state.isLoading,

    // New computed properties from refactoring plan
    logchefQlTranslationResult: _logchefQlTranslationResult,
    sqlForExecution,
    isQueryStateDirty,
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
  };
});
