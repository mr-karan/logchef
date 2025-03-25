export interface APISuccessResponse<T> {
  status: "success";
  data: T | null;
}

export interface APIListResponse<T> {
  status: "success";
  data: T[];
  count?: number;
  total?: number;
  page?: number;
  per_page?: number;
}

export interface APIPaginatedResponse<T> extends APIListResponse<T> {
  count: number;
  total: number;
  page: number;
  per_page: number;
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

export function isAPIListResponse<T>(response: any): response is APIListResponse<T> {
  return response?.status === "success" && Array.isArray(response?.data);
}

export function isPaginatedResponse<T>(response: any): response is APIPaginatedResponse<T> {
  return response?.status === "success" && 
         typeof response?.data?.count === 'number' && 
         Array.isArray(response?.data);
}

// Import from our new error handler utility
import { formatErrorMessage } from "@/api/error-handler";

export function getErrorMessage(error: unknown): string {
  return formatErrorMessage(error);
}
