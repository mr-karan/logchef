import { apiClient } from "./apiUtils";
import type { User } from "@/types";
import type { APIToken, CreateAPITokenRequest, CreateAPITokenResponse } from "@/api/apiTokens";

export interface CreateServiceAccountRequest {
  name: string;
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
};
