import { api } from "./config";
import type { APIResponse } from "./types";
import type { Source } from "./sources";

export interface Team {
  id: string;
  name: string;
  description: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface TeamMember {
  team_id: string;
  user_id: string;
  role: "admin" | "member";
  created_at: string;
}

export interface CreateTeamRequest {
  name: string;
  description: string;
}

export interface UpdateTeamRequest {
  name: string;
  description: string;
}

export interface AddTeamMemberRequest {
  user_id: string;
  role: "admin" | "member";
}

export const teamsApi = {
  /**
   * List all teams
   */
  async listTeams(): Promise<APIResponse<Team[]>> {
    const response = await api.get<APIResponse<Team[]>>("/teams");
    return response.data;
  },

  /**
   * Get a team by ID
   */
  async getTeam(id: string): Promise<APIResponse<Team>> {
    const response = await api.get<APIResponse<Team>>(`/teams/${id}`);
    return response.data;
  },

  /**
   * Create a new team
   */
  async createTeam(data: CreateTeamRequest): Promise<APIResponse<Team>> {
    const response = await api.post<APIResponse<Team>>("/teams", data);
    return response.data;
  },

  /**
   * Update a team
   */
  async updateTeam(
    id: string,
    data: UpdateTeamRequest
  ): Promise<APIResponse<Team>> {
    const response = await api.put<APIResponse<Team>>(`/teams/${id}`, data);
    return response.data;
  },

  /**
   * Delete a team
   */
  async deleteTeam(id: string): Promise<APIResponse<{ message: string }>> {
    if (!id) {
      throw new Error("Team ID is required for deletion");
    }
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/teams/${id}`
    );
    return response.data;
  },

  /**
   * List team members
   */
  async listTeamMembers(teamId: string): Promise<APIResponse<TeamMember[]>> {
    const response = await api.get<APIResponse<TeamMember[]>>(
      `/teams/${teamId}/members`
    );
    return response.data;
  },

  /**
   * Add team member
   */
  async addTeamMember(
    teamId: string,
    data: AddTeamMemberRequest
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.post<APIResponse<{ message: string }>>(
      `/teams/${teamId}/members`,
      data
    );
    return response.data;
  },

  /**
   * Remove team member
   */
  async removeTeamMember(
    teamId: string,
    userId: string
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/teams/${teamId}/members/${userId}`
    );
    return response.data;
  },

  /**
   * List team sources
   */
  async listTeamSources(teamId: string): Promise<APIResponse<Source[]>> {
    const response = await api.get<APIResponse<Source[]>>(
      `/teams/${teamId}/sources`
    );
    return response.data;
  },

  /**
   * Add source to team
   */
  async addTeamSource(
    teamId: string,
    sourceId: string
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.post<APIResponse<{ message: string }>>(
      `/teams/${teamId}/sources`,
      { source_id: sourceId }
    );
    return response.data;
  },

  /**
   * Remove source from team
   */
  async removeTeamSource(
    teamId: string,
    sourceId: string
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/teams/${teamId}/sources/${sourceId}`
    );
    return response.data;
  },
};
