import { api } from "./config";
import type { APIResponse } from "./types";
import type { QueryMode } from "@/lib/constants";

export interface FilterCondition {
  field: string;
  operator:
    | "="
    | "!="
    | "contains"
    | "startsWith"
    | "endsWith"
    | ">"
    | "<"
    | ">="
    | "<=";
  value: string;
}

export interface FilterGroup {
  operator: "OR";
  conditions: FilterCondition[];
}

export interface BaseQueryParams {
  limit: number;
  start_timestamp: number;
  end_timestamp: number;
}

export interface FiltersQueryParams extends BaseQueryParams {
  mode: "filters";
  conditions: FilterCondition[]; // All conditions are implicitly AND
}

export interface RawSqlQueryParams extends BaseQueryParams {
  mode: "raw_sql";
  raw_sql: string;
}

export type QueryParams = FiltersQueryParams | RawSqlQueryParams;

export interface QueryStats {
  execution_time_ms: number;
}

export interface QuerySuccessResponse {
  logs: Record<string, any>[];
  stats: QueryStats;
  params: QueryParams & {
    source_id: string;
  };
}

export interface QueryErrorResponse {
  error: string;
}

export type QueryResponse = QuerySuccessResponse | QueryErrorResponse;

export const exploreApi = {
  async getLogs(sourceId: string, params: QueryParams) {
    try {
      const response = await api.post<APIResponse<QueryResponse>>(
        `/sources/${sourceId}/logs/search`,
        params
      );
      return response.data;
    } catch (error) {
      console.error("Error fetching logs:", error);
      throw error;
    }
  },
};
