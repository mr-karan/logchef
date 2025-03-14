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
// import { addTimeRangeToSQL } from "@/utils/clickhouse-sql/parser";
import { useTeamsStore } from "@/stores/teams";

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
  // DSL code state
  dslCode?: string;
  // Active mode (dsl or sql)
  activeMode: "dsl" | "sql";
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
    dslCode: undefined,
    activeMode: "dsl",
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

  // Set the active mode (dsl or sql)
  function setActiveMode(mode: "dsl" | "sql") {
    state.data.value.activeMode = mode;
  }

  // Set the DSL code
  function setDslCode(code: string) {
    state.data.value.dslCode = code;
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
      dslCode: state.data.value.dslCode,
      activeMode: state.data.value.activeMode,
    };
  }

  // Helper to ensure SQL has proper timestamp conditions
  function ensureSqlHasTimeConditions(
    sql: string,
    timeField: string,
    start: number,
    end: number
  ): string {
    if (!sql || !sql.trim()) {
      // If SQL is empty or only whitespace, generate a default query
      return getDefaultSqlQuery(timeField, start, end);
    }

    const startIso = new Date(start).toISOString();
    const endIso = new Date(end).toISOString();

    // Check if the query already has timestamp conditions
    const tsRegex = new RegExp(`${timeField}\\s+BETWEEN`, "i");

    if (!tsRegex.test(sql)) {
      // If the query has a WHERE clause, append the timestamp condition
      if (/\bWHERE\b/i.test(sql)) {
        sql = sql.replace(
          /WHERE\s+(.*?)(?:\s+ORDER\s+BY|\s+GROUP\s+BY|\s+LIMIT|\s*$)/i,
          (match, whereClause) => {
            return `WHERE (${whereClause}) AND ${timeField} BETWEEN '${startIso}' AND '${endIso}'`;
          }
        );
      } else {
        // If the query doesn't have a WHERE clause, add one after FROM
        sql = sql.replace(
          /FROM\s+(.*?)(?:\s+ORDER\s+BY|\s+GROUP\s+BY|\s+LIMIT|\s*$)/i,
          (match, fromClause) => {
            return `FROM ${fromClause} WHERE ${timeField} BETWEEN '${startIso}' AND '${endIso}'`;
          }
        );
      }
    }

    // Ensure the query has a LIMIT
    if (!/\bLIMIT\b/i.test(sql)) {
      sql = `${sql}\nLIMIT ${state.data.value.limit}`;
    }

    return sql;
  }

  // Generate a default SQL query with proper timestamp conditions
  function getDefaultSqlQuery(
    timeField: string,
    start: number,
    end: number
  ): string {
    const startIso = new Date(start).toISOString();
    const endIso = new Date(end).toISOString();

    return `SELECT * FROM logs.vector_logs
      WHERE ${timeField} BETWEEN '${startIso}' AND '${endIso}'
      ORDER BY ${timeField} DESC
      LIMIT ${state.data.value.limit}`;
  }

  // Main query execution
  async function executeQuery(rawSql?: string) {
    if (!canExecuteQuery.value) {
      throw new Error("Cannot execute query: Invalid source or time range");
    }

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
          await sourcesStore.loadTeamSources(teamsStore.currentTeamId, true);

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

    state.isLoading.value = true;

    try {
      const timestamps = getTimestamps();
      let sqlToExecute = rawSql || state.data.value.rawSql || "";

      // Get time field from source metadata, default to 'timestamp'
      const timeField = currentSource._meta_ts_field || "timestamp";

      // Format the table name correctly
      const formattedTableName = getFormattedTableName(currentSource);
      console.log("Using formatted table name:", formattedTableName);

      // Replace any generic 'logs' table placeholder with the proper formatted table name
      if (sqlToExecute.includes(" FROM logs ")) {
        sqlToExecute = sqlToExecute.replace(
          " FROM logs ",
          ` FROM ${formattedTableName} `
        );
      }

      // If the query doesn't have a FROM clause, add one with the correct table
      if (!sqlToExecute.toUpperCase().includes("FROM")) {
        if (sqlToExecute.trim()) {
          // If there's some SQL but no FROM, assume it's just a WHERE condition
          sqlToExecute = `SELECT * FROM ${formattedTableName} WHERE ${sqlToExecute}`;
        } else {
          // If completely empty, use a default query
          sqlToExecute = `SELECT * FROM ${formattedTableName}`;
        }
      }

      console.log("SQL after table name formatting:", sqlToExecute);

      // Ensure the SQL has proper timestamp conditions
      sqlToExecute = ensureSqlHasTimeConditions(
        sqlToExecute,
        timeField,
        timestamps.start,
        timestamps.end
      );

      // Log the SQL for debugging
      console.log(
        "Original SQL:",
        rawSql || state.data.value.rawSql || "(empty)"
      );
      console.log("Final SQL with time range and limit:", sqlToExecute);
      console.log("Timestamps:", {
        startMs: timestamps.start,
        endMs: timestamps.end,
        startSeconds: timestamps.start / 1000,
        endSeconds: timestamps.end / 1000,
        startFormatted: new Date(timestamps.start).toISOString(),
        endFormatted: new Date(timestamps.end).toISOString(),
      });

      // Use the modified SQL
      const params: QueryParams = {
        raw_sql: sqlToExecute,
        limit: state.data.value.limit,
        start_timestamp: timestamps.start,
        end_timestamp: timestamps.end,
      };

      // Execute the query
      const result = await state.callApi<QueryResponse>({
        apiCall: () => {
          // Get the teams store synchronously
          const teamsStore = useTeamsStore();
          const currentTeamId = teamsStore.currentTeamId;

          if (!currentTeamId) {
            throw new Error(
              "No team selected. Please select a team before executing a query."
            );
          }

          return exploreApi.getLogs(
            state.data.value.sourceId,
            params,
            currentTeamId
          );
        },
        onSuccess: (response) => {
          if ("error" in response) {
            throw new Error(response.error);
          }

          state.data.value.logs = response.logs || [];
          state.data.value.columns = response.columns;
          state.data.value.queryStats = response.stats;
        },
        showToast: false,
      });

      return result;
    } finally {
      state.isLoading.value = false;
    }
  }

  // Get log context
  async function getLogContext(sourceId: number, params: LogContextRequest) {
    if (!sourceId) {
      throw new Error("Source ID is required for getting log context");
    }

    // Get the teams store
    const teamsStore = useTeamsStore();
    const currentTeamId = teamsStore.currentTeamId;

    if (!currentTeamId) {
      throw new Error(
        "No team selected. Please select a team before getting log context."
      );
    }

    return await state.callApi<LogContextResponse>({
      apiCall: () => exploreApi.getLogContext(sourceId, params, currentTeamId),
      showToast: false,
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
    dslCode: computed(() => state.data.value.dslCode),
    activeMode: computed(() => state.data.value.activeMode),

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
    setDslCode,
    setActiveMode,
    resetState,
    executeQuery,
    getLogContext,
  };
});
