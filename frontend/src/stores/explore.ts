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
import type { DateValue } from "@internationalized/date";
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
    };
  }

  // Main query execution
  async function executeQuery(rawSql?: string) {
    return await state.withLoading('executeQuery', async () => {
      if (!canExecuteQuery.value) {
        return state.handleError(
          { 
            status: "error",
            message: "Cannot execute query: Invalid source or time range", 
            error_type: "ValidationError" 
          } as APIErrorResponse, 
          'executeQuery'
        );
      }

      // Reset state before executing query
      state.data.value.logs = [];
      state.data.value.queryStats = DEFAULT_QUERY_STATS;
      state.data.value.columns = [];
      state.data.value.queryId = null;
      state.data.value.error = null;

      // Use the centralized API calling mechanism from base store
      return await state.callApi({
      apiCall: async () => {
        // Get the source details
        const sourcesStore = useSourcesStore();

        // First check in teamSources (sources available to the current team)
        let currentSource = sourcesStore.teamSources.find(
          (s) => s.id === state.data.value.sourceId
        );

        // If not found in teamSources, try to find in all sources
        if (!currentSource) {
          // Check if we need to load team sources first
          if (sourcesStore.teamSources.length === 0) {
            const teamsStore = useTeamsStore();
            if (teamsStore.currentTeamId) {
              console.log("Team sources not loaded, loading them now");
              await sourcesStore.loadTeamSources(teamsStore.currentTeamId);

              // Try again after loading
              currentSource = sourcesStore.teamSources.find(
                (s) => s.id === state.data.value.sourceId
              );
            }
          }

          // If still not found, check all sources
          if (!currentSource) {
            currentSource = sourcesStore.sources.find(
              (s) => s.id === state.data.value.sourceId
            );

            // If source is found in all sources but not in team sources, it means
            // the current team doesn't have access to this source
            if (currentSource) {
              console.warn(
                `Source ${state.data.value.sourceId} found but not accessible by current team`
              );
            }
          }
        }

        if (!currentSource) {
          console.error(`Source not found: ID ${state.data.value.sourceId}`);
          console.log("Available team sources:", sourcesStore.teamSources);
          console.log("All sources:", sourcesStore.sources);
          throw new Error(
            `Source with ID ${state.data.value.sourceId} not found. Please select a valid source.`
          );
        }

        // Ensure source has connection details
        if (
          !currentSource.connection ||
          !currentSource.connection.database ||
          !currentSource.connection.table_name
        ) {
          console.error("Source missing connection details:", currentSource);
          throw new Error(
            "Source configuration is incomplete. Missing database or table information."
          );
        }

        // Get time parameters
        const timestamps = getTimestamps();
        const startTimestampSec = Math.floor(timestamps.start / 1000);
        const endTimestampSec = Math.floor(timestamps.end / 1000);

        // Get time field from source metadata, default to 'timestamp'
        const timeField = currentSource._meta_ts_field || "timestamp";

        // Format the table name correctly
        const formattedTableName = getFormattedTableName(currentSource);
        console.log("Using formatted table name:", formattedTableName);

        // Build query options to pass to QueryBuilder
        const queryOptions = {
          tableName: formattedTableName,
          tsField: timeField,
          startTimestamp: startTimestampSec,
          endTimestamp: endTimestampSec,
          limit: state.data.value.limit,
          includeTimeFilter: true,
          forDisplay: false, // Use functions for execution, not readable dates
        };

        // Centralized query building - handle both LogchefQL and SQL modes appropriately
        let sqlToExecute = "";
        let displaySql = "";

        if (state.data.value.activeMode === "logchefql") {
          console.log(
            "Building SQL from LogchefQL:",
            state.data.value.logchefqlCode
          );
          // Convert LogchefQL to SQL using centralized QueryBuilder
          const logchefQLResult = QueryBuilder.buildSqlFromLogchefQL({
            ...queryOptions,
            logchefqlQuery: state.data.value.logchefqlCode || "",
          });

          if (!logchefQLResult.success) {
            throw new Error(
              logchefQLResult.error || "Failed to convert LogchefQL to SQL"
            );
          }

          sqlToExecute = logchefQLResult.sql;
          
          // Use formatQueryForDisplay for the display version
          displaySql = QueryBuilder.formatQueryForDisplay(sqlToExecute, timeField);
        } else {
          // SQL mode - use raw SQL directly
          console.log("Executing in SQL mode with raw SQL.");

          // Get the raw SQL from argument or store
          const sqlToExecuteRaw = rawSql || state.data.value.rawSql || "";

          if (!sqlToExecuteRaw.trim()) {
            throw new Error("Cannot execute empty SQL query.");
          }

          // The SQL to execute is simply the raw SQL provided by the user.
          // The backend API is responsible for handling the full query.
          sqlToExecute = sqlToExecuteRaw;

          // Use formatQueryForDisplay for the display version
          displaySql = QueryBuilder.formatQueryForDisplay(sqlToExecute, timeField);
        }

        // Store the display version for reference
        state.data.value.displaySql = displaySql;

        // Log queries for debugging
        console.log("Mode:", state.data.value.activeMode);
        console.log(
          "Original query:",
          state.data.value.activeMode === "logchefql"
            ? state.data.value.logchefqlCode
            : rawSql || state.data.value.rawSql
        );
        console.log("SQL to execute:", sqlToExecute);
        console.log("Display SQL:", displaySql);

        // Create API params
        const params: QueryParams = {
          raw_sql: sqlToExecute,
          limit: state.data.value.limit,
          start_timestamp: startTimestampSec,
          end_timestamp: endTimestampSec,
        };

        console.log("Executing query with params:", params);

        // Get the teams store
        const teamsStore = useTeamsStore();
        const currentTeamId = teamsStore.currentTeamId;

        if (!currentTeamId) {
          throw new Error(
            "No team selected. Please select a team before executing a query."
          );
        }

        // Execute the API call
        const response = await exploreApi.getLogs(
          state.data.value.sourceId,
          params,
          currentTeamId
        );

        // The actual response will be handled by the callApi function in base store
        return response;
      },
      // Provide success handler to update the state
      onSuccess: (data: QuerySuccessResponse) => {
        // Update state with results
        state.data.value.logs = data.logs || [];
        state.data.value.columns = data.columns || [];
        state.data.value.queryStats = data.stats || DEFAULT_QUERY_STATS;

        // Store query ID if available
        if (data.params && "query_id" in data.params) {
          state.data.value.queryId = data.params.query_id as string;
        }
      },
      // Error handler will automatically add the error to state.data.value.error
      // and will show a toast notification with the error message
      operationKey: 'executeQuery',
    });
  });
  }

  // Get log context
  async function getLogContext(sourceId: number, params: LogContextRequest) {
    return await state.withLoading(`getLogContext-${sourceId}`, async () => {
      if (!sourceId) {
        return state.handleError(
          { 
            status: "error",
            message: "Source ID is required for getting log context", 
            error_type: "ValidationError" 
          } as APIErrorResponse, 
          "getLogContext"
        );
      }

      // Get the teams store
      const teamsStore = useTeamsStore();
      const currentTeamId = teamsStore.currentTeamId;

      if (!currentTeamId) {
        return state.handleError(
          { 
            status: "error",
            message: "No team selected. Please select a team before getting log context.", 
            error_type: "ValidationError" 
          } as APIErrorResponse, 
          "getLogContext"
        );
      }

      return await state.callApi<LogContextResponse>({
        apiCall: () => exploreApi.getLogContext(sourceId, params, currentTeamId),
        operationKey: `getLogContext-${sourceId}`,
        showToast: false,
      });
    });
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
    resetState,
    executeQuery,
    getLogContext,
    
    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
