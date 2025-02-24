import { defineStore } from "pinia";
import { useBaseStore, handleApiCall } from "./base";
import { exploreApi } from "@/api/explore";
import type {
  ColumnInfo,
  QueryStats,
  FilterCondition,
  QueryResponse,
  QueryParams,
} from "@/api/explore";
import type { DateValue } from "@internationalized/date";
import { computed } from "vue";

export interface ExploreState {
  logs: Record<string, any>[];
  columns: ColumnInfo[];
  queryStats: QueryStats;
  // Core query state
  sourceId: string;
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
    sourceId: "",
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

    return {
      start: startDate.getTime(),
      end: endDate.getTime(),
    };
  }

  // Actions
  function setSource(sourceId: string) {
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
    state.data.value.filterConditions = filters;
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

    return await state.withLoading(async () => {
      const timestamps = getTimestamps();
      const params: QueryParams = {
        raw_sql: rawSql || state.data.value.rawSql,
        limit: state.data.value.limit,
        start_timestamp: timestamps.start,
        end_timestamp: timestamps.end,
      };

      const result = await handleApiCall<QueryResponse>({
        apiCall: () => exploreApi.getLogs(state.data.value.sourceId, params),
        onSuccess: (response) => {
          if ("error" in response) {
            throw new Error(response.error);
          }

          state.data.value.logs = response.logs;
          state.data.value.columns = response.columns;
          state.data.value.queryStats = response.stats;
        },
        onError: (error) => {
          resetState();
        },
      });

      return result;
    });
  }

  return {
    ...state,
    // Getters
    hasValidSource,
    hasValidTimeRange,
    canExecuteQuery,
    // Actions
    setSource,
    setTimeRange,
    setLimit,
    setFilterConditions,
    setRawSql,
    executeQuery,
    resetState,
  };
});
