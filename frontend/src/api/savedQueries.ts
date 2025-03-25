import { apiClient } from "./apiUtils";

/**
 * Saved query content structure
 */
export interface SavedQueryContent {
  version: number;
  sourceId: number;
  timeRange: {
    absolute: {
      start: number;
      end: number;
    };
  };
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
  query_type: "logchefql" | "sql";
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
  listQueries: (teamId: number) => 
    apiClient.get<SavedTeamQuery[]>(`/teams/${teamId}/queries`),
  
  listSourceQueries: (sourceId: number, teamId?: number) => 
    apiClient.get<SavedTeamQuery[]>(
      `/sources/${sourceId}/queries${teamId ? `?team_id=${teamId}` : ''}`
    ),
  
  listTeamSourceQueries: (teamId: number, sourceId: number) => 
    apiClient.get<SavedTeamQuery[]>(`/teams/${teamId}/sources/${sourceId}/queries`),
  
  getQuery: (teamId: number, queryId: string) => 
    apiClient.get<SavedTeamQuery>(`/teams/${teamId}/queries/${queryId}`),
  
  createQuery: (teamId: number, query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">) => 
    apiClient.post<SavedTeamQuery>(`/teams/${teamId}/queries`, query),
  
  createSourceQuery: (teamId: number, sourceId: number, query: {
    name: string;
    description: string;
    query_type: string;
    query_content: string;
  }) => apiClient.post<SavedTeamQuery>(`/teams/${teamId}/sources/${sourceId}/queries`, query),
  
  updateQuery: (teamId: number, queryId: string, query: Partial<SavedTeamQuery>) => 
    apiClient.put<SavedTeamQuery>(`/teams/${teamId}/queries/${queryId}`, query),
  
  deleteQuery: (teamId: number, queryId: string) => 
    apiClient.delete<{ success: boolean }>(`/teams/${teamId}/queries/${queryId}`),
  
  getUserTeams: () => apiClient.get<Team[]>("/users/me/teams")
};
