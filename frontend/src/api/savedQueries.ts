import { api } from "./config";
import type { APIResponse } from "./types";

/**
 * Saved query content structure
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
  queryType: "dsl" | "sql";
  rawSql: string;
  dslContent?: string;
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
  query_type: "dsl" | "sql";
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
  /**
   * List queries for a team
   */
  async listQueries(teamId: number): Promise<APIResponse<SavedTeamQuery[]>> {
    const response = await api.get<APIResponse<SavedTeamQuery[]>>(
      `/teams/${teamId}/queries`
    );
    return response.data;
  },

  /**
   * List queries for a source, optionally filtered by team
   */
  async listSourceQueries(
    sourceId: number,
    teamId?: number
  ): Promise<APIResponse<SavedTeamQuery[]>> {
    let url = `/sources/${sourceId}/queries`;
    if (teamId) {
      url += `?team_id=${teamId}`;
    }
    const response = await api.get<APIResponse<SavedTeamQuery[]>>(url);
    return response.data;
  },
  
  /**
   * List queries for a team's source
   */
  async listTeamSourceQueries(
    teamId: number,
    sourceId: number
  ): Promise<APIResponse<SavedTeamQuery[]>> {
    const response = await api.get<APIResponse<SavedTeamQuery[]>>(
      `/teams/${teamId}/sources/${sourceId}/queries`
    );
    return response.data;
  },

  /**
   * Get a single query
   */
  async getQuery(
    teamId: number,
    queryId: string
  ): Promise<APIResponse<SavedTeamQuery>> {
    const response = await api.get<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/queries/${queryId}`
    );
    return response.data;
  },

  /**
   * Create a new query
   */
  async createQuery(
    teamId: number,
    query: Omit<SavedTeamQuery, "id" | "created_at" | "updated_at">
  ): Promise<APIResponse<SavedTeamQuery>> {
    const response = await api.post<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/queries`,
      query
    );
    return response.data;
  },

  /**
   * Create a new source query
   */
  async createSourceQuery(
    teamId: number,
    sourceId: number,
    query: {
      name: string;
      description: string;
      query_type: string;
      query_content: string;
    }
  ): Promise<APIResponse<SavedTeamQuery>> {
    const response = await api.post<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/sources/${sourceId}/queries`,
      query
    );
    return response.data;
  },

  /**
   * Update an existing query
   */
  async updateQuery(
    teamId: number,
    queryId: string,
    query: Partial<SavedTeamQuery>
  ): Promise<APIResponse<SavedTeamQuery>> {
    const response = await api.put<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/queries/${queryId}`,
      query
    );
    return response.data;
  },

  /**
   * Delete a query
   */
  async deleteQuery(
    teamId: number,
    queryId: string
  ): Promise<APIResponse<{ success: boolean }>> {
    const response = await api.delete<APIResponse<{ success: boolean }>>(
      `/teams/${teamId}/queries/${queryId}`
    );
    return response.data;
  },

  /**
   * Get user teams for saved queries
   */
  async getUserTeams(): Promise<APIResponse<Team[]>> {
    const response = await api.get<APIResponse<Team[]>>("/users/me/teams");
    return response.data;
  },
};
