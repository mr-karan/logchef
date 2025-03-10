import { api } from "./config";
import type { APIResponse, SavedTeamQuery } from "./types";

/**
 * Saved Queries API client
 */
export const savedQueriesApi = {
  /**
   * List queries for a team
   */
  listQueries: async (teamId: number) => {
    try {
      const response = await api.get<APIResponse<SavedTeamQuery[]>>(
        `/teams/${teamId}/queries`
      );
      return response.data;
    } catch (error) {
      throw error;
    }
  },

  /**
   * List queries for a source, optionally filtered by team
   */
  listSourceQueries: async (sourceId: number, teamId?: number) => {
    try {
      let url = `/sources/${sourceId}/queries`;
      if (teamId) {
        url += `?team_id=${teamId}`;
      }
      const response = await api.get<APIResponse<SavedTeamQuery[]>>(url);
      return response.data;
    } catch (error) {
      throw error;
    }
  },

  /**
   * Get a single query
   */
  getQuery: async (teamId: number, queryId: string) => {
    try {
      const response = await api.get<APIResponse<SavedTeamQuery>>(
        `/teams/${teamId}/queries/${queryId}`
      );
      return response.data;
    } catch (error) {
      throw error;
    }
  },

  /**
   * Create a new query
   */
  createQuery: async (
    teamId: number,
    query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ) => {
    try {
      const response = await api.post<APIResponse<SavedTeamQuery>>(
        `/teams/${teamId}/queries`,
        query
      );
      return response.data;
    } catch (error) {
      throw error;
    }
  },

  /**
   * Update an existing query
   */
  updateQuery: async (
    teamId: number,
    queryId: string,
    query: Partial<SavedTeamQuery>
  ) => {
    try {
      const response = await api.put<APIResponse<SavedTeamQuery>>(
        `/teams/${teamId}/queries/${queryId}`,
        query
      );
      return response.data;
    } catch (error) {
      throw error;
    }
  },

  /**
   * Delete a query
   */
  deleteQuery: async (teamId: number, queryId: string) => {
    try {
      const response = await api.delete<APIResponse<{ success: boolean }>>(
        `/teams/${teamId}/queries/${queryId}`
      );
      return response.data;
    } catch (error) {
      throw error;
    }
  },
  
  /**
   * Get user teams for saved queries
   */
  getUserTeams: async () => {
    try {
      const response = await api.get<APIResponse<any[]>>("/users/me/teams");
      return response.data;
    } catch (error) {
      throw error;
    }
  },
};
