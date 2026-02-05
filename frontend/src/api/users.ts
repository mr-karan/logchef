import { apiClient } from "./apiUtils";
import type { User } from "@/types";

export interface CreateUserRequest {
  email: string;
  full_name: string;
  role: "admin" | "member";
}

export interface UpdateUserRequest {
  full_name?: string;
  email?: string;
  role?: "admin" | "member";
  status?: "active" | "inactive";
}

export const usersApi = {
  // Note: /users is accessible to any authenticated user to allow team admins
  // to select users when adding members to their teams.
  listUsers: () => apiClient.get<User[]>("/users"),
  getUser: (id: string) => apiClient.get<{ user: User }>(`/admin/users/${id}`),
  createUser: (data: CreateUserRequest) => apiClient.post<User>("/admin/users", data),
  updateUser: (id: string, data: UpdateUserRequest) =>
    apiClient.put<{ user: User }>(`/admin/users/${id}`, data),
  deleteUser: (id: string) =>
    apiClient.delete<{ message: string }>(`/admin/users/${id}`)
};
