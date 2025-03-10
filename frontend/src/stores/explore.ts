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
} from "@/api/explore";
import type { DateValue } from "@internationalized/date";
import { computed } from "vue";
import { useSourcesStore } from "./sources";
import { addTimeRangeToSQL } from "@/utils/clickhouse-sql/parser";

// Helper function to get formatted table name
export function getFormattedTableName(source: any): string {
  if (source?.connection?.database && source?.connection?.table_name) {
    return `${source.connection.database}.${source.connection.table_name}`;
  }
  return "logs.vector_logs"; // Default fallback
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
      return { start: 0, end: 0 };
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
    };
  }

  // Main query execution
  async function executeQuery(rawSql?: string) {
    if (!canExecuteQuery.value) {
      throw new Error("Cannot execute query: Invalid source or time range");
    }

    const sourcesStore = useSourcesStore();
    const currentSource = sourcesStore.sources.find(
      (s) => s.id === state.data.value.sourceId
    );

    if (!currentSource) {
      throw new Error("Source not found. Please select a valid source.");
    }

    state.isLoading.value = true;

    try {
      const timestamps = getTimestamps();
      const sqlQuery = rawSql || state.data.value.rawSql;

      // Get time field from source metadata, default to 'timestamp'
      const timeField = currentSource._meta_ts_field || "timestamp";

      // Format the table name correctly
      const formattedTableName = getFormattedTableName(currentSource);

      // Use our SQL parser to add time conditions and limit to the query
      let finalSql = sqlQuery;

      // Replace any generic 'logs' table placeholder with the proper formatted table name
      if (finalSql.includes(" FROM logs ")) {
        finalSql = finalSql.replace(
          " FROM logs ",
          ` FROM ${formattedTableName} `
        );
      }

      // If the query doesn't have a FROM clause, add one with the correct table
      if (!finalSql.toUpperCase().includes("FROM")) {
        finalSql = `SELECT * FROM ${formattedTableName} WHERE ${finalSql}`;
      }

      console.log("SQL after table name formatting:", finalSql);

      // Add time range and limit
      const sqlWithTimeRange = addTimeRangeToSQL(
        finalSql,
        timeField,
        timestamps.start / 1000, // Convert to seconds for ClickHouse DateTime
        timestamps.end / 1000, // Convert to seconds for ClickHouse DateTime
        state.data.value.limit
      );

      // Log the SQL for debugging
      console.log("Original SQL:", sqlQuery);
      console.log("SQL with time range and limit:", sqlWithTimeRange);
      console.log("Timestamps:", {
        startMs: timestamps.start,
        endMs: timestamps.end,
        startSeconds: timestamps.start / 1000,
        endSeconds: timestamps.end / 1000,
        startFormatted: new Date(timestamps.start).toISOString(),
        endFormatted: new Date(timestamps.end).toISOString(),
        startClickhouse: new Date(timestamps.start)
          .toISOString()
          .replace("T", " ")
          .replace("Z", ""),
        endClickhouse: new Date(timestamps.end)
          .toISOString()
          .replace("T", " ")
          .replace("Z", ""),
      });

      // Use the modified SQL
      const params: QueryParams = {
        raw_sql: sqlWithTimeRange,
        limit: state.data.value.limit,
        start_timestamp: timestamps.start,
        end_timestamp: timestamps.end,
      };

      const result = await state.callApi<QueryResponse>({
        apiCall: () => exploreApi.getLogs(state.data.value.sourceId, params),
        onSuccess: (response) => {
          if ("error" in response) {
            throw new Error(response.error);
          }

          state.data.value.logs = response.logs || [];
          state.data.value.columns = response.columns;
          state.data.value.queryStats = response.stats;
        },
        onError: () => {
          // Don't reset everything on error - just clear logs and stats
          state.data.value.logs = [];
          state.data.value.columns = [];
          state.data.value.queryStats = DEFAULT_QUERY_STATS;
        },
        showToast: true,
      });

      return result;
    } finally {
      state.isLoading.value = false;
    }
  }

  // Get log context
  async function getLogContext(sourceId: number, params: LogContextRequest) {
    return await state.callApi<LogContextResponse>({
      apiCall: () => exploreApi.getLogContext(sourceId, params),
      showToast: true,
    });
  }

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

    // Loading state
    isLoading: computed(() => state.isLoading.value),
    error: computed(() => state.error.value),

    // Computed
    hasValidSource,
    hasValidTimeRange,
    canExecuteQuery,

    // Actions
    setSource,
    setTimeRange,
    setLimit,
    setFilterConditions,
    setRawSql,
    resetState,
    executeQuery,
    getLogContext,
  };
});
