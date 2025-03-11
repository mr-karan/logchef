import { api } from "./config";
import type { APIResponse } from "./types";

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
  async getLogs(
    sourceId: number,
    params: QueryParams,
    teamId: number
  ): Promise<APIResponse<QueryResponse>> {
    if (!teamId) {
      throw new Error("Team ID is required for querying logs");
    }

    const response = await api.post<APIResponse<QueryResponse>>(
      `/teams/${teamId}/sources/${sourceId}/logs/query`,
      params
    );
    return response.data;
  },

  async getLogContext(
    sourceId: number,
    params: LogContextRequest,
    teamId: number
  ): Promise<APIResponse<LogContextResponse>> {
    if (!teamId) {
      throw new Error("Team ID is required for getting log context");
    }

    const response = await api.post<APIResponse<LogContextResponse>>(
      `/${teamId}/sources/${sourceId}/logs/context`,
      params
    );
    return response.data;
  },
};
