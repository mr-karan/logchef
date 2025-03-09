import { api } from "./config";
import type { APIResponse } from "./types";
import type { User } from "@/types";

export interface CreateUserRequest {
  email: string;
  full_name: string;
  role: "admin" | "member";
}

export interface UpdateUserRequest {
  full_name?: string;
  role?: "admin" | "member";
  status?: "active" | "inactive";
}

export const usersApi = {
  /**
   * List all users
   */
  async listUsers(): Promise<APIResponse<User[]>> {
    const response = await api.get<APIResponse<User[]>>("/admin/users");
    return response.data;
  },

  /**
   * Get a user by ID
   */
  async getUser(id: string): Promise<APIResponse<{ user: User }>> {
    const response = await api.get<APIResponse<{ user: User }>>(`/admin/users/${id}`);
    return response.data;
  },

  /**
   * Create a new user
   */
  async createUser(
    data: CreateUserRequest
  ): Promise<APIResponse<{ user: User }>> {
    const response = await api.post<APIResponse<{ user: User }>>(
      "/admin/users",
      data
    );
    return response.data;
  },

  /**
   * Update a user
   */
  async updateUser(
    id: string,
    data: UpdateUserRequest
  ): Promise<APIResponse<{ user: User }>> {
    const response = await api.put<APIResponse<{ user: User }>>(
      `/admin/users/${id}`,
      data
    );
    return response.data;
  },

  /**
   * Delete a user
   */
  async deleteUser(id: string): Promise<APIResponse<{ message: string }>> {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/admin/users/${id}`
    );
    return response.data;
  },
};
