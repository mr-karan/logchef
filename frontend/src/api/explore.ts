import { apiClient } from "./apiUtils";

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

export interface ColumnInfo {
  name: string;
  type: string;
}

// Simplified query parameters - only raw SQL
export interface QueryParams {
  raw_sql: string;
  limit: number;
  start_timestamp: number;
  end_timestamp: number;
  sort?: {
    field: string;
    order: "ASC" | "DESC";
  };
  original_query?: string; // Original LogchefQL query if applicable
  query_type?: string; // 'logchefql' or 'sql'
}

export interface QueryStats {
  execution_time_ms: number;
  rows_read: number;
  bytes_read: number;
}

export interface QuerySuccessResponse {
  logs: Record<string, any>[] | null;
  stats: QueryStats;
  params?: QueryParams & {
    source_id: number;
  };
  columns: ColumnInfo[];
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
}

export interface LogContextResponse {
  target_timestamp: number;
  before_logs: Record<string, any>[];
  target_logs: Record<string, any>[];
  after_logs: Record<string, any>[];
  stats: QueryStats;
}

export const exploreApi = {
  getLogs: (sourceId: number, params: QueryParams, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for querying logs");
    }
    return apiClient.post<QueryResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/query`,
      params
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
  }
};
