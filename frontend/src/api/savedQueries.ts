import { apiClient } from "./apiUtils";
import type { VariableState } from "@/stores/variables";

export interface SavedQueryContent {
  version: number;
  sourceId: number | string;
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

/**
 * Saved query representation. Cross-team — visibility is via source access
 * through any team membership; edit access is creator + global admin.
 */
export interface SavedQuery {
  id: number;
  source_id: number;
  name: string;
  description: string;
  query_type: string;
  query_content: string; // JSON string of SavedQueryContent
  is_bookmarked: boolean;
  created_by?: number | null;
  created_at: string;
  updated_at: string;
  source_name?: string;
}

/**
 * Toggle bookmark response
 */
export interface ToggleBookmarkResponse {
  is_bookmarked: boolean;
  message: string;
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
 * Saved Queries API client
 */
export const savedQueriesApi = {
  list: (sourceId?: number) => {
    const url =
      sourceId !== undefined && sourceId !== null
        ? `/saved-queries?source_id=${sourceId}`
        : "/saved-queries";
    return apiClient.get<SavedQuery[]>(url);
  },

  get: (queryId: number | string) =>
    apiClient.get<SavedQuery>(`/saved-queries/${queryId}`),

  create: (query: {
    source_id: number;
    name: string;
    description: string;
    query_type: string;
    query_content: string;
  }) => apiClient.post<SavedQuery>("/saved-queries", query),

  update: (
    queryId: number | string,
    query: Partial<Omit<SavedQuery, "id" | "source_id" | "created_by" | "created_at" | "updated_at">>
  ) => apiClient.put<SavedQuery>(`/saved-queries/${queryId}`, query),

  delete: (queryId: number | string) =>
    apiClient.delete<{ message: string }>(`/saved-queries/${queryId}`),

  toggleBookmark: (queryId: number | string) =>
    apiClient.patch<ToggleBookmarkResponse>(`/saved-queries/${queryId}/bookmark`),

  resolve: (queryId: number | string) =>
    apiClient.get<SavedQuery>(`/saved-queries/${queryId}/resolve`),

  getUserTeams: () => apiClient.get<Team[]>("/me/teams"),
};
