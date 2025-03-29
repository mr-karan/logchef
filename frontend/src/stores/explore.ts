import { defineStore } from "pinia";
import { useBaseStore } from "./base";
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
import type { DateValue, CalendarDateTime } from "@internationalized/date";
import { computed } from "vue";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { QueryBuilder } from "@/utils/query-builder";

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

            // Assuming QueryBuildOptions is NOT exported, construct options directly if needed by QueryBuilder
            const buildResult = QueryBuilder.buildSqlFromLogchefQL({
              tableName: tableName,
              tsField: tsField,
              startDateTime: timeRange.start as CalendarDateTime,
              endDateTime: timeRange.end as CalendarDateTime,
              limit: state.data.value.limit,
              logchefqlQuery: logchefqlQuery
            });

            if (!buildResult.success) {
                return state.handleError({ status: "error", message: buildResult.error || 'Failed to build SQL from LogchefQL', error_type: "BuildError" }, operationKey);
            }
            sqlToExecute = buildResult.sql;
          } else { // SQL mode
            sqlToExecute = rawSql;
            // Basic validation for SQL mode query presence
            if (!sqlToExecute.trim()) {
                return state.handleError({ status: "error", message: `Cannot execute empty SQL query`, error_type: "ValidationError" }, operationKey);
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
    setLastExecutedState,
    resetState,
    executeQuery,
    getLogContext,
    clearError,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
