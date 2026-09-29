import { apiClient } from "./apiUtils";
import { apiBaseURL } from "./config";
import type { APIResponse } from "./types";
import type { Session, User } from "@/types";

export interface SessionResponse {
  user: User;
  session: Session;
}

export const authApi = {
  /**
   * Get current session information
   */
  getSession: () => apiClient.get<SessionResponse>("/me"),

  /**
   * Get login URL for OIDC authentication
   */
  getLoginUrl(redirectPath?: string): string {
    const loginUrl = `${apiBaseURL}/auth/login`;
    const params = redirectPath ? new URLSearchParams({ redirect: redirectPath }) : null;
    return params ? `${loginUrl}?${params}` : loginUrl;
  },

  /**
   * Local (email + password) login. Only available when the server has
   * [auth.local] enabled — see /api/v1/meta local_auth_enabled.
   */
  localLogin: (email: string, password: string) =>
    apiClient.post<{ user: User }>("/auth/local/login", { email, password }),

  /**
   * Secure logout
   */
  async logout(): Promise<APIResponse<void>> {
    const response = await apiClient.post<void>("/auth/logout");
    sessionStorage.clear();
    localStorage.clear();
    return response;
  },
};
