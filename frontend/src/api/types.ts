import type { AxiosError } from "axios";

export interface APISuccessResponse<T> {
  status: "success";
  data: T;
}

export interface APIErrorResponse {
  status: "error";
  data: { error: string };
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

export function getErrorMessage(error: unknown): string {
  // If it's an API response object
  if (error && typeof error === "object" && "status" in error) {
    const response = error as APIResponse<unknown>;
    if (isErrorResponse(response)) {
      return response.data.error;
    }
  }

  // If it's an Axios error
  if (error && typeof error === "object" && "isAxiosError" in error) {
    const axiosError = error as AxiosError<APIResponse<unknown>>;

    // Network error (no response received)
    if (!axiosError.response) {
      return "Network error: Unable to connect to the server";
    }

    // Server responded with error
    if (axiosError.response.data && isErrorResponse(axiosError.response.data)) {
      return axiosError.response.data.data.error;
    }

    // Fallback to status text
    return `Server error: ${axiosError.response.statusText || "Unknown error"}`;
  }

  // For any other type of error
  if (error instanceof Error) {
    return error.message;
  }

  return "An unexpected error occurred";
}
