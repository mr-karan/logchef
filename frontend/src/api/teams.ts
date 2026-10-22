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

export type TeamSummary = Pick<Team, "id" | "name"> & Partial<Pick<Team, "description">>;

export interface UserTeamMembership {
  id: number;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
  member_count: number;
  role: "admin" | "member" | "editor";
}

export interface TeamMember {
  team_id: number;
  user_id: number;
  role: "admin" | "member" | "editor";
  created_at: string;
  updated_at: string;
  email: string;
  full_name: string;
  account_type?: "human" | "service";
}

export interface CreateTeamRequest {
  name: string;
  description: string;
}

export interface UpdateTeamRequest {
  name: string;
  description: string;
}

export interface UserIdentifier {
  user_id: number;
}

export interface AddTeamMemberRequest {
  user_id: number;
  role: 'admin' | 'member' | 'editor';
}

export interface TeamWithMemberCount extends Team {
  member_count: number;
}

export const teamsApi = {
  listUserTeams: (options?: { signal?: AbortSignal }) => apiClient.get<UserTeamMembership[]>("/me/teams", options),
  listAllTeams: (options?: { signal?: AbortSignal }) => apiClient.get<TeamWithMemberCount[]>("/admin/teams", options),
  getTeam: (id: number, options?: { signal?: AbortSignal }) => apiClient.get<Team & { member_count?: number }>(`/teams/${id}`, options),
  createTeam: (data: CreateTeamRequest) => apiClient.post<Team>("/admin/teams", data),
  updateTeam: (id: number, data: UpdateTeamRequest) =>
    apiClient.put<Team>(`/teams/${id}`, data),
  deleteTeam: (id: number) =>
    apiClient.delete<{ message: string }>(`/admin/teams/${id}`),

  // Team members
  listTeamMembers: (teamId: number, options?: { signal?: AbortSignal }) =>
    apiClient.get<TeamMember[]>(`/teams/${teamId}/members`, options),
  addTeamMember: (teamId: number, data: AddTeamMemberRequest) =>
    apiClient.post<TeamMember>(`/teams/${teamId}/members`, data),
  removeTeamMember: (teamId: number, userId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/members/${userId}`),

  // Team sources
  listTeamSources: (teamId: number, options?: { signal?: AbortSignal }) =>
    apiClient.get<Source[]>(`/teams/${teamId}/sources`, options),
  addTeamSource: (teamId: number, sourceId: number) =>
    apiClient.post<Source>(`/teams/${teamId}/sources`, { source_id: sourceId }),
  removeTeamSource: (teamId: number, sourceId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/sources/${sourceId}`)
};
