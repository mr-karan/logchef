import { apiClient } from "./apiUtils";

/**
 * Saved query content structure
 */
export interface SavedQueryContent {
  version: number;
  sourceId: number | string;
  timeRange: {
    absolute: {
      start: number;
      end: number;
    };
  } | null;
  limit: number;
  content: string; // The content of the query (either LogchefQL or SQL)
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
  listTeamSourceCollections: (teamId: number, sourceId: number) =>
    apiClient.get<SavedTeamQuery[]>(`/teams/${teamId}/sources/${sourceId}/collections`),

  getTeamSourceCollection: (teamId: number, sourceId: number, collectionId: string) =>
    apiClient.get<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}`),

  createTeamSourceCollection: (teamId: number, sourceId: number, query: {
    name: string;
    description: string;
    query_type: string;
    query_content: string;
  }) => apiClient.post<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections`, query),

  updateTeamSourceCollection: (
    teamId: number,
    sourceId: number,
    collectionId: string,
    query: Partial<Omit<SavedTeamQuery, "id" | "team_id" | "source_id" | "created_at" | "updated_at">>
  ) =>
    apiClient.put<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}`, query),

  deleteTeamSourceCollection: (teamId: number, sourceId: number, collectionId: string) =>
    apiClient.delete<{ success: boolean }>(`/teams/${teamId}/sources/${sourceId}/collections/${collectionId}`),

  // For retrieving user teams
  getUserTeams: () => apiClient.get<Team[]>("/me/teams")
};
