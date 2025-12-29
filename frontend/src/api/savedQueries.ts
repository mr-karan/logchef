import { apiClient } from "./apiUtils";

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
}

/**
 * Saved team query representation
 */
export interface SavedTeamQuery {
  id: number;
  team_id: number;
  source_id: number;
  name: string;
  description: string;
  query_type: string;
  query_content: string; // JSON string of SavedQueryContent
  is_bookmarked: boolean;
  created_at: string;
  updated_at: string;
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
 * Team grouped query
 */
export interface TeamGroupedQuery {
  team_id: number;
  team_name: string;
  queries: SavedTeamQuery[];
}

/**
 * Saved Queries API client
 */
export const savedQueriesApi = {
  listTeamSourceQueries: (teamId: number, sourceId: number) =>
    apiClient.get<SavedTeamQuery[]>(`/teams/${teamId}/sources/${sourceId}/collections`),

  getTeamSourceQuery: (teamId: number, sourceId: number, collectionId: string) =>
    apiClient.get<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}`),

  createTeamSourceQuery: (teamId: number, sourceId: number, query: {
    name: string;
    description: string;
    query_type: string;
    query_content: string;
  }) => apiClient.post<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections`, query),

  updateTeamSourceQuery: (
    teamId: number,
    sourceId: number,
    collectionId: string,
    query: Partial<Omit<SavedTeamQuery, "id" | "team_id" | "source_id" | "created_at" | "updated_at">>
  ) =>
    apiClient.put<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}`, query),

  deleteTeamSourceQuery: (teamId: number, sourceId: number, collectionId: string) =>
    apiClient.delete<{ success: boolean }>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}`),

  toggleBookmark: (teamId: number, sourceId: number, collectionId: number) =>
    apiClient.patch<ToggleBookmarkResponse>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}/bookmark`),

  resolveQuery: (teamId: number, sourceId: number, collectionId: number) =>
    apiClient.get<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}/resolve`),

  getUserTeams: () => apiClient.get<Team[]>("/me/teams")
};
