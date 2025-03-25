export interface APISuccessResponse<T> {
  status: "success";
  data: T | null;
}

export interface APIErrorResponse {
  status: "error";
  message: string;
  error_type: string;
  data?: any;
}

export type APIResponse<T = any> = APISuccessResponse<T> | APIErrorResponse;

/**
 * Saved team query representation
 */
export interface SavedTeamQuery {
  id: number;
  team_id: number;
  source_id: number;
  name: string;
  description: string;
  query_content: string;
  created_at: string;
  updated_at: string;
}

/**
 * Team information
 */
export interface Team {
  id: number;
  name: string;
  description?: string;
}

/**
 * Query content structure
 */
export interface SavedQueryContent {
  version: number;
  activeTab: "filters" | "raw_sql";
  sourceId: number;
  timeRange: {
    absolute: {
      start: number;
      end: number;
    };
  };
  limit: number;
  rawSql: string;
}

export function isErrorResponse<T>(
  response: APIResponse<T>
): response is APIErrorResponse {
  return response.status === "error";
}

export function isSuccessResponse<T>(
  response: APIResponse<T>
): response is APISuccessResponse<T> {
  return response.status === "success";
}

// Import from our new error handler utility
import { formatErrorMessage } from "@/api/error-handler";

export function getErrorMessage(error: unknown): string {
  return formatErrorMessage(error);
}
