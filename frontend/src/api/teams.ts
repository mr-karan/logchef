import { apiClient } from "./apiUtils";
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
  listUserTeams: () => apiClient.get<TeamWithMemberCount[]>("/users/me/teams"),
  listAllTeams: () => apiClient.get<TeamWithMemberCount[]>("/admin/teams"),
  getTeam: (id: number) => apiClient.get<Team>(`/teams/${id}`),
  createTeam: (data: CreateTeamRequest) => apiClient.post<Team>("/admin/teams", data),
  updateTeam: (id: number, data: UpdateTeamRequest) =>
    apiClient.put<Team>(`/admin/teams/${id}`, data),
  deleteTeam: (id: number) =>
    apiClient.delete<{ message: string }>(`/admin/teams/${id}`),

  // Team members
  listTeamMembers: (teamId: number) =>
    apiClient.get<TeamMember[]>(`/teams/${teamId}/members`),
  addTeamMember: (teamId: number, data: AddTeamMemberRequest) =>
    apiClient.post<{ message: string }>(`/teams/${teamId}/members`, data),
  removeTeamMember: (teamId: number, userId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/members/${userId}`),

  // Team sources
  listTeamSources: (teamId: number) =>
    apiClient.get<Source[]>(`/teams/${teamId}/sources`),
  addTeamSource: (teamId: number, sourceId: number) =>
    apiClient.post<{ message: string }>(`/teams/${teamId}/sources`, { source_id: sourceId }),
  removeTeamSource: (teamId: number, sourceId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/sources/${sourceId}`)
};
