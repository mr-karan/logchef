import { apiClient } from "./apiUtils";
import type { User } from "@/types";
import type { APIToken, CreateAPITokenRequest, CreateAPITokenResponse } from "@/api/apiTokens";
import type { UserTeamMembership } from "@/api/teams";

export interface CreateServiceAccountRequest {
  name: string;
}

export interface AddServiceAccountToTeamRequest {
  team_id: number;
  role: "admin" | "member" | "editor";
}

export const serviceAccountsApi = {
  listServiceAccounts: () => apiClient.get<User[]>("/admin/service-accounts"),
  createServiceAccount: (data: CreateServiceAccountRequest) =>
    apiClient.post<User>("/admin/service-accounts", data),
  deleteServiceAccount: (id: string) =>
    apiClient.delete<{ message: string }>(`/admin/service-accounts/${id}`),
  listTokens: (id: string) =>
    apiClient.get<APIToken[]>(`/admin/service-accounts/${id}/tokens`),
  createToken: (id: string, data: CreateAPITokenRequest) =>
    apiClient.post<CreateAPITokenResponse>(`/admin/service-accounts/${id}/tokens`, data),
  deleteToken: (id: string, tokenId: number) =>
    apiClient.delete<{ message: string }>(`/admin/service-accounts/${id}/tokens/${tokenId}`),
  listTeams: (id: string) =>
    apiClient.get<UserTeamMembership[]>(`/admin/service-accounts/${id}/teams`),
  addToTeam: (id: string, data: AddServiceAccountToTeamRequest) =>
    apiClient.post<{ message: string }>(`/admin/service-accounts/${id}/teams`, data),
  removeFromTeam: (id: string, teamId: number) =>
    apiClient.delete<{ message: string }>(`/admin/service-accounts/${id}/teams/${teamId}`),
};
