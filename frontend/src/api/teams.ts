import { apiClient } from "./apiUtils";
import type { Source } from "./sources";
import type { APIResponse } from "./types";

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
  updated_at: string;
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
  listUserTeams: () => apiClient.get<APIResponse<TeamWithMemberCount[]>>("/me/teams"),
  listAllTeams: () => apiClient.get<APIResponse<TeamWithMemberCount[]>>("/admin/teams"),
  getTeam: (id: number) => apiClient.get<APIResponse<Team>>(`/teams/${id}`),
  createTeam: (data: CreateTeamRequest) => apiClient.post<APIResponse<Team>>("/admin/teams", data),
  updateTeam: (id: number, data: UpdateTeamRequest) =>
    apiClient.put<APIResponse<Team>>(`/teams/${id}`, data),
  deleteTeam: (id: number) =>
    apiClient.delete<APIResponse<{ message: string }>>(`/admin/teams/${id}`),

  // Team members
  listTeamMembers: (teamId: number) =>
    apiClient.get<APIResponse<TeamMember[]>>(`/teams/${teamId}/members`),
  addTeamMember: (teamId: number, data: AddTeamMemberRequest) =>
    apiClient.post<APIResponse<TeamMember>>(`/teams/${teamId}/members`, data),
  removeTeamMember: (teamId: number, userId: number) =>
    apiClient.delete<APIResponse<{ message: string }>>(`/teams/${teamId}/members/${userId}`),

  // Team sources
  listTeamSources: (teamId: number) =>
    apiClient.get<APIResponse<Source[]>>(`/teams/${teamId}/sources`),
  addTeamSource: (teamId: number, sourceId: number) =>
    apiClient.post<APIResponse<Source>>(`/teams/${teamId}/sources`, { source_id: sourceId }),
  removeTeamSource: (teamId: number, sourceId: number) =>
    apiClient.delete<APIResponse<{ message: string }>>(`/teams/${teamId}/sources/${sourceId}`)
};
