import type { VariableState } from "@/stores/variables";

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
  sourceId: number;
  timeRange: {
    relative?: string;
    absolute?: {
      start: number;
      end: number;
    };
  } | null;
  limit: number;
  content: string;
  variables?: VariableState[];
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
