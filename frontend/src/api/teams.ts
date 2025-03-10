import { api } from "./config";
import type { APIResponse } from "./types";
import type { Source } from "./sources";

export interface Team {
  id: number;
  name: string;
  description: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  member_count?: number;
}

export interface TeamMember {
  team_id: number;
  user_id: number;
  role: "admin" | "member";
  created_at: string;
  email: string;
  full_name: string;
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
  user_id: number;
  role: "admin" | "member";
}

export interface TeamWithMemberCount extends Team {
  member_count: number;
}

export const teamsApi = {
  /**
   * List teams for the current user
   */
  async listUserTeams(): Promise<APIResponse<TeamWithMemberCount[]>> {
    const response = await api.get<APIResponse<TeamWithMemberCount[]>>(
      "/users/me/teams"
    );
    return response.data;
  },

  /**
   * Get a team by ID
   */
  async getTeam(id: number): Promise<APIResponse<Team>> {
    const response = await api.get<APIResponse<Team>>(`/teams/${id}`);
    return response.data;
  },

  /**
   * Create a new team
   */
  async createTeam(data: CreateTeamRequest): Promise<APIResponse<Team>> {
    const response = await api.post<APIResponse<Team>>("/admin/teams", data);
    return response.data;
  },

  /**
   * Update a team
   */
  async updateTeam(
    id: number,
    data: UpdateTeamRequest
  ): Promise<APIResponse<Team>> {
    const response = await api.put<APIResponse<Team>>(
      `/admin/teams/${id}`,
      data
    );
    return response.data;
  },

  /**
   * Delete a team
   */
  async deleteTeam(id: number): Promise<APIResponse<{ message: string }>> {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/admin/teams/${id}`
    );
    return response.data;
  },

  /**
   * List team members
   */
  async listTeamMembers(teamId: number): Promise<APIResponse<TeamMember[]>> {
    const response = await api.get<APIResponse<TeamMember[]>>(
      `/teams/${teamId}/members`
    );
    return response.data;
  },

  /**
   * Add a member to a team
   */
  async addTeamMember(
    teamId: number,
    data: AddTeamMemberRequest
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.post<APIResponse<{ message: string }>>(
      `/teams/${teamId}/members`,
      data
    );
    return response.data;
  },

  /**
   * Remove a member from a team
   */
  async removeTeamMember(
    teamId: number,
    userId: number
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/teams/${teamId}/members/${userId}`
    );
    return response.data;
  },

  /**
   * List team sources
   */
  async listTeamSources(teamId: number): Promise<APIResponse<Source[]>> {
    const response = await api.get<APIResponse<Source[]>>(
      `/teams/${teamId}/sources`
    );
    return response.data;
  },

  /**
   * Add a source to a team
   */
  async addTeamSource(
    teamId: number,
    sourceId: number
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.post<APIResponse<{ message: string }>>(
      `/teams/${teamId}/sources`,
      { source_id: sourceId }
    );
    return response.data;
  },

  /**
   * Remove a source from a team
   */
  async removeTeamSource(
    teamId: number,
    sourceId: number
  ): Promise<APIResponse<{ message: string }>> {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/teams/${teamId}/sources/${sourceId}`
    );
    return response.data;
  },
};
