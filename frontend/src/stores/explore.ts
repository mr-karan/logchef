import { defineStore } from "pinia";
import { computed } from "vue";
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
} from "@/api/explore";
import type { DateValue } from "@internationalized/date";
import { now, getLocalTimeZone, CalendarDateTime } from "@internationalized/date";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useBaseStore } from "./base";
import { QueryService } from '@/services/QueryService'

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

// Helper to get the default time range (1 hour)
export function getDefaultTimeRange() {
  const now = new Date();
  const oneHourAgo = new Date(now);
  oneHourAgo.setHours(now.getHours() - 1); // One hour ago

  // Return timestamps in milliseconds
  return {
    start: oneHourAgo.getTime(),
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
  logchefqlCode?: string;
  // Active mode (logchefql or sql)
  activeMode: "logchefql" | "sql";
  // Loading and error states
  isLoading?: boolean;
  error?: string | null;
  // Query ID for tracking
  queryId?: string | null;
  // NEW: ID of the currently loaded saved query (if any)
  selectedQueryId?: string | null;
  // NEW: Name of the currently active saved query (if any)
  activeSavedQueryName?: string | null;
  // Query stats
  stats?: any;
  // Last executed query state
  lastExecutedState?: {
    timeRange: string;
    limit: number;
    query: string;
  };
  // Add field for last successful execution timestamp
  lastExecutionTimestamp?: number | null;
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
    filterConditions: [],
    rawSql: "",
    logchefqlCode: undefined,
    activeMode: "logchefql",
    lastExecutionTimestamp: null,
    selectedQueryId: null, // Initialize new state
  });

  // Getters
  const hasValidSource = computed(() => !!state.data.value.sourceId);
  const hasValidTimeRange = computed(() => !!state.data.value.timeRange);
  const canExecuteQuery = computed(
    () => hasValidSource.value && hasValidTimeRange.value
  );

  // Helper to get timestamps in milliseconds
  function getTimestamps() {
    const { timeRange } = state.data.value;
    if (!timeRange) {
      // If no time range is set, default to "now to 1 hour ago"
      console.warn("No time range set, using default (now to 1 hour ago)");
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

  // Actions
  function setSource(sourceId: number) {
    state.data.value.sourceId = sourceId;
  }

  function setTimeRange(range: ExploreState["timeRange"]) {
    console.log('Explore store: Setting time range:', {
      oldRange: JSON.stringify(state.data.value.timeRange),
      newRange: JSON.stringify(range),
      lastExecutedRange: state.data.value.lastExecutedState?.timeRange
    });
    state.data.value.timeRange = range;
  }

  function setLimit(limit: number) {
    if (limit > 0 && limit <= 10000) {
      state.data.value.limit = limit;
    }
  }

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

  function setRawSql(sql: string) {
    state.data.value.rawSql = sql;
  }

  // Set the active mode (logchefql or sql)
  function setActiveMode(mode: "logchefql" | "sql") {
    state.data.value.activeMode = mode;
  }

  // Set the LogchefQL code
  function setLogchefqlCode(code: string) {
    state.data.value.logchefqlCode = code;
  }

  // NEW: Action to set the selected saved query ID
  function setSelectedQueryId(queryId: string | null) {
    state.data.value.selectedQueryId = queryId;
  }

  // NEW: Action to set the active saved query name
  function setActiveSavedQueryName(name: string | null) {
    state.data.value.activeSavedQueryName = name;
  }

  // NEW: Action to reset the query state to defaults
  function resetQueryStateToDefault() {
    console.log("Explore store: Resetting query state to default");

    // Create a default time range (last hour)
    const nowDt = now(getLocalTimeZone());
    const timeRange = {
      start: new CalendarDateTime(
        nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
      ).subtract({ hours: 1 }),
      end: new CalendarDateTime(
        nowDt.year, nowDt.month, nowDt.day, nowDt.hour, nowDt.minute, nowDt.second
      )
    };

    // Set time range
    state.data.value.timeRange = timeRange;

    // Reset limit to default
    state.data.value.limit = 100;

    // Get current source details
    const sourcesStore = useSourcesStore();

    // Make sure we have a valid table name by checking multiple possible sources
    let tableName = sourcesStore.getCurrentSourceTableName;
    if (!tableName && sourcesStore.currentSourceDetails?.connection?.table_name) {
      tableName = sourcesStore.currentSourceDetails.connection.table_name;
    }
    if (!tableName && sourcesStore.currentSourceDetails?.connection?.database) {
      // Try to build a full table name from database and default table
      tableName = `${sourcesStore.currentSourceDetails.connection.database}.vector_logs`;
    }
    // If still no table name, use a reasonable default
    if (!tableName) {
      tableName = 'logs.vector_logs';
    }

    // Get timestamp field with fallback
    const timestampField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';

    // Generate default queries using the QueryService
    if (state.data.value.activeMode === 'sql') {
      // Generate default SQL
      const result = QueryService.generateDefaultSQL({
        tableName,
        tsField: timestampField,
        timeRange,
        limit: state.data.value.limit
      });

      state.data.value.rawSql = result.success ? result.sql : '';
      state.data.value.logchefqlCode = ''; // Clear other mode
    } else {
      // For LogchefQL, we start with an empty query
      state.data.value.logchefqlCode = '';

      // But generate a default SQL for the other mode
      const result = QueryService.generateDefaultSQL({
        tableName,
        tsField: timestampField,
        timeRange,
        limit: state.data.value.limit
      });

      state.data.value.rawSql = ''; // Clear other mode
    }

    // Clear active saved query information
    state.data.value.selectedQueryId = null;
    state.data.value.activeSavedQueryName = null;
  }

  // Reset state
  function resetState() {
    state.data.value = {
      logs: [],
      columns: [],
      queryStats: DEFAULT_QUERY_STATS,
      sourceId: state.data.value.sourceId, // Preserve source
      limit: state.data.value.limit, // Preserve limit
      timeRange: state.data.value.timeRange, // Preserve time range
      filterConditions: [],
      rawSql: "",
      logchefqlCode: state.data.value.logchefqlCode,
      activeMode: state.data.value.activeMode,
      lastExecutedState: undefined,
      lastExecutionTimestamp: null,
      selectedQueryId: null, // Reset selectedQueryId
    };
  }

  // Main query execution
  async function executeQuery(finalSql?: string) {
    // Reset timestamp at the start of execution attempt
    state.data.value.lastExecutionTimestamp = null;
    const operationKey = 'executeQuery'; // Define operation key for errors
    return await state.withLoading(operationKey, async () => {
      // Get current team ID
      const currentTeamId = useTeamsStore().currentTeamId;
      if (!currentTeamId) {
        // Provide operationKey to handleError
        return state.handleError({ status: "error", message: "No team selected", error_type: "ValidationError" }, operationKey);
      }

      let sqlToExecute: string;

      // --- Logic to determine sqlToExecute (largely restored) ---
      if (finalSql) {
        sqlToExecute = finalSql;
      } else {
        const mode = state.data.value.activeMode;
        const logchefqlQuery = state.data.value.logchefqlCode || "";
        const rawSql = state.data.value.rawSql || "";

        try {
          if (mode === 'logchefql') {
            const sourcesStore = useSourcesStore();
            const timeRange = state.data.value.timeRange;
            const tsField = sourcesStore.currentSourceDetails?._meta_ts_field || 'timestamp';
            const tableName = sourcesStore.getCurrentSourceTableName || '';

            if (!timeRange?.start || !timeRange?.end) {
              return state.handleError({ status: "error", message: "Invalid time range selected", error_type: "ValidationError" }, operationKey);
            }
            if (!tableName) {
              return state.handleError({ status: "error", message: "Could not determine table name for source.", error_type: "ConfigurationError" }, operationKey);
            }

            // Use the new QueryService to prepare the query
            const result = QueryService.translateLogchefQLToSQL({
              tableName: tableName,
              tsField: tsField,
              timeRange: timeRange,
              limit: state.data.value.limit,
              logchefqlQuery: logchefqlQuery
            });

            if (!result.success) {
              return state.handleError({
                status: "error",
                message: result.error || 'Failed to build SQL from LogchefQL',
                error_type: "BuildError"
              }, operationKey);
            }
            sqlToExecute = result.sql;
          } else { // SQL mode
            sqlToExecute = rawSql;
            // Basic validation for SQL mode query presence
            if (!sqlToExecute.trim()) {
              return state.handleError({
                status: "error",
                message: `Cannot execute empty SQL query`,
                error_type: "ValidationError"
              }, operationKey);
            }

            // Optionally validate the SQL
            if (!QueryService.validateSQL(sqlToExecute)) {
              return state.handleError({
                status: "error",
                message: "Invalid SQL syntax",
                error_type: "ValidationError"
              }, operationKey);
            }
          }
        } catch (error: any) {
            return state.handleError({ status: "error", message: error.message || "Failed to prepare query", error_type: "QueryError" }, operationKey);
        }
      }
      // --- End logic to determine sqlToExecute ---

      // Store current state before execution
      const executionState = {
        timeRange: JSON.stringify(state.data.value.timeRange),
        limit: state.data.value.limit,
        query: (state.data.value.activeMode === 'logchefql' ? state.data.value.logchefqlCode : state.data.value.rawSql) || ''
      };
      console.log('Explore store: Setting last executed state:', executionState);
      state.data.value.lastExecutedState = executionState;

      // Reset previous results
      state.data.value.logs = [];
      state.data.value.queryStats = DEFAULT_QUERY_STATS;
      state.data.value.columns = [];
      state.data.value.queryId = null;

      // Prepare parameters for the correct API call (getLogs)
      const timestamps = getTimestamps();
      const params: QueryParams = {
          raw_sql: sqlToExecute,
          limit: state.data.value.limit,
          start_timestamp: timestamps.start,
          end_timestamp: timestamps.end,
          query_type: state.data.value.activeMode
      };

      // Use the centralized API calling mechanism from base store
      // This structure assumes callApi returns the API response directly or throws/handles errors
      const response = await state.callApi({
        apiCall: async () => exploreApi.getLogs(state.data.value.sourceId, params, currentTeamId),
        // Modify onSuccess to handle potential null and use correct properties
        onSuccess: (data: QuerySuccessResponse | null) => {
          if (data) {
              state.data.value.logs = data.logs || []; // Use data.logs
              state.data.value.columns = data.columns || [];
              state.data.value.queryStats = data.stats || DEFAULT_QUERY_STATS;
              if (data.params && "query_id" in data.params) {
                  state.data.value.queryId = data.params.query_id as string;
              }
              state.data.value.lastExecutionTimestamp = Date.now(); // Set timestamp on success
          } else {
              console.warn("Query successful but received null data.");
              // Reset state even on null success data
              state.data.value.logs = [];
              state.data.value.columns = [];
              state.data.value.queryStats = DEFAULT_QUERY_STATS;
              state.data.value.queryId = null;
              state.data.value.lastExecutionTimestamp = Date.now(); // Also set timestamp here
          }
        },
        operationKey: operationKey,
      });

      // IMPORTANT: Return structure expected by useQueryExecution
      if (response.success) {
          return { success: true, data: response.data };
      } else {
          // Assuming callApi handled the error state, return error structure
          return { success: false, error: response.error };
      }
    });
  }

  // Get log context
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
      // Assuming callApi structure is similar
      return await state.callApi<LogContextResponse>({
        apiCall: () => exploreApi.getLogContext(sourceId, params, currentTeamId),
        operationKey: operationKey,
        showToast: false, // Typically don't toast for context fetches
      });
    });
  }

  // Add function to set last executed state
  function setLastExecutedState(executedState: NonNullable<ExploreState['lastExecutedState']>) {
    console.log('Explore store: Setting last executed state:', {
      oldState: state.data.value.lastExecutedState,
      newState: executedState
    });
    state.data.value.lastExecutedState = executedState;
  }

  // Clear error helper
  function clearError() {
    state.error.value = null;
  }

  // Return the store
  return {
    // State
    logs: computed(() => state.data.value.logs),
    columns: computed(() => state.data.value.columns),
    queryStats: computed(() => state.data.value.queryStats),
    sourceId: computed(() => state.data.value.sourceId),
    limit: computed(() => state.data.value.limit),
    timeRange: computed(() => state.data.value.timeRange),
    filterConditions: computed(() => state.data.value.filterConditions),
    rawSql: computed(() => state.data.value.rawSql),
    pendingRawSql: computed({
      get: () => state.data.value.pendingRawSql,
      set: (v) => state.data.value.pendingRawSql = v
    }),
    logchefqlCode: computed(() => state.data.value.logchefqlCode),
    activeMode: computed(() => state.data.value.activeMode),
    error: state.error,
    queryId: computed(() => state.data.value.queryId),
    stats: computed(() => state.data.value.stats),
    lastExecutedState: computed(() => state.data.value.lastExecutedState),
    lastExecutionTimestamp: computed(() => state.data.value.lastExecutionTimestamp),
    selectedQueryId: computed(() => state.data.value.selectedQueryId),
    activeSavedQueryName: computed(() => state.data.value.activeSavedQueryName),

    // Loading state
    isLoading: state.isLoading,

    // Derived values
    canExecuteQuery,

    // Actions
    setSource,
    setTimeRange,
    setLimit,
    setFilterConditions,
    setRawSql,
    setActiveMode,
    setLogchefqlCode,
    setSelectedQueryId,
    setActiveSavedQueryName,
    resetQueryStateToDefault,
    setLastExecutedState,
    resetState,
    executeQuery,
    getLogContext,
    clearError,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
