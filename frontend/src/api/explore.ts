import { apiClient } from "./apiUtils";
import type { DateValue } from "@internationalized/date";
import { useSourcesStore } from "@/stores/sources";
import { QueryService } from "@/services/QueryService";
import type { TimeRange } from "@/types/query";

// Keep these for the UI filter builder
export interface FilterCondition {
  field: string;
  operator:
    | "="
    | "!="
    | "~"
    | "!~"
    | "contains"
    | "not_contains"
    | "icontains"
    | "startswith"
    | "endswith"
    | "in"
    | "not_in"
    | "is_null"
    | "is_not_null";
  value: string;
}

// AI Query generation types
export interface AIGenerateSQLRequest {
  natural_language_query: string;
  current_query?: string; // Optional current query for context
}

export interface AIGenerateSQLResponse {
  sql_query: string;
}

// Add APIErrorResponse for proper type checking
import type { APIErrorResponse } from "./types";

export interface ColumnInfo {
  name: string;
  type: string;
}

// Simplified query parameters - intended for API communication
export interface QueryParams {
  raw_sql: string;
  limit: number;
  window?: string;
  group_by?: string;
  timezone?: string; // User's timezone identifier (e.g., 'America/New_York', 'UTC')
  start_time?: string; // ISO formatted start time
  end_time?: string;   // ISO formatted end time
  query_timeout?: number; // Query timeout in seconds
}

export interface QueryStats {
  execution_time_ms: number;
  rows_read: number;
  bytes_read: number;
}

export interface QuerySuccessResponse {
  logs?: Record<string, any>[] | null; // For backward compatibility
  data?: Record<string, any>[] | null; // New structure
  stats: QueryStats;
  params?: QueryParams & {
    source_id: number;
  };
  columns: ColumnInfo[];
  query_id?: string; // Add query_id for cancellation
}

export interface QueryErrorResponse {
  error: string;
  details?: string; // For exposing ClickHouse errors
}

export type QueryResponse = QuerySuccessResponse | QueryErrorResponse;

// Log context types
export interface LogContextRequest {
  timestamp: number;
  before_limit: number;
  after_limit: number;
  before_offset?: number;     // Offset for before query (for pagination)
  after_offset?: number;      // Offset for after query (for pagination)
  exclude_boundary?: boolean; // When true, excludes logs at exact timestamp (for pagination)
}

export interface LogContextResponse {
  target_timestamp: number;
  before_logs: Record<string, any>[];
  target_logs: Record<string, any>[];
  after_logs: Record<string, any>[];
  stats: QueryStats;
}

// Histogram data types
export interface HistogramDataPoint {
  bucket: string;
  log_count: number;
  group_value?: string; // Optional field for grouped data
}

export interface HistogramResponse {
  granularity: string;
  data: HistogramDataPoint[];
}

/**
 * Helper function to prepare query parameters with proper SQL based on mode
 * This ensures we use a consistent approach for both logs and histogram queries
 */
export function prepareQueryParams(params: {
  query: string;
  limit?: number;
  window?: string;
  groupBy?: string;
  timezone?: string;
  queryTimeout?: number;
}): QueryParams {
  const { query, limit = 100, window, groupBy, timezone, queryTimeout } = params;

  // Use the raw SQL value as is - SQL transformation should happen before calling this function
  return {
    raw_sql: query,
    limit,
    window,
    group_by: groupBy,
    timezone,
    query_timeout: queryTimeout
  };
}

export const exploreApi = {
  getLogs: (sourceId: number, params: QueryParams, teamId: number, signal?: AbortSignal) => {
    if (!teamId) {
      throw new Error("Team ID is required for querying logs");
    }
    
    // Extract timeout from params and convert to axios options
    const timeout = params.query_timeout || 30; // Default to 30 seconds
    
    return apiClient.post<QueryResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/query`,
      params,
      { timeout, signal }
    );
  },

  getHistogramData: (sourceId: number, params: QueryParams, teamId: number, signal?: AbortSignal) => {
    if (!teamId) {
      throw new Error("Team ID is required for getting histogram data");
    }

    // Clean up params to ensure group_by is only included when it has a meaningful value
    const histogramParams = {
      ...params
    };

    // Let the body-level params come through as they are,
    // but don't add an empty string for group_by if it's not meaningful
    if (histogramParams.group_by === '') {
      delete histogramParams.group_by;
    }

    // Extract timeout from params
    const timeout = params.query_timeout || 30; // Default to 30 seconds

    return apiClient.post<HistogramResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/histogram`,
      histogramParams,
      { timeout, signal }
    );
  },

  getLogContext: (sourceId: number, params: LogContextRequest, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for getting log context");
    }
    return apiClient.post<LogContextResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/context`,
      params
    );
  },

  generateAISQL: (sourceId: number, params: AIGenerateSQLRequest, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for AI SQL generation");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for AI SQL generation");
    }
    return apiClient.post<AIGenerateSQLResponse>(
      `/teams/${teamId}/sources/${sourceId}/generate-sql`,
      params
    );
  },

  cancelQuery: (sourceId: number, queryId: string, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for cancelling queries");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for cancelling queries");
    }
    if (!queryId) {
      throw new Error("Query ID is required for cancelling queries");
    }
    return apiClient.post<{message: string; query_id: string}>(
      `/teams/${teamId}/sources/${sourceId}/logs/query/${queryId}/cancel`,
      {}
    );
  }
};
