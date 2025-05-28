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
} from "@/api/explore";
import type { QueryResult } from "@/types/query";
import type { DateValue } from "@internationalized/date";
import { now, getLocalTimeZone, CalendarDateTime } from "@internationalized/date";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "@/stores/teams";
import { useBaseStore } from "./base";
import { QueryService } from '@/services/QueryService'
import { parseRelativeTimeString } from "@/utils/time";

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
  // Flag to skip next URL sync (for multi-component coordination)
  skipNextUrlSync?: boolean;
  // Query stats
  stats?: any;
  // Last executed query state
  lastExecutedState?: {
    timeRange: string;
    limit: number;
    query: string;
    mode?: "logchefql" | "sql";
    logchefqlQuery?: string;
    sqlQuery?: string;
  };
  // Add field for last successful execution timestamp
  lastExecutionTimestamp?: number | null;
  // Group by field for histogram
  groupByField?: string | null;
  // User's selected timezone identifier (e.g., 'America/New_York', 'UTC')
  selectedTimezoneIdentifier?: string | null;
  // Add a new state property to hold the reactively generated SQL for display/internal use
  generatedDisplaySql?: string | null;
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
    logchefqlCode: undefined,
    activeMode: "logchefql",
    lastExecutionTimestamp: null,
    selectedQueryId: null, // Initialize new state
    groupByField: null, // Initialize the groupByField
    selectedTimezoneIdentifier: null, // Initialize the timezone identifier
    generatedDisplaySql: null, // Initialize the new state property
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
    // Clear the generated SQL immediately to prevent using previous source's SQL
    state.data.value.generatedDisplaySql = null;
    // Clear the logs and result data as well to avoid showing old data
    state.data.value.logs = [];
    state.data.value.columns = [];
    state.data.value.queryStats = DEFAULT_QUERY_STATS;
    
    // Set the new source ID
    state.data.value.sourceId = sourceId;
  }

  function setTimeRange(range: ExploreState["timeRange"]) {
    console.log('Explore store: Setting time range:', {
      oldRange: JSON.stringify(state.data.value.timeRange),
      newRange: JSON.stringify(range),
      lastExecutedRange: state.data.value.lastExecutedState?.timeRange
    });

    // Update the time range
    state.data.value.timeRange = range;

    // When setTimeRange is called directly, we are setting absolute times
    // Clear the relative time selection to maintain consistency
    state.data.value.selectedRelativeTime = null;
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

    // Set time range
    state.data.value.timeRange = timeRange;

    // Set the relative time to "15m" since we're using a 15-minute window
    state.data.value.selectedRelativeTime = "15m";

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
    
    // Reset URL sync flag
    state.data.value.skipNextUrlSync = false;
  }

  // Reset state
  function resetState() {
    // Initialize the state data with default values
    const initialData = {
      logs: [],
      columns: [],
      queryStats: DEFAULT_QUERY_STATS,
      sourceId: state.data.value.sourceId, // Preserve source
      limit: state.data.value.limit, // Preserve limit
      timeRange: state.data.value.timeRange, // Preserve time range
      selectedRelativeTime: state.data.value.selectedRelativeTime, // Preserve relative time selection
      filterConditions: [],
      rawSql: "",
      logchefqlCode: state.data.value.logchefqlCode,
      activeMode: state.data.value.activeMode,
      lastExecutedState: undefined,
      lastExecutionTimestamp: null,
      selectedQueryId: null, // Reset selectedQueryId
      activeSavedQueryName: null, // Reset activeSavedQueryName
      skipNextUrlSync: false, // Reset URL sync flag
      groupByField: state.data.value.groupByField, // Preserve groupByField
      selectedTimezoneIdentifier: state.data.value.selectedTimezoneIdentifier, // Preserve timezone identifier
      generatedDisplaySql: null, // Reset generatedDisplaySql
    };
    
    // Update the state data with the initial values
    state.data.value = initialData;
  }

  // Main query execution
  async function executeQuery(finalSql?: string) {
    // Store the relative time so we can restore it after execution
    const relativeTime = state.data.value.selectedRelativeTime;

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

      // Verify we have a valid table name for the current source
      const tableName = sourcesStore.getCurrentSourceTableName;
      if (!tableName) {
        return state.handleError({
          status: "error",
          message: "Cannot determine table name for current source.",
          error_type: "ValidationError"
        }, operationKey);
      }

      // Use the reactively generated SQL if available, otherwise generate on the fly (fallback)
      let sqlToExecute = state.data.value.generatedDisplaySql;

      if (!sqlToExecute || sqlToExecute.startsWith('-- Error') || sqlToExecute.startsWith('-- Exception')) {
        console.warn("executeQuery: generatedDisplaySql not usable, regenerating SQL on the fly.");
        if (!state.data.value.timeRange) {
          return state.handleError({
            status: "error",
            message: 'Cannot generate query without a valid time range.',
            error_type: "ValidationError"
          }, operationKey);
        }
        const generationOptions = {
            tableName: tableName,
            tsField: sourceDetails?._meta_ts_field || 'timestamp',
            timeRange: state.data.value.timeRange,
            limit: state.data.value.limit,
            timezone: getTimezoneIdentifier(),
            logchefqlQuery: state.data.value.logchefqlCode || ''
        };
        const result = state.data.value.activeMode === 'logchefql'
            ? QueryService.translateLogchefQLToSQL(generationOptions)
            : QueryService.generateDefaultSQL(generationOptions); // Or maybe use rawSql here? Sticking to generation

        if (result.success) {
            sqlToExecute = result.sql;
        } else {
            return state.handleError({
              status: "error",
              message: result.error || 'Failed to generate query for execution',
              error_type: "QueryError"
            }, operationKey);
        }
      }

      // Use rawSql directly if in SQL mode and generatedDisplaySql is null/error
      // This handles cases where the user manually typed SQL that hasn't been re-gen by watcher yet
      if (state.data.value.activeMode === 'sql' && (!sqlToExecute || sqlToExecute.startsWith('-- '))) {
         sqlToExecute = state.data.value.rawSql;
      }

      if (!sqlToExecute || !sqlToExecute.trim()) {
         return state.handleError({
            status: "error",
            message: "Cannot execute an empty query.",
            error_type: "ValidationError"
         }, operationKey);
      }

      // Validate the SQL references the correct table name before execution
      if (tableName && !sqlToExecute.includes(tableName)) {
        const tableFromSql = extractTableNameFromSql(sqlToExecute);
        if (tableFromSql && tableFromSql !== tableName) {
          console.error(`SQL references table ${tableFromSql} but current source uses ${tableName}`);
          return state.handleError({
            status: "error",
            message: `Query references incorrect table (${tableFromSql}). Current source uses ${tableName}.`,
            error_type: "ValidationError"
          }, operationKey);
        }
      }

      // Store current state before execution
      const executionState = {
        timeRange: JSON.stringify(state.data.value.timeRange), // Keep original time range info
        limit: state.data.value.limit,
        query: sqlToExecute, // Store the SQL that was actually executed
        mode: state.data.value.activeMode,
        logchefqlQuery: state.data.value.logchefqlCode, // Store original LogchefQL
        sqlQuery: sqlToExecute // Store executed SQL here too for consistency?
      };
      console.log('Explore store: Setting last executed state:', executionState);
      state.data.value.lastExecutedState = executionState;

      // Prepare parameters for the correct API call (getLogs)
      const params: QueryParams = {
          raw_sql: sqlToExecute,
          limit: state.data.value.limit, // Keep limit
          // start_timestamp, end_timestamp, and timezone are now baked into raw_sql
      };

      // Use the centralized API calling mechanism from base store
      const response = await state.callApi({
        apiCall: async () => exploreApi.getLogs(state.data.value.sourceId, params, currentTeamId),
        // Update results ONLY on successful API call with data
        onSuccess: (data: QuerySuccessResponse | null) => {
          if (data && data.logs && data.logs.length > 0) {
              // We have new data, update the store
              state.data.value.logs = data.logs;
              state.data.value.columns = data.columns || [];
              state.data.value.queryStats = data.stats || DEFAULT_QUERY_STATS;
              // Check if query_id exists in params before accessing it
              if (data.params && typeof data.params === 'object' && "query_id" in data.params) {
                  state.data.value.queryId = data.params.query_id as string;
              } else {
                  state.data.value.queryId = null; // Reset if not present
              }
              state.data.value.lastExecutionTimestamp = Date.now(); // Set timestamp on success

              // Restore the relative time if it was set before execution
              if (relativeTime) {
                // Use setRelativeTimeRange to ensure both relative string and absolute range are updated
                setRelativeTimeRange(relativeTime);
              }
          } else {
              // Query was successful but returned no logs or null data
              console.warn("Query successful but received no logs or null data.");
              // Clear the logs, columns, stats now that the API call is complete
              state.data.value.logs = [];
              state.data.value.columns = [];
              state.data.value.queryStats = DEFAULT_QUERY_STATS;
              state.data.value.queryId = null;
              state.data.value.lastExecutionTimestamp = Date.now(); // Also set timestamp here

              // Restore the relative time if it was set before
              if (relativeTime) {
                state.data.value.selectedRelativeTime = relativeTime;
              }
          }
        },
        operationKey: operationKey,
      });

      // Ensure lastExecutionTimestamp is set even if there was an error
      // This helps the histogram know when to refresh
      if (!response.success && state.data.value.lastExecutionTimestamp === null) {
          state.data.value.lastExecutionTimestamp = Date.now();

          // Restore the relative time if it was set before execution, even on error
          if (relativeTime) {
            // Use setRelativeTimeRange to ensure both relative string and absolute range are updated
            setRelativeTimeRange(relativeTime);
          }
      }

      // IMPORTANT: Return structure expected by useQueryExecution
      if (response.success) {
          return { success: true, data: response.data };
      } else {
          // Assuming callApi handled the error state, return error structure
          return { success: false, error: response.error };
      }
    });
  }

  // Helper function to extract table name from SQL
  function extractTableNameFromSql(sql: string): string | null {
    try {
      // Simple regex to find table name after FROM
      const fromMatch = /\bFROM\s+(?:`?([^`\s()]+\.[^`\s()]+)`?|\(?`?([^`\s()]+)`?\s+AS\s+[^`\s()]+\)?)/i.exec(sql);
      if (fromMatch) {
        // Return the first captured group that isn't undefined
        return fromMatch[1] || fromMatch[2] || null;
      }
      return null;
    } catch (error) {
      console.error("Error extracting table name from SQL:", error);
      return null;
    }
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

  // Add helper to update the execution timestamp
  function updateExecutionTimestamp() {
    console.log('Explore store: Updating execution timestamp');
    state.data.value.lastExecutionTimestamp = Date.now();
  }
  
  // Add helper to set skipNextUrlSync flag
  function setSkipNextUrlSync(value: boolean) {
    console.log(`Explore store: Setting skipNextUrlSync to ${value}`);
    state.data.value.skipNextUrlSync = value;
  }

  // Clear error helper
  function clearError() {
    state.error.value = null;
  }

  // Set the group by field
  function setGroupByField(field: string | null) {
    state.data.value.groupByField = field;
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

  // --- Reactive Query Generation Watcher ---
  watch(
    // Watch multiple sources
    [
      () => state.data.value.sourceId,
      () => state.data.value.selectedTimezoneIdentifier,
      () => state.data.value.timeRange,
      () => state.data.value.logchefqlCode,
      () => state.data.value.activeMode,
      () => state.data.value.limit,
      // Also watch source details, as table name/ts field might change
      () => useSourcesStore().currentSourceDetails,
    ],
    async (
      [
        sourceId,
        timezone,
        timeRange,
        logchefqlCode,
        activeMode,
        limit,
        sourceDetails,
      ],
      oldValues
    ) => {
      console.log("Explore Store Watcher: Detected change in state.");

      // --- Check if the *only* change was mode switching to SQL ---
      const [
        oldSourceId,
        oldTimezone,
        oldTimeRange,
        oldLogchefqlCode,
        oldActiveMode,
        oldLimit,
        oldSourceDetails,
      ] = oldValues;
      const modeJustSwitchedToSql = oldActiveMode !== activeMode && activeMode === 'sql';
      const otherParamsChanged =
        oldSourceId !== sourceId ||
        oldTimezone !== timezone ||
        JSON.stringify(oldTimeRange) !== JSON.stringify(timeRange) ||
        // Don't consider logchefqlCode change if mode switched *to* sql
        // oldLogchefqlCode !== logchefqlCode ||
        oldLimit !== limit ||
        JSON.stringify(oldSourceDetails) !== JSON.stringify(sourceDetails);

      // If the mode just switched to SQL AND no other relevant parameters changed,
      // skip the watcher's SQL generation. Let useQuery.changeMode handle it.
      if (modeJustSwitchedToSql && !otherParamsChanged) {
        console.log("Explore Store Watcher: Mode switched to SQL, skipping internal SQL generation to allow useQuery translation.");
        return;
      }

      // --- Check if only time or limit changed while in SQL mode ---
      const timeJustChanged = !modeJustSwitchedToSql && activeMode === 'sql' && JSON.stringify(oldTimeRange) !== JSON.stringify(timeRange) && !otherParamsChanged;
      const limitJustChanged = !modeJustSwitchedToSql && activeMode === 'sql' && oldLimit !== limit && !otherParamsChanged;

      if ((timeJustChanged || limitJustChanged) && activeMode === 'sql') {
        console.log(`Explore Store Watcher: ${timeJustChanged ? 'Time range' : 'Limit'} changed in SQL mode, skipping default SQL generation. Handlers in useQuery should update rawSql.`);
        // Do not generate default SQL or update rawSql here.
        // The dedicated handlers (handleTimeRangeUpdate/handleLimitUpdate) modify
        // sqlQuery in useQuery, which updates the store's rawSql.
        // We still need to update generatedDisplaySql though, based on the (now updated) rawSql.
        try {
            const validationResult = QueryService.validateSQLWithDetails(state.data.value.rawSql);
            if (validationResult.valid) {
                // Use the potentially updated rawSql directly
                let finalSql = state.data.value.rawSql;
                const queryAnalysis = QueryService.analyzeQuery(finalSql);
                if (queryAnalysis && !queryAnalysis.hasLimit) {
                    finalSql = `${finalSql}\nLIMIT ${limit}`;
                }
                state.data.value.generatedDisplaySql = finalSql;
                console.log("Explore Store Watcher: Updated generatedDisplaySql after time/limit change in SQL mode.");
            } else {
                 state.data.value.generatedDisplaySql = `-- Error validating query after time/limit update: ${validationResult.error}`;
            }
        } catch (error) {
             state.data.value.generatedDisplaySql = `-- Exception validating query after time/limit update: ${error}`;
        }
        return; // Exit watcher early
      }
      // --- End Check ---

      // Prevent running if essential data is missing
      if (
        !sourceId ||
        !timeRange ||
        !sourceDetails ||
        !sourceDetails.connection?.table_name ||
        !sourceDetails._meta_ts_field
      ) {
        console.log("Explore Store Watcher: Missing essential data, skipping SQL generation.");
        // Clear generated SQL if conditions aren't met
        state.data.value.generatedDisplaySql = null;
        return;
      }

      const tableName = `${sourceDetails.connection.database}.${sourceDetails.connection.table_name}`;
      const tsField = sourceDetails._meta_ts_field;
      const effectiveTimezone = timezone || getTimezoneIdentifier(); // Use state value or default

      let result: QueryResult | null = null; // Use nullable type

      try {
        if (activeMode === "logchefql") {
          console.log("Explore Store Watcher: Generating SQL from LogchefQL.");
          result = QueryService.translateLogchefQLToSQL({
            tableName,
            tsField,
            timeRange,
            limit,
            logchefqlQuery: logchefqlCode || "",
            timezone: effectiveTimezone,
          });
        } else { // activeMode === 'sql'
          // --- Watcher NO LONGER generates default SQL or modifies rawSql here ---
          console.log("Explore Store Watcher: In SQL mode, validating current rawSql for display/execution.");
          const currentRawSql = state.data.value.rawSql;
          if (!currentRawSql || !currentRawSql.trim()) {
              // Handle case where rawSql is empty in SQL mode
              console.log("Explore Store Watcher: rawSql is empty in SQL mode.");
              result = { success: true, sql: '', error: null }; // Treat as valid empty query
          } else {
              const validationResult = QueryService.validateSQLWithDetails(currentRawSql);
              if (validationResult.valid) {
                  // Use the rawSql from the store directly
                  let finalSql = currentRawSql;
                  const queryAnalysis = QueryService.analyzeQuery(finalSql);
                  // Auto-add limit *for display/execution* if missing, but don't modify rawSql
                  if (queryAnalysis && !queryAnalysis.hasLimit) {
                      finalSql = `${finalSql}\nLIMIT ${limit}`;
                  }
                  result = { success: true, sql: finalSql, error: null }; // Create a simple result structure
              } else {
                   result = { success: false, sql: currentRawSql, error: `Error validating query: ${validationResult.error}` };
              }
          }
        }

        // Update generatedDisplaySql based on the outcome
        if (result && result.success) {
          console.log("Explore Store Watcher: SQL processing successful.");
          state.data.value.generatedDisplaySql = result.sql;

          // --- REMOVED the conditional update of rawSql based on parameters ---
          // The specific handlers in useQuery are now responsible for rawSql updates in SQL mode.

        } else {
          console.error("Explore Store Watcher: SQL processing failed:", result?.error);
          state.data.value.generatedDisplaySql = `-- Error: ${result?.error || 'Failed to process query'}`;
        }
      } catch (error) {
          console.error("Explore Store Watcher: Exception during SQL processing:", error);
          state.data.value.generatedDisplaySql = `-- Exception: ${error}`;
      }
    },
    {
      deep: true, // Watch nested properties like timeRange
      immediate: true, // Run once on store initialization
    }
  );
  // --- End Watcher ---

  // Return the store
  return {
    // State
    logs: computed(() => state.data.value.logs),
    columns: computed(() => state.data.value.columns),
    queryStats: computed(() => state.data.value.queryStats),
    sourceId: computed(() => state.data.value.sourceId),
    limit: computed(() => state.data.value.limit),
    timeRange: computed(() => state.data.value.timeRange),
    selectedRelativeTime: computed(() => state.data.value.selectedRelativeTime),
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
    skipNextUrlSync: computed({
      get: () => state.data.value.skipNextUrlSync,
      set: (v) => state.data.value.skipNextUrlSync = v
    }),
    groupByField: computed(() => state.data.value.groupByField),
    selectedTimezoneIdentifier: computed(() => state.data.value.selectedTimezoneIdentifier),
    generatedDisplaySql: computed(() => state.data.value.generatedDisplaySql), // Expose the new computed property

    // Loading state
    isLoading: state.isLoading,

    // Derived values
    canExecuteQuery,

    // Actions
    setSource,
    setTimeRange,
    setRelativeTimeRange,
    getSelectedRelativeTime,
    setLimit,
    setFilterConditions,
    setRawSql,
    setActiveMode,
    setLogchefqlCode,
    setSelectedQueryId,
    setActiveSavedQueryName,
    resetQueryStateToDefault,
    setLastExecutedState,
    updateExecutionTimestamp,
    setSkipNextUrlSync,
    resetState,
    executeQuery,
    getLogContext,
    clearError,
    setGroupByField,
    setTimezoneIdentifier,

    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
