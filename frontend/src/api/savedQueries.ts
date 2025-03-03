import { api } from "./config";
import type { APIResponse, SavedTeamQuery } from "./types";

/**
 * Saved Queries API client
 */
export const savedQueriesApi = {
  /**
   * Get user teams
   */
  getUserTeams: async () => {
    const response = await api.get<
      APIResponse<Array<{ id: number; name: string; description?: string }>>
    >("/teams");
    return response.data;
  },

  /**
   * List queries for a team
   */
  listQueries: async (teamId: number) => {
    const response = await api.get<APIResponse<SavedTeamQuery[]>>(
      `/teams/${teamId}/queries`
    );
    return response.data;
  },

  /**
   * List queries for a source, optionally filtered by team
   */
  listSourceQueries: async (sourceId: number, teamId?: number) => {
    let url = `/sources/${sourceId}/queries`;
    if (teamId) {
      url += `?team_id=${teamId}`;
    }
    const response = await api.get<APIResponse<SavedTeamQuery[]>>(url);
    return response.data;
  },

  /**
   * Get a single query
   */
  getQuery: async (teamId: number, queryId: string) => {
    const response = await api.get<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/queries/${queryId}`
    );
    return response.data;
  },

  /**
   * Create a new query
   */
  createQuery: async (
    teamId: number,
    query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ) => {
    const response = await api.post<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/queries`,
      query
    );
    return response.data;
  },

  /**
   * Update an existing query
   */
  updateQuery: async (
    teamId: number,
    queryId: string,
    query: Partial<SavedTeamQuery>
  ) => {
    const response = await api.put<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/queries/${queryId}`,
      query
    );
    return response.data;
  },

  /**
   * Delete a query
   */
  deleteQuery: async (teamId: number, queryId: string) => {
    const response = await api.delete<APIResponse<{ success: boolean }>>(
      `/teams/${teamId}/queries/${queryId}`
    );
    return response.data;
  },
};
